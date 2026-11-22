package main

import (
	"fmt"
	"math"
	"minerwatch/internal/store"
	"sort"
	"strings"
	"time"
)

type deviceHistoryStats struct {
	Samples, Online, Valid, Zero int
	Min, Max, Mean               *float64
	First, Last                  time.Time
}

func historyValue(v *float64) bool {
	return v != nil && *v >= 0 && !math.IsNaN(*v) && !math.IsInf(*v, 0)
}
func historySamples24(samples []store.Sample, now time.Time) []store.Sample {
	start := now.Add(-24 * time.Hour)
	out := make([]store.Sample, 0, len(samples))
	for _, s := range samples {
		if !s.At.Before(start) && !s.At.After(now) {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}
func summarizeDeviceHistory(samples []store.Sample) deviceHistoryStats {
	r := deviceHistoryStats{Samples: len(samples)}
	sum := 0.
	for _, s := range samples {
		if r.First.IsZero() || s.At.Before(r.First) {
			r.First = s.At
		}
		if s.At.After(r.Last) {
			r.Last = s.At
		}
		if !s.Online {
			continue
		}
		r.Online++
		if !historyValue(s.RealtimeTH) {
			continue
		}
		v := *s.RealtimeTH
		r.Valid++
		sum += v
		if v == 0 {
			r.Zero++
		}
		if r.Min == nil || v < *r.Min {
			r.Min = num(v)
		}
		if r.Max == nil || v > *r.Max {
			r.Max = num(v)
		}
	}
	if r.Valid > 0 {
		r.Mean = num(sum / float64(r.Valid))
	}
	return r
}
func (l uiLanguage) deviceHistorySummary(samples []store.Sample) string {
	r := summarizeDeviceHistory(samples)
	return fmt.Sprintf(l.text("最近24小时 · 实时算力采样统计（TH/s）\r\n均值 %s    最高 %s    最低 %s\r\n读取成功 %d / %d 次    有效算力 %d 次    零算力 %d 次    缺失/失败 %d 次"), nativeMetric(r.Mean), nativeMetric(r.Max), nativeMetric(r.Min), r.Online, r.Samples, r.Valid, r.Zero, r.Samples-r.Valid)
}
func deviceHistoryReport(samples []store.Sample, now time.Time) string {
	return chinese.deviceHistoryReport(samples, now)
}

func (l uiLanguage) deviceHistoryReport(samples []store.Sample, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, l.text("统计窗口：%s — %s\r\n"), now.Add(-24*time.Hour).Local().Format("01-02 15:04:05"), now.Local().Format("01-02 15:04:05"))
	b.WriteString(l.text("均值按有效实时算力样本计算；蓝线是矿机上报平均算力。读取失败不当作零算力，缺测不连线。\r\n"))
	if len(samples) == 0 {
		b.WriteString(l.text("尚无记录，开始监控后逐步积累24小时历史。\r\n"))
		return b.String()
	}
	fmt.Fprintf(&b, l.text("已有记录：%s — %s；窗口内未采集时段留空。\r\n\r\n"), samples[0].At.Local().Format("01-02 15:04:05"), samples[len(samples)-1].At.Local().Format("01-02 15:04:05"))
	b.WriteString(l.text("全部采样记录（最新在前）\r\n时间 | 实时 TH/s | 矿机平均 TH/s | 状态\r\n"))
	for i := len(samples) - 1; i >= 0; i-- {
		s := samples[i]
		rt, av := "—", "—"
		state := l.text("读取失败")
		if s.Online {
			state = l.text("读取成功")
			if historyValue(s.RealtimeTH) {
				rt = nativeMetric(s.RealtimeTH)
				if *s.RealtimeTH == 0 {
					state = l.text("零算力")
				}
			} else {
				state = l.text("未上报实时算力")
			}
			if historyValue(s.AverageTH) {
				av = nativeMetric(s.AverageTH)
			}
		}
		fmt.Fprintf(&b, "%s | %s | %s | %s\r\n", s.At.Local().Format("01-02 15:04:05"), rt, av, state)
	}
	return b.String()
}
