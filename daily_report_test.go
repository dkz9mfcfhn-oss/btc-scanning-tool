package main

import (
	"context"
	"errors"
	"fmt"
	"minerwatch/internal/store"
	"strings"
	"testing"
	"time"
)

func TestDailyReportWindow(t *testing.T) {
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	end, due := dailyReportWindow(now)
	if !due || end.Hour() != 8 || !end.Equal(now) {
		t.Fatal(end, due)
	}
	if _, due = dailyReportWindow(now.Add(-time.Second)); due {
		t.Fatal("sent before 8 Beijing")
	}
	if e, ok := dailyReportWindow(now.Add(7 * time.Hour)); !ok || !e.Equal(end) {
		t.Fatal("late start must use fixed 8am cutoff")
	}
}
func TestDailyReportSummaryRetryAndRestart(t *testing.T) {
	dir := t.TempDir()
	a, e := newApp(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	end, _ := dailyReportWindow(time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC))
	endpoint := "https://example.test/key12345678"
	a.pushURL = endpoint
	for i := 1; i <= 90; i++ {
		ip := fmt.Sprintf("192.168.1.%d", i)
		a.devices[ip] = &Device{IP: ip}
	}
	a.devices["192.168.1.1"].Hidden = true
	a.dailySender = func(context.Context, string, string, string) error { return errors.New("temporary failure") }
	if e = a.sendDailyReport(context.Background(), end, endpoint); e == nil {
		t.Fatal("expected failure")
	}
	a.db.Close()
	b, e := newApp(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	defer b.db.Close()
	b.devices = a.devices
	b.pushURL = endpoint
	b.devices["192.168.1.2"].Hidden = true
	sent := 0
	b.dailySender = func(_ context.Context, _, _, body string) error {
		sent++
		if !strings.Contains(body, "0 / 88 台") {
			t.Fatal(body)
		}
		return nil
	}
	if e = b.sendDailyReport(context.Background(), end, endpoint); e != nil {
		t.Fatal(e)
	}
	if e = b.sendDailyReport(context.Background(), end, endpoint); e != nil || sent != 1 {
		t.Fatal(e, sent)
	}
}
func TestDailyReportMeanTextAndMissing(t *testing.T) {
	end := time.Now()
	ds := []*Device{{IP: "192.168.1.2", WorkerNames: []string{"farm.two"}}, {IP: "192.168.1.3", Hidden: true}, {IP: "192.168.1.4"}}
	averages := map[string]store.HashrateAverage{"192.168.1.2": {MeanTH: 123, Covered: 5 * time.Minute}}
	state := buildDailyReport(ds, averages, end, "https://example.test/key12345678")
	body := dailyReportBody(state, 0, nil)
	if !strings.Contains(body, "123.00 TH/s") || !strings.Contains(body, "1 / 2 台") || strings.Contains(body, "192.168.1.3") {
		t.Fatal(body)
	}
}

func TestLateDailyReportReadsFullFixedWindow(t *testing.T) {
	a, err := newApp(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	end := time.Now().UTC().Truncate(24 * time.Hour).In(reportZone)
	start := end.Add(-farmAverageWindow)
	late := end.Add(15*time.Hour + 59*time.Minute)
	ip := "192.168.1.2"
	hiddenIP := "192.168.1.3"
	a.devices[ip] = &Device{IP: ip}
	a.devices[hiddenIP] = &Device{IP: hiddenIP, Hidden: true}
	endpoint := "https://example.test/key12345678"
	a.pushURL = endpoint
	if err := a.db.Append([]store.Record{
		{IP: ip, Sample: store.Sample{At: end, Online: true, RealtimeTH: num(999), IntervalSeconds: 300}},
		{IP: ip, Sample: store.Sample{At: end.Add(-5 * time.Minute), Online: true, RealtimeTH: num(0), IntervalSeconds: 300}},
		{IP: ip, Sample: store.Sample{At: start.Add(3 * time.Minute), Online: true, RealtimeTH: num(200), IntervalSeconds: 120}},
		{IP: ip, Sample: store.Sample{At: start.Add(-2 * time.Minute), Online: true, RealtimeTH: num(100), IntervalSeconds: 300}},
		{IP: hiddenIP, Sample: store.Sample{At: start, Online: true, RealtimeTH: num(99999), IntervalSeconds: 300}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Maintain(late); err != nil {
		t.Fatal(err)
	}
	if got := a.db.RecentHashrateAverages([]string{ip}, start, end)[ip]; got.MeanTH != 0 || got.Covered != 5*time.Minute {
		t.Fatalf("test must exclude old observations from RAM: %+v", got)
	}
	// An unsent snapshot from before this update must be rebuilt, while a sent
	// snapshot still prevents duplicate pushes after upgrading.
	a.dailyState = buildDailyReport([]*Device{a.devices[ip]}, nil, end, endpoint)
	a.dailyState.WindowVersion = 0
	var body string
	a.dailySender = func(_ context.Context, _, _, message string) error { body = message; return nil }
	a.maybeDailyReport(context.Background(), late)
	a.jobs.Wait()
	for _, want := range []string{"24小时平均算力合计", "70.00 TH/s", "1 / 1 台", "0.7%", start.Format("01-02 15:04") + " — " + end.Format("01-02 15:04")} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestDailyReportSchedulerAtEightRetryAndPause(t *testing.T) {
	a, err := newApp(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	a.pushURL = "https://example.test/key12345678"
	a.devices["192.168.1.2"] = &Device{IP: "192.168.1.2"}
	a.paused = true // Reporting does not silently stop when collection is paused.
	calls := 0
	a.dailySender = func(context.Context, string, string, string) error {
		calls++
		if calls == 1 {
			return errors.New("offline")
		}
		return nil
	}
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	a.maybeDailyReport(context.Background(), now.Add(-time.Second))
	a.jobs.Wait()
	if calls != 0 {
		t.Fatal("sent before eight")
	}
	a.maybeDailyReport(context.Background(), now)
	a.jobs.Wait()
	if calls != 1 {
		t.Fatal(calls)
	}
	a.maybeDailyReport(context.Background(), now.Add(time.Minute))
	a.jobs.Wait()
	if calls != 1 {
		t.Fatal("retry storm")
	}
	a.maybeDailyReport(context.Background(), now.Add(5*time.Minute))
	a.jobs.Wait()
	if calls != 2 || a.dailyError != "" {
		t.Fatal(calls, a.dailyError)
	}
	a.maybeDailyReport(context.Background(), now.Add(time.Hour))
	a.jobs.Wait()
	if calls != 2 {
		t.Fatal("duplicate report")
	}
	a.maybeDailyReport(context.Background(), now.Add(24*time.Hour))
	a.jobs.Wait()
	if calls != 3 {
		t.Fatal("next day not sent")
	}
}

func TestFarmDailySummaryUnitsAndHiddenExclusion(t *testing.T) {
	state := dailyReportState{SummaryOnly: true, End: time.Now(), Parts: [][]dailyReportRow{{
		{IP: "a", Mean: num(1000), CoveredHours: 24}, {IP: "b", Mean: num(2000), CoveredHours: 12}, {IP: "hidden", Mean: num(99999), CoveredHours: 24}, {IP: "missing"},
	}}}
	body := dailyReportBody(state, 0, map[string]bool{"hidden": true})
	for _, want := range []string{"3.000 PH/s", "3000.00 TH/s", "172.800 EH", "2 / 3 台", "50.0%"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}
