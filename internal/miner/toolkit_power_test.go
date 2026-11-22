package miner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestToolkitStockPowerAndWhatsStatusPage(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		switch r.URL.Path {
		case "/miner_power":
			w.Write([]byte(`{"response":"some field: 9\nminer power: 3186\n"}`))
		case "/cgi-bin/luci/admin/status/btminerstatus":
			w.Write([]byte(`<input value="7,118 W" id="cbid.table.1.power">`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer s.Close()
	value(t, stockPower(context.Background(), s.URL, Credentials{}).PowerW, 3186)
	value(t, whatsPowerPage(context.Background(), s.URL, Credentials{}).PowerW, 7118)
	value(t, parseStockPower([]byte("miner power: 0")), 0)
	value(t, parseBraiinsPower([]byte(`{"TUNERSTATUS":[{"ApproximateMinerPowerConsumption":1419,"PowerLimit":1450}]}`)).PowerW, 1419)
	if parseStockPower([]byte("miner power: -1")) != nil || parseWhatsPowerPage([]byte(`<input id="cbid.table.1.power_limit" value="3000">`)) != nil {
		t.Fatal("invalid or ambiguous power accepted")
	}
}

func TestAvalonWallPowerPreferredToPS(t *testing.T) {
	r, err := ParseCGMiner([]byte(`{"STATS":[{"MM ID0":"Ver[1566HA] PS[0 1198 2505 372 9320 2503 9912] WALLPOWER[10124] GHSmm[561000]"}]}`))
	if err != nil || r.PowerSource != avalonWallSource {
		t.Fatal(r, err)
	}
	value(t, r.PowerW, 10124)
	ps, _ := ParseAvalonLog([]byte("PS[0 1198 2505 372 9320 2503 9912]"))
	value(t, merge(ps, r).PowerW, 10124)
}
