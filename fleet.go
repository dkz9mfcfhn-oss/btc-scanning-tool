package main

import (
	"math"
	"minerwatch/internal/store"
	"sort"
	"time"
)

func validCount(v *float64) bool {
	return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0 && *v <= 9007199254740991 && math.Trunc(*v) == *v
}
func validShares(s *store.Sample) bool {
	return s != nil && validCount(s.Accepted) && validCount(s.Rejected)
}
func sharePercent(a, r float64) *float64 {
	if a+r <= 0 || math.IsInf(a+r, 0) {
		return nil
	}
	return num(100 * r / (a + r))
}
func rejection(s *store.Sample) *float64 {
	if s == nil {
		return nil
	}
	if validShares(s) {
		return sharePercent(*s.Accepted, *s.Rejected)
	}
	if s.RejectPercent != nil && !math.IsNaN(*s.RejectPercent) && *s.RejectPercent >= 0 && *s.RejectPercent <= 100 {
		return s.RejectPercent
	}
	return nil
}

// Center the measured range in the plot. Allow visual padding beyond the
// percentage limits so constant readings at 0% and 100% are centered too.
func rejectRateAxis(points []FleetPoint) (low, high float64) {
	minRate, maxRate := math.Inf(1), math.Inf(-1)
	for _, p := range points {
		if v := p.RejectPercent; v != nil {
			minRate = min(minRate, *v)
			maxRate = max(maxRate, *v)
		}
	}
	if minRate > maxRate {
		return 0, 1
	}
	center := (minRate + maxRate) / 2
	halfSpan := max(0.05, (maxRate-minRate)*0.75)
	return center - halfSpan, center + halfSpan
}

func rejectRateTicks(low, high float64, plotHeight int32) []float64 {
	first, last := max(0, low), min(100, high)
	visibleHeight := float64(plotHeight) * (last - first) / (high - low)
	switch {
	case visibleHeight < 18:
		return []float64{(low + high) / 2}
	case visibleHeight < 36:
		return []float64{first, last}
	default:
		return []float64{first, (first + last) / 2, last}
	}
}

// One current sample per IP at each time step. Carry measurements only through
// their freshness window, never turn manual refreshes into extra miners. The
// rate is a cumulative share-count ratio, not an interval delta or mean of %s.
func fleetHistory(records []store.Record, known map[string]*Device) []FleetPoint {
	metrics := make([]store.MetricRecord, 0, len(records))
	for _, r := range records {
		s := r.Sample
		metrics = append(metrics, store.MetricRecord{IP: r.IP, At: s.At, IntervalSeconds: s.IntervalSeconds, Online: s.Online, RealtimeTH: s.RealtimeTH, AverageTH: s.AverageTH, Accepted: s.Accepted, Rejected: s.Rejected, RejectPercent: s.RejectPercent})
	}
	return fleetMetricHistory(metrics, known)
}
func fleetMetricHistory(records []store.MetricRecord, known map[string]*Device) []FleetPoint {
	rs := make([]store.MetricRecord, 0, len(records))
	step := int64(1800)
	for _, r := range records {
		if d, ok := known[r.IP]; ok && d != nil && !d.Hidden {
			rs = append(rs, r)
			sample := r.Sample()
			n := int64(sampleInterval(&sample) / time.Second)
			if n < step {
				step = n
			}
		}
	}
	if len(rs) == 0 {
		return nil
	}
	if step < 30 {
		step = 30
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].At.Before(rs[j].At) })
	first, last := rs[0].At.Unix()/step, rs[len(rs)-1].At.Unix()/step
	latest := map[string]*Device{}
	seen := map[string]bool{}
	index := 0
	out := []FleetPoint{}
	for k := first; k <= last; k++ {
		at := time.Unix((k+1)*step-1, 0).UTC()
		if at.After(rs[len(rs)-1].At) {
			at = rs[len(rs)-1].At
		}
		for index < len(rs) && !rs[index].At.After(at) {
			r := rs[index]
			sample := r.Sample()
			latest[r.IP] = &Device{Latest: &sample}
			seen[r.IP] = true
			index++
		}
		devices := make([]*Device, 0, len(latest))
		observed := 0
		for _, d := range latest {
			devices = append(devices, d)
			if currentSampleTime(d.Latest, at) {
				observed++
			}
		}
		t := totals(devices, at)
		p := FleetPoint{At: at, Count: len(seen), Online: t.Online, Reported: t.RealtimeReported, Observed: observed, RejectPercent: t.RejectPercent, RejectReported: t.RejectReported, IntervalSeconds: int(step)}
		if t.RealtimeReported > 0 {
			p.RealtimeTH = num(t.RealtimeTH)
		}
		out = append(out, p)
	}
	return out
}
