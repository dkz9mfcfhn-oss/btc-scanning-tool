package miner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Probe reads common stock-firmware APIs. It never sends a configuration command.
// Caller should provide a deadline and never overlap probes for the same IP.
func Probe(ctx context.Context, ip string, creds Credentials) (Reading, error) {
	addr := net.ParseIP(ip)
	if addr == nil || addr.To4() == nil {
		return Reading{}, errors.New("请输入 IPv4 地址")
	}
	httpDetection := make(chan string, 1)
	go func() {
		httpDetection <- detectBTCHTTP(ctx, "http://"+ip)
	}()
	r := Reading{}
	var failures []string
	var braiins, checkedVersion bool

	// Identification commands must not consume the entire device deadline before
	// HTTP-only firmware power statistics have a chance to run.
	tcpCtx, cancelTCP := context.WithTimeout(ctx, 6*time.Second)
	defer cancelTCP()
	// Including both aliases performs the same read operation on CGMiner and the
	// older Whatsminer API. Neither alias can mutate device state.
	query := func(cmd string) ([]byte, error) {
		return tcpJSON(tcpCtx, net.JoinHostPort(ip, "4028"), map[string]string{"command": cmd, "cmd": cmd}, false)
	}
	// BTC Tools identifies the device with stats, then reads pools. Only
	// supplement incomplete telemetry with the remaining CGMiner commands.
	read := func(command string) error {
		if command == "version" {
			checkedVersion = true
		}
		data, err := query(command)
		if err != nil {
			return err
		}
		if command == "stats" || command == "version" {
			id := strings.ToLower(string(data))
			braiins = braiins || strings.Contains(id, "bosminer") || strings.Contains(id, "braiins")
		}
		parsed, err := ParseCGMiner(data)
		if err == nil {
			r = merge(r, parsed)
		}
		return err
	}
	statsErr := read("stats")
	triedDetails := false
	if statsErr == nil {
		_ = read("pools")
		if r.Model == "" && !hasData(r) {
			triedDetails = true
			_ = read("devdetails")
		}
	}
	if needsHashrateDiagnosis(r) || r.AverageTH == nil || r.ElapsedSeconds == nil || (r.PowerW == nil && (r.Brand == "Whatsminer" || r.Brand == "")) {
		// Reserve a separate deadline: a stalled pool response must not use
		// the entire budget of the summary fallback.
		summaryCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		data, err := tcpJSON(summaryCtx, net.JoinHostPort(ip, "4028"), map[string]string{"command": "summary", "cmd": "summary"}, false)
		cancel()
		if err == nil {
			if parsed, parseErr := ParseCGMiner(data); parseErr == nil {
				r = merge(r, parsed)
				if parsed.ElapsedSeconds != nil {
					r.ElapsedSeconds = parsed.ElapsedSeconds
				}
			}
		} else if statsErr != nil {
			failures = append(failures, "4028 读取失败")
		}
	}
	if statsErr == nil || hasObservation(r) {
		if !r.WorkersKnown && statsErr != nil {
			_ = read("pools")
		}
		if !hasBoardMeans(r) && (r.Brand == "Whatsminer" || r.Brand == "") {
			_ = read("edevs")
		}
		if r.Model == "" && !triedDetails {
			_ = read("devdetails")
		}
		if r.Model == "" || (r.Brand == "Antminer" && r.PowerW == nil && !braiins && !checkedVersion) {
			_ = read("version")
		}
	}
	if braiins && r.PowerW == nil {
		if data, err := query("tunerstatus"); err == nil {
			r = merge(r, parseBraiinsPower(data))
		}
	}
	cancelTCP()
	httpFamily := <-httpDetection
	if !is1566(r) && !hasBoardMeans(r) && (r.Brand == "Avalon" || (r.Brand == "" && hasObservation(r))) {
		r = readAvalonDetail(ctx, net.JoinHostPort(ip, "4028"), r)
	}
	// BTC Tools uses DEVS.Temperature when earlier commands did not expose it.
	if !is1566(r) && (r.Brand == "Antminer" || r.Brand == "Avalon" || httpFamily != "") && !hasBoardMeans(r) {
		one, cancel := context.WithTimeout(ctx, 3*time.Second)
		if data, e := tcpJSON(one, net.JoinHostPort(ip, "4028"), map[string]string{"command": "devs"}, false); e == nil {
			if extra, e := ParseCGMiner(data); e == nil {
				r = merge(r, extra)
			}
		}
		cancel()
	}
	// BTC Tools avoids re-reading the Avalon log when stats already has the
	// module data. Keep the HTTP fallback for any missing telemetry here.
	avalonComplete := r.RealtimeTH != nil && r.AverageTH != nil && r.ElapsedSeconds != nil &&
		r.Model != "" && r.WorkersKnown && hasBoardMeans(r) && btcTemperatureRank(r.Temperatures) == 2
	avalonHTTPRead := false
	if !is1566(r) && !avalonComplete && (strings.HasPrefix(httpFamily, "Avalon") || r.Brand == "Avalon") {
		avalonHTTPRead = true
		family := httpFamily
		if strings.Contains(strings.ToLower(r.Model), "1566") {
			family = "AvalonDevice"
		}
		extra, e := readAvalonHTTPFamily(ctx, "http://"+ip, family, r.Brand == "Avalon", nil)
		if e == nil && btcTemperatureRank(extra.Temperatures) < 2 && strings.Contains(strings.ToLower(extra.Model), "1566") {
			extra, e = readAvalonHTTPFamily(ctx, "http://"+ip, "AvalonDevice", true, nil)
		}
		if e == nil {
			if btcTemperatureRank(extra.Temperatures) == 2 {
				r = mergeFreshAvalonLog(r, extra)
			} else {
				r = merge(r, extra)
			}
		} else {
			r.Warnings = append(r.Warnings, "阿瓦隆后台日志读取失败："+e.Error())
		}
	}
	if httpFamily == "Antminer" || r.Brand == "Antminer" {
		extra, failed, notes := readAntminerHTTP(ctx, "http://"+ip, creds)
		r = merge(r, extra)
		if failed {
			r.Warnings = append(r.Warnings, notes...)
		}
	}
	// Whatsminer v3 (including recent M63S firmware) moved to framed port 4433.
	if !hasData(r) || r.Brand == "Whatsminer" {
		v3Ctx, cancelV3 := context.WithTimeout(ctx, 4*time.Second)
		status, e := tcpJSON(v3Ctx, net.JoinHostPort(ip, "4433"), map[string]string{"cmd": "get.miner.status", "param": "summary+pools+edevs"}, true)
		if e == nil {
			info, _ := tcpJSON(v3Ctx, net.JoinHostPort(ip, "4433"), map[string]string{"cmd": "get.device.info"}, true)
			if p, e := ParseWhatsminerV3(status, info); e == nil && (hasData(p) || p.StatusKnown) {
				r = merge(p, r)
			}
		} else {
			failures = append(failures, "4433 读取失败")
		}
		cancelV3()
	}
	if httpFamily != "Antminer" && (!hasData(r) || r.Brand == "Antminer" || r.Brand == "") {
		restCtx, cancelREST := context.WithTimeout(ctx, 10*time.Second)
		if rest, err := probeByteon(restCtx, "http://"+ip, creds); err == nil {
			r = merge(rest, r)
		} else if !strings.Contains(err.Error(), "endpoint unavailable") {

			failures = append(failures, err.Error())
			if hasData(r) {
				r.Warnings = append(r.Warnings, err.Error())
			}
		}
		cancelREST()
	}

	// REST may provide rates while stock CGI has the missing power field.
	if httpFamily != "Antminer" && r.Brand != "Antminer" && (!hasData(r) || r.Brand == "") && (needsHashrateDiagnosis(r) || r.Accepted == nil || r.TempC == nil || r.Model == "" || !hasBoardMeans(r)) {
		httpReading, failed, notes := readAntminerHTTP(ctx, "http://"+ip, creds)
		failures = append(failures, notes...)
		r = merge(r, httpReading)
		if failed && !hasBoardMeans(r) {
			for _, note := range notes {
				r.Warnings = append(r.Warnings, "温度补读："+note)
			}
		}
	}
	if !hasObservation(r) {
		if ctx.Err() != nil {
			return r, fmt.Errorf("采集超时或取消：%w", ctx.Err())
		}
		return r, fmt.Errorf("未读取到矿机算力；检查 API 权限、网段与登录信息（%s）", strings.Join(failures, "；"))
	}
	if r.Brand == "" {
		r.Brand = "CGMiner"
	}
	if r.Model == "" {
		r.Model = "型号未上报"
	}
	if is1566(r) {
		r = readAvalonFMS(ctx, net.JoinHostPort(ip, "4028"), r)
	} else {
		correctAvalonOverview(&r)
	}
	if !avalonHTTPRead {
		r = supplementAvalonHealth(ctx, "http://"+ip, httpFamily, r, nil)
	}
	if r.PowerW == nil {
		switch r.Brand {
		case "Antminer":
			if !r.FirmwareKnown && !braiins {
				r = merge(r, stockPower(ctx, "http://"+net.JoinHostPort(ip, "6060"), creds))
			}
		case "Whatsminer":
			r = merge(r, whatsPowerPage(ctx, "http://"+ip, creds))
		}
	}
	if r.TempC == nil {
		r.Warnings = append(r.Warnings, "固件未上报可识别的温度")
	}
	if !hasBoardMeans(r) {
		r.Warnings = append(r.Warnings, "未取得每板芯片均温；仅有单个测温点或最高/最低温时不推算均温")
	}
	r = addAntminerLogEvidence(ctx, "http://"+ip, creds, r, nil)
	if warning := avalonZeroTemperatureWarning(r); warning != "" {
		r.Warnings = append(r.Warnings, warning)
	}
	return collectTemperatures(r), nil
}
