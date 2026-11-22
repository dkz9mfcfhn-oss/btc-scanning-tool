package store

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"minerwatch/internal/telemetry"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func ptr(v float64) *float64 { return &v }
func recordAt(at time.Time) Record {
	return Record{IP: "192.168.1.1", Sample: Sample{At: at, Online: true, RealtimeTH: ptr(100), AverageTH: ptr(99), TempC: ptr(72)}}
}

func temperatureRecord(at time.Time) Record {
	r := recordAt(at)
	r.Sample.Boards = []telemetry.BoardTemperature{
		{ID: "0", Name: "板 1", Chip: telemetry.TemperatureSeries{AverageC: ptr(68.5), MinC: ptr(65), MaxC: ptr(72), SensorsC: []*float64{ptr(65), nil, ptr(72)}, AverageKind: "reported", Source: "stats/chain[0]/chip"}, PCB: telemetry.TemperatureSeries{AverageC: ptr(48), MinC: ptr(47), MaxC: ptr(49), SensorsC: []*float64{ptr(47), ptr(49)}, AverageKind: "sensor_mean", Source: "stats/chain[0]/pcb"}, Other: []telemetry.Sensor{{Name: "板进水", ValueC: ptr(0), Source: "edevs[0]/Inlet"}}},
		{ID: "1", Name: "板 2", Chip: telemetry.TemperatureSeries{MaxC: ptr(70), Source: "stats/chain[1]/maximum"}},
		{ID: "2", Name: "板 3", Chip: telemetry.TemperatureSeries{AverageC: ptr(69), AverageKind: "reported", Source: "stats/chain[2]/average"}},
	}
	r.Sample.Temperatures = []telemetry.Sensor{{Name: "环境温度", ValueC: ptr(-2), Source: "stats/ambient"}, {Name: "未上报传感器", ValueC: nil, Source: "stats/sensor[1]"}}
	return r
}

func TestTemperatureRoundtripAndDeepClone(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	r := temperatureRecord(now)
	if err := s.Append([]Record{r}); err != nil {
		t.Fatal(err)
	}
	expected := s.History(r.IP, now.Add(-time.Minute))[0]
	// Input mutation must not change live history or the independently saved file.
	*r.Sample.Boards[0].Chip.AverageC = 199
	*r.Sample.Boards[0].Chip.MinC = 190
	*r.Sample.Boards[0].Chip.MaxC = 200
	*r.Sample.Boards[0].Chip.SensorsC[0] = 198
	*r.Sample.Boards[0].PCB.AverageC = 180
	*r.Sample.Boards[0].PCB.SensorsC[0] = 181
	*r.Sample.Boards[0].Other[0].ValueC = 182
	*r.Sample.Temperatures[0].ValueC = 183
	r.Sample.Boards[0].Name = "mutated"
	if got := s.History(r.IP, now.Add(-time.Minute))[0]; !reflect.DeepEqual(got, expected) {
		t.Fatal("input temperatures alias stored history")
	}
	// Both read methods must return copies all the way down to nested sensors.
	copy := s.History(r.IP, now.Add(-time.Minute))[0]
	*copy.Boards[0].Chip.SensorsC[0] = 170
	copy.Boards[0].Chip.SensorsC[1] = ptr(171)
	*copy.Boards[0].Other[0].ValueC = 172
	*copy.Temperatures[0].ValueC = 173
	all := s.All(now.Add(-time.Minute))
	*all[0].Sample.Boards[0].PCB.MaxC = 174
	if got := s.History(r.IP, now.Add(-time.Minute))[0]; !reflect.DeepEqual(got, expected) {
		t.Fatal("returned temperatures alias stored history")
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := s.History(r.IP, now.Add(-time.Minute))[0]
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("temperature roundtrip changed readings or provenance: got %+v expected %+v", got, expected)
	}
	if got.Boards[0].Chip.SensorsC[1] != nil || got.Boards[1].Chip.AverageC != nil || *got.Boards[0].Other[0].ValueC != 0 {
		t.Fatal("missing sensor, missing mean, or zero temperature changed")
	}
}

func TestLegacySamplesWithoutTemperatureDetails(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	legacy := `{"ip":"192.168.1.1","sample":{"at":"` + now.Format(time.RFC3339Nano) + `","online":true,"tempC":71,"realtimeTH":95}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, now.Format("2006-01-02")+".jsonl"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := s.History("192.168.1.1", now.Add(-time.Minute))
	if len(h) != 1 || h[0].TempC == nil || *h[0].TempC != 71 || h[0].Boards != nil || h[0].Temperatures != nil {
		t.Fatalf("legacy sample changed: %+v", h)
	}
	p, err := json.Marshal(h[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(p), `"boards"`) || strings.Contains(string(p), `"temperatures"`) {
		t.Fatal("missing legacy fields should remain omitted", string(p))
	}
	if err := s.Append([]Record{temperatureRecord(now.Add(time.Second))}); err != nil {
		t.Fatal(err)
	}
	if got := s.History("192.168.1.1", now.Add(-time.Minute)); len(got) != 2 || len(got[1].Boards) != 3 {
		t.Fatal("new data cannot append alongside old samples")
	}
}

func TestPersistenceNullZeroOfflineAndCopies(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	a := recordAt(now.Add(-5 * time.Minute))
	a.Sample.PowerW = ptr(0)
	b := Record{IP: a.IP, Sample: Sample{At: now, Online: false, Error: "timeout"}}
	if err := s.Append([]Record{a, b}); err != nil {
		t.Fatal(err)
	}
	*a.Sample.RealtimeTH = 900
	h := s.History(a.IP, now.Add(-time.Hour))
	if len(h) != 2 || *h[0].RealtimeTH != 100 || h[0].PowerW == nil || *h[0].PowerW != 0 || h[1].Online || h[1].RealtimeTH != nil {
		t.Fatalf("bad samples: %+v", h)
	}
	*h[0].RealtimeTH = 800
	if *s.History(a.IP, now.Add(-time.Hour))[0].RealtimeTH != 100 {
		t.Fatal("external mutation changed storage")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h = s.History(a.IP, now.Add(-time.Hour))
	if len(h) != 2 || h[1].Error != "timeout" || h[0].PowerW == nil || *h[0].PowerW != 0 {
		t.Fatalf("bad persisted samples %+v", h)
	}
	if empty := s.History("missing", now); empty == nil || len(empty) != 0 {
		t.Fatalf("empty must be array: %v", empty)
	}
	bytes, _ := json.Marshal(h[1])
	if !strings.Contains(string(bytes), `"realtimeTH":null`) {
		t.Fatal(string(bytes))
	}
}

func TestRecoverTornTail(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	r := recordAt(now)
	b, _ := json.Marshal(r)
	path := filepath.Join(dir, now.Format("2006-01-02")+".jsonl")
	if err := os.WriteFile(path, append(append(b, '\n'), []byte(`{"ip":"192.168.`)...), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Warning() == "" {
		t.Fatal("missing recovery warning")
	}
	if len(s.All(now.Add(-time.Hour))) != 1 {
		t.Fatal("valid record not recovered")
	}
	if err := s.Append([]Record{recordAt(now.Add(time.Second))}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.All(now.Add(-time.Hour))) != 2 {
		t.Fatal("append after recovery failed")
	}
}

func TestRepairCompleteTailAndRejectInteriorCorruption(t *testing.T) {
	now := time.Now().UTC()
	r := recordAt(now)
	b, _ := json.Marshal(r)
	dir := t.TempDir()
	path := filepath.Join(dir, now.Format("2006-01-02")+".jsonl")
	os.WriteFile(path, b, 0600)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append([]Record{recordAt(now.Add(time.Second))}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.All(now.Add(-time.Hour))) != 2 {
		t.Fatal("complete tail lost")
	}
	s.Close()
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, now.Format("2006-01-02")+".jsonl"), []byte("broken\n"+string(b)+"\n"), 0600)
	if _, err := Open(dir2); err == nil {
		t.Fatal("interior corruption accepted")
	}
}

func TestOpenBufferedRecordsAndTailRecovery(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := filepath.Join(dir, now.Format("2006-01-02")+".jsonl")
	r := temperatureRecord(now)
	r.Sample.Warnings = []string{strings.Repeat("a", 40<<10)}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	fragment := strings.Repeat("x", (1<<20)+1)
	if err := os.WriteFile(path, append(append([]byte(nil), data...), fragment...), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenContext(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled history load returned %v", err)
	}
	if stat, err := os.Stat(path); err != nil || stat.Size() != int64(len(data)+len(fragment)) {
		t.Fatalf("canceled load touched history: %+v, %v", stat, err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok := s.Latest(r.IP)
	if !ok || !got.At.Equal(now) || !reflect.DeepEqual(got.Warnings, r.Sample.Warnings) {
		t.Fatal("valid buffered record was lost while repairing the tail")
	}
	if stat, err := os.Stat(path); err != nil || stat.Size() != int64(len(data)) || s.Warning() == "" {
		t.Fatalf("oversized final fragment was not repaired: %+v, %v", stat, err)
	}
	if err := os.WriteFile(path, append([]byte(fragment), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "单条记录过大") {
		t.Fatalf("oversized complete record should be a load error, got %v", err)
	}
}

func TestLatestReturnsNewestIndependentSample(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	newest, oldest := temperatureRecord(now), temperatureRecord(now.Add(-time.Hour))
	if err := s.Append([]Record{newest, oldest}); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Latest(newest.IP)
	if !ok || !got.At.Equal(now) {
		t.Fatal("Latest did not choose newest sample by timestamp")
	}
	*got.Boards[0].Chip.SensorsC[0] = 301
	got, ok = s.Latest(newest.IP)
	if !ok || *got.Boards[0].Chip.SensorsC[0] == 301 {
		t.Fatal("Latest returned mutable storage data")
	}
	if _, ok := s.Latest("missing"); ok {
		t.Fatal("Latest fabricated a sample for an unknown device")
	}
}

func TestRetentionAndOrdering(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	oldPath := filepath.Join(dir, now.AddDate(0, 0, -7).Format("2006-01-02")+".jsonl")
	os.WriteFile(oldPath, []byte("old"), 0600)
	os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("data"), 0600)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("expired file retained")
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatal("unrelated file removed")
	}
	if err := s.Append([]Record{recordAt(now), recordAt(now.Add(-26 * time.Hour)), recordAt(now.Add(-time.Hour))}); err != nil {
		t.Fatal(err)
	}
	h := s.History("192.168.1.1", now.Add(-30*time.Hour))
	if len(h) != 2 || !h[0].At.Before(h[1].At) {
		t.Fatalf("retention/order incorrect: %+v", h)
	}
}

func TestRejectedBatchHasNoSideEffects(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	bad := recordAt(now)
	bad.Sample.PowerW = ptr(math.NaN())
	if err := s.Append([]Record{recordAt(now), bad}); err == nil {
		t.Fatal("NaN accepted")
	}
	if len(s.All(now.Add(-time.Hour))) != 0 {
		t.Fatal("partial invalid batch visible")
	}
	s.Close()
	if err := s.Append([]Record{recordAt(now)}); err == nil {
		t.Fatal("append after close accepted")
	}
}

func TestConcurrentReadAndAppend(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if err := s.Append([]Record{recordAt(now.Add(time.Duration(i*5+j) * time.Millisecond))}); err != nil {
					t.Error(err)
				}
				s.History("192.168.1.1", now.Add(-time.Hour))
				s.All(now.Add(-time.Hour))
				s.Warning()
			}
		}(i)
	}
	wg.Wait()
	if len(s.All(now.Add(-time.Hour))) != 25 {
		t.Fatal("concurrent records lost")
	}
}

func TestThreeDayDiskLimitAndIdleCleanup(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	s := &Store{dir: dir, samples: make(map[string][]Sample)}
	for _, offset := range []int{-4, -3, -2, -1, 0} {
		name := now.AddDate(0, 0, offset).Format("2006-01-02") + ".jsonl"
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte("keep"), 0600)
	s.samples["old"] = []Sample{{At: now.Add(-26 * time.Hour)}}
	s.samples["current"] = []Sample{{At: now}}
	if err := s.Maintain(now); err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int{-4, -3, -2, -1, 0} {
		_, err := os.Stat(filepath.Join(dir, now.AddDate(0, 0, offset).Format("2006-01-02")+".jsonl"))
		if offset < -2 && !os.IsNotExist(err) {
			t.Fatal("expired file retained", offset, err)
		}
		if offset >= -2 && err != nil {
			t.Fatal("recent file removed", offset, err)
		}
	}
	if len(s.samples) != 1 {
		t.Fatal("expired memory retained")
	}
	// The day rolls over without any Append calls, as when monitoring is paused.
	if err := s.Maintain(now.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-10-31.jsonl")); !os.IsNotExist(err) {
		t.Fatal("idle rollover did not clean")
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
		t.Fatal("unrelated file removed")
	}
}

func TestThreeDayAppendRejectsExpiredBatch(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	if err := s.Append([]Record{recordAt(now), recordAt(now.AddDate(0, 0, -3))}); err == nil {
		t.Fatal("old batch accepted")
	}
	if len(s.All(now.Add(-time.Hour))) != 0 {
		t.Fatal("part of rejected batch stored")
	}
	if err := s.Append([]Record{recordAt(now.AddDate(0, 0, -2)), recordAt(now)}); err != nil {
		t.Fatal(err)
	}
}
