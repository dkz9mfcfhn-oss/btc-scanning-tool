package miner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestByteonProtectedInfoUnlockRereadsUnits(t *testing.T) {
	unlocks, authedInfo := 0, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><title>倍腾 OS - Miner</title></html>`))
		case "/api/v1/unlock":
			if r.Method != "POST" {
				t.Error("expected authentication POST")
			}
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["pw"] != "admin" {
				t.Error("incorrect unlock body")
			}
			unlocks++
			w.Write([]byte(`{"token":"fixture-token"}`))
		case "/api/v1/info", "/api/v1/summary":
			if r.Header.Get("Authorization") != "Bearer fixture-token" {
				w.WriteHeader(401)
				return
			}
			if strings.HasSuffix(r.URL.Path, "info") {
				authedInfo++
				w.Write(byteonInfo)
			} else {
				w.Write([]byte(`{"miner":{"hr_realtime":95000,"hr_average":94000,"power_consumption":3187}}`))
			}
		default:
			t.Error("unexpected route", r.URL.Path)
		}
	}))
	defer s.Close()
	r, err := readByteon(context.Background(), s.Client(), s.URL, Credentials{Password: "admin"})
	if err != nil || unlocks != 1 || authedInfo != 1 {
		t.Fatal(r, err, unlocks, authedInfo)
	}
	value(t, r.PowerW, 3187)
	value(t, r.RealtimeTH, 95)
}

func TestByteonEnvelopeAndUnknownUnitsKeepPower(t *testing.T) {
	for _, info := range []string{
		`{"data":{"fw_name":"Byteon","model":"S19","hr_measure":"GH/s"}}`,
		`{"result":{"firmware":{"name":"倍腾 OS"},"model":"S19","hr_measure":"unknown"}}`,
	} {
		r, err := ParseByteon([]byte(`{"data":{"miner":{"hr_realtime":95000,"power_consumption":3180}}}`), []byte(info))
		if err != nil {
			t.Fatal(err)
		}
		value(t, r.PowerW, 3180)
		if strings.Contains(info, "unknown") {
			if r.RealtimeTH != nil {
				t.Fatal("guessed hashrate unit")
			}
		} else {
			value(t, r.RealtimeTH, 95)
		}
	}
}

func TestByteonFailedUnlockIsNotMissingPower(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/info" {
			w.Write(byteonInfo)
			return
		}
		w.WriteHeader(403)
	}))
	defer s.Close()
	_, err := readByteon(context.Background(), s.Client(), s.URL, Credentials{Password: "wrong"})
	if err == nil || !strings.Contains(err.Error(), "解锁失败") {
		t.Fatal(err)
	}
}

func TestStockNoLongerFetchesOptionalPower(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/stats.cgi":
			w.Write([]byte(`{"STATS":[{"rate_unit":"TH/s","rate_5s":95,"rate_avg":94,"temp_chip":[70]}]}`))
		case "/cgi-bin/summary.cgi":
			w.Write([]byte(`{"SUMMARY":[{"watt":3160}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	stock, failed, _ := readAntminerHTTP(context.Background(), s.URL, Credentials{"admin", "admin"})
	if failed {
		t.Fatal("missing optional route treated as failure")
	}
	rest, _ := ParseByteon([]byte(`{"miner":{"hr_realtime":95000}}`), byteonInfo)
	result := merge(rest, stock)
	if result.PowerW != nil {
		t.Fatal("optional power fetch should be retired")
	}
	value(t, result.RealtimeTH, 95)
	if !strings.Contains(result.Protocol, "Byteon") {
		t.Fatal("lost REST identity")
	}
}

func TestStockMissingPowerVersusAuthenticationFailure(t *testing.T) {
	for _, authenticated := range []bool{true, false} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !authenticated {
				w.WriteHeader(403)
				return
			}
			if strings.HasSuffix(r.URL.Path, "stats.cgi") {
				w.Write([]byte(`{"STATS":[{"rate_unit":"TH/s","rate_5s":95,"power_limit":3250}]}`))
				return
			}
			w.WriteHeader(404)
		}))
		r, failed, notes := readAntminerHTTP(context.Background(), s.URL, Credentials{"admin", "admin"})
		s.Close()
		if r.PowerW != nil || failed == authenticated {
			t.Fatal(r, failed, notes)
		}
	}
}

func TestDiagnosticRedactionRetainsMetricsOnly(t *testing.T) {
	raw := []byte(`{"token":"SECRET_TOKEN","password":"SECRET_PASSWORD","miner":{"power_consumption":3180,"hr_realtime":95000,"power":{"actual":3180,"token":"NESTED_TOKEN"},"pools":[{"user":"PRIVATE_WALLET","url":"stratum+tcp://PRIVATE_HOST"}],"hashrate_user":"SECRET_USER","chip_temp":{"max":76},"config":{"secret":"HIDDEN_CONFIG"}},"info":{"fw_name":"Byteon private-note","fw_version":"1.2.6-private-id","model":"Antminer S19 OWNER_NAME","serial":"PRIVATE_SERIAL"},"STATS":[{"MM ID0":"GHSmm[460000] PS[0 1200 8800] User[PRIVATE_USER]"}]}`)
	sanitized := redactTelemetry(raw)
	b, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SECRET", "PRIVATE", "HIDDEN", "OWNER", "private-id", "private-note"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("leaked %s: %s", secret, b)
		}
	}
	for _, metric := range []string{"3180", "95000", "76", "8800", "Byteon", "S19", "1.2.6"} {
		if !strings.Contains(string(b), metric) {
			t.Fatalf("lost %s: %s", metric, b)
		}
	}
	if got := diagnosticError(context.DeadlineExceeded); got != "timeout" {
		t.Fatal(got)
	}
}

func TestDiagnosticTraceOmitsUnlockSecret(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/info":
			w.Write(byteonInfo)
		case "/api/v1/unlock":
			w.Write([]byte(`{"token":"PRIVATE_TOKEN"}`))
		case "/api/v1/summary":
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"miner":{"power_consumption":3200},"user":"PRIVATE_USER"}`))
		}
	}))
	defer s.Close()
	traceCount := 0
	_, err := readByteonTraced(context.Background(), s.Client(), s.URL, Credentials{Password: "PRIVATE_PASSWORD"}, func(path, method string, status int, data []byte, err error, elapsed time.Duration) {
		traceCount++
		if path == "/api/v1/unlock" && len(data) > 0 {
			t.Error("raw unlock response exposed")
		}
		safe, _ := json.Marshal(redactTelemetry(data))
		if strings.Contains(string(safe), "PRIVATE") {
			t.Error("secret in diagnostic", string(safe))
		}
	})
	if err != nil || traceCount != 4 {
		t.Fatal(err, traceCount)
	}
}
