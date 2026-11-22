package main

import (
	"math"
	"minerwatch/internal/store"
	"strings"
	"testing"
	"time"
)

func TestDevice24hStatsExcludeMissingAndFuture(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	raw := []store.Sample{
		{At: now.Add(-25 * time.Hour), Online: true, RealtimeTH: num(999)},
		{At: now.Add(time.Second), Online: true, RealtimeTH: num(888)},
		{At: now, Online: true, RealtimeTH: num(0)},
		{At: now.Add(-time.Hour), Online: true, RealtimeTH: num(100), AverageTH: num(95)},
		{At: now.Add(-2 * time.Hour), Online: false, RealtimeTH: num(900)},
		{At: now.Add(-3 * time.Hour), Online: true},
		{At: now.Add(-4 * time.Hour), Online: true, RealtimeTH: num(math.NaN())},
	}
	samples := historySamples24(raw, now)
	r := summarizeDeviceHistory(samples)
	if r.Samples != 5 || r.Online != 4 || r.Valid != 2 || r.Zero != 1 || *r.Mean != 50 || *r.Min != 0 || *r.Max != 100 {
		t.Fatal(r)
	}
	text := deviceHistoryReport(samples, now)
	if strings.Contains(text, "900.00") || strings.Contains(text, "888.00") || !strings.Contains(text, "零算力") || !strings.Contains(text, "读取失败") {
		t.Fatal(text)
	}
	if !samples[0].At.Before(samples[1].At) {
		t.Fatal("not sorted")
	}
	if len(raw) != 7 {
		t.Fatal("input mutated")
	}
}
func TestDeviceHistoryEmptyAndFullDay(t *testing.T) {
	now := time.Now()
	empty := summarizeDeviceHistory(nil)
	if empty.Mean != nil || empty.Min != nil || empty.Max != nil {
		t.Fatal(empty)
	}
	if !strings.Contains(deviceHistoryReport(nil, now), "尚无记录") {
		t.Fatal("missing empty state")
	}
	var samples []store.Sample
	for i := 0; i < 288; i++ {
		samples = append(samples, store.Sample{At: now.Add(-time.Duration(i) * 5 * time.Minute), Online: true, RealtimeTH: num(100)})
	}
	samples = historySamples24(samples, now)
	r := summarizeDeviceHistory(samples)
	if r.Valid != 288 || *r.Mean != 100 {
		t.Fatal(r)
	}
	if strings.Count(deviceHistoryReport(samples, now), "| 读取成功") != 288 {
		t.Fatal("truncated sample records")
	}
}
