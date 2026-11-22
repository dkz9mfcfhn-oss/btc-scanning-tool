package store

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HashrateAverage records actual coverage of a time window. A measured zero
// has positive coverage, while unavailable measurements have zero coverage.
type HashrateAverage struct {
	MeanTH                               float64
	Covered                              time.Duration
	EnergyKWh, MatchedKWh, MatchedWorkEH float64
	ManualKWh                            float64
	PowerCovered, Matched                time.Duration
	ManualCovered                        time.Duration
}

const maxHashrateInterval = 30 * time.Minute

type hashrateObservation struct {
	At              time.Time `json:"at"`
	IntervalSeconds int       `json:"intervalSeconds"`
	Online          bool      `json:"online"`
	RealtimeTH      *float64  `json:"realtimeTH"`
	PowerW          *float64  `json:"powerW"`
	ManualPowerW    *float64  `json:"manualPowerW"`
	PowerSource     string    `json:"powerSource"`
}

type hashrateWindow struct {
	start, end time.Time
	previous   hashrateObservation
	weighted   float64
	covered    time.Duration
	power      HashrateAverage
}

func (w *hashrateWindow) add(next hashrateObservation) {
	w.accumulate(next.At)
	w.previous = next
}

func (w *hashrateWindow) accumulate(nextAt time.Time) {
	s := w.previous
	if !s.Online {
		return
	}
	seconds := min(s.IntervalSeconds, int(maxHashrateInterval/time.Second))
	if seconds < 30 {
		seconds = 300 // Records older than the configurable interval used five minutes.
	}
	from := s.At
	if from.Before(w.start) {
		from = w.start
	}
	until := s.At.Add(time.Duration(seconds) * time.Second)
	if nextAt.Before(until) {
		until = nextAt
	}
	if w.end.Before(until) {
		until = w.end
	}
	covered := until.Sub(from)
	if covered <= 0 {
		return
	}
	hasRate := validMeterValue(s.RealtimeTH)
	if hasRate {
		w.covered += covered
		w.weighted += *s.RealtimeTH * covered.Seconds()
	}
	if watts, manual := ResolvePower(s.PowerW, s.PowerSource, s.ManualPowerW); watts != nil && (manual || isInputPower(s.PowerSource)) {
		kwh := *watts * covered.Hours() / 1000
		w.power.EnergyKWh += kwh
		w.power.PowerCovered += covered
		if manual {
			w.power.ManualKWh += kwh
			w.power.ManualCovered += covered
		}
		if hasRate {
			w.power.MatchedWorkEH += *s.RealtimeTH * covered.Seconds() / 1e6
			w.power.MatchedKWh += kwh
			w.power.Matched += covered
		}
	}
}

func validMeterValue(v *float64) bool {
	return v != nil && *v >= 0 && !math.IsInf(*v, 0)
}

func isInputPower(source string) bool {
	return !strings.Contains(source, "P_P") && !strings.Contains(source, "power_usage")
}

// ResolvePower prefers firmware power usable for electricity estimates. A
// manual fallback may replace a reading that represents only PSU output or
// efficiency. If no fallback was configured, preserve the reported value for display.
func ResolvePower(reported *float64, source string, fallback *float64) (watts *float64, manual bool) {
	if validMeterValue(reported) && (isInputPower(source) || !validMeterValue(fallback)) {
		return reported, false
	}
	if validMeterValue(fallback) {
		return fallback, true
	}
	return nil, false
}

func (w *hashrateWindow) average() HashrateAverage {
	w.accumulate(w.end)
	result := w.power
	if w.covered > 0 && !math.IsInf(w.weighted, 0) {
		result.MeanTH, result.Covered = w.weighted/w.covered.Seconds(), w.covered
	}
	return result
}

// RecentHashrateAverages calculates the rolling average from the retained
// in-memory samples, without disk reads or copies of the sample history.
func (s *Store) RecentHashrateAverages(ips []string, start, end time.Time) map[string]HashrateAverage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]HashrateAverage, len(ips))
	since := start.Add(-maxHashrateInterval)
	for _, ip := range ips {
		samples := s.samples[ip]
		first := sort.Search(len(samples), func(i int) bool { return !samples[i].At.Before(since) })
		window := hashrateWindow{start: start, end: end}
		for _, sample := range samples[first:] {
			if !sample.At.Before(end) {
				break
			}
			window.add(hashrateObservation{sample.At, sample.IntervalSeconds, sample.Online, sample.RealtimeTH, sample.PowerW, sample.ManualPowerW, sample.PowerSource})
		}
		result[ip] = window.average()
	}
	return result
}

// RetainedHashrateAverages reads the JSONL files because a late morning report's
// fixed window can start before the 25-hour in-memory retention boundary.
func (s *Store) RetainedHashrateAverages(ctx context.Context, ips []string, start, end time.Time) (map[string]HashrateAverage, error) {
	observations := make(map[string][]hashrateObservation, len(ips))
	for _, ip := range ips {
		observations[ip] = nil
	}
	since := start.Add(-maxHashrateInterval)
	firstDay := since.UTC().Truncate(24 * time.Hour)
	for day := firstDay; day.Before(end); day = day.AddDate(0, 0, 1) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := day.Format("2006-01-02") + ".jsonl"
		file, size, err := s.openHistorySnapshot(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("读取历史文件 %s: %w", name, err)
		}
		err = readHashrateObservations(ctx, io.NewSectionReader(file, 0, size), observations, since, end)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("读取历史文件 %s: %w", name, err)
		}
	}
	result := make(map[string]HashrateAverage, len(ips))
	for ip, samples := range observations {
		sort.SliceStable(samples, func(i, j int) bool { return samples[i].At.Before(samples[j].At) })
		window := hashrateWindow{start: start, end: end}
		for _, sample := range samples {
			window.add(sample)
		}
		result[ip] = window.average()
	}
	return result, nil
}

// Capture a synced file size under the store lock. Subsequent appends cannot
// extend the snapshot or leave a partially written record in the report.
func (s *Store) openHistorySnapshot(name string) (*os.File, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := filepath.Join(s.dir, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("历史文件不是普通文件")
	}
	f, err := os.Open(path)
	return f, info.Size(), err
}

func readHashrateObservations(ctx context.Context, reader io.Reader, observations map[string][]hashrateObservation, since, end time.Time) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 16<<10), 1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var record struct {
			IP     string              `json:"ip"`
			Sample hashrateObservation `json:"sample"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return err
		}
		if samples, selected := observations[record.IP]; selected && !record.Sample.At.Before(since) && record.Sample.At.Before(end) {
			observations[record.IP] = append(samples, record.Sample)
		}
	}
	return scanner.Err()
}
