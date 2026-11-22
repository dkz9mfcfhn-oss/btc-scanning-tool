package telemetry

import "testing"

func TestCompleteNeverAveragesOnePointOrExtremePair(t *testing.T) {
	for _, s := range []TemperatureSeries{{MinC: number(50), MaxC: number(80)}, {SensorsC: []*float64{number(70)}}} {
		if Complete(s).AverageC != nil {
			t.Fatal("fabricated mean")
		}
	}
	s := Complete(TemperatureSeries{SensorsC: []*float64{number(0), nil, number(10), number(255)}})
	if s.AverageC == nil || *s.AverageC != 5 || s.AverageKind != "sensor_mean" || len(s.SensorsC) != 4 || s.SensorsC[3] != nil {
		t.Fatal(s)
	}
}
func TestReportedAverageWinsOverSensorMean(t *testing.T) {
	s := Complete(TemperatureSeries{AverageC: number(65), SensorsC: []*float64{number(60), number(80)}})
	if *s.AverageC != 65 || s.AverageKind != "reported" {
		t.Fatal(s)
	}
}
