package main

import (
	"strings"
	"testing"
	"time"

	"minerwatch/internal/store"
	"minerwatch/internal/telemetry"
)

func TestNativeRowShowsSharesAndReportedPower(t *testing.T) {
	now := time.Now()
	d := &Device{Latest: &store.Sample{At: now, Online: true, Accepted: num(90), Rejected: num(10), PowerW: num(3000), PowerSource: "retired-source"}}
	row := nativeRow(d, now)
	if row[8] != "10.00" || row[9] != "90.00" || row[11] != "10.00" || row[powerColumn] != "3000" {
		t.Fatal(row)
	}
	if strings.Contains(strings.Join(row, " "), "retired-source") {
		t.Fatal(row)
	}
	d.Latest.Accepted = num(0)
	d.Latest.Rejected = num(0)
	d.Latest.PowerW = nil
	if row := nativeRow(d, now); row[8] != "—" || row[powerColumn] != "未上报" {
		t.Fatal("no submitted shares is not zero rejection")
	}
}

func TestNativeRowStaleAndOfflineHideEveryCurrentMetric(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name   string
		age    time.Duration
		online bool
		state  string
	}{
		{"stale", 11 * time.Minute, true, "待更新"},
		{"offline", time.Second, false, "读取失败"},
		{"future timestamp", -2 * time.Minute, true, "待更新"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Device{Latest: &store.Sample{At: now.Add(-tc.age), Online: tc.online, RealtimeTH: num(95), AverageTH: num(94), TempC: num(70), InletC: num(30), OutletC: num(40), PowerW: num(3000), PowerSource: "设备上报 PSU"}}
			row := nativeRow(d, now)
			if row[2] != tc.state {
				t.Fatalf("state=%q, want %q", row[2], tc.state)
			}
			for col := 3; col <= 9; col++ {
				if row[col] != "—" {
					t.Fatalf("column %q leaks an old live value: %q", nativeListColumns[col].Name, row[col])
				}
			}
			if row[11] != "—" || row[12] == "—" || row[powerColumn] != "—" {
				t.Fatalf("diagnostic source/time should be retained: %q", row)
			}
		})
	}
}

func TestNativeRowRespectsActualSamplingInterval(t *testing.T) {
	now := time.Now()
	d := &Device{Latest: &store.Sample{At: now.Add(-20 * time.Minute), IntervalSeconds: 1800, Online: true, RealtimeTH: num(95), PowerW: num(3000)}}
	if got := nativeRow(d, now); got[2] != "在线" || got[3] != "95.00" {
		t.Fatalf("30-minute sampling incorrectly stale: %q", got)
	}
	d.Latest.IntervalSeconds = 60
	if got := nativeRow(d, now); got[2] != "待更新" || got[3] != "—" {
		t.Fatalf("1-minute sampling incorrectly current: %q", got)
	}
}

func TestNativeRowMissingSharesDoesNotBecomeZero(t *testing.T) {
	now := time.Now()
	d := &Device{Latest: &store.Sample{At: now, Online: true, RealtimeTH: num(100)}}
	row := nativeRow(d, now)
	if row[8] != "—" || row[9] != "—" || row[11] != "—" {
		t.Fatalf("missing power shown incorrectly: %q", row)
	}
	d.Latest = nil
	row = nativeRow(d, now)
	if row[2] != "待更新" || row[8] != "—" || row[12] != "—" {
		t.Fatalf("unsampled device shown incorrectly: %q", row)
	}
	if got := nativeRow(nil, now); len(got) != len(nativeListColumns) {
		t.Fatal("nil device broke list layout")
	}
}

func TestMatchesDeviceIdentifyingFieldsOnlyAndCaseInsensitive(t *testing.T) {
	now := time.Now()
	d := &Device{IP: "192.168.1.50", Brand: "Antminer", Model: "S19", Label: "North Rack A", Protocol: "secret-protocol", Latest: &store.Sample{At: now, Online: true, PowerSource: "not-searchable", Error: "password-do-not-search"}}
	for _, q := range []string{"", "  ", "192.168.1.", "ANTMINER", "s19", "  north rack  "} {
		if !matchesDevice(d, q, 0, now) {
			t.Errorf("expected identifying search %q to match", q)
		}
	}
	for _, q := range []string{"Avalon", "secret-protocol", "not-searchable", "password-do-not-search", "Antminer S19"} {
		if matchesDevice(d, q, 0, now) {
			t.Errorf("unexpected search match for %q", q)
		}
	}
}

func TestMatchesDeviceStatusAndMissingSharesFilters(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		sample *store.Sample
		want   []bool
	}{
		{"online real zero", &store.Sample{At: now, Online: true, Accepted: num(1), Rejected: num(0)}, []bool{true, true, false, false}},
		{"online missing power", &store.Sample{At: now, Online: true}, []bool{true, true, false, true}},
		{"offline", &store.Sample{At: now, Online: false}, []bool{true, false, true, false}},
		{"stale", &store.Sample{At: now.Add(-11 * time.Minute), Online: true}, []bool{true, false, true, false}},
		{"not sampled", nil, []bool{true, false, true, false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &Device{IP: "192.168.1.50", Latest: tc.sample}
			for filter, want := range tc.want {
				if got := matchesDevice(d, "192.168", filter, now); got != want {
					t.Errorf("filter %d=%v, want %v", filter, got, want)
				}
				if matchesDevice(d, "no-such-model", filter, now) {
					t.Errorf("filter %d ignored search", filter)
				}
			}
		})
	}
	if matchesDevice(nil, "", 0, now) {
		t.Fatal("nil device matched")
	}
}

func TestNativeListColumnsHaveLegibleHeaders(t *testing.T) {
	want := []string{"IP", "品牌", "状态", "实时", "平均", "温度", "进水", "出水", "拒绝率", "接受份额", "24h", "拒绝份额", "最近采样", "温度明细", "各板PCB均温", "无算力原因"}
	for i, label := range want {
		if !strings.Contains(nativeListColumns[i].Name, label) || nativeListColumns[i].Width < 80 {
			t.Fatalf("column %d is missing or too narrow: %+v", i, nativeListColumns[i])
		}
	}
	for _, i := range []int{13, 14} {
		if nativeListColumns[i].Width != 350 {
			t.Fatalf("board average column %d width=%d, want 350", i, nativeListColumns[i].Width)
		}
	}
}

func TestNativeHashrateCauseNormalZeroMissingAndRecovery(t *testing.T) {
	now := time.Now()
	fault := telemetry.Fault{Code: "fan", Message: "风扇异常", Source: "summary.fan_status", Confidence: "reported"}
	d := &Device{Latest: &store.Sample{At: now, Online: true, RealtimeTH: num(0), AverageTH: num(400), Health: telemetry.AssessHashrate(num(0), true, []telemetry.Fault{fault}, "")}}
	row := nativeRow(d, now)
	if row[2] != "无算力" || row[3] != "0.00" || row[4] != "400.00" || !strings.Contains(row[15], "风扇异常") {
		t.Fatalf("zero realtime hidden by average or reason missing: %q", row)
	}
	if strings.Contains(row[15], fault.Source) || strings.Contains(row[15], "summary") {
		t.Fatal("diagnostic source leaked into main list")
	}
	if !matchesDevice(d, "", 4, now) {
		t.Fatal("zero hashrate filter missed current zero")
	}
	d.Latest.RealtimeTH = nil // Even a stored zero classification cannot override missing current data.
	row = nativeRow(d, now)
	if row[2] != "算力未报" || row[3] != "—" || !strings.Contains(row[15], "未上报") || matchesDevice(d, "", 4, now) {
		t.Fatalf("nil realtime treated as zero: %q", row)
	}
	d.Latest.RealtimeTH = num(390) // Recovery must clear stale persisted cause/evidence immediately.
	row = nativeRow(d, now)
	if row[2] != "在线" || row[15] != "" || matchesDevice(d, "", 4, now) {
		t.Fatalf("recovery retained prior cause: %q", row)
	}
}

func TestNativeCauseExpiredFailedAndUnobservedSamples(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name                   string
		sample                 *store.Sample
		status, reasonContains string
		zero                   bool
	}{
		{"legacy zero", &store.Sample{At: now, Online: true, RealtimeTH: num(0), AverageTH: num(100)}, "无算力", "原因未确认", true},
		{"timeout", &store.Sample{At: now, Online: false, Error: "context deadline exceeded"}, "读取失败", "超时", false},
		{"authentication", &store.Sample{At: now, Online: false, Error: "HTTP 401 authentication"}, "读取失败", "认证失败", false},
		{"stale zero", &store.Sample{At: now.Add(-11 * time.Minute), Online: true, RealtimeTH: num(0), Health: telemetry.HashrateHealth{State: "zero", Reason: "旧过热故障"}}, "待更新", "", false},
		{"stale failure", &store.Sample{At: now.Add(-11 * time.Minute), Online: false, Error: "timeout"}, "待更新", "", false},
		{"never sampled", nil, "待更新", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &Device{IP: "192.168.1.2", Latest: tc.sample}
			row := nativeRow(d, now)
			if row[2] != tc.status {
				t.Fatalf("state=%q want %q", row[2], tc.status)
			}
			if tc.reasonContains == "" && row[15] != "" {
				t.Fatalf("old reason presented as current: %q", row[15])
			}
			if tc.reasonContains != "" && !strings.Contains(row[15], tc.reasonContains) {
				t.Fatalf("missing reason: %q", row[15])
			}
			if strings.Contains(row[15], "断电") {
				t.Fatalf("read failure asserted loss of power: %q", row[15])
			}
			if got := matchesDevice(d, "", 4, now); got != tc.zero {
				t.Fatalf("zero filter=%v want %v", got, tc.zero)
			}
			if matchesDevice(d, "does not match", 4, now) {
				t.Fatal("zero filter bypassed identity search")
			}
		})
	}
}

func TestNativeColumnLayoutsKeepPrimaryMetrics(t *testing.T) {
	for _, simple := range []bool{false, true} {
		order, widths := nativeColumnOrder(simple), nativeColumnWidths(simple)
		if len(order) != len(nativeListColumns) || len(widths) != len(nativeListColumns) {
			t.Fatalf("layout size wrong: order %d widths %d columns %d", len(order), len(widths), len(nativeListColumns))
		}
		seen := map[int32]bool{}
		for _, index := range order {
			if index < 0 || int(index) >= len(nativeListColumns) || seen[index] {
				t.Fatalf("missing/duplicate column: %v", order)
			}
			seen[index] = true
		}
		if order[0] != backendColumn || order[1] != 0 || order[2] != 17 || order[3] != 2 || order[4] != 3 || order[5] != 4 || order[6] != powerColumn || order[7] != 5 || order[9] != 15 || widths[15] < 200 {
			t.Fatalf("primary metrics not leading: order %v widths %v", order, widths)
		}
		if widths[10] == 0 || widths[13] == 0 {
			t.Fatal("trend or chip temperature hidden")
		}
		if !simple && widths[14] == 0 {
			t.Fatal("PCB temperature missing from all-columns layout")
		}
	}
}

func TestNativeRowBoardAveragesPreserveKindsIDsAndMissingValues(t *testing.T) {
	now := time.Now()
	d := &Device{Latest: &store.Sample{At: now, Online: true, Boards: []telemetry.BoardTemperature{
		{ID: "0", Name: "算力板 A", Chip: telemetry.TemperatureSeries{AverageC: num(0), AverageKind: "reported"}, PCB: telemetry.TemperatureSeries{AverageC: num(43.5), AverageKind: "sensor_mean"}},
		{ID: "2", Name: "算力板 C", Chip: telemetry.TemperatureSeries{SensorsC: []*float64{num(61)}}},
		{Name: "来源未标板号", PCB: telemetry.TemperatureSeries{MinC: num(30), MaxC: num(50)}},
	}}}
	row := nativeRow(d, now)
	for _, want := range []string{"算力板 A 0.0", "算力板 C 61.0（测温）", "来源未标板号 —"} {
		if !strings.Contains(row[13], want) {
			t.Errorf("chip board summary lost %q: %q", want, row[13])
		}
	}
	if !strings.Contains(row[14], "算力板 A 43.5*") || !strings.Contains(row[14], "来源未标板号 —") {
		t.Fatalf("PCB summary lost its kind or invented mean: %q", row[14])
	}
	d.Latest.Boards = nil
	if row := nativeRow(d, now); row[13] != "未上报" || row[14] != "未上报" {
		t.Fatalf("empty board results must not invent values: %q", row[13:])
	}
}

func TestNativeRowBoardAveragesHiddenForUnavailableSamples(t *testing.T) {
	now := time.Now()
	s := &store.Sample{At: now, Online: true, Boards: []telemetry.BoardTemperature{{Name: "板1", Chip: telemetry.TemperatureSeries{AverageC: num(70), AverageKind: "reported"}, PCB: telemetry.TemperatureSeries{AverageC: num(40), AverageKind: "sensor_mean"}}}}
	d := &Device{Latest: s}
	for _, tc := range []struct {
		name   string
		age    time.Duration
		online bool
	}{
		{"stale", 11 * time.Minute, true}, {"offline", 0, false}, {"future", -2 * time.Minute, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.At, s.Online = now.Add(-tc.age), tc.online
			row := nativeRow(d, now)
			if row[13] != "—" || row[14] != "—" {
				t.Fatalf("old temperatures shown as current: %q", row[13:])
			}
		})
	}
	d.Latest = nil
	if row := nativeRow(d, now); row[13] != "—" || row[14] != "—" {
		t.Fatal("unsampled board summaries must be empty")
	}
}

func TestAvalonOverviewDoesNotKeepZeroOverBoardTemperatures(t *testing.T) {
	now := time.Now()
	boards := []telemetry.BoardTemperature{
		{ID: "avalon:stats:board:0", Chip: telemetry.TemperatureSeries{AverageC: num(66)}},
		{ID: "avalon:stats:board:1", Chip: telemetry.TemperatureSeries{AverageC: num(68)}},
		{ID: "avalon:stats:board:2", Chip: telemetry.TemperatureSeries{AverageC: num(63)}},
		{ID: "avalon:stats:board:3", Chip: telemetry.TemperatureSeries{AverageC: num(64)}},
	}
	d := &Device{Brand: "Avalon", Latest: &store.Sample{At: now, Online: true, TempC: num(0), Boards: boards}}
	if got := nativeRow(d, now)[5]; got != "68.00" {
		t.Fatal(got)
	}
	d.Latest.Boards[1].Chip.MaxC = num(82)
	if got := nativeRow(d, now)[5]; got != "82.00" {
		t.Fatal(got)
	}
	if *d.Latest.TempC != 0 {
		t.Fatal("view mutated recorded telemetry")
	}
	d.Latest.Boards = nil
	d.Latest.TempC = nil
	if got := nativeRow(d, now)[5]; got != "—" {
		t.Fatal(got)
	}
}

func TestWorkerColumnAndStaleLabel(t *testing.T) {
	now := time.Now()
	d := &Device{WorkerNames: []string{"farm.01", "backup.01"}, WorkerReadAt: now, Latest: &store.Sample{At: now, Online: true}}
	row := nativeRow(d, now)
	if len(row) != len(nativeListColumns) || row[17] != "farm.01 / backup.01" {
		t.Fatal(row)
	}
	d.Latest.At = now.Add(time.Second)
	if !strings.Contains(workerDisplay(d), "上次读取") {
		t.Fatal(workerDisplay(d))
	}
	if workerDisplay(&Device{}) != "未读取" {
		t.Fatal("missing worker name")
	}
}

func TestRuntimeColumnTracksFreshSamples(t *testing.T) {
	now := time.Now()
	sample := &store.Sample{Online: true, At: now, ElapsedSeconds: num(90061)}
	device := &Device{Latest: sample}
	if got := nativeRow(device, now)[18]; got != "1d 01h 01m 01s" {
		t.Fatal(got)
	}
	if got := nativeRow(device, now.Add(time.Hour))[18]; got != "—" {
		t.Fatal(got)
	}
	sample.ElapsedSeconds = nil
	if got := nativeRow(device, now)[18]; got != "未上报" {
		t.Fatal(got)
	}
}

func TestListVisibilitySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	a, err := newApp(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	const ip = "192.168.1.10"
	a.devices[ip] = &Device{IP: ip, Hidden: true}
	a.config.ColumnVisibility = map[string]bool{"实时 TH/s": false, "进水 °C": true}
	err = a.saveLocked()
	a.db.Close()
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := newApp(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	widths := configuredColumnWidths(reopened.config)
	if widths[3] != 0 || widths[6] <= 0 {
		t.Fatal("column choices were lost", widths)
	}
	d := reopened.devices[ip]
	now := time.Now()
	if listIncludesDevice(d, "", 0, now) || !listIncludesDevice(d, ip, hiddenMinersFilter, now) {
		t.Fatal("hidden device visibility was lost")
	}
	d.Hidden = false
	if !listIncludesDevice(d, "", 0, now) || listIncludesDevice(d, "", hiddenMinersFilter, now) {
		t.Fatal("restored device still hidden")
	}
}
