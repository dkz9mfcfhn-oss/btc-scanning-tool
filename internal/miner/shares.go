package miner

import "math"

func shareCount(v *float64) *float64 {
	if v == nil || *v < 0 || *v > 9007199254740991 || math.Trunc(*v) != *v {
		return nil
	}
	return v
}
func percent(v *float64) *float64 {
	if v == nil || *v < 0 || *v > 100 {
		return nil
	}
	return v
}
func sharePair(m map[string]any) Reading {
	a, r := shareCount(number(m, "Accepted", "accepted")), shareCount(number(m, "Rejected", "rejected"))
	if a == nil || r == nil {
		return Reading{}
	}
	return Reading{Accepted: a, Rejected: r}
}
func sharePools(v any) Reading {
	rows, ok := v.([]any)
	if !ok || len(rows) == 0 {
		return Reading{}
	}
	a, r := 0.0, 0.0
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			return Reading{}
		}
		pair := sharePair(m)
		if pair.Accepted == nil {
			return Reading{}
		}
		a += *pair.Accepted
		r += *pair.Rejected
	}
	if shareCount(&a) == nil || shareCount(&r) == nil {
		return Reading{}
	}
	return Reading{Accepted: &a, Rejected: &r}
}
func shareCGMiner(m map[string]any) Reading {
	for _, s := range section(m, "SUMMARY") {
		if r := sharePair(s); r.Accepted != nil {
			return r
		}
	}
	v := m["POOLS"]
	if v == nil {
		v = m["pools"]
	}
	return sharePools(v)
}
