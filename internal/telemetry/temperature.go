// Package telemetry contains persisted, provenance-aware temperature readings.
package telemetry

import "math"

type Sensor struct {
	Name   string   `json:"name"`
	ValueC *float64 `json:"valueC"`
	Source string   `json:"source"`
}

type TemperatureSeries struct {
	AverageC *float64   `json:"averageC"`
	MinC     *float64   `json:"minC"`
	MaxC     *float64   `json:"maxC"`
	SensorsC []*float64 `json:"sensorsC,omitempty"`
	// reported = firmware mean; sensor_mean = mean of available sensor readings.
	// A single reading, or a min/max pair, is never presented as a board mean.
	AverageKind string `json:"averageKind,omitempty"`
	Source      string `json:"source,omitempty"`
}

type BoardTemperature struct {
	ID    string            `json:"id"`
	Name  string            `json:"name"`
	Chip  TemperatureSeries `json:"chip"`
	PCB   TemperatureSeries `json:"pcb"`
	Other []Sensor          `json:"other,omitempty"`
}

func Valid(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v < -50 || *v > 200 {
		return nil
	}
	x := *v
	return &x
}
func number(v float64) *float64 { return &v }

// Complete fills only absent extrema and a sample mean from actual readings.
// Missing sensor slots remain nil, preserving indexes for full detail views.
func Complete(s TemperatureSeries) TemperatureSeries {
	s.AverageC, s.MinC, s.MaxC = Valid(s.AverageC), Valid(s.MinC), Valid(s.MaxC)
	if s.AverageC != nil && s.AverageKind == "" {
		s.AverageKind = "reported"
	}
	var lo, hi *float64
	sum := 0.0
	count := 0
	for i, v := range s.SensorsC {
		s.SensorsC[i] = Valid(v)
		v = s.SensorsC[i]
		if v == nil {
			continue
		}
		count++
		sum += *v
		if lo == nil || *v < *lo {
			lo = number(*v)
		}
		if hi == nil || *v > *hi {
			hi = number(*v)
		}
	}
	if s.MinC == nil {
		s.MinC = lo
	}
	if s.MaxC == nil {
		s.MaxC = hi
	}
	if s.AverageC == nil && count >= 2 {
		s.AverageC = number(sum / float64(count))
		s.AverageKind = "sensor_mean"
	}
	if s.AverageC == nil {
		s.AverageKind = ""
	}
	return s
}
func clone(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return number(*v)
}
func CloneSeries(s TemperatureSeries) TemperatureSeries {
	s.AverageC, s.MinC, s.MaxC = clone(s.AverageC), clone(s.MinC), clone(s.MaxC)
	s.SensorsC = append([]*float64(nil), s.SensorsC...)
	for i := range s.SensorsC {
		s.SensorsC[i] = clone(s.SensorsC[i])
	}
	return s
}
func CloneSensors(s []Sensor) []Sensor {
	out := append([]Sensor(nil), s...)
	for i := range out {
		out[i].ValueC = clone(out[i].ValueC)
	}
	return out
}
func CloneBoards(s []BoardTemperature) []BoardTemperature {
	out := append([]BoardTemperature(nil), s...)
	for i := range out {
		out[i].Chip = CloneSeries(out[i].Chip)
		out[i].PCB = CloneSeries(out[i].PCB)
		out[i].Other = CloneSensors(out[i].Other)
	}
	return out
}

// MergeSensors uses provenance as the identity; repeated reads do not duplicate
// sensor fields. Different source fields remain individually inspectable.
func MergeSensors(a, b []Sensor) []Sensor {
	out := CloneSensors(a)
	for _, v := range b {
		found := false
		for i := range out {
			if out[i].Source == v.Source && out[i].Name == v.Name {
				if out[i].ValueC == nil {
					out[i] = v
				}
				found = true
				break
			}
		}
		if !found {
			out = append(out, v)
		}
	}
	return CloneSensors(out)
}
