//go:build windows

package main

import (
	"fmt"
	"time"
)

func drawFleetChart(dc uintptr, bounds rect) {
	a := desktop.app
	now := time.Now()
	start := now.Add(-24 * time.Hour)
	points := a.fleetPoints(now)
	label(dc, 20, 3, tr("最近 24 小时"), desktop.font, ink)
	labelBox(dc, rect{155, 2, bounds.R - 20, 26}, tr("点击标题切换单机曲线 →"), ui.smallFont, blue, 2)
	if len(points) == 0 {
		label(dc, 20, 60, tr("开始采样后逐步积累；缺失数据留空。"), desktop.font, muted)
		return
	}
	width := (bounds.R - 40) / 3
	compact := width < 202
	rateDivisor, rateUnit := 1.0, "TH/s"
	if compact {
		for _, p := range points {
			if p.RealtimeTH != nil && *p.RealtimeTH >= 10000 {
				rateDivisor, rateUnit = 1000, "PH/s"
				break
			}
		}
	}
	titles := []string{tr("总算力 · ") + rateUnit, tr("在线机器 · 台"), tr("拒绝率 · % / 累计份额")}
	if compact {
		titles[2] = tr("拒绝率 · %")
	}
	colors := []uintptr{teal, blue, warningColor}
	values := func(p FleetPoint, k int) *float64 {
		switch k {
		case 0:
			return p.RealtimeTH
		case 1:
			if p.Observed > 0 {
				return num(float64(p.Online))
			}
		case 2:
			return p.RejectPercent
		}
		return nil
	}
	for k := 0; k < 3; k++ {
		left := 20 + int32(k)*width
		right := left + width - 18
		top, bottom := int32(82), bounds.B-26
		minVal, maxVal := 0.0, 1.0
		if k == 2 {
			minVal, maxVal = rejectRateAxis(points)
		} else {
			for _, p := range points {
				if v := values(p, k); v != nil && *v > maxVal {
					maxVal = *v
				}
			}
			if k == 0 {
				maxVal *= 1.1
			}
		}
		labelBox(dc, rect{left + 10, 34, right - 8, 53}, titles[k], ui.smallFont, muted, 0)
		divisor := 1.0
		if k == 0 {
			divisor = rateDivisor
		}
		latest := "—"
		if v := values(points[len(points)-1], k); v != nil {
			latest = fmt.Sprintf("%.2f", *v/divisor)
			if k == 1 {
				latest = fmt.Sprintf("%.0f", *v)
			}
		}
		valueRight := right - 66
		if compact {
			valueRight = right - 4
		} else {
			labelBox(dc, rect{right - 64, 54, right, 74}, points[len(points)-1].At.Local().Format("15:04"), ui.smallFont, muted, 2)
		}
		labelBox(dc, rect{left + 10, 54, valueRight, 74}, latest, desktop.font, colors[k], 0)
		axisX := left + 45
		x := func(t time.Time) int32 { return axisX + int32(t.Sub(start).Seconds()/86400*float64(right-axisX-3)) }
		y := func(v float64) int32 { return bottom - int32((v-minVal)/(maxVal-minVal)*float64(bottom-top)) }
		ticks := []float64{0, maxVal / 2, maxVal}
		tickFormat := "%.0f"
		if k == 2 {
			ticks = rejectRateTicks(minVal, maxVal, bottom-top)
			tickFormat = "%.2f"
		} else if bottom-top < 36 {
			ticks = []float64{0, maxVal}
		}
		for _, v := range ticks {
			line(dc, axisX, y(v), right, y(v), mixColor(panel, border, 65))
			text := fmt.Sprintf(tickFormat, v/divisor)
			labelBox(dc, rect{left, y(v) - 8, axisX - 4, y(v) + 8}, text, ui.smallFont, muted, 2)
		}
		label(dc, axisX, bottom+7, start.Local().Format("15:04"), ui.smallFont, muted)
		label(dc, right-32, bottom+7, tr("现在"), ui.smallFont, muted)
		pen := call(gdi, "CreatePen", 0, 2, colors[k])
		old := call(gdi, "SelectObject", dc, pen)
		prev := time.Time{}
		for _, p := range points {
			v := values(p, k)
			if v == nil {
				prev = time.Time{}
				continue
			}
			xx, yy := x(p.At), y(*v)
			if prev.IsZero() || p.At.Sub(prev) > time.Duration(p.IntervalSeconds)*2*time.Second {
				call(gdi, "MoveToEx", dc, uintptr(xx), uintptr(yy), 0)
				fill(dc, rect{xx - 2, yy - 2, xx + 2, yy + 2}, colors[k])
			} else {
				call(gdi, "LineTo", dc, uintptr(xx), uintptr(yy))
			}
			prev = p.At
		}
		call(gdi, "SelectObject", dc, old)
		call(gdi, "DeleteObject", pen)
	}
}
