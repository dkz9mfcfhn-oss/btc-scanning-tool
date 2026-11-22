package miner

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticTemperatureSlotsPreserved(t *testing.T) {
	out := redactTelemetry([]byte(`{"STATS":[{"chain":[{"index":0,"temp_chip":[60,null,64,66]},{"index":1,"temp_chip":"70, ,74,76"},{"index":2,"temp_chip":"private-secret"}],"password":"private-password"}]}`))
	stats := out["STATS"].([]any)[0].(map[string]any)
	chains := stats["chain"].([]any)
	v := chains[0].(map[string]any)["temp_chip"].([]any)
	if len(v) != 4 || v[1] != nil {
		t.Fatal(v)
	}
	w := chains[1].(map[string]any)["temp_chip"].([]*float64)
	if len(w) != 4 || w[1] != nil {
		t.Fatal(w)
	}
	encoded, e := json.Marshal(out)
	if e != nil || strings.Contains(string(encoded), "private") {
		t.Fatalf("%s %v", encoded, e)
	}
}

func TestDiagnosticContinuesAfterFailedStats(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 7; i++ {
			c, e := listener.Accept()
			if e != nil {
				done <- e
				return
			}
			var q map[string]string
			e = json.NewDecoder(c).Decode(&q)
			if e == nil && q["command"] == "estats" {
				_, e = c.Write([]byte("'STATS':{" + flat1566 + "}\x00"))
			}
			// stats and other commands close without a body, simulating failed APIs.
			c.Close()
			if e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoints := []string{}
	failures := 0
	r := diagnoseCGMiner(ctx, listener.Addr().String(), func(path, method string, status int, data []byte, err error, elapsed time.Duration) {
		endpoints = append(endpoints, path)
		if err != nil {
			failures++
		}
	})
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if len(endpoints) != 7 || failures != 6 || len(r.Boards) != 4 || !hasBoardMeans(r) {
		t.Fatalf("%v failures=%d boards=%d", endpoints, failures, len(r.Boards))
	}
}
