package store

import (
	"encoding/json"
	"minerwatch/internal/telemetry"
	"testing"
	"time"
)

func TestTemperatureRoundTripAndDeepCopy(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	record := Record{IP: "192.168.1.10", Sample: Sample{At: now, Online: true, Boards: []telemetry.BoardTemperature{{ID: "slot:2", Name: "板2", Chip: telemetry.TemperatureSeries{AverageC: ptr(65), SensorsC: []*float64{ptr(60), nil, ptr(70)}, AverageKind: "reported", Source: "fixture.avg"}, Other: []telemetry.Sensor{{Name: "出口", ValueC: ptr(40), Source: "fixture.out"}}}}, Temperatures: []telemetry.Sensor{{Name: "环境", ValueC: ptr(26), Source: "fixture.ambient"}}}}
	if err = s.Append([]Record{record}); err != nil {
		t.Fatal(err)
	}
	*record.Sample.Boards[0].Chip.SensorsC[0] = 99
	*record.Sample.Temperatures[0].ValueC = 99
	record.Sample.Boards[0].Other[0].Name = "changed"
	h := s.History(record.IP, now.Add(-time.Hour))
	if *h[0].Boards[0].Chip.SensorsC[0] != 60 || *h[0].Temperatures[0].ValueC != 26 || h[0].Boards[0].Other[0].Name != "出口" {
		t.Fatal("append shared mutable data")
	}
	*h[0].Boards[0].Chip.AverageC = 99
	h[0].Boards[0].Other[0].Name = "changed"
	again := s.All(now.Add(-time.Hour))
	if *again[0].Sample.Boards[0].Chip.AverageC != 65 || again[0].Sample.Boards[0].Other[0].Name != "出口" {
		t.Fatal("history shared mutable data")
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h = s.History(record.IP, now.Add(-time.Hour))
	if len(h) != 1 || len(h[0].Boards[0].Chip.SensorsC) != 3 || h[0].Boards[0].Chip.SensorsC[1] != nil || h[0].Boards[0].Chip.AverageKind != "reported" {
		t.Fatal(h)
	}
}
func TestOldSampleHasNoFabricatedBoardHistory(t *testing.T) {
	var s Sample
	if err := json.Unmarshal([]byte(`{"at":"2026-10-29T00:00:00Z","online":true,"tempC":76}`), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Boards) != 0 || len(s.Temperatures) != 0 || *s.TempC != 76 {
		t.Fatal(s)
	}
	b, _ := json.Marshal(cloneSample(s))
	var copy Sample
	if err := json.Unmarshal(b, &copy); err != nil || len(copy.Boards) != 0 {
		t.Fatal(copy, err)
	}
}
