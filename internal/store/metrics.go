package store

import "time"

// Compact metric snapshots exclude boards, sensor arrays, warnings and fault evidence.
type MetricRecord struct {
	IP                                                       string
	At                                                       time.Time
	IntervalSeconds                                          int
	Online                                                   bool
	RealtimeTH, AverageTH, Accepted, Rejected, RejectPercent *float64
}

func (m MetricRecord) Sample() Sample {
	return Sample{At: m.At, IntervalSeconds: m.IntervalSeconds, Online: m.Online, RealtimeTH: m.RealtimeTH, AverageTH: m.AverageTH, Accepted: m.Accepted, Rejected: m.Rejected, RejectPercent: m.RejectPercent}
}
func (s *Store) Metrics(since time.Time) []MetricRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, ss := range s.samples {
		for _, v := range ss {
			if !v.At.Before(since) {
				count++
			}
		}
	}
	out := make([]MetricRecord, 0, count)
	for ip, ss := range s.samples {
		for _, v := range ss {
			if !v.At.Before(since) {
				out = append(out, MetricRecord{ip, v.At, v.IntervalSeconds, v.Online, cloneFloat(v.RealtimeTH), cloneFloat(v.AverageTH), cloneFloat(v.Accepted), cloneFloat(v.Rejected), cloneFloat(v.RejectPercent)})
			}
		}
	}
	return out
}
func (s *Store) Revision() uint64 { s.mu.RLock(); defer s.mu.RUnlock(); return s.revision }

// MetricHistory serves per-row sparklines without cloning board sensor arrays.
func (s *Store) MetricHistory(ip string, since time.Time) []Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Sample, 0, len(s.samples[ip]))
	for _, v := range s.samples[ip] {
		if !v.At.Before(since) {
			out = append(out, Sample{At: v.At, IntervalSeconds: v.IntervalSeconds, Online: v.Online, RealtimeTH: cloneFloat(v.RealtimeTH), AverageTH: cloneFloat(v.AverageTH)})
		}
	}
	return out
}
