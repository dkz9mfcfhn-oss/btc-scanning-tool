// Package miner contains read-only, bounded LAN miner collectors.
package miner

import (
	"encoding/json"
	"fmt"
	"math"
	"minerwatch/internal/telemetry"
	"strconv"
	"strings"
)

type Credentials struct{ Username, Password string }

type Reading struct {
	ElapsedSeconds *float64                     `json:"elapsedSeconds,omitempty"`
	WorkerNames    []string                     `json:"workerNames,omitempty"`
	WorkersKnown   bool                         `json:"workersKnown,omitempty"`
	Accepted       *float64                     `json:"accepted,omitempty"`
	Rejected       *float64                     `json:"rejected,omitempty"`
	RejectPercent  *float64                     `json:"rejectPercent,omitempty"`
	Faults         []telemetry.Fault            `json:"faults,omitempty"`
	StatusKnown    bool                         `json:"statusKnown,omitempty"`
	FirmwareKnown  bool                         `json:"firmwareKnown,omitempty"`
	Boards         []telemetry.BoardTemperature `json:"boards,omitempty"`
	Temperatures   []telemetry.Sensor           `json:"temperatures,omitempty"`
	Brand          string                       `json:"brand"`
	Model          string                       `json:"model"`
	Protocol       string                       `json:"protocol"`
	RealtimeTH     *float64                     `json:"realtimeTH"`
	AverageTH      *float64                     `json:"averageTH"`
	TempC          *float64                     `json:"tempC"`
	InletC         *float64                     `json:"inletC"`
	OutletC        *float64                     `json:"outletC"`
	PowerW         *float64                     `json:"powerW"`
	PowerSource    string                       `json:"powerSource"`
	HashrateSource string                       `json:"hashrateSource"`
	Warnings       []string                     `json:"warnings"`
}

func ptr(v float64) *float64 { return &v }
func numeric(v any) *float64 {
	var n float64
	var err error
	switch x := v.(type) {
	case float64:
		n = x
	case json.Number:
		n, err = x.Float64()
	case string:
		n, err = strconv.ParseFloat(strings.TrimSpace(x), 64)
	default:
		return nil
	}
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}
func nonnegative(v *float64) *float64 {
	if v != nil && *v >= 0 {
		return v
	}
	return nil
}
func number(m map[string]any, keys ...string) *float64 {
	for _, k := range keys {
		if v := numeric(m[k]); v != nil {
			return v
		}
	}
	return nil
}
func text(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
func scale(v *float64, factor float64) *float64 {
	if v == nil || *v < 0 {
		return nil
	}
	return ptr(*v * factor)
}
func maximum(a, b *float64) *float64 {
	if a == nil {
		return b
	}
	if b == nil || *a >= *b {
		return a
	}
	return b
}
func arrayMax(v any) *float64 {
	var out *float64
	switch a := v.(type) {
	case []any:
		for _, x := range a {
			out = maximum(out, arrayMax(x))
		}
	case string:
		for _, s := range strings.FieldsFunc(a, func(r rune) bool { return r == ',' || r == ' ' || r == '/' }) {
			out = maximum(out, numeric(s))
		}
	default:
		out = numeric(v)
	}
	if out != nil && (*out < -50 || *out > 200) {
		return nil
	}
	return out
}
func decode(data []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("empty response")
	}
	return m, nil
}
func objects(v any) []map[string]any {
	var out []map[string]any
	switch a := v.(type) {
	case map[string]any:
		out = append(out, a)
	case []any:
		for _, x := range a {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}
func section(m map[string]any, name string) []map[string]any {
	if x, ok := m[name]; ok {
		return objects(x)
	}
	if x, ok := m[strings.ToLower(name)]; ok {
		return objects(x)
	}
	return nil
}
func hasData(r Reading) bool { return r.RealtimeTH != nil || r.AverageTH != nil }

// A confirmed compatible firmware with actual telemetry is still a discovered
// miner when it omits the rate during startup or shutdown. Brand alone is not
// proof: the generic CGI adapter labels any parseable response Antminer.
func hasObservation(r Reading) bool {
	return hasData(r) || r.StatusKnown || (r.FirmwareKnown && (r.PowerW != nil || r.TempC != nil || len(r.Boards) > 0 || len(r.Temperatures) > 0))
}

// Fault-log requests require an actual measured zero. Missing realtime hashrate
// can justify a metric fallback, but never a fault-log request.
func explicitZeroHashrate(r Reading) bool   { return r.RealtimeTH != nil && *r.RealtimeTH == 0 }
func needsHashrateDiagnosis(r Reading) bool { return r.RealtimeTH == nil || explicitZeroHashrate(r) }

func merge(a, b Reading) Reading {
	if a.ElapsedSeconds == nil {
		a.ElapsedSeconds = b.ElapsedSeconds
	}
	if !a.WorkersKnown && b.WorkersKnown {
		a.WorkersKnown = true
		a.WorkerNames = append([]string(nil), b.WorkerNames...)
	}
	if a.Accepted == nil && b.Accepted != nil && b.Rejected != nil {
		a.Accepted, a.Rejected = b.Accepted, b.Rejected
	}
	if a.RejectPercent == nil {
		a.RejectPercent = b.RejectPercent
	}
	if a.Brand == "" {
		a.Brand = b.Brand
	}
	if a.Model == "" {
		a.Model = b.Model
	}
	if a.Protocol == "" {
		a.Protocol = b.Protocol
	}
	if a.RealtimeTH == nil {
		a.RealtimeTH = b.RealtimeTH
		a.HashrateSource = b.HashrateSource
	}
	if a.AverageTH == nil {
		a.AverageTH = b.AverageTH
	}
	a.Faults = telemetry.MergeFaults(a.Faults, b.Faults)
	a.StatusKnown = a.StatusKnown || b.StatusKnown
	a.FirmwareKnown = a.FirmwareKnown || b.FirmwareKnown
	a.Boards = mergeBoards(a.Boards, b.Boards)
	// A later successful BTC-compatible endpoint replaces, rather than unions,
	// its temperature list, matching the original scanner's overwrite semantics.
	if btcTemperatureRank(a.Temperatures) > btcTemperatureRank(b.Temperatures) && btcTemperatureRank(b.Temperatures) > 0 {
		var kept []telemetry.Sensor
		for _, sensor := range b.Temperatures {
			if !strings.HasPrefix(sensor.Source, btcTemperaturePrefix) {
				kept = append(kept, sensor)
			}
		}
		b.Temperatures = kept
	}
	for _, sensor := range b.Temperatures {
		if strings.HasPrefix(sensor.Source, btcTemperaturePrefix) {
			var kept []telemetry.Sensor
			for _, old := range a.Temperatures {
				if !strings.HasPrefix(old.Source, btcTemperaturePrefix) {
					kept = append(kept, old)
				}
			}
			a.Temperatures = kept
			break
		}
	}
	a.Temperatures = telemetry.MergeSensors(a.Temperatures, b.Temperatures)
	a.TempC = maximum(a.TempC, b.TempC)
	if a.InletC == nil {
		a.InletC = b.InletC
	}
	if a.OutletC == nil {
		a.OutletC = b.OutletC
	}
	if a.PowerW == nil || (b.PowerW != nil && b.PowerSource == avalonWallSource && a.PowerSource != avalonWallSource) {
		a.PowerW = b.PowerW
		a.PowerSource = b.PowerSource
	}
	a.Warnings = append(a.Warnings, b.Warnings...)
	selectAvalonBoardMeans(&a)
	return a
}
