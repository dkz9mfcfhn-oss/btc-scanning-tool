package miner

import (
	"fmt"
	"strings"
	"unicode"
)

func workerName(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 256 {
		return ""
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return s
}
func poolWorkers(v any) Reading {
	r := Reading{}
	if v == nil {
		return r
	}
	switch v.(type) {
	case []any:
		r.WorkersKnown = len(v.([]any)) == 0
	case map[string]any:
	default:
		return r
	}
	seen := map[string]bool{}
	for _, p := range objects(v) {
		for _, key := range []string{"User", "user", "worker", "worker_name", "user_name"} {
			if _, ok := p[key]; ok {
				r.WorkersKnown = true
			}
		}
		n := workerName(text(p, "User", "user", "worker", "worker_name", "user_name"))
		if n != "" && !seen[n] {
			r.WorkerNames = append(r.WorkerNames, n)
			seen[n] = true
		}
	}
	return r
}
func avalonConfigWorkers(body []byte) Reading {
	raw := strings.TrimSpace(string(body))
	if strings.HasPrefix(raw, "CGConfCallback(") {
		raw = strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(raw, "CGConfCallback("), ";"), ")")
	}
	m, e := decode([]byte(raw))
	if e != nil {
		return Reading{}
	}
	r := Reading{}
	seen := map[string]bool{}
	for i := 1; i <= 3; i++ {
		v, ok := m[fmt.Sprintf("worker%d", i)]
		if !ok {
			continue
		}
		r.WorkersKnown = true
		s, _ := v.(string)
		n := workerName(s)
		if n != "" && !seen[n] {
			r.WorkerNames = append(r.WorkerNames, n)
			seen[n] = true
		}
	}
	return r
}
