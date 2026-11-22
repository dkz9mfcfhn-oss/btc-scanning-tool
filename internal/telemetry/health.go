package telemetry

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// Fault contains bounded evidence selected from status or device logs.
// reported means the device reported that condition, not that we proved causality.
type Fault struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Source     string `json:"source"`
	Evidence   string `json:"evidence,omitempty"`
	Confidence string `json:"confidence"` // reported, possible, log, historical
}

var avalonBootByPattern = regexp.MustCompile(`^BOOTBY\[(?:[0-9]{1,8}|0[xX][0-9A-Fa-f]{1,8}(?:\.[0-9A-Fa-f]{1,16})?)\]$`)

// AvalonBootByEvidence permits the complete code, including a firmware suffix,
// without admitting arbitrary text from the log into history or notifications.
func AvalonBootByEvidence(raw string) string {
	if avalonBootByPattern.MatchString(raw) {
		return raw
	}
	return ""
}

type HashrateHealth struct {
	State    string  `json:"state"` // normal, zero, missing, unavailable
	Reason   string  `json:"reason,omitempty"`
	Evidence []Fault `json:"evidence,omitempty"`
}

func CloneHealth(h HashrateHealth) HashrateHealth {
	h.Evidence = append([]Fault(nil), h.Evidence...)
	return h
}
func MergeFaults(a, b []Fault) []Fault {
	out := append([]Fault(nil), a...)
	for _, v := range b {
		found := false
		for _, old := range out {
			if old.Code == v.Code && old.Source == v.Source && old.Evidence == v.Evidence {
				found = true
				break
			}
		}
		if !found && len(out) < 24 {
			out = append(out, v)
		}
	}
	return out
}
func validHash(v *float64) bool { return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0 }
func AssessHashrate(rt *float64, online bool, faults []Fault, readError string) HashrateHealth {
	h := HashrateHealth{}
	if !online {
		h.State = "unavailable"
		h.Reason = "无法读取算力，请检查网络和矿机API"
		e := strings.ToLower(readError)
		switch {
		case strings.Contains(e, "认证") || strings.Contains(e, "用户名") || strings.Contains(e, "解锁") || strings.Contains(e, "401") || strings.Contains(e, "403") || strings.Contains(e, "authentication"):
			h.Reason = "认证失败，无法读取算力"
		case strings.Contains(e, "超时") || strings.Contains(e, "timeout") || strings.Contains(e, "deadline"):
			h.Reason = "连接超时，无法判断矿机是否停机"
		}
		return h
	}
	if !validHash(rt) {
		h.State = "missing"
		h.Reason = "实时算力未上报，不能判定为零算力"
		return h
	}
	if *rt > 0 {
		h.State = "normal"
		return h
	}
	h.State = "zero"
	h.Evidence = append([]Fault(nil), faults...)
	h.Reason = "实时算力为零，原因未确认"
	if reason := faultReason(faults); reason != "" {
		h.Reason = reason
	}
	return h
}

func faultReason(faults []Fault) string {
	priority := func(f Fault) int {
		if f.Code == "hashrate" && f.Message == "矿机报告算力异常" {
			// This only restates the symptom. A specific fault or log clue
			// explains more while retaining its own confidence label.
			return 10
		}
		n := 0
		switch f.Confidence {
		case "reported":
			n = 100
		case "possible":
			n = 50
		case "log":
			n = 20
		}
		switch f.Code {
		case "overheat", "temperature", "power", "fan", "board":
			n += 20
		case "paused", "starting", "tuning":
			n += 15
		case "pool":
			n += 10
		}
		return n
	}
	ordered := append([]Fault(nil), faults...)
	sort.SliceStable(ordered, func(i, j int) bool { return priority(ordered[i]) > priority(ordered[j]) })
	for _, f := range ordered {
		if f.Message == "" {
			continue
		}
		switch f.Confidence {
		case "reported":
			return f.Message + "（设备报告）"
		case "possible":
			return "待确认：" + f.Message
		case "log":
			return "日志提示" + f.Message + "（待确认）"
		default:
			continue
		}
	}
	return ""
}
