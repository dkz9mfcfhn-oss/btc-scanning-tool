package miner

import (
	"fmt"
	"minerwatch/internal/telemetry"
	"strings"
)

const btcTemperaturePrefix = "BTC Tools temperature/"

// Preserve BTC Tools' selected ordered values independently of physical boards.
// A numbered temp field is not proof of a board ID or a board average.
func setBTCTemperatures(r *Reading, values []*float64, source string) {
	var sensors []telemetry.Sensor
	for _, s := range r.Temperatures {
		if !strings.HasPrefix(s.Source, btcTemperaturePrefix) {
			sensors = append(sensors, s)
		}
	}
	n := 0
	for _, v := range values {
		if v != nil {
			n++
			r.TempC = maximum(r.TempC, v)
			sensors = append(sensors, telemetry.Sensor{Name: fmt.Sprintf("原始温度 %d", n), ValueC: v, Source: fmt.Sprintf("%s%s/%d", btcTemperaturePrefix, source, n)})
		}
	}
	if n > 0 {
		r.Temperatures = sensors
	}
}
func btcNumberedTemps(m map[string]any) []*float64 {
	var values []*float64
	for i := 1; i <= 256; i++ {
		v, ok := m[fmt.Sprintf("temp%d", i)]
		if !ok {
			break
		}
		if n := tempValue(v); n != nil && *n > 0 {
			values = append(values, n)
		}
	}
	return values
}
func btcCGMinerTemperatures(m map[string]any, r *Reading) {
	stats := section(m, "STATS")
	if len(stats) > 0 {
		if raw, ok := stats[0]["MM ID0"].(string); ok {
			for _, match := range bracket.FindAllStringSubmatch(raw, -1) {
				if match[1] == "MTavg" {
					setBTCTemperatures(r, tempSamples(match[2]), "MM ID0.MTavg")
				}
			}
		}
	}
	if len(stats) > 1 {
		setBTCTemperatures(r, btcNumberedTemps(stats[1]), "STATS[1].tempN")
	}
	var values []*float64
	for _, dev := range section(m, "DEVS") {
		if n := tempValue(dev["Temperature"]); n != nil && *n > -100 {
			values = append(values, n)
		}
	}
	setBTCTemperatures(r, values, "DEVS.Temperature")
}
func btcAntminerTemperatures(m map[string]any, r *Reading) {
	devs := objects(m["devs"])
	if len(devs) == 0 {
		return
	}
	fields := map[string]any{}
	for k, v := range devs[0] {
		fields[k] = v
	}
	for _, part := range strings.Split(text(devs[0], "freq"), ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			fields[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	setBTCTemperatures(r, btcNumberedTemps(fields), "devs[0].freq.tempN")
}

// MTavg describes board means. A generic DEVS/LuCI scalar is not a replacement
// for this sequence, including when the scalar is a firmware placeholder zero.
func btcTemperatureRank(sensors []telemetry.Sensor) int {
	rank := 0
	for _, s := range sensors {
		if strings.HasPrefix(s.Source, btcTemperaturePrefix) {
			rank = 1
			if strings.Contains(s.Source, "MTavg") {
				return 2
			}
		}
	}
	return rank
}
func selectAvalonBoardMeans(r *Reading) {
	if r.Brand != "Avalon" {
		return
	}
	for _, prefix := range []string{"avalon:mm:", "avalon:stats:"} {
		var values []*float64
		for _, b := range r.Boards {
			if strings.HasPrefix(b.ID, prefix) && strings.Contains(b.Chip.Source, "MTavg") && b.Chip.AverageKind == "reported" {
				values = append(values, b.Chip.AverageC)
			}
		}
		valid := false
		for _, v := range values {
			if v != nil {
				valid = true
			}
		}
		if valid {
			setBTCTemperatures(r, values, "Avalon MTavg boards")
			return
		}
	}
}
func hasAvalonMMMeans(r Reading) bool {
	found := false
	for _, b := range r.Boards {
		if !strings.HasPrefix(b.ID, "avalon:mm:") {
			continue
		}
		found = true
		if b.Chip.AverageC == nil || b.Chip.AverageKind != "reported" {
			return false
		}
	}
	return found
}

func avalonZeroTemperatureWarning(r Reading) string {
	if r.Brand != "Avalon" || btcTemperatureRank(r.Temperatures) != 1 {
		return ""
	}
	count := 0
	for _, s := range r.Temperatures {
		if strings.HasPrefix(s.Source, btcTemperaturePrefix) && s.ValueC != nil {
			if *s.ValueC != 0 {
				return ""
			}
			count++
		}
	}
	if count > 0 {
		return "温度接口仅返回0，尚未取得MTavg分板均温；此值不能证明四块板实际为0°C，请采集诊断核对来源"
	}
	return ""
}

// A generic DEVS zero is not the only temperature when a valid independent
// overview reading exists. Do not turn that overview into board averages.
func correctAvalonOverview(r *Reading) {
	if r.Brand != "Avalon" || r.TempC == nil || *r.TempC <= 0 || btcTemperatureRank(r.Temperatures) != 1 {
		return
	}
	found := false
	for _, sensor := range r.Temperatures {
		if strings.HasPrefix(sensor.Source, btcTemperaturePrefix) {
			if !strings.Contains(sensor.Source, "DEVS.Temperature") || sensor.ValueC == nil || *sensor.ValueC != 0 {
				return
			}
			found = true
		}
	}
	if found {
		setBTCTemperatures(r, []*float64{r.TempC}, "Avalon reported overview")
		r.Warnings = append(r.Warnings, "已读取整机温度；DEVS返回0未作为板温，分板均温仍以MTavg日志为准")
	}
}
