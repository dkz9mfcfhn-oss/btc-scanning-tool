package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"minerwatch/internal/miner"
	"minerwatch/internal/store"
	"minerwatch/internal/telemetry"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTemperatureCapturePersistenceAndCSV(t *testing.T) {
	a, err := newApp(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	boards, sensors := demoTemperatures(80)
	boards[0].Name = `=HYPERLINK("https://example.invalid")`
	boards[0].Chip.Source = "fixture/chip\nsource"
	boards[1].Chip.SensorsC[1] = nil
	sensors = append(sensors, telemetry.Sensor{Name: "=formula-like-name", ValueC: nil, Source: "fixture/missing"})
	expectedBoards, expectedSensors := telemetry.CloneBoards(boards), telemetry.CloneSensors(sensors)
	a.discover = func(context.Context, string) bool { return true }
	a.probe = func(context.Context, string, miner.Credentials) (miner.Reading, error) {
		return miner.Reading{Brand: "Antminer", Model: "+test-model", Protocol: "fixture", RealtimeTH: num(95), TempC: num(80), Boards: boards, Temperatures: sensors}, nil
	}
	ip := "192.168.1.50"
	a.runJob(context.Background(), []string{ip}, a.config, miner.Credentials{}, "scan")
	*boards[0].Chip.AverageC = 199
	*sensors[0].ValueC = 198
	latest := a.devices[ip].Latest
	if latest == nil || !reflect.DeepEqual(latest.Boards, expectedBoards) || !reflect.DeepEqual(latest.Temperatures, expectedSensors) {
		t.Fatal("collector temperature data was lost or aliased")
	}
	stored := a.db.History(ip, time.Now().Add(-time.Hour))
	if len(stored) != 1 || !reflect.DeepEqual(stored[0].Boards, expectedBoards) || !reflect.DeepEqual(stored[0].Temperatures, expectedSensors) {
		t.Fatal("complete temperatures were not persisted")
	}
	w := httptest.NewRecorder()
	a.export(w, httptest.NewRequest("GET", "/api/export?ip="+ip, nil))
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || len(rows[0]) != 18 || len(rows[1]) != 18 || rows[0][12] != "每板温度(JSON)" || rows[0][13] != "其他温度(JSON)" {
		t.Fatalf("CSV temperature columns missing or old columns moved: %+v", rows)
	}
	if rows[1][2] != "'+test-model" {
		t.Fatal("CSV formula guard changed", rows[1][2])
	}
	if !strings.HasPrefix(rows[1][12], "[") || !strings.HasPrefix(rows[1][13], "[") {
		t.Fatal("nested untrusted strings became spreadsheet formulas")
	}
	var csvBoards []telemetry.BoardTemperature
	var csvSensors []telemetry.Sensor
	if err := json.Unmarshal([]byte(rows[1][12]), &csvBoards); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(rows[1][13]), &csvSensors); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(csvBoards, expectedBoards) || !reflect.DeepEqual(csvSensors, expectedSensors) {
		t.Fatal("CSV lost per-board means, sensor slots, sources, or average provenance")
	}
	b, err := newApp(a.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.db.Close()
	if !reflect.DeepEqual(b.devices[ip].Latest.Boards, expectedBoards) || !reflect.DeepEqual(b.devices[ip].Latest.Temperatures, expectedSensors) {
		t.Fatal("restart lost detailed temperatures")
	}
}

func TestDemoTemperaturesAreClearlyMarkedAndPerBoard(t *testing.T) {
	boards, sensors := demoTemperatures(80)
	if len(boards) != 3 || len(sensors) == 0 {
		t.Fatal("demo missing boards or ambient readings")
	}
	for _, b := range boards {
		if b.Chip.AverageC == nil || b.PCB.AverageC == nil || !strings.Contains(b.Name, "演示") || !strings.Contains(b.Chip.Source, "演示") || !strings.Contains(b.PCB.Source, "演示") {
			t.Fatal("demo readings must include both means and provenance", b)
		}
	}
	if sensors[0].ValueC == nil || !strings.Contains(sensors[0].Source, "演示") {
		t.Fatal("ambient demo not explicitly marked")
	}
}

func TestHashrateHealthCaptureAndCSV(t *testing.T) {
	a, err := newApp(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	faults := []telemetry.Fault{{Code: "starting", Message: "=启动中", Source: "fixture/status\nstartup", Evidence: "state=starting", Confidence: "reported"}}
	wantFaults := append([]telemetry.Fault(nil), faults...)
	ipNormal, ipZero, ipMissing, ipOffline, ipLegacy := "192.168.1.60", "192.168.1.61", "192.168.1.62", "192.168.1.63", "192.168.1.64"
	a.devices[ipOffline] = &Device{IP: ipOffline, Model: "fixture"}
	a.probe = func(_ context.Context, ip string, _ miner.Credentials) (miner.Reading, error) {
		r := miner.Reading{Brand: "Antminer", Model: "fixture", Protocol: "fixture"}
		switch ip {
		case ipNormal:
			r.RealtimeTH = num(95)
		case ipZero:
			r.RealtimeTH, r.Faults = num(0), faults
		case ipMissing:
			r.AverageTH = num(95)
		case ipOffline:
			return r, errors.New("dial timeout")
		}
		return r, nil
	}
	a.runJob(context.Background(), []string{ipNormal, ipZero, ipMissing, ipOffline}, a.config, miner.Credentials{}, "poll")
	faults[0].Evidence = "changed after probe"
	for ip, state := range map[string]string{ipNormal: "normal", ipZero: "zero", ipMissing: "missing", ipOffline: "unavailable"} {
		if d := a.devices[ip]; d == nil || d.Latest == nil || d.Latest.Health.State != state {
			t.Fatalf("%s: expected persisted %s health", ip, state)
		}
	}
	if !reflect.DeepEqual(a.devices[ipZero].Latest.Health.Evidence, wantFaults) {
		t.Fatal("capture health aliases collector fault evidence")
	}
	// A pre-2.6 record has no health block. Export can infer zero, but cannot
	// manufacture the device's historical reason or fault evidence.
	a.devices[ipLegacy] = &Device{IP: ipLegacy, Model: "legacy"}
	if err := a.db.Append([]store.Record{{IP: ipLegacy, Sample: store.Sample{At: time.Now().UTC(), Online: true, RealtimeTH: num(0)}}}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.export(w, httptest.NewRequest("GET", "/api/export", nil))
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 || len(rows[0]) != 18 || rows[0][14] != "算力状态" || rows[0][15] != "无算力原因" || rows[0][16] != "依据(JSON)" {
		t.Fatalf("health CSV schema incorrect: %+v", rows)
	}
	byIP := map[string][]string{}
	for _, row := range rows[1:] {
		if len(row) != 18 {
			t.Fatalf("wrong CSV width: %d", len(row))
		}
		byIP[row[1]] = row
	}
	if byIP[ipNormal][14] != "normal" || byIP[ipZero][14] != "zero" || byIP[ipMissing][14] != "missing" || byIP[ipOffline][14] != "unavailable" {
		t.Fatal("export confused normal, zero, missing, or unavailable data")
	}
	if !strings.HasPrefix(byIP[ipZero][15], "'=启动中") {
		t.Fatal("formula-like device reason was not guarded", byIP[ipZero][15])
	}
	var evidence []telemetry.Fault
	if err := json.Unmarshal([]byte(byIP[ipZero][16]), &evidence); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(evidence, wantFaults) {
		t.Fatal("export lost fault code, source, confidence, or evidence")
	}
	if !strings.Contains(byIP[ipOffline][15], "超时") || !strings.Contains(byIP[ipMissing][15], "未上报") {
		t.Fatal("export lost the distinction between read failure and an omitted hashrate")
	}
	if byIP[ipLegacy][14] != "zero" || !strings.Contains(byIP[ipLegacy][15], "原因未确认") || byIP[ipLegacy][16] != "null" {
		t.Fatal("legacy health export invented fault evidence", byIP[ipLegacy])
	}
}

func TestDemoHashrateHealth(t *testing.T) {
	a, err := newApp(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	for ip, d := range a.devices {
		if d.Latest == nil || d.Latest.Health.State == "" {
			t.Fatalf("demo %s lacks sample health", ip)
		}
	}
	s := a.devices["192.168.10.112"].Latest
	if !s.Online || s.RealtimeTH == nil || *s.RealtimeTH != 0 || s.Health.State != "zero" || !strings.Contains(s.Health.Reason, "演示") || len(s.Health.Evidence) != 1 || s.Health.Evidence[0].Code != "starting" || !strings.Contains(s.Health.Evidence[0].Source, "演示") {
		t.Fatal("demo starting/zero case missing or not clearly marked", s)
	}
	if a.devices["192.168.10.101"].Latest.Health.State != "normal" || a.devices["192.168.10.105"].Latest.Health.State != "unavailable" {
		t.Fatal("demo lacks normal/offline distinction")
	}
}

func TestTotalsExcludeOfflineMissingStale(t *testing.T) {
	now := time.Now()
	ds := []*Device{{Latest: &store.Sample{At: now, Online: true, RealtimeTH: num(0), PowerW: nil}}, {Latest: &store.Sample{At: now.Add(-11 * time.Minute), Online: true, RealtimeTH: num(100)}}, {Latest: &store.Sample{At: now, Online: false, RealtimeTH: num(100)}}}
	v := totals(ds, now)
	if v.RealtimeTH != 0 || v.RealtimeReported != 1 || v.Stale != 1 || v.Offline != 1 || v.RejectReported != 0 {
		t.Fatalf("%+v", v)
	}
}
func TestFleetManualSamplesNotDoubleCounted(t *testing.T) {
	at := time.Now().Truncate(interval)
	rs := []store.Record{{IP: "a", Sample: store.Sample{At: at, Online: true, RealtimeTH: num(50)}}, {IP: "a", Sample: store.Sample{At: at.Add(time.Second), Online: true, RealtimeTH: num(60)}}}
	v := fleetHistory(rs, map[string]*Device{"a": {}})
	if len(v) != 1 || *v[0].RealtimeTH != 60 || v[0].Reported != 1 {
		t.Fatal(v)
	}
}
func TestGuardHostAndOrigin(t *testing.T) {
	h := guard("127.0.0.1:17828", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, host := range []string{"evil.test", "127.0.0.1:17828"} {
		r := httptest.NewRequest("POST", "http://"+host+"/api/shutdown", strings.NewReader("{}"))
		r.Header.Set("Origin", "http://evil.test")
		r.Header.Set("X-MinerWatch", "1")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
}
func TestConfigPasswordNotPersisted(t *testing.T) {
	a, err := newApp(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	a.password = "test-secret"
	a.config.Ranges = "192.168.1.1"
	if err = a.saveLocked(); err != nil {
		t.Fatal(err)
	}
	b, err := newApp(a.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.db.Close()
	if b.password == "test-secret" {
		t.Fatal("password persisted")
	}
}

func TestIntervalPersistAndDeadline(t *testing.T) {
	a, e := newApp(t.TempDir(), false)
	if e != nil {
		t.Fatal(e)
	}
	defer a.db.Close()
	before := time.Now()
	if e = a.setInterval(1800); e != nil {
		t.Fatal(e)
	}
	if a.nextPoll.Sub(before) < 1799*time.Second {
		t.Fatal(a.nextPoll)
	}
	b, e := newApp(a.dir, false)
	if e != nil {
		t.Fatal(e)
	}
	defer b.db.Close()
	if b.config.pollInterval() != 30*time.Minute {
		t.Fatal(b.config)
	}
	s := store.Sample{At: time.Now().Add(-20 * time.Minute), Online: true, IntervalSeconds: 1800}
	if !fresh(&s, time.Now()) {
		t.Fatal("30min sample falsely stale")
	}
	s.IntervalSeconds = 60
	if fresh(&s, time.Now()) {
		t.Fatal("1min sample should be stale")
	}
}
func TestIntervalRejectInvalid(t *testing.T) {
	a, e := newApp(t.TempDir(), false)
	if e != nil {
		t.Fatal(e)
	}
	defer a.db.Close()
	for _, sec := range []int{-1, 1, 29, 1801} {
		if e = a.setInterval(sec); e == nil {
			t.Fatal(sec)
		}
	}
}

func TestDiagnosticBlocksOverlappingJob(t *testing.T) {
	a, err := newApp(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	a.diagnosticRunning = true
	if err := a.startJob("poll"); err == nil || !strings.Contains(err.Error(), "采集诊断") {
		t.Fatal("diagnostic overlapped polling", err)
	}
	a.diagnosticRunning = false
	if err := a.startJob("poll"); err != nil {
		t.Fatal("polling stayed blocked", err)
	}
}
