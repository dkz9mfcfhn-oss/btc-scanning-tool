package miner

import (
	"fmt"
	"minerwatch/internal/telemetry"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// These parsers only inspect documented health-bearing objects. API reply
// STATUS/code, lifetime error counters and previous restart/log entries are not
// current mining faults. Free-form device messages are classified, never copied.
func healthCGMiner(m map[string]any) Reading {
	r := Reading{}
	for _, s := range section(m, "SUMMARY") {
		for _, status := range objects(s["status"]) {
			kind := strings.ToLower(text(status, "type"))
			category, ok := map[string]string{"rate": "hashrate", "network": "pool", "fans": "fan", "temp": "temperature"}[kind]
			state := strings.ToLower(text(status, "status"))
			if !ok || (state != "s" && state != "e") {
				continue
			}
			r.StatusKnown = true
			if state == "e" {
				msg := map[string]string{"rate": "矿机报告算力异常", "network": "矿机报告网络异常", "fans": "矿机报告风扇异常", "temp": "矿机报告温度异常"}[kind]
				evidence := "type=" + kind + "; status=e"
				if code := healthCode(status["code"]); code != "" {
					evidence += "; code=" + code
				}
				if c, safe := classifiedHealthDescription(text(status, "msg")); safe != "" {
					category, msg = c, safe
				}
				addHealthFault(&r, category, msg, "Antminer SUMMARY.status", evidence, "reported")
			}
		}
	}
	for _, s := range section(m, "STATS") {
		if mode := healthInteger(s["miner-mode"]); mode != "" && (mode == "0" || mode == "1" || mode == "2" || mode == "3") {
			r.StatusKnown = true
			if mode == "1" {
				addHealthFault(&r, "paused", "矿机处于休眠模式", "Antminer STATS.miner-mode", "miner-mode=1", "reported")
			}
		}
		keys := make([]string, 0)
		for key := range s {
			if avalonMMKey.MatchString(key) || key == avalonFlatKey {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			if value, ok := s[key].(string); ok {
				healthAvalonModule(&r, key, value)
			}
		}
	}
	pools, present := m["POOLS"]
	if !present {
		pools = m["pools"]
	}
	healthPools(&r, pools, "CGMiner POOLS")
	return r
}

func healthWhatsminerV3(msg map[string]any) Reading {
	r := Reading{}
	if miner, ok := msg["miner"].(map[string]any); ok {
		if working, valid := healthBoolean(miner["working"]); valid {
			r.StatusKnown = true
			if !working {
				addHealthFault(&r, "not-working", "矿机报告未工作，具体原因未确认", "Whatsminer miner.working", "working=false", "reported")
			}
		}
	}
	if errors, ok := msg["error-code"].([]any); ok {
		// An explicit empty error list is a valid status observation. Malformed
		// nonempty data must not be accepted as proof of a healthy miner.
		if len(errors) == 0 {
			r.StatusKnown = true
		}
		for _, value := range errors {
			err, ok := value.(map[string]any)
			if !ok {
				continue
			}
			keys := make([]string, 0)
			for code := range err {
				if healthDecimalCode.MatchString(code) {
					keys = append(keys, code)
				}
			}
			sort.Strings(keys)
			for _, code := range keys {
				timestamp := healthTimestamp(err[code])
				if timestamp == "" {
					continue
				}
				r.StatusKnown = true
				description := "矿机报告错误码 " + code + "（有效期未确认）"
				if _, safe := classifiedHealthDescription(text(err, "reason")); safe != "" {
					description = safe + "（错误码 " + code + "，有效期未确认）"
				}
				addHealthFault(&r, code, description, "Whatsminer error-code", "code="+code+"; time="+timestamp, "possible")
			}
		}
	}
	healthPools(&r, msg["pools"], "Whatsminer pools")
	return r
}

func healthByteon(miner map[string]any) Reading {
	r := Reading{}
	if status, ok := miner["miner_status"].(map[string]any); ok {
		state := text(status, "miner_state")
		code, description, valid := byteonHealthState(state, false)
		if valid {
			r.StatusKnown = true
			if description != "" {
				if state == "failure" {
					if c, safe := classifiedHealthDescription(text(status, "description")); safe != "" {
						code, description = c, safe
					}
				}
				addHealthFault(&r, code, description, "REST miner.miner_status", "miner_state="+state, "reported")
			}
		}
	}
	for i, chain := range objects(miner["chains"]) {
		if i >= 64 {
			break
		}
		status, ok := chain["status"].(map[string]any)
		if !ok {
			continue
		}
		state := text(status, "state")
		code, description, valid := byteonHealthState(state, true)
		if !valid {
			continue
		}
		r.StatusKnown = true
		if description == "" {
			continue
		}
		label := "算力板"
		evidence := "state=" + state
		if id := healthInteger(chain["id"]); id != "" {
			label += " " + id
			evidence = "chain=" + id + "; " + evidence
		}
		if state == "failure" {
			if c, safe := classifiedHealthDescription(text(status, "description")); safe != "" {
				code, description = c, safe
			}
		}
		addHealthFault(&r, code, label+"："+description, "REST miner.chains.status", evidence, "reported")
	}
	return r
}

func byteonHealthState(state string, chain bool) (code, description string, valid bool) {
	if chain {
		switch state {
		case "mining":
			return "", "", true
		case "initializing":
			return "starting", "正在初始化", true
		case "stopped":
			return "paused", "已停止", true
		case "failure":
			return "board", "固件报告异常", true
		case "disconnected":
			return "board", "固件报告连接断开", true
		case "disabled":
			return "paused", "已禁用", true
		case "unknown":
			return "", "", true
		}
		return "", "", false
	}
	switch state {
	case "mining":
		return "", "", true
	case "initializing":
		return "starting", "矿机正在初始化", true
	case "starting":
		return "starting", "矿机正在启动", true
	case "auto-tuning":
		return "tuning", "矿机正在自动调频", true
	case "restarting":
		return "starting", "矿机正在重新启动", true
	case "shutting-down":
		return "paused", "矿机正在停止", true
	case "stopped":
		return "paused", "矿机已停止", true
	case "failure":
		return "firmware-failure", "固件报告运行异常", true
	}
	return "", "", false
}

func healthPools(r *Reading, value any, source string) {
	var pools []any
	switch v := value.(type) {
	case []any:
		pools = v
	case map[string]any:
		pools = []any{v}
	default:
		return
	}
	dead, alive, uncertain := 0, 0, false
	for _, value := range pools {
		pool, ok := value.(map[string]any)
		if !ok {
			uncertain = true
			continue
		}
		// Empty configured slots are not a pool. The address is never retained.
		if url, ok := pool["URL"].(string); ok && strings.TrimSpace(url) == "" {
			continue
		}
		if url, ok := pool["url"].(string); ok && strings.TrimSpace(url) == "" {
			continue
		}
		switch strings.ToLower(text(pool, "Status", "status")) {
		case "alive":
			alive++
		case "dead":
			dead++
		default:
			uncertain = true
		}
	}
	if dead+alive > 0 {
		r.StatusKnown = true
	}
	if dead > 0 && alive == 0 && !uncertain {
		addHealthFault(r, "pool", "全部已返回状态的矿池均不可用", source, fmt.Sprintf("dead=%d; alive=0", dead), "possible")
	}
}

var (
	avalonMMKey       = regexp.MustCompile(`^MM ID[0-9]{1,4}$`)
	healthDecimalCode = regexp.MustCompile(`^[1-9][0-9]{0,7}$`)
	healthCodePattern = regexp.MustCompile(`^(?:[A-Za-z]{1,8}[-_]?)?[0-9]{1,8}$`)
	avalonSystemWork  = regexp.MustCompile(`(?i)(?:^|,)\s*Work:\s*([^,]{1,40})(?:,|$)`)
	avalonBoardCount  = regexp.MustCompile(`(?i)(?:^|,)\s*Hash Board:\s*([0-9]{1,3})\s*(?:,|$)`)
	healthSlotAbsent  = regexp.MustCompile(`(?i)^slot\s*([0-9]{1,3})\s+not found\.?$`)
)

func healthAvalonModule(r *Reading, module, value string) {
	for _, part := range bracket.FindAllStringSubmatch(value, -1) {
		field, raw := part[1], part[2]
		source := "Avalon " + module + "." + field
		switch field {
		case "BOOTBY":
			addAvalonBootBy(r, source, raw)
		case "SYSTEMSTATU":
			match := avalonSystemWork.FindStringSubmatch(raw)
			if len(match) < 2 {
				continue
			}
			// Only fixed state words enter evidence; arbitrary strings could contain
			// account or pool information. Do not guess an unknown state's meaning.
			state := strings.TrimSpace(match[1])
			label, recognized := map[string]string{"in work": "", "working": "", "not work": "系统未工作", "in idle": "系统处于空闲状态", "idle": "系统处于空闲状态", "initializing": "系统正在初始化", "starting": "系统正在启动", "stopped": "系统已停止", "sleep": "系统处于休眠状态", "sleeping": "系统处于休眠状态"}[strings.ToLower(state)]
			if !recognized {
				continue
			}
			r.StatusKnown = true
			evidence := "Work=" + state
			if count := avalonBoardCount.FindStringSubmatch(raw); len(count) > 1 {
				evidence += "; Hash Board=" + count[1]
			}
			if label != "" {
				addHealthFault(r, "not-working", label, source, evidence, "reported")
			}
		case "ECHU", "ECMM":
			codes, valid := healthNumericList(raw)
			if !valid {
				continue
			}
			r.StatusKnown = true
			abnormal := false
			for _, code := range codes {
				n, _ := strconv.ParseUint(code, 10, 32)
				if n != 0 && !(field == "ECHU" && n == 512) {
					abnormal = true
				}
			}
			if abnormal {
				addHealthFault(r, "avalon-"+strings.ToLower(field), "矿机报告 "+field+" 状态码（机型含义待确认）", source, field+"["+strings.Join(codes, " ")+"]", "possible")
			}
		}
	}
}

func addAvalonBootBy(r *Reading, source, raw string) {
	evidence := telemetry.AvalonBootByEvidence("BOOTBY[" + strings.TrimSpace(raw) + "]")
	if evidence != "" {
		addHealthFault(r, "avalon-bootby", "上次系统重启原因码", source, evidence, "historical")
	}
}

func hasAvalonBootBy(r Reading) bool {
	for _, item := range r.Faults {
		if item.Code == "avalon-bootby" && item.Confidence == "historical" {
			return true
		}
	}
	return false
}

func healthNumericList(raw string) ([]string, bool) {
	parts := strings.Fields(raw)
	if len(parts) == 0 || len(parts) > 64 {
		return nil, false
	}
	for _, value := range parts {
		if len(value) > 8 {
			return nil, false
		}
		if _, err := strconv.ParseUint(value, 10, 32); err != nil {
			return nil, false
		}
	}
	return parts, true
}

func healthInteger(value any) string {
	n := numeric(value)
	if n == nil || *n < 0 || *n > 99999999 || *n != float64(int64(*n)) {
		return ""
	}
	return strconv.FormatInt(int64(*n), 10)
}

func healthCode(value any) string {
	if n := healthInteger(value); n != "" {
		return n
	}
	if s, ok := value.(string); ok && healthCodePattern.MatchString(s) {
		return s
	}
	return ""
}

func healthBoolean(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func healthTimestamp(value any) string {
	s, ok := value.(string)
	if !ok || len(s) > 35 {
		return ""
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if _, err := time.Parse(layout, s); err == nil {
			return s
		}
	}
	return ""
}

// A small, exact allowlist avoids saving wallet addresses, accounts, tokens or
// arbitrary logs in descriptive fields. Unknown descriptions retain only their
// containing structured state's generic category.
func classifiedHealthDescription(raw string) (string, string) {
	if len(raw) > 256 {
		return "", ""
	}
	value := strings.ToLower(strings.TrimSpace(raw))
	if match := healthSlotAbsent.FindStringSubmatch(value); len(match) > 1 {
		return "board", "未检测到算力板插槽 " + match[1]
	}
	value = strings.TrimSuffix(value, ".")
	for _, group := range []struct {
		code, message string
		values        []string
	}{
		{"overheat", "矿机报告过热", []string{"overheat", "overheated", "over temperature", "over-temperature", "temperature too high", "温度过高", "过热"}},
		{"fan", "矿机报告风扇异常", []string{"fan error", "fan failure", "fan failed", "fan lost", "fan speed too low", "not enough fans", "风扇异常", "风扇故障"}},
		{"power", "矿机报告电源异常", []string{"power supply error", "power supply failure", "psu error", "psu failure", "电源异常", "电源故障"}},
		{"board", "矿机报告算力板异常", []string{"hashboard error", "hashboard failure", "hashboard not found", "hash board not found", "算力板异常", "算力板故障"}},
		{"pool", "矿机报告矿池连接异常", []string{"pool connection failed", "all pools dead", "no active pools", "矿池连接失败"}},
	} {
		for _, allowed := range group.values {
			if value == allowed {
				return group.code, group.message
			}
		}
	}
	return "", ""
}

func addHealthFault(r *Reading, code, message, source, evidence, confidence string) {
	r.Faults = telemetry.MergeFaults(r.Faults, []telemetry.Fault{{Code: code, Message: message, Source: source, Evidence: evidence, Confidence: confidence}})
}
