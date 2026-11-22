package miner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSharesFromBrandProtocols(t *testing.T) {
	for _, raw := range []string{`{"SUMMARY":[{"Accepted":900,"Rejected":5}]}`, `{"POOLS":[{"accepted":500,"rejected":2},{"accepted":400,"rejected":3}]}`, `SUMMARY,Accepted=900,Rejected=5|`} {
		r, e := ParseCGMiner([]byte(raw))
		if e != nil || r.Accepted == nil || *r.Accepted != 900 || *r.Rejected != 5 {
			t.Fatal(r, e)
		}
	}
	r, e := ParseByteon([]byte(`{"miner":{"hr_realtime":95,"pools":[{"accepted":90,"rejected":10}]}}`), []byte(`{"fw_name":"Byteon","hr_measure":"TH/s"}`))
	if e != nil || r.Accepted == nil || *r.Accepted != 90 {
		t.Fatal(r, e)
	}
	r, e = ParseWhatsminerV3([]byte(`{"code":0,"msg":{"summary":{"hash-realtime":399,"power-on-stats":{"reject-percent":2.3}}}}`), nil)
	if e != nil || r.Accepted != nil || r.RejectPercent == nil || *r.RejectPercent != 2.3 {
		t.Fatal(r, e)
	}
}
func TestSharesRejectPartialInvalidAndPercentageOnlyPools(t *testing.T) {
	for _, raw := range []string{`{"POOLS":[{"accepted":90,"rejected":10},{}]}`, `{"SUMMARY":[{"Accepted":-1,"Rejected":0}]}`, `{"SUMMARY":[{"Accepted":1.5,"Rejected":1}]}`, `{"POOLS":[{"reject-percent":9}]}`} {
		r, e := ParseCGMiner([]byte(raw))
		if e != nil || r.Accepted != nil || r.Rejected != nil {
			t.Fatal(raw, r, e)
		}
	}
}
func TestStockReadsShareCountsWhenHealthy(t *testing.T) {
	read := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/stats.cgi":
			w.Write([]byte(`{"STATS":[{"Type":"S19","rate_unit":"TH/s","rate_5s":95,"rate_avg":94,"chain":[{"index":0,"temp_chip":[60,65]}]}]}`))
		case "/cgi-bin/pools.cgi":
			read = true
			w.Write([]byte(`{"POOLS":[{"accepted":990,"rejected":10}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	r, failed, _ := readAntminerHTTP(context.Background(), s.URL, Credentials{})
	if failed || !read || r.Rejected == nil || *r.Rejected != 10 {
		t.Fatal(r, failed, read)
	}
}
