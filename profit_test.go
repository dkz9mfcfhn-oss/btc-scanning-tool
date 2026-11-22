package main

import (
	"context"
	"math"
	"minerwatch/internal/miner"
	"minerwatch/internal/store"
	"strings"
	"testing"
	"time"
)

func TestProfitTracksActualCoveredIntervals(t *testing.T) {
	a, err := newApp(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-time.Hour)
	ip := "192.168.1.2"
	rec := func(at time.Time, rate, power *float64, source string) store.Record {
		return store.Record{IP: ip, Sample: store.Sample{At: at, Online: true, IntervalSeconds: 300, RealtimeTH: rate, PowerW: power, PowerSource: source}}
	}
	if err := a.db.Append([]store.Record{
		rec(start.Add(-time.Minute), num(100), num(3000), "WALLPOWER"),
		rec(start.Add(2*time.Minute), num(100), nil, ""),
		{IP: ip, Sample: store.Sample{At: start.Add(20 * time.Minute), Online: true, IntervalSeconds: 300, RealtimeTH: num(0), ManualPowerW: num(1200)}},
		rec(start.Add(30*time.Minute), num(100), num(3000), "P_P"),
		rec(end, num(900), num(9000), "WALLPOWER"),
	}); err != nil {
		t.Fatal(err)
	}
	rolling := a.db.RecentHashrateAverages([]string{ip}, start, end)[ip]
	retained, err := a.db.RetainedHashrateAverages(context.Background(), []string{ip}, start, end)
	if err != nil || rolling != retained[ip] || rolling.Covered != 17*time.Minute || rolling.Matched != 7*time.Minute || math.Abs(rolling.EnergyKWh-0.2) > 1e-9 || rolling.ManualCovered != 5*time.Minute || math.Abs(rolling.ManualKWh-0.1) > 1e-9 {
		t.Fatal(rolling, retained, err)
	}
	q := marketQuote{At: end, Height: 976000, Difficulty: 135e12, BTCCNY: 400000}
	var farm farmAverage
	farm.add(rolling.MeanTH, rolling.Covered.Hours())
	p := estimateProfit(farm, sampleProfit(rolling), q, num(0.5), 2.5)
	if !p.Partial || p.Gross == nil || p.PoolFee == nil || p.Electricity == nil || p.Net == nil || math.Abs(*p.Electricity-0.1) > 1e-9 || math.Abs(*p.Net-(0.012*q.CNYPerEH()*0.975-0.1)) > 1e-9 || math.Abs(*p.PoolFee-*p.Gross*0.025) > 1e-9 {
		t.Fatal(p)
	}
	state := buildDailyReport([]*Device{{IP: ip}}, retained, end, "fixture")
	state.Market, state.Tariff, state.PoolFeePercent = q, num(0.5), 2.5
	body := dailyReportBody(state, 0, nil)
	if !strings.Contains(body, "电费：¥0.10") || !strings.Contains(body, "配对净收益：") || !strings.Contains(body, "0.20 度") || !strings.Contains(body, "矿池手续费（2.5%）") || !strings.Contains(body, "手动功率估算：1 台 · 0.10 度") {
		t.Fatal(body)
	}
	if p := estimateProfit(farm, sampleProfit(rolling), q, nil, 0); p.Net != nil || p.Electricity != nil || p.Gross == nil {
		t.Fatal("missing tariff substituted with free electricity", p)
	}
	if p := estimateProfit(farm, sampleProfit(rolling), marketQuote{}, num(0.5), 2.5); p.Net != nil || p.Gross != nil || p.Electricity == nil || p.PoolFee != nil {
		t.Fatal("missing market quote hid energy cost or produced revenue", p)
	}
}

func TestManualPowerFeedsOnlyMissingFutureReadings(t *testing.T) {
	dir := t.TempDir()
	a, err := newApp(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.db.Close() })
	ip := "192.168.1.2"
	old := store.Sample{At: time.Now().Add(-time.Minute), Online: true, RealtimeTH: num(100)}
	a.devices[ip] = &Device{IP: ip, Latest: &old}
	if err := a.db.Append([]store.Record{{IP: ip, Sample: old}}); err != nil {
		t.Fatal(err)
	}
	if err := a.setManualPower(ip, "3200"); err != nil {
		t.Fatal(err)
	}
	if err := a.setPoolFee("2.5%"); err != nil {
		t.Fatal(err)
	}
	if err := a.setPoolFee("101"); err == nil || a.config.PoolFeePercent != 2.5 {
		t.Fatal("invalid fee overwrote a saved fee")
	}
	d := a.devices[ip]
	if sum := totals([]*Device{d}, time.Now()); sum.PowerW != 3200 || sum.PowerManual != 1 || sum.PowerReported != 0 || nativeRow(d, time.Now())[powerColumn] != "3200 手动" {
		t.Fatal(sum, nativeRow(d, time.Now()))
	}
	var measured *float64
	a.probe = func(context.Context, string, miner.Credentials) (miner.Reading, error) {
		return miner.Reading{Brand: "Antminer", RealtimeTH: num(100), PowerW: measured}, nil
	}
	a.runJob(context.Background(), []string{ip}, a.config, miner.Credentials{}, "poll")
	if s, _ := a.db.Latest(ip); s.PowerW != nil || s.ManualPowerW == nil || *s.ManualPowerW != 3200 {
		t.Fatal(s)
	}
	measured = num(0)
	a.runJob(context.Background(), []string{ip}, a.config, miner.Credentials{}, "poll")
	if s, _ := a.db.Latest(ip); s.PowerW == nil || *s.PowerW != 0 || s.ManualPowerW != nil {
		t.Fatal(s)
	}
	if rows := a.db.History(ip, old.At); len(rows) != 3 || rows[0].ManualPowerW != nil {
		t.Fatal("manual fallback was applied to older history", rows)
	}
	a.db.Close()
	b, err := newApp(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.db.Close()
	if b.config.PoolFeePercent != 2.5 || b.devices[ip].ManualPowerW == nil || *b.devices[ip].ManualPowerW != 3200 {
		t.Fatal("settings lost on restart")
	}
	if err := b.setManualPower(ip, ""); err != nil || b.devices[ip].ManualPowerW != nil {
		t.Fatal("unable to clear manual fallback", err)
	}
}

func TestElectricityTariffCanBeZeroAndCleared(t *testing.T) {
	a, err := newApp(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	if err := a.setElectricityTariff("0"); err != nil || a.config.ElectricityCNY == nil || *a.config.ElectricityCNY != 0 {
		t.Fatal(err, a.config)
	}
	if err := a.setElectricityTariff("NaN"); err == nil || *a.config.ElectricityCNY != 0 {
		t.Fatal(err, a.config)
	}
	if err := a.setElectricityTariff(""); err != nil || a.config.ElectricityCNY != nil {
		t.Fatal(err, a.config)
	}
}
