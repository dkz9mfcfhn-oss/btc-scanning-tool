package miner

import (
	"fmt"
	"regexp"
	"strings"
)

// ParseCGMiner accepts CGMiner JSON and the legacy pipe-separated response.
// Hashrates use the units in field names; missing metrics remain nil.
func ParseCGMiner(data []byte) (Reading, error) {
	m, err := decode(data)
	if err != nil {
		m = parseLegacy(string(data))
		if len(m) == 0 {
			return Reading{}, err
		}
	}
	normalizeAvalonStats(m)
	r := merge(Reading{Protocol: "CGMiner TCP 4028"}, healthCGMiner(m))
	r = merge(r, shareCGMiner(m))
	if v, ok := m["POOLS"]; ok {
		r = merge(r, poolWorkers(v))
	} else {
		r = merge(r, poolWorkers(m["pools"]))
	}
	for _, s := range section(m, "SUMMARY") {
		r = merge(r, parseRates(s))
		if r.ElapsedSeconds == nil {
			r.ElapsedSeconds = elapsedSeconds(number(s, "Elapsed", "elapsed"))
		}
		addSensor(&r.Temperatures, "整机温度（SUMMARY）", "CGMiner SUMMARY.Temperature", s["Temperature"])
		for _, f := range []struct{ key, name string }{{"Chip Temp Avg", "整机芯片均温"}, {"Chip Temp Min", "整机芯片最低温"}, {"Chip Temp Max", "整机芯片最高温"}} {
			addSensor(&r.Temperatures, f.name, "CGMiner SUMMARY."+f.key, s[f.key])
		}
		r.TempC = maximum(r.TempC, arrayMax(s["Temperature"]))
		r.PowerW = reportedPower(s)
		if r.PowerW != nil {
			r.PowerSource = "矿机上报功率（W）"
		}
		if _, ok := s["HS RT"]; ok {
			r.Brand = "Whatsminer"
			r.RealtimeTH = scale(number(s, "HS RT"), 1e-6)
			r.HashrateSource = "HS RT（矿机实时算力）"
		}
	}
	stats := section(m, "STATS")
	if r.ElapsedSeconds == nil && len(stats) > 1 {
		// BTC Tools reads the second record, after the version header.
		r.ElapsedSeconds = elapsedSeconds(number(stats[1], "Elapsed", "elapsed"))
	}
	for _, s := range stats {
		if r.ElapsedSeconds == nil {
			r.ElapsedSeconds = elapsedSeconds(number(s, "Elapsed", "elapsed"))
		}
		r = merge(r, parseRates(s))
		r = merge(r, antminerTemperatures(s))
		r = merge(r, avalonTemperatures(s))
		if model := text(s, "Type", "type", "miner_type"); model != "" {
			r.Model = model
		}
		if p := reportedPower(s); p != nil {
			r.PowerW = p
			r.PowerSource = "矿机状态接口上报功率（W）"
		}
		// Older Antminer firmware returns per-chain comma-separated chip temperatures.
		for k, v := range s {
			if strings.HasPrefix(k, "temp2_") || strings.HasPrefix(k, "temp3_") || k == "temp_chip" {
				r.TempC = maximum(r.TempC, arrayMax(v))
			}
		}
		if r.TempC == nil {
			for k, v := range s {
				if strings.HasPrefix(k, "temp") && k != "temp_max" && k != "temp_pcb" {
					r.TempC = maximum(r.TempC, arrayMax(v))
				}
			}
		}
		for _, c := range objects(s["chain"]) {
			r.TempC = maximum(r.TempC, arrayMax(c["temp_chip"]))
			if r.TempC == nil {
				r.TempC = maximum(r.TempC, arrayMax(c["temp_pcb"]))
			}
		}
		var modules []string
		for k, v := range s {
			if strings.HasPrefix(k, "MM ID") || k == avalonFlatKey {
				if x, ok := v.(string); ok {
					modules = append(modules, x)
				}
			}
		}
		if len(modules) > 0 {
			r = mergeAvalon(r, modules)
		}
	}
	for _, s := range section(m, "VERSION") {
		if r.Model == "" {
			r.Model = text(s, "PROD", "MODEL", "Type", "type")
		}
		desc := strings.ToLower(text(s, "Description", "CGMiner", "BMMiner", "BTMiner"))
		if strings.Contains(desc, "btminer") {
			r.Brand = "Whatsminer"
		}
	}
	for _, s := range section(m, "DEVDETAILS") {
		if r.Model == "" {
			r.Model = text(s, "Model", "model")
		}
	}
	for _, s := range section(m, "DEVS") {
		r.TempC = maximum(r.TempC, number(s, "Chip Temp Max", "Temperature"))
	}
	devs := append(section(m, "DEVS"), section(m, "EDEVS")...)
	for _, d := range devs {
		if d["Slot"] != nil || d["Chip Temp Avg"] != nil || d["Chip Temp Max"] != nil {
			r = merge(r, whatsminerLegacyTemperatures(devs))
			break
		}
	}
	model := strings.ToLower(r.Model)
	if strings.Contains(model, "antminer") || strings.Contains(model, "s19") || strings.Contains(model, "s21") {
		r.Brand = "Antminer"
	}
	if strings.Contains(model, "avalon") {
		r.Brand = "Avalon"
	}
	if strings.Contains(model, "whatsminer") || strings.HasPrefix(model, "m63") || strings.HasPrefix(model, "m30") {
		r.Brand = "Whatsminer"
	}
	if r.Brand == "" {
		if _, ok := m["BMMiner"]; ok {
			r.Brand = "Antminer"
		}
	}
	btcCGMinerTemperatures(m, &r)
	selectAvalonBoardMeans(&r)
	return collectTemperatures(r), nil
}

func parseRates(s map[string]any) Reading {
	r := Reading{}
	for _, u := range []struct {
		key    string
		factor float64
	}{{"THS", 1}, {"GHS", 1e-3}, {"MHS", 1e-6}, {"KHS", 1e-9}} {
		if r.RealtimeTH == nil {
			for _, period := range []string{" 5s", " 1m", " 5m"} {
				if v := scale(number(s, u.key+period), u.factor); v != nil {
					r.RealtimeTH = v
					r.HashrateSource = u.key + period
					break
				}
			}
		}
		if r.AverageTH == nil {
			r.AverageTH = scale(number(s, u.key+" av"), u.factor)
		}
	}
	unit := text(s, "rate_unit")
	if unit != "" {
		f, ok := map[string]float64{"TH/s": 1, "GH/s": 1e-3, "MH/s": 1e-6, "KH/s": 1e-9, "H/s": 1e-12, "PH/s": 1e3}[unit]
		if ok {
			if v := scale(number(s, "rate_5s"), f); v != nil {
				r.RealtimeTH = v
				r.HashrateSource = "rate_5s（5 秒）"
			}
			if v := scale(number(s, "rate_avg"), f); v != nil {
				r.AverageTH = v
			}
		}
	}
	return r
}

// ParseAntminer parses modern Bitmain HTTP and older miner-status responses.
func ParseAntminer(data []byte) (Reading, error) {
	r, err := ParseCGMiner(data)
	if err != nil {
		return r, err
	}
	m, _ := decode(data)
	r = merge(r, antminerTemperatures(m))
	r.Brand = "Antminer"
	r.Protocol = "Antminer HTTP Digest"
	for _, obj := range []map[string]any{m} {
		if p := reportedPower(obj); p != nil {
			r.PowerW = p
			r.PowerSource = "蚂蚁后台报告功率（W）"
		}
	}

	if s := text(m, "miner_type", "minertype", "type"); s != "" {
		r.Model = s
	}
	for _, s := range section(m, "SUMMARY") {
		// Legacy get_miner_status.cgi uses GH/s under these compact keys.
		if r.RealtimeTH == nil {
			r.RealtimeTH = scale(number(s, "GHS5s"), 1e-3)
			if r.RealtimeTH != nil {
				r.HashrateSource = "GHS5s（5 秒）"
			}
		}
		if r.AverageTH == nil {
			r.AverageTH = scale(number(s, "GHSav"), 1e-3)
		}
	}
	btcAntminerTemperatures(m, &r)
	return collectTemperatures(r), nil
}

var bracket = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_]*)[ \t]*\[([^\]]*)\]`)

func mergeAvalon(r Reading, modules []string) Reading {
	r.Brand = "Avalon"
	var rt, av, pw, wall *float64
	for _, module := range modules {
		fields := map[string]string{}
		for _, p := range bracket.FindAllStringSubmatch(module, -1) {
			fields[p[1]] = p[2]
		}
		if r.ElapsedSeconds == nil {
			r.ElapsedSeconds = elapsedSeconds(numeric(fields["Elapsed"]))
		}
		if v := scale(numeric(fields["GHSmm"]), 1e-3); v != nil {
			if rt == nil {
				rt = ptr(0)
			}
			*rt += *v
		}
		if v := scale(numeric(fields["GHSavg"]), 1e-3); v != nil {
			if av == nil {
				av = ptr(0)
			}
			*av += *v
		}
		r.TempC = maximum(r.TempC, arrayMax(fields["TMax"]))
		if r.TempC == nil {
			r.TempC = maximum(r.TempC, arrayMax(fields["TAvg"]))
		}
		ps := strings.Fields(fields["PS"])
		if p := nonnegative(numeric(fields["WALLPOWER"])); p != nil && wall == nil {
			wall = p // The input-power reading describes the whole miner.
		}
		if len(ps) > 0 {
			if p := nonnegative(numeric(ps[len(ps)-1])); p != nil {
				if pw == nil {
					pw = ptr(0)
				}
				*pw += *p
			}
		}
		if r.Model == "" {
			if v := fields["Ver"]; v != "" {
				r.Model = "Avalon " + strings.SplitN(v, "-", 2)[0]
			}
		}
	}
	if rt != nil {
		r.RealtimeTH = rt
		r.HashrateSource = "GHSmm（芯片原始算力）"
		r.Warnings = append(r.Warnings, "Avalon GHSmm 为校准前芯片算力，可能与矿池有效算力不同")
	}
	if av != nil {
		r.AverageTH = av
	}
	if wall != nil {
		r.PowerW, r.PowerSource = wall, avalonWallSource
	} else if pw != nil {
		r.PowerW = pw
		r.PowerSource = "矿机日志 PS 末项（W）"
	}
	if r.PowerW == nil {
		for _, module := range modules {
			if p, e := ParseAvalonLog([]byte(module)); e == nil {
				r.PowerW = p.PowerW
				r.PowerSource = p.PowerSource
			}
		}
	}
	return r
}

// ParseAvalonLog extracts the newest PSU snapshot rather than adding historic records.
func ParseAvalonLog(data []byte) (Reading, error) {
	r := Reading{Brand: "Avalon", Protocol: "Avalon MM 日志"}
	matches := bracket.FindAllStringSubmatch(string(data), -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if matches[i][1] == "WALLPOWER" {
			if p := nonnegative(numeric(matches[i][2])); p != nil {
				r.PowerW, r.PowerSource = p, avalonWallSource
				return r, nil
			}
		}
	}
	for i := len(matches) - 1; i >= 0; i-- {
		if matches[i][1] != "PS" {
			continue
		}
		v := strings.Fields(matches[i][2])
		if len(v) == 0 {
			continue
		}
		if p := nonnegative(numeric(v[len(v)-1])); p != nil {
			r.PowerW = p
			r.PowerSource = "MM 日志最新 PS 末项（W）"
			return r, nil
		}
	}
	pps := regexp.MustCompile(`\bP_P\s*[:=]\s*([0-9]+(?:\.[0-9]+)?)`).FindAllStringSubmatch(string(data), -1)
	if len(pps) > 0 {
		r.PowerW = nonnegative(numeric(pps[len(pps)-1][1]))
		r.PowerSource = "MM 日志最新 P_P（电源输出 W）"
		return r, nil
	}
	return r, fmt.Errorf("未发现可识别的 WALLPOWER、PS 或 P_P 功率日志")
}

func parseLegacy(s string) map[string]any {
	if m := flatAvalonLegacy(s); m != nil {
		return m
	}
	out := map[string]any{}
	for _, record := range strings.Split(strings.Trim(s, "\x00\r\n "), "|") {
		pairs := splitLegacyFields(record)
		if len(pairs) == 0 {
			continue
		}
		first := strings.SplitN(pairs[0], "=", 2)
		name := strings.TrimSpace(first[0])
		switch name {
		case "SUMMARY", "STATS", "VERSION", "DEVDETAILS", "DEVS", "EDEVS", "POOLS":
		default:
			continue
		}
		m := map[string]any{}
		for _, pair := range pairs {
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) == 2 {
				m[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
		out[name] = append(objectsAny(out[name]), m)
	}
	return out
}

// Avalon MM ID strings contain commas inside bracket values such as
// SYSTEMSTATU[Work: In Work, Hash Board: 3]. Only top-level commas delimit fields.
func splitLegacyFields(s string) []string {
	var fields []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				fields = append(fields, s[start:i])
				start = i + 1
			}
		}
	}
	return append(fields, s[start:])
}
func objectsAny(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}

// ParseWhatsminerV3 combines get.miner.status and get.device.info, both read-only.
func ParseWhatsminerV3(status, info []byte) (Reading, error) {
	r := Reading{Brand: "Whatsminer", Protocol: "Whatsminer API v3 TCP 4433"}
	m, err := decode(status)
	if err != nil {
		return r, err
	}
	if c := number(m, "code"); c == nil || *c != 0 {
		return r, fmt.Errorf("Whatsminer API 拒绝读取")
	}
	msg, _ := m["msg"].(map[string]any)
	r = merge(r, healthWhatsminerV3(msg))
	r = merge(r, poolWorkers(msg["pools"]))
	for _, summary := range objects(msg["summary"]) {
		if stats, ok := summary["power-on-stats"].(map[string]any); ok {
			r.RejectPercent = percent(number(stats, "reject-percent"))
		}
	}
	r = merge(r, whatsminerV3Temperatures(msg))
	for _, s := range objects(msg["summary"]) {
		r.ElapsedSeconds = elapsedSeconds(number(s, "elapsed"))
		r.RealtimeTH = nonnegative(number(s, "hash-realtime", "hash-1min"))
		r.HashrateSource = "hash-realtime（矿机实时算力）"
		if s["hash-realtime"] == nil {
			r.HashrateSource = "hash-1min（1 分钟）"
		}
		r.AverageTH = nonnegative(number(s, "hash-average"))
		r.TempC = number(s, "chip-temp-max")
		if r.TempC == nil {
			r.TempC = arrayMax(s["board-temperature"])
		}
		r.PowerW = nonnegative(number(s, "power-realtime"))
		if r.PowerW != nil {
			r.PowerSource = "矿机 power-realtime 上报（W）"
		}
		r.InletC = number(s, "water-temp-in", "water-temperature-in")
		r.OutletC = number(s, "water-temp-out", "water-temperature-out")
	}
	for _, s := range objects(msg["edevs"]) {
		r.TempC = maximum(r.TempC, number(s, "chip-temp-max"))
	}
	if len(info) > 0 {
		if im, e := decode(info); e == nil {
			if c := number(im, "code"); c != nil && *c == 0 {
				imsg, _ := im["msg"].(map[string]any)
				r = merge(r, healthWhatsminerV3(imsg))
				for _, s := range objects(imsg["miner"]) {
					r.Model = text(s, "type")
				}
				for _, s := range objects(imsg["power"]) {
					addSensor(&r.Temperatures, "电源温度", "Whatsminer v3 get.device.info.power.temp0", s["temp0"])
					if r.PowerW == nil {
						r.PowerW = nonnegative(number(s, "pin"))
						if r.PowerW != nil {
							r.PowerSource = "电源 pin 上报（W）"
						}
					}
				}
			}
		}
	}
	return collectTemperatures(r), nil
}

// Only reported status measurements are accepted, never target/limit/rated values.
func reportedPower(m map[string]any) *float64 {
	for _, key := range []string{"watt", "Power Real", "power_real", "power_actual", "power_w", "power_consumption", "Power", "power"} {
		if v := nonnegative(numeric(m[key])); v != nil {
			return v
		}
	}
	if obj, ok := m["power"].(map[string]any); ok {
		return nonnegative(number(obj, "actual", "realtime", "real", "watt"))
	}
	return nil
}
