package miner

import (
	"context"
	"encoding/json"
	"math"
	"minerwatch/internal/telemetry"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const fixtureBootBy = "BOOTBY[0x11.00000000]"

func requireAvalonBootHistory(t *testing.T, r Reading) {
	t.Helper()
	for _, f := range r.Faults {
		if f.Code == "avalon-bootby" && f.Confidence == "historical" && f.Evidence == fixtureBootBy {
			return
		}
	}
	t.Fatalf("safe restart history missing: %+v", r.Faults)
}

func TestAvalonBootByTCPFormats(t *testing.T) {
	log := flat1566 + " " + fixtureBootBy
	for _, data := range []string{
		`{"STATS":[{"MM ID0":"` + log + `"}]}`,
		`STATUS=S|STATS=0,MM ID0=` + log + `|`,
		`'STATS':{` + log + `}`,
	} {
		r, err := ParseCGMiner([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		requireAvalonBootHistory(t, r)
		if h := telemetry.AssessHashrate(r.RealtimeTH, hasObservation(r), r.Faults, ""); h.State != "normal" || h.Reason != "" {
			t.Fatalf("historical code marked the working miner faulty: %+v", h)
		}
		encoded, err := json.Marshal(r.Faults)
		if err != nil || strings.Contains(string(encoded), "private-test-marker") {
			t.Fatalf("diagnosis included another raw device field: %s, %v", encoded, err)
		}
	}
}

func TestAvalonFMSKeepsBootByEvenIfTemperaturesOmitted(t *testing.T) {
	for _, log := range []string{
		"Ver[1566HA] " + fixtureBootBy,
		"Ver[1566HA] " + fixtureBootBy + " TMax[82]",
	} {
		fresh, err := parseAvalonFMS([]byte(log))
		got := applyAvalonFMS(Reading{Brand: "Avalon", Model: "Avalon 1566HA", RealtimeTH: ptr(0)}, fresh, err)
		requireAvalonBootHistory(t, got)
		if got.StatusKnown || got.RealtimeTH == nil || *got.RealtimeTH != 0 {
			t.Fatalf("restart history changed current status: %+v", got)
		}
	}
}

func TestAvalonBootByDiagnosisOnlyHTTP(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/login.cgi":
			w.Write([]byte(`{"success":true}`))
		case "/get_minerinfo.cgi":
			w.Write([]byte(`{"hwtype":"Avalon 1566HA"}`))
		case "/updatecglog.cgi":
			w.Write([]byte("'STATS':{" + fixtureBootBy + " DNA[private-test-marker]}"))
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	current := Reading{Brand: "Avalon", Model: "Avalon 1566HA", TempC: ptr(70)}
	for _, rate := range []*float64{nil, ptr(300), ptr(-1), ptr(math.Inf(1)), ptr(math.NaN())} {
		current.RealtimeTH = rate
		supplementAvalonHealth(context.Background(), server.URL, "AvalonDevice", current, nil)
	}
	if len(requests) != 0 {
		t.Fatalf("working miner received additional HTTP probes: %v", requests)
	}
	current.RealtimeTH = ptr(0)
	got := supplementAvalonHealth(context.Background(), server.URL, "AvalonDevice", current, nil)
	want := []string{"POST /login.cgi", "GET /get_minerinfo.cgi", "GET /updatecglog.cgi"}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("unexpected read flow: %v", requests)
	}
	requireAvalonBootHistory(t, got)
	supplementAvalonHealth(context.Background(), server.URL, "AvalonDevice", got, nil)
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("firmware with inline BOOTBY received additional HTTP probes: %v", requests)
	}
	if got.StatusKnown || got.TempC == nil || *got.TempC != 70 || got.RealtimeTH == nil || *got.RealtimeTH != 0 {
		t.Fatalf("log changed current telemetry: %+v", got)
	}
}
