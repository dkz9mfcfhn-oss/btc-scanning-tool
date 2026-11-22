package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type restTrace func(path, method string, status int, data []byte, err error, elapsed time.Duration)

func restClient() *http.Client {
	tr := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 1500 * time.Millisecond}).DialContext, DisableKeepAlives: true, ResponseHeaderTimeout: requestTimeout}
	return &http.Client{Transport: tr, Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("REST redirect refused") }}
}

// Anthill's published Miner Dash API is used as a compatibility adapter.
// Firmware identity is established before a supplied password is sent.
func probeByteon(ctx context.Context, base string, creds Credentials) (Reading, error) {
	c := restClient()
	defer c.CloseIdleConnections()
	return readByteon(ctx, c, base, creds)
}

func restJSON(ctx context.Context, c *http.Client, base, path, method, token string, body []byte) ([]byte, int, error) {
	req, e := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if e != nil {
		return nil, 0, e
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := c.Do(req)
	if e != nil {
		return nil, 0, e
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if e != nil {
		return nil, resp.StatusCode, e
	}
	if len(data) > maxResponse {
		return nil, resp.StatusCode, errors.New("REST response exceeds 2 MiB")
	}
	if resp.StatusCode != 200 {
		return nil, resp.StatusCode, fmt.Errorf("REST HTTP %d", resp.StatusCode)
	}
	if !json.Valid(data) {
		return nil, 200, errors.New("REST response is not JSON")
	}
	return data, 200, nil
}

func familyName(name string) string {
	v := strings.ToLower(strings.TrimSpace(name))
	for _, f := range []struct{ needle, canonical string }{{"byteon", "Byteon"}, {"倍腾", "Byteon"}, {"vnish", "Vnish"}, {"hashcore", "HashCore"}, {"anthill", "Anthill"}, {"asicto", "Asic.to"}, {"asic.to", "Asic.to"}} {
		if strings.Contains(v, f.needle) {
			return f.canonical
		}
	}
	return ""
}

// unwrapREST accepts ordinary data/result envelopes without searching arbitrary
// descendants such as pool configuration for values that merely resemble stats.
func unwrapREST(m map[string]any, expected ...string) map[string]any {
	for depth := 0; depth < 4; depth++ {
		for _, key := range expected {
			if _, ok := m[key]; ok {
				return m
			}
		}
		moved := false
		for _, key := range []string{"data", "result", "info", "summary"} {
			if child, ok := m[key].(map[string]any); ok {
				m = child
				moved = true
				break
			}
		}
		if !moved {
			break
		}
	}
	return m
}

func firmwareFamily(info map[string]any) string {
	info = unwrapREST(info, "fw_name", "firmware_name", "firmware", "fw")
	for _, k := range []string{"fw_name", "firmware_name", "firmware", "fw"} {
		if s, ok := info[k].(string); ok {
			if family := familyName(s); family != "" {
				return family
			}
		}
		if obj, ok := info[k].(map[string]any); ok {
			if family := familyName(text(obj, "name", "fw_name")); family != "" {
				return family
			}
		}
	}
	return ""
}

var pageTitle = regexp.MustCompile(`(?is)<title\b[^>]*>([^<]{1,160})</title>`)

func identifyRESTPage(ctx context.Context, c *http.Client, base string, trace restTrace) string {
	start := time.Now()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, base+"/", nil)
	if e != nil {
		return ""
	}
	resp, e := c.Do(req)
	status := 0
	var data []byte
	if e == nil {
		status = resp.StatusCode
		data, e = io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		resp.Body.Close()
	}
	family := ""
	if e == nil && status == 200 {
		if match := pageTitle.FindSubmatch(data); len(match) > 1 {
			family = familyName(string(match[1]))
		}
	}
	// Do not put an HTML page (which can contain embedded secrets) into diagnostics.
	if trace != nil {
		safe, _ := json.Marshal(map[string]string{"fw_name": family})
		trace("/", "GET", status, safe, e, time.Since(start))
	}
	return family
}

func readByteon(ctx context.Context, c *http.Client, base string, creds Credentials) (Reading, error) {
	return readByteonTraced(ctx, c, base, creds, nil)
}

func readByteonTraced(ctx context.Context, c *http.Client, base string, creds Credentials, trace restTrace) (Reading, error) {
	fetch := func(path, method, token string, body []byte) ([]byte, int, error) {
		started := time.Now()
		data, status, e := restJSON(ctx, c, base, path, method, token, body)
		if trace != nil {
			safe := data
			if path == "/api/v1/unlock" {
				safe = nil
			}
			trace(path, method, status, safe, e, time.Since(started))
		}
		return data, status, e
	}
	info, infoCode, infoErr := fetch("/api/v1/info", "GET", "", nil)
	// A missing REST route is normal on stock firmware; do not spend the rest of
	// this device's deadline probing unrelated HTTP routes.
	if infoCode == 404 || infoCode == 405 {
		return Reading{}, errors.New("REST info endpoint unavailable")
	}
	if infoErr != nil && infoCode != 401 && infoCode != 403 {
		return Reading{}, infoErr
	}
	im, _ := decode(info)
	if im == nil {
		im = map[string]any{}
	}
	family := firmwareFamily(im)
	summary, summaryCode, summaryErr := fetch("/api/v1/summary", "GET", "", nil)
	if sm, e := decode(summary); e == nil && family == "" {
		family = firmwareFamily(sm)
	}
	needsAuth := infoCode == 401 || infoCode == 403 || summaryCode == 401 || summaryCode == 403
	if family == "" && (needsAuth || infoErr == nil) {
		family = identifyRESTPage(ctx, c, base, trace)
	}
	if family == "" {
		return Reading{}, errors.New("REST 接口可达，但未确认倍腾/兼容固件标识；未发送解锁密码")
	}
	token := ""
	if needsAuth {
		if creds.Password == "" {
			return Reading{}, errors.New("倍腾兼容 REST 需要解锁密码")
		}
		payload, _ := json.Marshal(map[string]string{"pw": creds.Password})
		unlocked, _, ue := fetch("/api/v1/unlock", "POST", "", payload)
		if ue != nil {
			return Reading{}, errors.New("倍腾兼容 REST 解锁失败，请检查密码或固件接口版本")
		}
		um, ue := decode(unlocked)
		if ue != nil {
			return Reading{}, errors.New("解锁响应无效")
		}
		token = text(um, "token")
		if token == "" || len(token) > 8192 {
			return Reading{}, errors.New("解锁未返回有效令牌")
		}
		// info can require the same bearer token. Re-read it after authentication to
		// recover hr_measure and firmware identity, then read current measurements.
		if infoErr != nil {
			info, _, infoErr = fetch("/api/v1/info", "GET", token, nil)
			if infoErr == nil {
				im, _ = decode(info)
			}
		}
		summary, _, summaryErr = fetch("/api/v1/summary", "GET", token, nil)
	}
	if summaryErr != nil {
		return Reading{}, summaryErr
	}
	if im == nil {
		im = map[string]any{}
	}
	im = unwrapREST(im, "fw_name", "firmware_name", "miner", "model", "hr_measure")
	// An identified HTML title is enough to select the documented compatibility
	// parser; it supplies no rate units or metric values.
	if firmwareFamily(im) == "" {
		im["fw_name"] = family
	}
	info, _ = json.Marshal(im)
	r, e := ParseByteon(summary, info)
	if e == nil && infoErr != nil {
		r.Warnings = append(r.Warnings, "REST info 读取失败：算力单位/型号可能缺失")
	}
	if e == nil && explicitZeroHashrate(r) && (r.Protocol == "Byteon REST API" || r.Protocol == "Anthill REST API") {
		if clue := readByteonLogClue(ctx, c, base, token, trace); clue.token != "" {
			addHealthFault(&r, clue.category, clue.description, "倍腾/Anthill REST 矿机日志", "code="+clue.token, "log")
		}
	}
	return r, e
}

// ParseByteon decodes the published nested summary schema, not control settings.
func ParseByteon(summary, info []byte) (Reading, error) {
	im, e := decode(info)
	if e != nil {
		return Reading{}, e
	}
	family := firmwareFamily(im)
	if family == "" {
		return Reading{}, errors.New("unknown REST firmware identity")
	}
	im = unwrapREST(im, "miner", "model", "hr_measure")
	sm, e := decode(summary)
	if e != nil {
		return Reading{}, e
	}
	sm = unwrapREST(sm, "miner")
	m, ok := sm["miner"].(map[string]any)
	if !ok {
		return Reading{}, errors.New("REST summary missing miner object")
	}
	r := Reading{Brand: "Antminer", Model: text(im, "miner", "model"), Protocol: family + " REST API", FirmwareKnown: true}
	r = merge(r, healthByteon(m))
	r = merge(r, sharePools(m["pools"]))
	r = merge(r, poolWorkers(m["pools"]))
	r = merge(r, byteonTemperatures(m))
	if r.Model == "" {
		r.Model = text(m, "miner_type")
	}
	if r.Model != "" {
		r.Model += " · " + family
	}
	unit := text(im, "hr_measure")
	factor, known := map[string]float64{"TH/s": 1, "GH/s": 1e-3, "MH/s": 1e-6, "PH/s": 1e3}[unit]
	if known {
		r.RealtimeTH = scale(number(m, "hr_realtime"), factor)
		r.AverageTH = scale(number(m, "hr_average"), factor)
		r.HashrateSource = family + " miner.hr_realtime（" + unit + "）"
	}
	// Deprecated instant_hashrate/average_hashrate units are not guessed.
	r.PowerW = nonnegative(number(m, "power_consumption"))
	if r.PowerW != nil {
		r.PowerSource = family + " REST miner.power_consumption（W，固件报告）"
	}
	if temp, ok := m["chip_temp"].(map[string]any); ok {
		r.TempC = arrayMax(temp["max"])
	}
	if r.TempC == nil {
		if temp, ok := m["pcb_temp"].(map[string]any); ok {
			r.TempC = arrayMax(temp["max"])
		}
	}
	if r.PowerW == nil && !hasData(r) && !r.StatusKnown {
		return Reading{}, errors.New("REST summary contains no recognizable measurements")
	}
	return collectTemperatures(r), nil
}
