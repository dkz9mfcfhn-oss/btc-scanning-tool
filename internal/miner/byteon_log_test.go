package miner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestByteonLogRequestRequiresExplicitZeroAndReusesSession(t *testing.T) {
	for _, tc := range []struct {
		name, metric string
		logCalls     int32
	}{
		{"zero", `,"hr_realtime":0`, 1},
		{"positive", `,"hr_realtime":95000`, 0},
		{"missing", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs, unlocks atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/info":
					w.Write(byteonInfo)
				case "/api/v1/summary":
					if r.Header.Get("Authorization") != "Bearer test-session" {
						w.WriteHeader(401)
						return
					}
					fmt.Fprintf(w, `{"miner":{"hr_average":95000%s}}`, tc.metric)
				case "/api/v1/unlock":
					unlocks.Add(1)
					fmt.Fprint(w, `{"token":"test-session"}`)
				case byteonMinerLogPath:
					logs.Add(1)
					if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-session" {
						t.Error("expected a read-only request with the existing session")
					}
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
					fmt.Fprintf(w, "%s Sweep error string = V:1 device-credential\n", time.Now().UTC().Format(time.RFC3339))
				default:
					t.Errorf("unexpected API: %s", r.URL.Path)
				}
			}))
			defer s.Close()
			c := restClient()
			defer c.CloseIdleConnections()
			r, err := readByteonTraced(context.Background(), c, s.URL, Credentials{Password: "test-password"}, func(path, method string, status int, data []byte, err error, elapsed time.Duration) {
				if path == byteonMinerLogPath && len(data) != 0 {
					t.Error("raw log reached diagnostics")
				}
			})
			if err != nil || logs.Load() != tc.logCalls || unlocks.Load() != 1 {
				t.Fatalf("unexpected read result %v, logs=%d, unlocks=%d", err, logs.Load(), unlocks.Load())
			}
			if tc.logCalls == 1 && (len(r.Faults) != 1 || r.Faults[0].Evidence != "code=SWEEP_V:1" || r.Faults[0].Confidence != "log") {
				t.Fatalf("missing safe clue: %+v", r.Faults)
			}
			if tc.logCalls == 0 && len(r.Faults) != 0 {
				t.Fatal(r.Faults)
			}
		})
	}
}

func TestByteonLogRejectsUnverifiableAgeAndPreviousRun(t *testing.T) {
	now := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	log := "Sweep error string = V:1\n2026-11-19T09:00:00Z ERROR_FAN_LOST\n2026-11-20T07:58:00Z over max temp\n2026-11-20T07:59:00Z start mining\n"
	if c := recentByteonLogClue(log, now); c.token != "" {
		t.Fatal(c)
	}
	if c := recentByteonLogClue(log+"2026-11-20T15:59:50+08:00 fail to read pic temp\n", now); c.token != "PIC_TEMP_READ_FAILED" {
		t.Fatal(c)
	}
}
