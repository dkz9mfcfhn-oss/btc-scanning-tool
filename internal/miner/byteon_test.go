package miner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var byteonInfo = []byte(`{"fw_name":"Byteon","fw_version":"1.2.6","miner":"Antminer S19","hr_measure":"GH/s"}`)

func TestByteonNestedPowerAndUnits(t *testing.T) {
	r, e := ParseByteon([]byte(`{"miner":{"hr_realtime":95000,"hr_average":94000,"power_consumption":3215,"chip_temp":{"min":60,"max":76}}}`), byteonInfo)
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.PowerW, 3215)
	value(t, r.RealtimeTH, 95)
	value(t, r.AverageTH, 94)
	value(t, r.TempC, 76)
}
func TestByteonPowerConsumptionZeroAndAmbiguousFields(t *testing.T) {
	r, e := ParseByteon([]byte(`{"miner":{"hr_realtime":0,"power_usage":0,"power_limit":9999}}`), byteonInfo)
	if e != nil {
		t.Fatal(e)
	}
	if e != nil || r.PowerW != nil {
		t.Fatal(r, e)
	}
	r, e = ParseByteon([]byte(`{"miner":{"hr_realtime":0,"power_consumption":0,"power_limit":3250}}`), byteonInfo)
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.PowerW, 0)
}
func TestByteonUnlockFlow(t *testing.T) {
	unlocks := 0
	gets := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/info":
			w.Write(byteonInfo)
		case "/api/v1/unlock":
			if r.Method != "POST" {
				t.Error("method")
			}
			var p map[string]string
			json.NewDecoder(r.Body).Decode(&p)
			if p["pw"] != "admin" {
				t.Error("password payload")
			}
			unlocks++
			w.Write([]byte(`{"token":"fixture-session"}`))
		case "/api/v1/summary":
			gets++
			if r.Header.Get("Authorization") != "Bearer fixture-session" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"miner":{"power_consumption":3200,"hr_realtime":94000}}`))
		default:
			t.Error("unexpected command", r.URL.Path)
		}
	}))
	defer s.Close()
	r, e := readByteon(context.Background(), s.Client(), s.URL, Credentials{Password: "admin"})
	if e != nil || unlocks != 1 || gets != 2 {
		t.Fatal(r, e, unlocks, gets)
	}
	value(t, r.PowerW, 3200)
}
func TestByteonPublicSummaryNoUnlock(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatal("must not unlock public summary")
		}
		if strings.HasSuffix(r.URL.Path, "info") {
			w.Write(byteonInfo)
		} else {
			w.Write([]byte(`{"miner":{"power_consumption":3210}}`))
		}
	}))
	defer s.Close()
	r, e := readByteon(context.Background(), s.Client(), s.URL, Credentials{Password: "admin"})
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.PowerW, 3210)
}
func TestByteonUnknownIdentityNoPassword(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "" {
			t.Error("credentials sent before identity")
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/api/v1/info":
			w.Write([]byte(`{"fw_name":"unknown"}`))
		case "/api/v1/summary":
			w.Write([]byte(`{"miner":{"power_consumption":3100}}`))
		case "/":
			w.Write([]byte(`<title>Unknown miner</title>`))
		default:
			t.Error("unexpected path", r.URL.Path)
		}
	}))
	defer s.Close()
	_, e := readByteon(context.Background(), s.Client(), s.URL, Credentials{Password: "secret"})
	if e == nil {
		t.Fatal("unknown firmware accepted")
	}
}
