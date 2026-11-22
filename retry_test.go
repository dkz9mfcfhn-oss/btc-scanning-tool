package main

import (
	"context"
	"errors"
	"minerwatch/internal/miner"
	"testing"
	"time"
)

func TestReliableReadRecoversFailuresAndTransientZero(t *testing.T) {
	for _, first := range []string{"timeout", "zero", "missing"} {
		calls := 0
		r, e := readReliably(context.Background(), "192.168.1.2", miner.Credentials{}, 4, func(ctx context.Context, _ string, _ miner.Credentials) (miner.Reading, error) {
			calls++
			deadline, _ := ctx.Deadline()
			if time.Until(deadline) < 35*time.Second {
				t.Error("CGI budget starved")
			}
			if calls == 1 {
				switch first {
				case "timeout":
					return miner.Reading{}, context.DeadlineExceeded
				case "zero":
					return miner.Reading{RealtimeTH: num(0)}, nil
				case "missing":
					return miner.Reading{}, nil
				}
			}
			return miner.Reading{RealtimeTH: num(95)}, nil
		}, make(chan struct{}, 1), 0, true)
		if e != nil || calls != 2 || r.RealtimeTH == nil || *r.RealtimeTH != 95 {
			t.Fatal(first, r, e, calls)
		}
	}
}
func TestReliableReadConfirmsZeroAndDoesNotMaskPersistentFailure(t *testing.T) {
	calls := 0
	r, e := readReliably(context.Background(), "192.168.1.2", miner.Credentials{}, 4, func(context.Context, string, miner.Credentials) (miner.Reading, error) {
		calls++
		return miner.Reading{RealtimeTH: num(0)}, nil
	}, make(chan struct{}, 1), 0, true)
	if e != nil || calls != 2 || *r.RealtimeTH != 0 {
		t.Fatal(r, e, calls)
	}
	calls = 0
	_, e = readReliably(context.Background(), "192.168.1.2", miner.Credentials{}, 4, func(context.Context, string, miner.Credentials) (miner.Reading, error) {
		calls++
		return miner.Reading{}, errors.New("timeout")
	}, make(chan struct{}, 1), 0, true)
	if e == nil || calls != 3 {
		t.Fatal(e, calls)
	}
}
func TestReliableReadCancellationAndSingleUnconfirmedZero(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := readReliably(ctx, "", miner.Credentials{}, 4, func(context.Context, string, miner.Credentials) (miner.Reading, error) {
		t.Fatal("called cancelled probe")
		return miner.Reading{}, nil
	}, make(chan struct{}, 1), time.Hour, true)
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	calls := 0
	r, e := readReliably(context.Background(), "", miner.Credentials{}, 4, func(context.Context, string, miner.Credentials) (miner.Reading, error) {
		calls++
		if calls < 3 {
			return miner.Reading{}, errors.New("timeout")
		}
		return miner.Reading{RealtimeTH: num(0)}, nil
	}, make(chan struct{}, 1), 0, true)
	if e != nil || r.RealtimeTH != nil {
		t.Fatal("unconfirmed zero", r, e)
	}
}
