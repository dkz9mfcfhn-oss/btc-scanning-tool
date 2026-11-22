// Package store persists measurements as append-only, daily JSONL files.
// Retention is three UTC calendar days on disk and 25 hours in memory.
package store

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"minerwatch/internal/telemetry"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Sample struct {
	ElapsedSeconds  *float64                     `json:"elapsedSeconds,omitempty"`
	Accepted        *float64                     `json:"accepted,omitempty"`
	Rejected        *float64                     `json:"rejected,omitempty"`
	RejectPercent   *float64                     `json:"rejectPercent,omitempty"`
	IntervalSeconds int                          `json:"intervalSeconds,omitempty"`
	At              time.Time                    `json:"at"`
	Online          bool                         `json:"online"`
	RealtimeTH      *float64                     `json:"realtimeTH"`
	AverageTH       *float64                     `json:"averageTH"`
	TempC           *float64                     `json:"tempC"`
	InletC          *float64                     `json:"inletC"`
	OutletC         *float64                     `json:"outletC"`
	PowerW          *float64                     `json:"powerW"`
	ManualPowerW    *float64                     `json:"manualPowerW,omitempty"`
	PowerSource     string                       `json:"powerSource"`
	HashrateSource  string                       `json:"hashrateSource"`
	Warnings        []string                     `json:"warnings"`
	Error           string                       `json:"error"`
	Boards          []telemetry.BoardTemperature `json:"boards,omitempty"`
	Temperatures    []telemetry.Sensor           `json:"temperatures,omitempty"`
	Health          telemetry.HashrateHealth     `json:"health,omitempty"`
}

type Record struct {
	IP     string `json:"ip"`
	Sample Sample `json:"sample"`
}

type Store struct {
	revision       uint64
	mu             sync.RWMutex
	dir            string
	samples        map[string][]Sample
	warning        string
	closed         bool
	lastPruned     time.Time
	lastCleanedDay string
}

var dayFile = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.jsonl$`)

func Open(dir string) (*Store, error) {
	return OpenContext(context.Background(), dir)
}

// OpenContext checks cancellation between records. Any in-progress repair of
// the trailing record is finished before the load can return.
func OpenContext(ctx context.Context, dir string) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, samples: make(map[string][]Sample)}
	now := time.Now()
	if err := s.cleanDisk(now); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !dayFile.MatchString(e.Name()) {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("历史记录不能是符号链接：%s", e.Name())
		}
		if err := s.load(ctx, filepath.Join(dir, e.Name()), now.Add(-25*time.Hour)); err != nil {
			return nil, err
		}
	}
	for ip := range s.samples {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sort.SliceStable(s.samples[ip], func(i, j int) bool { return s.samples[ip][i].At.Before(s.samples[ip][j].At) })
	}
	s.prune(time.Now())
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) addWarning(message string) {
	if s.warning != "" {
		s.warning += "; "
	}
	s.warning += message
}

func validRecord(r Record) error {
	if r.IP == "" || len(r.IP) > 64 || r.Sample.At.IsZero() {
		return errors.New("记录缺少有效 IP 或采样时间")
	}
	return nil
}

func (s *Store) load(ctx context.Context, path string, cutoff time.Time) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 32<<10)
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, oversized, readErr := readHistoryLine(ctx, r)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(line) == 0 && readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.EOF {
			return fmt.Errorf("读取历史失败 %s: %w", filepath.Base(path), readErr)
		}
		var record Record
		var parseErr error
		if oversized {
			parseErr = errors.New("单条记录过大")
		} else if parseErr = json.Unmarshal(bytes.TrimSpace(line), &record); parseErr == nil {
			parseErr = validRecord(record)
		}
		if parseErr != nil {
			if readErr == io.EOF {
				if err := f.Truncate(offset); err != nil {
					return err
				}
				if err := f.Sync(); err != nil {
					return err
				}
				s.addWarning("已恢复异常退出留下的不完整记录：" + filepath.Base(path))
				break
			}
			return fmt.Errorf("历史文件 %s 在字节 %d 损坏：%w", filepath.Base(path), offset, parseErr)
		}
		if record.Sample.At.After(cutoff) {
			// Unmarshal allocates this record's nested data. Transfer its
			// ownership to the store instead of cloning it again.
			record.Sample.At = record.Sample.At.UTC()
			s.samples[record.IP] = append(s.samples[record.IP], record.Sample)
		}
		offset += int64(len(line))
		if readErr == io.EOF {
			// A complete last JSON object without a newline is salvageable. Add
			// its terminator so the next append cannot concatenate two objects.
			if _, err := f.WriteAt([]byte{'\n'}, offset); err != nil {
				return err
			}
			if err := f.Sync(); err != nil {
				return err
			}
			s.addWarning("已修复历史文件末尾换行：" + filepath.Base(path))
			break
		}
	}
	return nil
}

// The common short record stays in the reader's buffer. Long records get a
// private copy; beyond the size limit, drain the line to distinguish an
// oversized final fragment (repairable) from an oversized complete record.
func readHistoryLine(ctx context.Context, r *bufio.Reader) ([]byte, bool, error) {
	const maxLine = 1 << 20
	line, err := r.ReadSlice('\n')
	if err != bufio.ErrBufferFull {
		return line, false, err
	}
	line = append([]byte(nil), line...)
	oversized := false
	for err == bufio.ErrBufferFull {
		if e := ctx.Err(); e != nil {
			return nil, false, e
		}
		var part []byte
		part, err = r.ReadSlice('\n')
		oversized = oversized || len(line)+len(part) > maxLine
		if !oversized {
			line = append(line, part...)
		}
	}
	return line, oversized, err
}

// Append syncs a complete day's batch before exposing it to readers. A disk
// write failure is returned to the caller; measurements are not silently lost.
func (s *Store) Append(records []Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("历史存储已关闭")
	}
	if len(records) == 0 {
		return nil
	}
	groups := map[string][]Record{}
	now := time.Now()
	touched := map[string]bool{}
	for _, r := range records {
		if err := validRecord(r); err != nil {
			return err
		}
		r.Sample = cloneSample(r.Sample)
		// Validate all values before writing any file (Marshal rejects NaN/Inf).
		if _, err := json.Marshal(r); err != nil {
			return err
		}
		day := r.Sample.At.UTC().Format("2006-01-02")
		if day < cutoffDay(now) {
			return errors.New("采样时间已超过三天保留范围")
		}
		groups[day] = append(groups[day], r)
	}
	days := make([]string, 0, len(groups))
	for day := range groups {
		days = append(days, day)
	}
	sort.Strings(days)
	for _, day := range days {
		var payload bytes.Buffer
		for _, r := range groups[day] {
			b, _ := json.Marshal(r)
			payload.Write(b)
			payload.WriteByte('\n')
		}
		path := filepath.Join(s.dir, day+".jsonl")
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("拒绝向符号链接写入历史记录")
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		stat, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		_, writeErr := f.Write(payload.Bytes())
		if writeErr == nil {
			writeErr = f.Sync()
		}
		if writeErr != nil {
			rollbackErr := f.Truncate(stat.Size())
			if rollbackErr == nil {
				rollbackErr = f.Sync()
			}
			f.Close()
			return fmt.Errorf("历史记录写入失败：%w (回滚: %v)", writeErr, rollbackErr)
		}
		if err := f.Close(); err != nil {
			return err
		}
		for _, r := range groups[day] {
			s.samples[r.IP] = append(s.samples[r.IP], r.Sample)
			s.revision++
			touched[r.IP] = true
		}
	}
	for ip := range touched {
		sort.SliceStable(s.samples[ip], func(i, j int) bool { return s.samples[ip][i].At.Before(s.samples[ip][j].At) })
		s.pruneIP(ip, now)
	}
	if now.Sub(s.lastPruned) >= 15*time.Minute {
		s.prune(now)
	}
	if err := s.cleanDisk(now); err != nil {
		return fmt.Errorf("记录已保存，但历史清理失败：%w", err)
	}
	return nil
}

func cutoffDay(now time.Time) string { return now.UTC().AddDate(0, 0, -2).Format("2006-01-02") }

func (s *Store) cleanDisk(now time.Time) error {
	day := now.UTC().Format("2006-01-02")
	if day == s.lastCleanedDay {
		return nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	cutoff := cutoffDay(now)
	for _, e := range entries {
		if e.IsDir() || !dayFile.MatchString(e.Name()) || strings.TrimSuffix(e.Name(), ".jsonl") >= cutoff {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, e.Name())); err != nil {
			return err
		}
	}
	s.lastCleanedDay = day
	return nil
}

func (s *Store) prune(now time.Time) {
	for ip := range s.samples {
		s.pruneIP(ip, now)
	}
	s.lastPruned = now
}

func (s *Store) pruneIP(ip string, now time.Time) {
	cutoff := now.UTC().Add(-25 * time.Hour)
	samples := s.samples[ip]
	i := sort.Search(len(samples), func(i int) bool { return !samples[i].At.Before(cutoff) })
	if i == len(samples) {
		delete(s.samples, ip)
	} else if i > 0 {
		s.samples[ip] = append([]Sample(nil), samples[i:]...)
	}
}

func cloneFloat(v *float64) *float64 {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}
func cloneSample(s Sample) Sample {
	s.At = s.At.UTC()
	s.ElapsedSeconds = cloneFloat(s.ElapsedSeconds)
	s.Accepted = cloneFloat(s.Accepted)
	s.Rejected = cloneFloat(s.Rejected)
	s.RejectPercent = cloneFloat(s.RejectPercent)
	s.RealtimeTH = cloneFloat(s.RealtimeTH)
	s.AverageTH = cloneFloat(s.AverageTH)
	s.TempC = cloneFloat(s.TempC)
	s.InletC = cloneFloat(s.InletC)
	s.OutletC = cloneFloat(s.OutletC)
	s.PowerW = cloneFloat(s.PowerW)
	s.ManualPowerW = cloneFloat(s.ManualPowerW)
	s.Boards = telemetry.CloneBoards(s.Boards)
	s.Temperatures = telemetry.CloneSensors(s.Temperatures)
	s.Health = telemetry.CloneHealth(s.Health)
	s.Warnings = append([]string{}, s.Warnings...)
	return s
}

func (s *Store) History(ip string, since time.Time) []Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Sample{}
	for _, v := range s.samples[ip] {
		if !v.At.Before(since) {
			out = append(out, cloneSample(v))
		}
	}
	return out
}

// Latest returns an independent copy of the newest retained sample for one
// device without loading copies of that device's history.
func (s *Store) Latest(ip string) (Sample, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	samples := s.samples[ip]
	if len(samples) == 0 {
		return Sample{}, false
	}
	return cloneSample(samples[len(samples)-1]), true
}

func (s *Store) HasSamplesSince(since time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, samples := range s.samples {
		if len(samples) > 0 && !samples[len(samples)-1].At.Before(since) {
			return true
		}
	}
	return false
}

func (s *Store) All(since time.Time) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Record{}
	for ip, samples := range s.samples {
		for _, v := range samples {
			if !v.At.Before(since) {
				out = append(out, Record{IP: ip, Sample: cloneSample(v)})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sample.At.Equal(out[j].Sample.At) {
			return out[i].IP < out[j].IP
		}
		return out[i].Sample.At.Before(out[j].Sample.At)
	})
	return out
}

func (s *Store) Warning() string { s.mu.RLock(); defer s.mu.RUnlock(); return s.warning }
func (s *Store) Close() error    { s.mu.Lock(); defer s.mu.Unlock(); s.closed = true; return nil }

// Maintain expires history even while monitoring is paused or there are no devices.
func (s *Store) Maintain(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("历史存储已关闭")
	}
	s.prune(now)
	return s.cleanDisk(now)
}
