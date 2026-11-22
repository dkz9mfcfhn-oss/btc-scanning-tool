package main

import (
	"fmt"
	"math"
	"minerwatch/internal/store"
	"strings"
	"time"
)

// Keep this order aligned with nativeRow. The list view owns horizontal
// scrolling; source text remains complete even when its column is narrower.
var nativeListColumns = []struct {
	Name  string
	Width int32
}{
	{"IP / 机位", 150},
	{"品牌 / 型号", 170},
	{"状态", 80},
	{"实时 TH/s", 100},
	{"平均 TH/s", 100},
	{"温度 °C", 80},
	{"进水 °C", 80},
	{"出水 °C", 80},
	{"拒绝率 %", 100},
	{"接受份额", 110},
	{"24h 趋势", 140},
	{"拒绝份额", 110},
	{"最近采样", 145},
	{"温度明细 °C", 350},
	{"各板PCB均温 °C", 350},
	{"无算力原因", 260},
	{"操作", 100},
	{"矿工名", 220},
	{"连续运行时间", 155},
	{"备注", 220},
	{"功率 W", 100},
}

const powerColumn = 20

// Both layouts retain all physical columns; hidden ones get zero width, so
// custom-drawn trends and the temperature columns keep their original indexes.
func nativeColumnOrder(simple bool) []int32 {
	return []int32{backendColumn, 0, 17, 2, 3, 4, powerColumn, 5, 13, 15, 10, 8, 1, 18, 19, 6, 7, 9, 14, 11, 12}
}

func nativeColumnWidths(simple bool) []int32 {
	widths := make([]int32, len(nativeListColumns))
	for i, col := range nativeListColumns {
		widths[i] = col.Width
		if simple {
			compact := map[int]int32{0: 140, 1: 145, 2: 90, 3: 100, 4: 100, 5: 78, 8: 80, 10: 130, 13: 280, 15: 220, 16: 80, 17: 160}
			if v, ok := compact[i]; ok {
				widths[i] = v
			}
			switch i {
			case 6, 7, 9, 11, 12, 14:
				widths[i] = 0
			}
		}
	}
	return widths
}

func nativeMetric(v *float64) string {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return "—"
	}
	return fmt.Sprintf("%.2f", *v)
}

// nativeRow only presents fresh telemetry as current. The last sample time and
// its power-source metadata remain visible so an unreachable device can still
// be diagnosed without confusing its previous measurements with live values.
func nativeRow(d *Device, now time.Time) []string { return chinese.nativeRow(d, now) }

func (l uiLanguage) nativeRow(d *Device, now time.Time) []string {
	row := []string{"—", "—", l.text("待更新"), "—", "—", "—", "—", "—", "—", "—", "", "—", "—", "—", "—", "", ""}
	row = append(row, l.text("未读取"), "—", "", "—")
	if d == nil {
		return row
	}
	row[0] = d.IP
	row[17] = l.workerDisplay(d)
	row[19] = d.Note
	if _, err := backendURL(d.IP); err == nil {
		row[backendColumn] = l.text("打开后台")
	}
	if label := strings.TrimSpace(d.Label); label != "" {
		row[0] += " / " + label
	}
	if name := strings.TrimSpace(d.Brand + " " + d.Model); name != "" {
		row[1] = name
	}
	s := d.Latest
	if s == nil {
		return row
	}
	if !s.At.IsZero() {
		row[12] = s.At.Local().Format("01-02 15:04:05")
	}

	health := sampleHealthView(s, now)
	row[2], row[15] = l.text(health.Status), l.diagnostic(health.Reason)
	if health.State == "zero" && health.Reason == "实时算力为零，原因未确认" {
		if codes := health.avalonRestartCodes(); codes != "" {
			row[15] = l.text("原因未确认 · 上次重启 ") + codes
		}
	}
	if !fresh(s, now) {
		// A prior power value is neither a current measurement nor a missing
		// reported field; show the same gap as every other stale metric.
		row[8] = "—"
		return row
	}
	row[3] = nativeMetric(s.RealtimeTH)
	row[powerColumn] = l.text("未上报")
	if watts, manual := store.ResolvePower(s.PowerW, s.PowerSource, d.ManualPowerW); watts != nil {
		row[powerColumn] = fmt.Sprintf("%.0f", *watts)
		if manual {
			row[powerColumn] += l.text(" 手动")
		}
	}
	row[4] = nativeMetric(s.AverageTH)
	row[18] = l.runtimeText(s)
	row[5] = nativeMetric(temperatureHeadline(s.TempC, s.Boards))
	row[6] = nativeMetric(s.InletC)
	row[7] = nativeMetric(s.OutletC)
	row[8] = nativeMetric(rejection(s))
	row[9] = nativeMetric(s.Accepted)
	row[11] = nativeMetric(s.Rejected)
	row[13] = compactBoardAverages(s.Boards, false)
	if original := btcTemperatureText(s.Temperatures); original != "" {
		row[13] = original
	}
	row[14] = compactBoardAverages(s.Boards, true)
	return row
}

// matchesDevice searches identifying information only. A missing-power filter
// is useful for online diagnostics, whereas an unavailable/stale device belongs
// to filter 2 and must not be diagnosed from an old power reading.
func matchesDevice(d *Device, search string, filter int, now time.Time) bool {
	if d == nil {
		return false
	}
	needle := strings.ToLower(strings.TrimSpace(search))
	if needle != "" {
		matched := false
		for _, field := range []string{d.IP, d.Brand, d.Model, d.Label, d.Note, strings.Join(d.WorkerNames, " ")} {
			if strings.Contains(strings.ToLower(field), needle) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	live := fresh(d.Latest, now)
	switch filter {
	case 0:
		return true
	case 1:
		return live
	case 2:
		return !live
	case 3:
		return live && rejection(d.Latest) == nil
	case 4:
		return live && sampleHealthView(d.Latest, now).State == "zero"
	default:
		return true
	}
}

func workerDisplay(d *Device) string { return chinese.workerDisplay(d) }

func (l uiLanguage) workerDisplay(d *Device) string {
	if d == nil || d.WorkerReadAt.IsZero() {
		return l.text("未读取")
	}
	value := strings.Join(d.WorkerNames, " / ")
	if value == "" {
		value = l.text("未配置")
	}
	if d.Latest == nil || !d.Latest.At.Equal(d.WorkerReadAt) {
		value += l.text("（上次读取）")
	}
	return value
}

// The device reports mining runtime, which can reset when its mining process restarts.
func (l uiLanguage) runtimeText(s *store.Sample) string {
	if s.ElapsedSeconds == nil {
		return l.text("未上报")
	}
	n := int64(*s.ElapsedSeconds)
	return fmt.Sprintf("%dd %02dh %02dm %02ds", n/86400, n/3600%24, n/60%60, n%60)
}

const hiddenMinersFilter = 5

func listIncludesDevice(d *Device, query string, filter int, now time.Time) bool {
	if d == nil || d.Hidden != (filter == hiddenMinersFilter) {
		return false
	}
	if filter == hiddenMinersFilter {
		filter = 0
	}
	return matchesDevice(d, query, filter, now)
}

func configuredColumnWidths(c Config) []int32 {
	widths := nativeColumnWidths(!c.FullColumns)
	for i, col := range nativeListColumns {
		if i == 0 {
			continue // Keep a visible IP column so rows remain identifiable.
		}
		visible, set := c.ColumnVisibility[col.Name]
		if !set {
			continue
		}
		if !visible {
			widths[i] = 0
		} else if widths[i] == 0 {
			widths[i] = col.Width
		}
	}
	return widths
}
