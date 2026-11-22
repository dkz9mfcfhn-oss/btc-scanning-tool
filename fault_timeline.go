package main

import (
	"fmt"
	"minerwatch/internal/store"
	"sort"
	"strings"
	"time"
)

type faultPeriod struct {
	Start, End    time.Time
	State, Reason string
	EndReason     string
	Open          bool
}

func faultTimeline(samples []store.Sample, now time.Time) []faultPeriod {
	ss := append([]store.Sample(nil), samples...)
	sort.SliceStable(ss, func(i, j int) bool { return ss[i].At.Before(ss[j].At) })
	var out []faultPeriod
	var active *faultPeriod
	var last *store.Sample
	finish := func(end time.Time, open bool, reason string) {
		if active != nil {
			active.End = end
			active.Open = open
			active.EndReason = reason
			out = append(out, *active)
			active = nil
		}
	}
	for i := range ss {
		s := &ss[i]
		if s.At.After(now) {
			continue
		}
		if last != nil && s.At.Sub(last.At) > 2*sampleInterval(last) {
			finish(last.At.Add(sampleInterval(last)), false, "后续采集缺失，恢复时间未知")
		}
		h := sampleHealthView(s, s.At)
		state := h.Status
		if h.State == "normal" {
			finish(s.At, false, "已恢复正常")
		} else if active == nil || active.State != state {
			finish(s.At, false, "状态变化")
			active = &faultPeriod{Start: s.At, State: state, Reason: h.Reason}
		}
		last = s
	}
	if active != nil && last != nil {
		end := last.At.Add(sampleInterval(last))
		open := now.Sub(last.At) <= 2*sampleInterval(last)
		if open {
			end = now
		} else if end.After(now) {
			end = now
		}
		finish(end, open, "后续采集缺失，恢复时间未知")
	}
	return out
}
func (l uiLanguage) faultTimelineText(samples []store.Sample, now time.Time) string {
	periods := faultTimeline(samples, now)
	var b strings.Builder
	b.WriteString(l.text("故障时间线 · 最近保留记录\r\n采集失败不等同于已确认硬件故障；监控空档不推定故障持续。\r\n\r\n"))
	if len(periods) == 0 {
		b.WriteString(l.text("没有记录到异常。\r\n"))
	}
	for i := len(periods) - 1; i >= 0; i-- {
		p := periods[i]
		end := p.End.Local().Format("01-02 15:04:05")
		if p.Open {
			end = l.text("持续中")
		} else {
			end += "（" + l.diagnostic(p.EndReason) + "）"
		}
		fmt.Fprintf(&b, l.text("%s → %s  |  %s  |  持续 %s\r\n原因：%s\r\n\r\n"), p.Start.Local().Format("01-02 15:04:05"), end, l.text(p.State), p.End.Sub(p.Start).Round(time.Second), l.diagnostic(p.Reason))
	}
	return b.String()
}
