package main

import (
	"context"
	"encoding/csv"
	"math"
	"minerwatch/internal/miner"
	"minerwatch/internal/store"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRejectRateAxisCentersMeasuredRange(t *testing.T) {
	for _, tc := range []struct {
		name     string
		min, max float64
	}{
		{"low", 0.06, 0.24},
		{"nearly constant", 0.30, 0.31},
		{"zero", 0, 0},
		{"all rejected", 100, 100},
		{"full range", 0, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			low, high := rejectRateAxis([]FleetPoint{{RejectPercent: num(tc.min)}, {}, {RejectPercent: num(tc.max)}})
			span := high - low
			first, last := (tc.min-low)/span, (tc.max-low)/span
			if first < 0.16 || last > 0.84 || math.Abs(first+last-1) > 1e-10 {
				t.Fatalf("curve occupies %.3f to %.3f of the plot, expected centered with padding", first, last)
			}
			for _, height := range []int32{16, 48, 110} {
				ticks := rejectRateTicks(low, high, height)
				for i, tick := range ticks {
					if tick < 0 || tick > 100 || tick < low || tick > high {
						t.Fatalf("invalid tick %v for plot range %v–%v", tick, low, high)
					}
					if i > 0 && (tick-ticks[i-1])/span*float64(height) < 17.999999 {
						t.Fatalf("overlapping tick labels at %d px: %v", height, ticks)
					}
				}
			}
		})
	}
	if low, high := rejectRateAxis([]FleetPoint{{}, {}}); low != 0 || high != 1 {
		t.Fatalf("missing samples should use the default axis, got %v–%v", low, high)
	}
}

func TestFleetRejectRatioUsesCountsNotAverageOrHashrate(t *testing.T) {
	now := time.Now()
	ds := []*Device{
		{Latest: &store.Sample{At: now, Online: true, Accepted: num(90), Rejected: num(10)}},
		{Latest: &store.Sample{At: now, Online: true, Accepted: num(900), Rejected: num(0)}},
		{Latest: &store.Sample{At: now, Online: true, RejectPercent: num(80)}},
		{Latest: &store.Sample{At: now, Online: false, Accepted: num(0), Rejected: num(1000)}},
	}
	v := totals(ds, now)
	if v.RejectPercent == nil || math.Abs(*v.RejectPercent-1) > 1e-9 || v.RejectReported != 2 || v.Online != 3 {
		t.Fatal(v)
	}
	if rejection(ds[2].Latest) == nil || *rejection(ds[2].Latest) != 80 {
		t.Fatal("lost device-only percentage")
	}
	if sharePercent(0, 0) != nil {
		t.Fatal("no submissions is not 0%")
	}
	ds[0].Latest.Accepted = num(-1)
	if validShares(ds[0].Latest) {
		t.Fatal("negative counter accepted")
	}
}
func TestFleetHistoryCarriesFreshOnlyAndLeavesLongGaps(t *testing.T) {
	at := time.Now().Truncate(5 * time.Minute).Add(-time.Hour)
	mk := func(ip string, sec int, online bool, rate float64) store.Record {
		return store.Record{IP: ip, Sample: store.Sample{At: at.Add(time.Duration(sec) * time.Second), IntervalSeconds: 300, Online: online, RealtimeTH: num(rate), Accepted: num(90), Rejected: num(10)}}
	}
	rs := []store.Record{mk("a", 299, true, 10), mk("b", 301, true, 20), mk("a", 302, true, 15), mk("a", 3600, false, 0)}
	ps := fleetHistory(rs, map[string]*Device{"a": {}, "b": {}})
	if len(ps) != 13 {
		t.Fatal(len(ps))
	}
	if ps[1].Online != 2 || *ps[1].RealtimeTH != 35 || *ps[1].RejectPercent != 10 {
		t.Fatal(ps[1])
	}
	if ps[5].Observed != 0 || ps[5].RealtimeTH != nil || ps[5].RejectPercent != nil {
		t.Fatal("filled monitoring gap", ps[5])
	}
	if ps[12].Online != 0 || ps[12].Observed != 1 || ps[12].RealtimeTH != nil {
		t.Fatal("offline inferred live zero", ps[12])
	}
}
func TestShareAndPowerCollectionStored(t *testing.T) {
	a, e := newApp(t.TempDir(), false)
	if e != nil {
		t.Fatal(e)
	}
	defer a.db.Close()
	a.discover = func(context.Context, string) bool { return true }
	a.probe = func(context.Context, string, miner.Credentials) (miner.Reading, error) {
		return miner.Reading{Brand: "Antminer", RealtimeTH: num(95), Accepted: num(99), Rejected: num(1), PowerW: num(3200), PowerSource: "6060"}, nil
	}
	a.runJob(context.Background(), []string{"192.168.1.10"}, a.config, miner.Credentials{}, "scan")
	s := a.devices["192.168.1.10"].Latest
	if s.PowerW == nil || *s.PowerW != 3200 || s.PowerSource != "6060" || *rejection(s) != 1 {
		t.Fatal(s)
	}
	history := a.db.History("192.168.1.10", time.Now().Add(-time.Hour))
	*history[0].Accepted = 2
	history = a.db.History("192.168.1.10", time.Now().Add(-time.Hour))
	if *history[0].Accepted != 99 || *history[0].PowerW != 3200 {
		t.Fatal("aliased share history")
	}
	w := httptest.NewRecorder()
	a.export(w, httptest.NewRequest("GET", "/api/export", nil))
	rows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\xEF\xBB\xBF"))).ReadAll()
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(strings.Join(rows[0], ","), "功率") || len(rows[0]) != 18 || rows[1][9] != "1.000" || rows[1][10] != "99.000" || rows[1][17] != "1.000" {
		t.Fatal(rows)
	}
}

func TestHiddenMinerExcludedFromTotalsHistoryAndPush(t *testing.T) {
	now := time.Now().UTC()
	visible := &Device{Latest: &store.Sample{At: now, Online: true, RealtimeTH: num(95), Accepted: num(9), Rejected: num(1)}}
	hidden := &Device{Hidden: true, Latest: &store.Sample{At: now, Online: true, RealtimeTH: num(300), Accepted: num(90), Rejected: num(0)}}
	tot := totals([]*Device{visible, hidden}, now)
	if tot.Count != 1 || tot.Online != 1 || tot.RealtimeTH != 95 || tot.RejectReported != 1 {
		t.Fatal(tot)
	}
	devices := map[string]*Device{"visible": visible, "hidden": hidden}
	points := fleetHistory([]store.Record{{IP: "visible", Sample: *visible.Latest}, {IP: "hidden", Sample: *hidden.Latest}}, devices)
	if len(points) != 1 || points[0].Count != 1 || points[0].RealtimeTH == nil || *points[0].RealtimeTH != 95 {
		t.Fatal(points)
	}

	hidden.Latest.Online = false
	a := &App{devices: devices, pushURL: "invalid", pushStates: map[string]pushState{"hidden": {Seen: true, Failures: 1}}}
	a.notifyScan(context.Background(), []string{"hidden"})
	if a.pushError != "" || a.pushStates["hidden"].Failures != 1 {
		t.Fatal("hidden miner entered notification processing")
	}
}
