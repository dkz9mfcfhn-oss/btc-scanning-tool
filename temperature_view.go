package main

import (
	"fmt"
	"strings"

	"minerwatch/internal/telemetry"
)

// These presentation helpers deliberately do not call telemetry.Complete:
// display never turns a lone sensor or a min/max pair into a board mean.
func (l uiLanguage) temperatureValue(v *float64) string {
	if telemetry.Valid(v) == nil {
		return l.text("未上报")
	}
	return fmt.Sprintf("%.2f", *v)
}

func (l uiLanguage) temperatureValueUnit(v *float64) string {
	text := l.temperatureValue(v)
	if text != l.text("未上报") {
		text += " °C"
	}
	return text
}

// Show embedded line breaks visibly, so one source field cannot break a table
// row or introduce bare LF lines in the Windows CRLF text report.
func (l uiLanguage) temperatureText(s string) string {
	if strings.TrimSpace(s) == "" {
		return l.text("未上报")
	}
	s = strings.ReplaceAll(s, "\r\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	return strings.ReplaceAll(s, "\n", `\n`)
}

func (l uiLanguage) temperatureBoardLabel(board telemetry.BoardTemperature) string {
	return l.temperatureText(board.Name) + " [ID: " + l.temperatureText(board.ID) + "]"
}

func (l uiLanguage) temperatureAverageKind(series telemetry.TemperatureSeries) string {
	if telemetry.Valid(series.AverageC) == nil {
		return l.text("未上报")
	}
	switch series.AverageKind {
	case "reported":
		return l.text("固件")
	case "sensor_mean":
		return l.text("采样")
	case "":
		return l.text("口径未上报")
	default:
		return l.text("口径未知: ") + l.temperatureText(series.AverageKind)
	}
}

func temperatureValidCount(series telemetry.TemperatureSeries) int {
	count := 0
	for _, value := range series.SensorsC {
		if telemetry.Valid(value) != nil {
			count++
		}
	}
	return count
}

func boardAverageSummary(boards []telemetry.BoardTemperature, pcb bool) string {
	return chinese.boardAverageSummary(boards, pcb)
}

func (l uiLanguage) boardAverageSummary(boards []telemetry.BoardTemperature, pcb bool) string {
	if len(boards) == 0 {
		return l.text("未上报")
	}
	parts := make([]string, 0, len(boards))
	for _, board := range boards {
		series := board.Chip
		if pcb {
			series = board.PCB
		}
		value := l.temperatureValue(series.AverageC)
		if value != l.text("未上报") {
			value += "（" + l.temperatureAverageKind(series) + "）"
		}
		parts = append(parts, l.temperatureBoardLabel(board)+": "+value)
	}
	return strings.Join(parts, "；")
}

// temperatureRows is the native temperature-detail table's ten-column model.
// Column 8 counts valid readings out of all reported slots, including nil slots.
func temperatureRows(boards []telemetry.BoardTemperature) [][]string {
	return chinese.temperatureRows(boards)
}

func (l uiLanguage) temperatureRows(boards []telemetry.BoardTemperature) [][]string {
	rows := make([][]string, 0, len(boards))
	for _, board := range boards {
		sources := []string{
			l.text("芯片: ") + l.temperatureText(board.Chip.Source),
			"PCB: " + l.temperatureText(board.PCB.Source),
		}
		for i, sensor := range board.Other {
			sources = append(sources, fmt.Sprintf("Other[%d] %s: %s", i, l.temperatureText(sensor.Name), l.temperatureText(sensor.Source)))
		}
		rows = append(rows, []string{
			l.temperatureBoardLabel(board),
			l.temperatureValue(board.Chip.AverageC),
			l.temperatureValue(board.Chip.MinC),
			l.temperatureValue(board.Chip.MaxC),
			l.temperatureValue(board.PCB.AverageC),
			l.temperatureValue(board.PCB.MinC),
			l.temperatureValue(board.PCB.MaxC),
			l.text("芯片: ") + l.temperatureAverageKind(board.Chip) + "；PCB: " + l.temperatureAverageKind(board.PCB),
			fmt.Sprintf(l.text("芯片 %d/%d；PCB %d/%d"), temperatureValidCount(board.Chip), len(board.Chip.SensorsC), temperatureValidCount(board.PCB), len(board.PCB.SensorsC)),
			strings.Join(sources, "；"),
		})
	}
	return rows
}

// temperatureReport keeps every board entry, sensor index (including nil), and
// provenance field. It is plain Windows text suitable for an EDIT control or
// copying to a local diagnostic file. Callers decide whether the sample is
// current and label archived/stale reports separately.
func temperatureReport(boards []telemetry.BoardTemperature, sensors []telemetry.Sensor) string {
	return chinese.temperatureReport(boards, sensors)
}

func (l uiLanguage) temperatureReport(boards []telemetry.BoardTemperature, sensors []telemetry.Sensor) string {
	lines := []string{
		l.text("温度明细（单位 °C）"),
		l.text("均温口径：固件 = 接口上报均温；采样 = 模型提供的有效传感器采样均值。"),
		l.text("仅显示已采集数据；未上报位置保留。SensorsC 索引从 0 开始。"),
		"",
		l.text("【各板温度】"),
	}
	if len(boards) == 0 {
		lines = append(lines, l.text("未上报"))
	}
	appendSeries := func(name string, series telemetry.TemperatureSeries) {
		lines = append(lines,
			name+l.text("均温: ")+l.temperatureValueUnit(series.AverageC),
			name+l.text("范围: ")+l.temperatureValueUnit(series.MinC)+" ～ "+l.temperatureValueUnit(series.MaxC),
			name+l.text("均温口径: ")+l.temperatureAverageKind(series)+" [AverageKind: "+l.temperatureText(series.AverageKind)+"]",
			name+l.text("字段来源: ")+l.temperatureText(series.Source),
			fmt.Sprintf(l.text("%s有效采样点数: %d/%d"), name, temperatureValidCount(series), len(series.SensorsC)),
		)
		if len(series.SensorsC) == 0 {
			lines = append(lines, name+l.text(" SensorsC: 未上报"))
		}
		for i, value := range series.SensorsC {
			lines = append(lines, fmt.Sprintf("%s SensorsC[%d]: %s", name, i, l.temperatureValueUnit(value)))
		}
	}
	for i, board := range boards {
		lines = append(lines, "", fmt.Sprintf(l.text("板记录 %d"), i+1), l.text("板号 ID: ")+l.temperatureText(board.ID), l.text("名称 Name: ")+l.temperatureText(board.Name))
		appendSeries(l.text("芯片"), board.Chip)
		appendSeries("PCB", board.PCB)
		if len(board.Other) == 0 {
			lines = append(lines, l.text("Other: 未上报"))
		}
		for index, sensor := range board.Other {
			lines = append(lines, fmt.Sprintf(l.text("Other[%d]: 名称 Name: %s；温度 ValueC: %s；字段来源 Source: %s"), index, l.temperatureText(sensor.Name), l.temperatureValueUnit(sensor.ValueC), l.temperatureText(sensor.Source)))
		}
	}
	lines = append(lines, "", l.text("【整机温度传感器】"))
	if len(sensors) == 0 {
		lines = append(lines, l.text("未上报"))
	}
	for i, sensor := range sensors {
		lines = append(lines, fmt.Sprintf(l.text("Sensors[%d]: 名称 Name: %s；温度 ValueC: %s；字段来源 Source: %s"), i, l.temperatureText(sensor.Name), l.temperatureValueUnit(sensor.ValueC), l.temperatureText(sensor.Source)))
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

// Default views show the most complete board source as a single set. Raw detail
// retains every protocol record, so ambiguous board numbering is never merged.
func preferredTemperatureBoards(boards []telemetry.BoardTemperature) []telemetry.BoardTemperature {
	groups := map[string][]telemetry.BoardTemperature{}
	order := []string{}
	for _, b := range boards {
		group := "other"
		for _, prefix := range []string{"antminer:chain:", "antminer:legacy:", "byteon:chain:", "whatsminer:v3:", "whatsminer:legacy:", "avalon:mm:", "avalon:stats:"} {
			if strings.HasPrefix(b.ID, prefix) {
				group = prefix
				break
			}
		}
		if _, ok := groups[group]; !ok {
			order = append(order, group)
		}
		groups[group] = append(groups[group], b)
	}
	// Explicit chain records establish board identity. Legacy temp suffixes can
	// denote sensor positions; their greater count must not invent extra boards.
	if chains := groups["antminer:chain:"]; len(chains) > 0 {
		for _, b := range chains {
			if b.Chip.AverageC != nil || b.Chip.MaxC != nil || b.PCB.MaxC != nil || len(b.Other) > 0 {
				return chains
			}
		}
	}
	best := ""
	bestScore := -1
	for _, group := range order {
		score := 0
		for _, b := range groups[group] {
			if b.Chip.MaxC != nil || b.PCB.MaxC != nil || b.Chip.AverageC != nil {
				score += 10
			}
			if b.Chip.AverageC != nil {
				score += 20
				if b.Chip.AverageKind == "reported" {
					score += 5
				}
			}
			if b.PCB.AverageC != nil {
				score += 10
			}
		}
		if score > bestScore {
			best, bestScore = group, score
		}
	}
	return groups[best]
}

func (l uiLanguage) simpleBoardName(b telemetry.BoardTemperature) string {
	id := b.ID
	if strings.HasPrefix(id, "avalon:mm:") {
		parts := strings.Split(id, ":")
		if len(parts) == 5 {
			return l.text("模块") + parts[2] + l.text(" · 板") + parts[4]
		}
	}
	for _, key := range []string{":slot:", ":Slot:", ":index:", ":id:", ":ASC:", ":board:"} {
		if at := strings.Index(id, key); at >= 0 {
			n := strings.SplitN(id[at+len(key):], ":", 2)[0]
			return l.text("板") + n
		}
	}
	if strings.HasPrefix(id, "antminer:legacy:") {
		return l.text("板") + strings.TrimPrefix(id, "antminer:legacy:")
	}
	if b.Name != "" {
		return l.temperatureText(b.Name)
	}
	if b.ID != "" {
		return l.text("板") + l.temperatureText(b.ID)
	}
	return l.text("板号未上报")
}
func compactBoardAverages(boards []telemetry.BoardTemperature, pcb bool) string {
	return chinese.compactBoardAverages(boards, pcb)
}

func (l uiLanguage) compactBoardAverages(boards []telemetry.BoardTemperature, pcb bool) string {
	parts := []string{}
	chosen := preferredTemperatureBoards(boards)
	modules := map[string]bool{}
	for _, b := range chosen {
		if strings.HasPrefix(b.ID, "avalon:mm:") {
			v := strings.Split(b.ID, ":")
			if len(v) == 5 {
				modules[v[2]] = true
			}
		}
	}
	for _, b := range chosen {
		s := b.Chip
		if pcb {
			s = b.PCB
		}
		value := "—"
		if telemetry.Valid(s.AverageC) != nil {
			value = fmt.Sprintf("%.1f", *s.AverageC)
			if s.AverageKind == "sensor_mean" {
				value += "*"
			}
		}
		if value == "—" && !pcb {
			var points []string
			for _, point := range s.SensorsC {
				if point != nil {
					points = append(points, fmt.Sprintf("%.1f", *point))
				}
			}
			for _, sensor := range b.Other {
				if sensor.ValueC != nil {
					points = append(points, fmt.Sprintf("%.1f", *sensor.ValueC))
				}
			}
			if len(points) > 0 {
				value = strings.Join(points, "/") + l.text("（测温）")
			}
		}
		name := l.simpleBoardName(b)
		if len(modules) == 1 && strings.HasPrefix(b.ID, "avalon:mm:") {
			v := strings.Split(b.ID, ":")
			if len(v) == 5 {
				name = l.text("板") + v[4]
			}
		}
		parts = append(parts, name+" "+value)
	}
	if len(parts) == 0 {
		return l.text("未上报")
	}
	return strings.Join(parts, "  |  ")
}
func simpleTemperatureRows(boards []telemetry.BoardTemperature) [][]string {
	return chinese.simpleTemperatureRows(boards)
}

func (l uiLanguage) simpleTemperatureRows(boards []telemetry.BoardTemperature) [][]string {
	out := [][]string{}
	for _, b := range preferredTemperatureBoards(boards) {
		avg := func(s telemetry.TemperatureSeries) string {
			v := l.temperatureValue(s.AverageC)
			if s.AverageC != nil && s.AverageKind == "sensor_mean" {
				v += "*"
			}
			return v
		}
		out = append(out, []string{l.simpleBoardName(b), avg(b.Chip), l.temperatureValue(b.Chip.MinC), l.temperatureValue(b.Chip.MaxC), avg(b.PCB), l.temperatureValue(b.PCB.MinC), l.temperatureValue(b.PCB.MaxC)})
	}
	return out
}
func (l uiLanguage) simpleTemperatureReport(boards []telemetry.BoardTemperature, sensors []telemetry.Sensor) string {
	lines := []string{l.text("其他温度")}
	for _, s := range sensors {
		lines = append(lines, l.temperatureText(s.Name)+"："+l.temperatureValueUnit(s.ValueC))
	}
	for _, b := range preferredTemperatureBoards(boards) {
		for _, s := range b.Other {
			lines = append(lines, l.simpleBoardName(b)+" · "+l.temperatureText(s.Name)+"："+l.temperatureValueUnit(s.ValueC))
		}
	}
	if len(lines) == 1 {
		lines = append(lines, l.text("未上报其他温度"))
	}
	lines = append(lines, "", l.text("* 表示已上报测温点的均值，不代表固件直接上报的整板均温。"), l.text("缺失显示“未上报”，不填零。完整测温点可点击右上角查看。"))
	return strings.Join(lines, "\r\n")
}

// Original scanner temperature sequence; it deliberately carries no board IDs.
func btcTemperatureText(sensors []telemetry.Sensor) string {
	var parts []string
	for _, s := range sensors {
		if strings.HasPrefix(s.Source, "BTC Tools temperature/") && s.ValueC != nil {
			parts = append(parts, fmt.Sprintf("%.1f", *s.ValueC))
		}
	}
	return strings.Join(parts, " / ")
}
func displayTemperatureBoards(boards []telemetry.BoardTemperature, sensors []telemetry.Sensor) []telemetry.BoardTemperature {
	if btcTemperatureText(sensors) == "" {
		return boards
	}
	var out []telemetry.BoardTemperature
	for _, b := range boards {
		// Legacy numeric suffixes aren't board identities. Preserve in full raw report,
		// but do not show them as physical boards alongside the original temperature list.
		if !strings.HasPrefix(b.ID, "antminer:legacy:") {
			out = append(out, b)
		}
	}
	return out
}

// The overview must not retain a generic zero when the same sample contains
// Avalon board telemetry. Prefer reported maxima; use means only as fallback.
func temperatureHeadline(value *float64, boards []telemetry.BoardTemperature) *float64 {
	out := telemetry.Valid(value)
	for _, b := range preferredTemperatureBoards(boards) {
		if !strings.HasPrefix(b.ID, "avalon:") {
			continue
		}
		candidate := telemetry.Valid(b.Chip.MaxC)
		if candidate == nil {
			candidate = telemetry.Valid(b.Chip.AverageC)
		}
		if candidate != nil && (out == nil || *candidate > *out) {
			out = candidate
		}
	}
	return out
}
