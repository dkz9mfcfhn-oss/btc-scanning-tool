package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"minerwatch/internal/miner"
)

func TestScanSkipsEmptyTargetsAndRechecksKnownMiners(t *testing.T) {
	a, err := newApp(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()

	const known, empty, live, unrelated = "192.168.0.1", "192.168.0.2", "192.168.0.3", "192.168.0.4"
	a.devices[known] = &Device{IP: known}
	a.discover = func(_ context.Context, ip string) bool {
		if ip == known {
			t.Error("a known device must bypass discovery")
		}
		return ip != empty
	}
	var mu sync.Mutex
	calls := map[string]int{}
	a.probe = func(_ context.Context, ip string, _ miner.Credentials) (miner.Reading, error) {
		mu.Lock()
		calls[ip]++
		mu.Unlock()
		if ip == live {
			return miner.Reading{RealtimeTH: num(95)}, nil
		}
		return miner.Reading{}, errors.New("no miner response")
	}
	a.config.Ranges = "192.168.0.1-4"
	if err = a.startJob("scan"); err != nil {
		t.Fatal(err)
	}
	a.jobs.Wait()

	if calls[empty] != 0 || calls[unrelated] != 1 || calls[known] != 3 || calls[live] != 1 {
		t.Fatalf("unexpected collection attempts: %v", calls)
	}
	if a.job.Done != 4 || a.job.Found != 1 || a.job.Running {
		t.Fatalf("unexpected scan progress: %+v", a.job)
	}
	if a.devices[empty] != nil || a.devices[unrelated] != nil {
		t.Fatal("discovery-only targets became devices")
	}
	for _, ip := range []string{known, live} {
		history := a.db.History(ip, time.Now().Add(-time.Hour))
		if len(history) != 1 || history[0].Online != (ip == live) {
			t.Fatalf("unexpected sample history for %s: %+v", ip, history)
		}
	}
}
