package store

import (
	"slices"
	"testing"
	"time"
)

func BenchmarkOpenRetainedTelemetry(b *testing.B) {
	fixture := metricFixture(50)
	var records []Record
	for ip, samples := range fixture.samples {
		for _, sample := range samples {
			records = append(records, Record{IP: ip, Sample: sample})
		}
	}
	dir := b.TempDir()
	s, err := Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	if err := s.Append(records); err != nil {
		b.Fatal(err)
	}
	s.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		s.Close()
	}
}

func BenchmarkRestoreNewestTelemetry(b *testing.B) {
	fixture := metricFixture(50)
	for _, samples := range fixture.samples {
		slices.Reverse(samples)
	}
	since := time.Now().Add(-7 * 24 * time.Hour)
	b.Run("CopyHistory", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for ip := range fixture.samples {
				samples := fixture.History(ip, since)
				if len(samples) == 0 {
					b.Fatal("no history")
				}
			}
		}
	})
	b.Run("Latest", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for ip := range fixture.samples {
				if _, ok := fixture.Latest(ip); !ok {
					b.Fatal("no history")
				}
			}
		}
	})
}
