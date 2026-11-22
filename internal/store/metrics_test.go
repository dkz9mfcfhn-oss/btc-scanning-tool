package store

import (
	"fmt"
	"minerwatch/internal/telemetry"
	"testing"
	"time"
)

func metricFixture(n int) *Store {
	s := &Store{samples: make(map[string][]Sample)}
	now := time.Now()
	for i := 0; i < n; i++ {
		ip := fmt.Sprintf("10.1.%d.%d", i/254, i%254+1)
		for j := 0; j < 288; j++ {
			v := float64(100 + i)
			t := 65.0
			a := Sample{At: now.Add(-time.Duration(j) * 5 * time.Minute), Online: true, IntervalSeconds: 300, RealtimeTH: &v, AverageTH: &v, TempC: &t, Warnings: []string{"fixture"}}
			for k := 0; k < 4; k++ {
				a.Boards = append(a.Boards, telemetry.BoardTemperature{ID: fmt.Sprint(k), Chip: telemetry.TemperatureSeries{AverageC: &t, SensorsC: []*float64{&t, &t, &t, &t}}})
			}
			s.samples[ip] = append(s.samples[ip], a)
		}
	}
	return s
}
func TestMetricsIndependentAndMissing(t *testing.T) {
	s := metricFixture(1)
	since := time.Now().Add(-24 * time.Hour)
	r := s.Metrics(since)
	if len(r) != 288 {
		t.Fatal(len(r))
	}
	*r[0].RealtimeTH = 999
	if *s.Metrics(since)[0].RealtimeTH == 999 {
		t.Fatal("snapshot aliases stored sample")
	}
	s.samples["missing"] = []Sample{{At: time.Now(), Online: false}}
	for _, r := range s.Metrics(since) {
		if r.IP == "missing" && r.RealtimeTH != nil {
			t.Fatal("missing converted to zero")
		}
	}
}
func BenchmarkHistorySnapshots(b *testing.B) {
	s := metricFixture(100)
	since := time.Now().Add(-24 * time.Hour)
	b.Run("Full", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			s.All(since)
		}
	})
	b.Run("Metrics", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			s.Metrics(since)
		}
	})
}
