package store

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestHashrateAveragesMatchOnDiskAndInMemory(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-24 * time.Hour)
	rate1, rate2, zero, ignored := 100.0, 200.0, 0.0, 999.0
	if err := s.Append([]Record{
		{IP: "miner", Sample: Sample{At: end.Add(-2 * time.Minute), Online: true, RealtimeTH: &zero, IntervalSeconds: 120}},
		{IP: "miner", Sample: Sample{At: end.Add(-5 * time.Minute), Online: true, IntervalSeconds: 300}}, // Missing cuts short the previous valid sample.
		{IP: "miner", Sample: Sample{At: end.Add(-10 * time.Minute), Online: true, RealtimeTH: &rate2, IntervalSeconds: 600}},
		{IP: "miner", Sample: Sample{At: end.Add(-15 * time.Minute), Online: true, RealtimeTH: &rate1, IntervalSeconds: 300}},
		{IP: "miner", Sample: Sample{At: end, Online: true, RealtimeTH: &ignored}},
		{IP: "miner", Sample: Sample{At: start.Add(-40 * time.Minute), Online: true, RealtimeTH: &ignored, IntervalSeconds: 1800}},
		{IP: "zero", Sample: Sample{At: end.Add(-2 * time.Minute), Online: true, RealtimeTH: &zero, IntervalSeconds: 120}},
	}); err != nil {
		t.Fatal(err)
	}
	ips := []string{"miner", "zero", "missing"}
	want := map[string]HashrateAverage{
		"miner":   {MeanTH: 125, Covered: 12 * time.Minute},
		"zero":    {MeanTH: 0, Covered: 2 * time.Minute},
		"missing": {},
	}
	disk, err := s.RetainedHashrateAverages(context.Background(), ips, start, end)
	if err != nil {
		t.Fatal(err)
	}
	mem := s.RecentHashrateAverages(ips, start, end)
	for ip, w := range want {
		if mem[ip] != w || disk[ip] != w {
			t.Errorf("%s memory=%+v disk=%+v want=%+v", ip, mem[ip], disk[ip], w)
		}
	}
}

func TestHashrateWindowIgnoresInvalidAndOfflineReadings(t *testing.T) {
	end := time.Now()
	window := hashrateWindow{start: end.Add(-24 * time.Hour), end: end}
	nan, inf, negative, rate := math.NaN(), math.Inf(1), -1.0, 200.0
	window.add(hashrateObservation{At: end.Add(-4 * time.Minute), Online: true, RealtimeTH: &nan})
	window.add(hashrateObservation{At: end.Add(-3 * time.Minute), Online: true, RealtimeTH: &inf})
	window.add(hashrateObservation{At: end.Add(-2 * time.Minute), Online: true, RealtimeTH: &negative})
	window.add(hashrateObservation{At: end.Add(-time.Minute), Online: false, RealtimeTH: &rate})
	if got := window.average(); got != (HashrateAverage{}) {
		t.Fatal(got)
	}
}
