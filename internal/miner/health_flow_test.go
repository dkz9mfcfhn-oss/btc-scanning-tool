package miner

import (
	"context"
	"minerwatch/internal/telemetry"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStockZeroHashrateReadsSummaryReasonEvenWithAllMetrics(t *testing.T) {
	summaryRead, poolsRead := false, false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "GET" {
			t.Error("health collection attempted mutation", req.Method)
		}
		switch req.URL.Path {
		case "/cgi-bin/stats.cgi":
			w.Write([]byte(`{"STATS":[{"Type":"Antminer S19","rate_unit":"TH/s","rate_5s":0,"rate_avg":90,"watt":80,"chain":[{"index":0,"temp_chip":[30,32]}]}]}`))
		case "/cgi-bin/summary.cgi":
			summaryRead = true
			w.Write([]byte(`{"SUMMARY":[{"status":[{"type":"fans","status":"e","code":200,"msg":"fan lost"}]}]}`))
		case "/cgi-bin/pools.cgi":
			poolsRead = true
			w.Write([]byte(`{"POOLS":[{"status":"Alive"},{"status":"Dead"}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	r, failed, notes := readAntminerHTTP(context.Background(), s.URL, Credentials{"admin", "admin"})
	h := telemetry.AssessHashrate(r.RealtimeTH, !failed, r.Faults, "")
	if failed || !summaryRead || !poolsRead || h.State != "zero" || !strings.Contains(h.Reason, "风扇") {
		t.Fatal(r, h, failed, notes, summaryRead, poolsRead)
	}
	value(t, r.AverageTH, 90)
	for _, f := range r.Faults {
		if f.Code == "pool" {
			t.Fatal("backup dead became root cause")
		}
	}
}

func TestHealthParsersKeepStoppedStateWithoutInventingZero(t *testing.T) {
	byteon, e := ParseByteon([]byte(`{"miner":{"miner_status":{"miner_state":"stopped"}}}`), []byte(`{"fw_name":"Byteon","hr_measure":"TH/s"}`))
	if e != nil {
		t.Fatal(e)
	}
	ant, e := ParseAntminer([]byte(`{"STATS":[{"miner-mode":1}]}`))
	if e != nil {
		t.Fatal(e)
	}
	whats, e := ParseWhatsminerV3([]byte(`{"code":0,"msg":{"summary":{}}}`), []byte(`{"code":0,"msg":{"miner":{"type":"M63S","working":false}}}`))
	if e != nil {
		t.Fatal(e)
	}
	avalon, e := ParseCGMiner([]byte(`STATS=0,MM ID0=SYSTEMSTATU[Work: In Idle, Hash Board: 0] ECMM[1]|`))
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range []Reading{byteon, ant, whats, avalon} {
		if !r.StatusKnown || r.RealtimeTH != nil || len(r.Faults) == 0 {
			t.Fatal("lost known state or fabricated zero", r)
		}
		h := telemetry.AssessHashrate(r.RealtimeTH, true, r.Faults, "")
		if h.State != "missing" || !strings.Contains(h.Reason, "未上报") {
			t.Fatal(h)
		}
	}
}

func TestHealthFullParsersRejectAPIErrorsAndRetainCodes(t *testing.T) {
	r, e := ParseWhatsminerV3([]byte(`{"code":-4,"msg":{"miner":{"working":false}}}`), nil)
	if e == nil || len(r.Faults) != 0 {
		t.Fatal("API denial became device fault", r, e)
	}
	r, e = ParseWhatsminerV3([]byte(`{"code":0,"msg":{"summary":{"hash-realtime":0,"hash-average":399},"pools":[{"status":"Alive"}]}}`), []byte(`{"code":0,"msg":{"error-code":[{"531":"2026-10-29 20:10:00","reason":"Slot1 not found."}]}}`))
	if e != nil || len(r.Faults) != 1 || r.Faults[0].Code != "531" || !strings.Contains(r.Faults[0].Evidence, "2026-10-29") {
		t.Fatal(r, e)
	}
	if h := telemetry.AssessHashrate(r.RealtimeTH, true, r.Faults, ""); h.State != "zero" || !strings.Contains(h.Reason, "待确认") {
		t.Fatal(h)
	}
	r, e = ParseCGMiner([]byte(`POOLS=0,Status=Dead|POOLS=1,Status=Dead|`))
	if e != nil || len(r.Faults) != 1 || r.Faults[0].Code != "pool" {
		t.Fatal("legacy pool diagnostics lost", r, e)
	}
}

func TestIdentifiedFirmwareTelemetryWithoutRateIsKept(t *testing.T) {
	r, e := ParseByteon([]byte(`{"miner":{"power_consumption":73}}`), []byte(`{"fw_name":"Byteon"}`))
	if e != nil || !hasObservation(r) || r.RealtimeTH != nil {
		t.Fatal(r, e)
	}
	if h := telemetry.AssessHashrate(r.RealtimeTH, hasObservation(r), r.Faults, ""); h.State != "missing" {
		t.Fatal(h)
	}
	for _, r := range []Reading{{Brand: "Antminer"}, {FirmwareKnown: true}, {PowerW: ptr(100)}} {
		if hasObservation(r) {
			t.Fatal("identity or arbitrary field alone counted as live miner", r)
		}
	}
}
