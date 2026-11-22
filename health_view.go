package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"minerwatch/internal/store"
	"minerwatch/internal/telemetry"
)

type healthView struct {
	State, Status, Reason string
	Evidence              []telemetry.Fault
}

// A failed read still has a sampling time. Treat an old failure as outdated,
// just like an old successful read, without carrying its reason forward.
func currentSampleTime(s *store.Sample, now time.Time) bool {
	if s == nil || s.At.IsZero() {
		return false
	}
	age := now.Sub(s.At)
	return age >= -time.Minute && age <= 2*sampleInterval(s)
}

func sampleHealthView(s *store.Sample, now time.Time) healthView {
	if !currentSampleTime(s, now) {
		return healthView{State: "stale", Status: "待更新"}
	}
	// Re-check the current reading before trusting persisted classification.
	// This also handles historical samples recorded before Health was added.
	assessed := telemetry.AssessHashrate(s.RealtimeTH, s.Online, s.Health.Evidence, s.Error)
	h := s.Health
	if assessed.State != "zero" || h.State != "zero" || h.Reason == "" {
		h = assessed
	}
	v := healthView{State: assessed.State, Reason: healthOneLine(h.Reason), Evidence: append([]telemetry.Fault(nil), h.Evidence...)}
	switch assessed.State {
	case "normal":
		v.Status, v.Reason, v.Evidence = "在线", "", nil
	case "zero":
		v.Status = "无算力"
	case "missing":
		v.Status = "算力未报"
	case "unavailable":
		v.Status = "读取失败"
	}
	return v
}

func healthOneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func (v healthView) avalonRestartCodes() string {
	if v.State != "zero" {
		return ""
	}
	var codes []string
	seen := map[string]bool{}
	for _, f := range v.Evidence {
		if f.Code != "avalon-bootby" || f.Confidence != "historical" {
			continue
		}
		code := telemetry.AvalonBootByEvidence(f.Evidence)
		if code != "" && !seen[code] {
			codes = append(codes, code)
			seen[code] = true
		}
		if len(codes) == 4 {
			break
		}
	}
	return strings.Join(codes, "、")
}

func healthSummary(s *store.Sample, now time.Time) string {
	return chinese.healthSummary(s, now)
}

func (l uiLanguage) healthSummary(s *store.Sample, now time.Time) string {
	v := sampleHealthView(s, now)
	if v.Reason == "" {
		return l.text(v.Status)
	}
	return l.text(v.Status) + " · " + l.diagnostic(v.Reason)
}

// Diagnostic evidence is deliberately confined to detail views. Keep just a
// few short source/code lines here; the raw diagnostic export remains separate.
func healthDetailText(s *store.Sample, now time.Time) string { return chinese.healthDetailText(s, now) }

func (l uiLanguage) healthDetailText(s *store.Sample, now time.Time) string {
	v := sampleHealthView(s, now)
	lines := []string{l.text("算力状态：") + l.healthSummary(s, now)}
	if v.State == "stale" {
		return strings.Join(lines, "\r\n") + l.text("\r\n当前尚无有效新采样，未沿用历史故障原因。\r\n")
	}
	if v.State == "normal" || len(v.Evidence) == 0 {
		return strings.Join(lines, "\r\n") + "\r\n"
	}
	if codes := v.avalonRestartCodes(); codes != "" {
		lines = append(lines, l.text("上次重启记录：")+codes)
	}
	current := make([]telemetry.Fault, 0, len(v.Evidence))
	for _, evidence := range v.Evidence {
		if evidence.Confidence != "historical" {
			current = append(current, evidence)
		}
	}
	if len(current) == 0 {
		return strings.Join(lines, "\r\n") + "\r\n"
	}
	lines = append(lines, l.text("诊断依据："))
	for i, evidence := range current {
		if i == 4 {
			lines = append(lines, fmt.Sprintf(l.text("另有 %d 条依据，可导出采集诊断查看。"), len(current)-i))
			break
		}
		confidence := map[string]string{"reported": l.text("设备报告"), "possible": l.text("疑似"), "log": l.text("日志提示，待确认")}[evidence.Confidence]
		if confidence == "" {
			confidence = l.text("待确认")
		}
		code, source := healthOneLine(evidence.Code), healthOneLine(evidence.Source)
		if code == "" {
			code = l.text("未提供")
		}
		if source == "" {
			source = l.text("未提供")
		}
		lines = append(lines, "· "+confidence+l.text("；故障码：")+healthShort(code, 48)+l.text("；来源：")+healthShort(source, 120))
		if message := healthOneLine(evidence.Message); message != "" {
			lines = append(lines, "  "+healthShort(l.diagnostic(message), 120))
		}
		if structured := safeHealthEvidence(evidence.Evidence); structured != "" {
			lines = append(lines, l.text("  结构化依据：")+structured)
		}
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

var healthEvidencePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^type=(rate|network|fans|temp); status=[es](; code=([A-Za-z]{1,8}[-_]?)?[0-9]{1,8})?$`),
	regexp.MustCompile(`^miner-mode=[0-3]$`),
	regexp.MustCompile(`^working=false$`),
	regexp.MustCompile(`^miner_state=(mining|initializing|starting|auto-tuning|restarting|shutting-down|stopped|failure)$`),
	regexp.MustCompile(`^(chain=[0-9]{1,8}; )?state=(mining|initializing|stopped|failure|disconnected|disabled|unknown)$`),
	regexp.MustCompile(`(?i)^Work=(in work|working|not work|in idle|idle|initializing|starting|stopped|sleep|sleeping)(; Hash Board=[0-9]{1,3})?$`),
	regexp.MustCompile(`^dead=[1-9][0-9]{0,7}; alive=0$`),
	regexp.MustCompile(`^code=(ERROR_FAN_LOST|SWEEP_[VFP]:1|SWEEP_J:6|PIC_TEMP_READ_FAILED|ASIC_NOT_FOUND|OVER_MAX_TEMP)$`),
}

var healthEvidenceAvalon = regexp.MustCompile(`^(ECHU|ECMM)\[([0-9]{1,8}( [0-9]{1,8}){0,63})\]$`)
var healthEvidenceTimedCode = regexp.MustCompile(`^code=[1-9][0-9]{0,7}; time=(.+)$`)

// Evidence may come from old local records, so validate again at the UI
// boundary. Only the structured formats emitted by miner/health.go are shown;
// arbitrary device text, appended fields, and control characters are rejected.
func safeHealthEvidence(raw string) string {
	// 64 Avalon values of at most eight digits need at most 581 bytes.
	if raw == "" || len(raw) > 640 {
		return ""
	}
	for _, r := range raw {
		if r < 0x20 || r > 0x7e {
			return ""
		}
	}
	if healthEvidenceAvalon.MatchString(raw) {
		return raw
	}
	if match := healthEvidenceTimedCode.FindStringSubmatch(raw); len(match) == 2 {
		if len(match[1]) > 35 {
			return ""
		}
		for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
			if _, err := time.Parse(layout, match[1]); err == nil {
				return raw
			}
		}
		return ""
	}
	for _, pattern := range healthEvidencePatterns {
		if pattern.MatchString(raw) {
			return raw
		}
	}
	return ""
}

func healthShort(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func displayWarnings(warnings []string) []string {
	out := []string{}
	for _, w := range warnings {
		if !strings.Contains(w, "功率") && !strings.Contains(w, "功耗") {
			out = append(out, w)
		}
	}
	return out
}
