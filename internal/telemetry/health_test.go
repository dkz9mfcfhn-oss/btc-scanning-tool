package telemetry

import (
	"math"
	"strings"
	"testing"
)

func TestAssessHashrateDistinguishesUnknownZeroFailureAndRecovery(t *testing.T) {
	zero, positive, negative, nan := 0.0, 0.01, -1.0, math.NaN()
	faults := []Fault{{Code: "paused", Message: "矿机已停止", Source: "current status", Confidence: "reported"}}
	for _, tc := range []struct {
		name                 string
		rate                 *float64
		online               bool
		err, state, contains string
	}{
		{"zero", &zero, true, "", "zero", "已停止"},
		{"missing", nil, true, "", "missing", "未上报"},
		{"negative", &negative, true, "", "missing", "未上报"},
		{"nan", &nan, true, "", "missing", "未上报"},
		{"authentication", &zero, false, "http_403", "unavailable", "认证"},
		{"timeout", &zero, false, "deadline exceeded", "unavailable", "超时"},
		{"unreachable", nil, false, "connection refused", "unavailable", "无法读取"},
		{"recovered", &positive, true, "", "normal", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := AssessHashrate(tc.rate, tc.online, faults, tc.err)
			if h.State != tc.state || !strings.Contains(h.Reason, tc.contains) {
				t.Fatalf("unexpected assessment: %+v", h)
			}
			if tc.state == "normal" && h.Reason != "" {
				t.Fatal("retained old reason after recovery", h)
			}
			if tc.state == "unavailable" && strings.Contains(h.Reason, "已停止") {
				t.Fatal("read failure inferred stop", h)
			}
			if tc.state != "zero" && len(h.Evidence) != 0 {
				t.Fatal("published fault evidence without explicit zero", h)
			}
		})
	}
	if h := AssessHashrate(&zero, true, nil, ""); !strings.Contains(h.Reason, "原因未确认") {
		t.Fatal(h)
	}
}

func TestAssessHashrateEvidencePriorityAndUncertainty(t *testing.T) {
	zero := 0.0
	faults := []Fault{
		{Code: "power", Message: "电源异常", Confidence: "log"},
		{Code: "pool", Message: "全部矿池不可用", Confidence: "possible"},
		{Code: "paused", Message: "矿机处于休眠模式", Confidence: "reported"},
	}
	if h := AssessHashrate(&zero, true, faults, ""); !strings.Contains(h.Reason, "休眠模式（设备报告）") {
		t.Fatal(h)
	}
	if h := AssessHashrate(&zero, true, faults[:2], ""); !strings.HasPrefix(h.Reason, "待确认：") {
		t.Fatal(h)
	}
	if h := AssessHashrate(&zero, true, faults[:1], ""); !strings.Contains(h.Reason, "待确认") {
		t.Fatal(h)
	}
	if h := AssessHashrate(nil, true, faults, ""); h.State != "missing" || strings.Contains(h.Reason, "休眠") || len(h.Evidence) != 0 {
		t.Fatal("fault diagnosis supplied without a realtime measurement", h)
	}
}

// A 0 TH/s API symptom should not suppress a recognized log clue, but a
// concrete firmware report keeps priority over diagnostic interpretation.
func TestSpecificFaultOutranksGenericHashrateStatus(t *testing.T) {
	zero := 0.0
	generic := Fault{Code: "hashrate", Message: "矿机报告算力异常", Confidence: "reported"}
	log := Fault{Code: "fan", Message: "风扇异常", Confidence: "log"}
	current := Fault{Code: "overheat", Message: "过热保护", Confidence: "reported"}
	if got := AssessHashrate(&zero, true, []Fault{generic, log}, "").Reason; got != "日志提示风扇异常（待确认）" {
		t.Fatal(got)
	}
	if got := AssessHashrate(&zero, true, []Fault{generic, log, current}, "").Reason; got != "过热保护（设备报告）" {
		t.Fatal(got)
	}
}
