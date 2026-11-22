package miner

import (
	"context"
	"minerwatch/internal/telemetry"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAntminerCurrentLogKeepsOnlyRecentRecognizedClue(t *testing.T) {
	for _, test := range []struct{ name, log, token string }{
		{"fan", "2026-11-20 08:30:00 ERROR_FAN_LOST\n", "ERROR_FAN_LOST"},
		{"recent code wins", "2026-11-20 08:30:00 ERROR_FAN_LOST\n[2026-11-20T08:31:01Z] [ERROR] Sweep error string = V:1\n", "SWEEP_V:1"},
		{"temperature", "Sweep error string = J:6", "SWEEP_J:6"},
		{"PIC", "fail to read pic temp", "PIC_TEMP_READ_FAILED"},
		{"board shutdown", "Chain 0 only find 0 asic,will power off hash board 0", "ASIC_NOT_FOUND"},
		{"new boot", "ERROR_FAN_LOST\n<6>[ 0.000 ] Linux version 4.9\nstarted init", ""},
		{"new mining run", "ERROR_FAN_LOST\n2026-11-20 08:30:00 [INFO] start mining mode", ""},
		{"new run failure", "ERROR_FAN_LOST\n2026-11-20 08:30:00 start mining\n2026-11-20 08:30:05 over max temp, pcb temp 95", "OVER_MAX_TEMP"},
		{"unknown code", "Sweep error string = V:10", ""},
		{"login page", "<!doctype html><title>Log in</title>ERROR_FAN_LOST", ""},
		{"arbitrary structured response", `{"error":"ERROR_FAN_LOST"}`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := lastAntminerLogClue([]byte(test.log)).token; got != test.token {
				t.Fatalf("got %q, want %q", got, test.token)
			}
		})
	}
}

func TestAntminerLogReadIsConditionalAndRedacted(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != antminerLogPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.Add(1)
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="mine", nonce="abc", qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !strings.Contains(r.Header.Get("Authorization"), `uri="/cgi-bin/log.cgi"`) {
			t.Error("Digest request not bound to current log path")
		}
		_, _ = w.Write([]byte("pool-user=fake-secret\nSweep error string = P:1, unrelated log text=fake-secret\n"))
	}))
	defer s.Close()

	for _, unneeded := range []Reading{{Brand: "Antminer", RealtimeTH: ptr(100)}, {Brand: "Avalon", RealtimeTH: ptr(0)}, {Brand: "Antminer", Protocol: "Byteon REST API", RealtimeTH: ptr(0)}, {Brand: "Antminer", StatusKnown: true}, {Brand: "Antminer", RealtimeTH: ptr(-1)}} {
		if r := addAntminerLogEvidence(context.Background(), s.URL, Credentials{}, unneeded, nil); len(r.Faults) != 0 || calls.Load() != 0 {
			t.Fatalf("unnecessary log fetch: %+v", r)
		}
	}
	traceCalled := false
	r := addAntminerLogEvidence(context.Background(), s.URL, Credentials{"admin", "test"}, Reading{Brand: "Antminer", RealtimeTH: ptr(0)}, func(path, method string, code int, data []byte, err error, elapsed time.Duration) {
		traceCalled = true
		if path != antminerLogPath || code != 200 || len(data) > 0 || err != nil {
			t.Errorf("diagnostic trace leaked raw data or lost read result: %q %d %d %v", path, code, len(data), err)
		}
	})
	if !traceCalled || calls.Load() != 2 || len(r.Faults) != 1 {
		t.Fatalf("log fallback did not collect a single clue: %+v", r)
	}
	f := r.Faults[0]
	if f.Evidence != "code=SWEEP_P:1" || f.Confidence != "log" || strings.Contains(f.Message, "fake-secret") || !explicitZeroHashrate(r) {
		t.Fatalf("fault must be redacted and telemetry must be preserved: %+v", r)
	}
	if h := telemetry.AssessHashrate(r.RealtimeTH, true, r.Faults, ""); h.State != "zero" || !strings.Contains(h.Reason, "高温保护") {
		t.Fatalf("zero rate state or current log clue lost: %+v", h)
	}
}
