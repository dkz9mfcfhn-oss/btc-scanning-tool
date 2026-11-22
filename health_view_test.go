package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"minerwatch/internal/store"
	"minerwatch/internal/telemetry"
)

func TestHealthViewRealtimeClassificationOverridesOldHealth(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name     string
		realtime *float64
		want     string
	}{
		{"positive", num(0.001), "normal"}, {"zero", num(0), "zero"}, {"nil", nil, "missing"},
		{"negative", num(-1), "missing"}, {"NaN", num(math.NaN()), "missing"}, {"infinite", num(math.Inf(1)), "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &store.Sample{At: now, Online: true, RealtimeTH: tc.realtime, AverageTH: num(100), Health: telemetry.HashrateHealth{State: "zero", Reason: "旧故障"}}
			got := sampleHealthView(s, now)
			if got.State != tc.want {
				t.Fatalf("state=%q want %q", got.State, tc.want)
			}
			if tc.want == "normal" && (got.Reason != "" || len(got.Evidence) != 0) {
				t.Fatalf("normal reading retained fault: %+v", got)
			}
			if tc.want == "missing" && strings.Contains(got.Reason, "旧故障") {
				t.Fatal("missing reading reused zero cause")
			}
		})
	}
}

func TestHealthViewFreshnessUsesSampleIntervalForSuccessAndFailure(t *testing.T) {
	now := time.Now()
	for _, online := range []bool{true, false} {
		s := &store.Sample{At: now.Add(-20 * time.Minute), Online: online, IntervalSeconds: 1800, RealtimeTH: num(0), Error: "timeout"}
		if v := sampleHealthView(s, now); v.State == "stale" {
			t.Fatalf("valid long-interval sample stale: %+v", v)
		}
		s.IntervalSeconds = 60
		v := sampleHealthView(s, now)
		if v.State != "stale" || v.Status != "待更新" || v.Reason != "" || len(v.Evidence) != 0 {
			t.Fatalf("old cause persisted: %+v", v)
		}
		s.At = now.Add(2 * time.Minute)
		if v = sampleHealthView(s, now); v.State != "stale" {
			t.Fatal("future sample accepted")
		}
	}
}

func TestHealthViewReadFailureIsUncertaintyNotZeroOrPowerOff(t *testing.T) {
	now := time.Now()
	for _, err := range []string{"timeout", "connection refused", "认证失败", "HTTP 403", "i/o timeout"} {
		s := &store.Sample{At: now, Online: false, RealtimeTH: num(0), Error: err}
		v := sampleHealthView(s, now)
		if v.State != "unavailable" || v.Status != "读取失败" || strings.Contains(v.Reason, "断电") || strings.Contains(v.Reason, "已停机") {
			t.Fatalf("unsafe inference for %q: %+v", err, v)
		}
	}
}

func TestHealthDetailEvidenceIsConciseAndConfinedToDetails(t *testing.T) {
	now := time.Now()
	faults := []telemetry.Fault{
		{Code: "overheat", Message: "温度保护", Source: "stats.chain[1].status", Confidence: "reported", Evidence: "large-raw-payload-never-show"},
		{Code: "pool", Message: "矿池连接异常", Source: "pools[0].status", Confidence: "possible"},
		{Code: "board", Message: "算力板异常", Source: "miner.log", Confidence: "log"},
		{Code: "fan", Message: "风扇异常", Source: strings.Repeat("x", 200), Confidence: "reported"},
		{Code: "power", Message: "电源异常", Source: "summary.power_status", Confidence: "reported"},
	}
	s := &store.Sample{At: now, Online: true, RealtimeTH: num(0), Health: telemetry.AssessHashrate(num(0), true, faults, "")}
	text := healthDetailText(s, now)
	for _, want := range []string{"算力状态：无算力", "故障码：overheat", "来源：stats.chain[1].status", "疑似", "日志提示，待确认", "另有 1 条依据"} {
		if !strings.Contains(text, want) {
			t.Errorf("detail missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "large-raw-payload-never-show") || strings.Contains(text, strings.Repeat("x", 121)) {
		t.Fatal("detail dumped oversized raw evidence")
	}
	if summary := healthSummary(s, now); strings.Contains(summary, "stats.chain") || strings.Contains(summary, "故障码") {
		t.Fatalf("main summary leaked diagnostic evidence: %q", summary)
	}
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		t.Fatal("detail text contains bare LF")
	}
	s.RealtimeTH = num(100)
	if text = healthDetailText(s, now); strings.Contains(text, "诊断依据") || strings.Contains(text, "温度保护") {
		t.Fatalf("recovered device retained old fault details: %q", text)
	}
	s.At = now.Add(-11 * time.Minute)
	if text = healthDetailText(s, now); strings.Contains(text, "overheat") || !strings.Contains(text, "待更新") {
		t.Fatalf("expired cause presented as current: %q", text)
	}
}

func TestSafeHealthEvidenceAcceptsOnlyKnownStructuredFormats(t *testing.T) {
	valid := []string{
		"ECHU[512 513 0]", "ECMM[0 65536]", "ECHU[00000512]",
		"code=2010; time=2026-10-30 11:22:33", "code=112; time=2026-10-30T11:22:33+08:00", "code=112; time=2026-10-30T03:22:33.123Z",
		"type=rate; status=e", "type=temp; status=e; code=1234", "type=fans; status=e; code=FAN_0123",
		"miner-mode=1", "working=false", "miner_state=auto-tuning", "miner_state=failure",
		"state=initializing", "chain=0; state=stopped", "chain=99999999; state=disabled",
		"Work=Not Work; Hash Board=0", "Work=In Idle", "Work=stopped; Hash Board=003",
		"dead=3; alive=0",
		"code=ERROR_FAN_LOST", "code=SWEEP_V:1", "code=PIC_TEMP_READ_FAILED",
	}
	for _, raw := range valid {
		if got := safeHealthEvidence(raw); got != raw {
			t.Errorf("known structured evidence rejected: %q => %q", raw, got)
		}
	}
	invalid := []string{
		"large-raw-payload-never-show", "ECHU[512 wallet-secret]", "ECMM[-1]", "ECMM[123456789]", "ECHU[1 2]; password=hidden",
		"ECHU[512\n513]", "ECHU[512\r\n513]", "ECHU[512\t513]", "ECMM[512]\x00", "ECMM[512]\x1b",
		"code=2010; time=2026-02-30 11:22:33", "code=2010; time=2026-10-30 25:22:33", "code=abc; time=2026-10-30T11:22:33Z", "code=1; time=2026-10-30T11:22:33Z; token=secret", "code=1; time=2026-10-30T11:22:33Z\nInjected",
		"type=network; status=e; code=wallet", "type=token; status=e", "type=rate; status=e; code=123; extra=456",
		"miner-mode=4", "working=token", "miner_state=secret-account", "chain=1; state=custom-message", "chain=1; state=stopped\nInjected",
		"Work=unknown-secret; Hash Board=3", "Work=Stopped; Hash Board=1; account=secret", "Work=Stopped; Hash Board=1000",
		"dead=3; alive=1", "dead=3; alive=0; url=secret", " ECHU[512]", "ECHU[512] ", "ECHU[５１２]",
		"code=SWEEP_V:1; password=secret", "code=FAKE_SWEEP", "code=ASIC_NOT_FOUND\n",
	}
	for _, raw := range invalid {
		if got := safeHealthEvidence(raw); got != "" {
			t.Errorf("unsafe evidence accepted: %q", got)
		}
	}
}

func TestAvalonBootByIsLabeledHistoricalInListDetailAndPush(t *testing.T) {
	now := time.Now()
	const code = "BOOTBY[0x11.00000000]"
	boot := telemetry.Fault{Code: "avalon-bootby", Message: "上次系统重启原因码", Confidence: "historical", Source: "Avalon 日志 BOOTBY", Evidence: code}
	unsafe := boot
	unsafe.Evidence += " account=do-not-display"
	setSample := func(rate float64, faults ...telemetry.Fault) *Device {
		rt := num(rate)
		return &Device{Model: "1566HA", Latest: &store.Sample{At: now, Online: true, RealtimeTH: rt, Health: telemetry.AssessHashrate(rt, true, faults, "")}}
	}
	d := setSample(0, boot, unsafe)
	if got := nativeRow(d, now)[15]; got != "原因未确认 · 上次重启 "+code {
		t.Fatalf("unexpected list cause: %q", got)
	}
	for _, got := range []string{healthDetailText(d.Latest, now), phoneAlertBody(d, now)} {
		if !strings.Contains(got, "原因未确认") || !strings.Contains(got, "上次重启记录："+code) || strings.Contains(got, "do-not-display") {
			t.Fatal(got)
		}
	}
	fan := telemetry.Fault{Code: "fan", Message: "风扇异常", Confidence: "reported"}
	d = setSample(0, boot, fan)
	if got := nativeRow(d, now)[15]; got != "风扇异常（设备报告）" {
		t.Fatalf("historical code beat current fault: %q", got)
	}
	d = setSample(569.733, boot)
	if got := nativeRow(d, now)[15]; strings.Contains(got, "BOOTBY") || strings.Contains(phoneAlertBody(d, now), "BOOTBY") {
		t.Fatal("reported old restart on a working miner")
	}
}

func TestSafeHealthEvidencePreservesFullAvalonArrayAtBound(t *testing.T) {
	values := make([]string, 64)
	for i := range values {
		values[i] = "99999999"
	}
	raw := "ECHU[" + strings.Join(values, " ") + "]"
	if got := safeHealthEvidence(raw); got != raw {
		t.Fatal("valid 64-slot numeric evidence was truncated or rejected")
	}
	if got := safeHealthEvidence("ECHU[" + strings.Join(append(values, "1"), " ") + "]"); got != "" {
		t.Fatal("overlong slot array accepted")
	}
}

func TestHealthDetailShowsStructuredCodesAndTimeWithoutListLeak(t *testing.T) {
	now := time.Now()
	faults := []telemetry.Fault{
		{Code: "avalon-echu", Message: "矿机报告 ECHU 状态码（机型含义待确认）", Source: "Avalon MM ID0.ECHU", Confidence: "possible", Evidence: "ECHU[512 513 0]"},
		{Code: "2010", Message: "矿机报告错误码 2010（有效期未确认）", Source: "Whatsminer error-code", Confidence: "possible", Evidence: "code=2010; time=2026-10-30T11:22:33+08:00"},
		{Code: "board", Message: "算力板异常", Source: "REST miner.chains.status", Confidence: "reported", Evidence: "chain=2; state=failure"},
		{Code: "raw", Message: "原因未确认", Source: "legacy", Confidence: "possible", Evidence: "large-raw-payload-never-show"},
	}
	s := &store.Sample{At: now, Online: true, RealtimeTH: num(0), Health: telemetry.AssessHashrate(num(0), true, faults, "")}
	text := healthDetailText(s, now)
	for _, f := range faults[:3] {
		if !strings.Contains(text, "结构化依据："+f.Evidence) {
			t.Errorf("structured code/time absent: %q", f.Evidence)
		}
		if strings.Contains(healthSummary(s, now), f.Evidence) || strings.Contains(nativeRow(&Device{Latest: s}, now)[15], f.Evidence) {
			t.Fatal("structured detail leaked into compact UI")
		}
	}
	if strings.Contains(text, "large-raw-payload-never-show") {
		t.Fatal("arbitrary evidence displayed")
	}
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		t.Fatal("detail contains non-CRLF line breaks")
	}
}
