package main

import (
	"context"
	"fmt"
	"minerwatch/internal/miner"
	"time"
)

// Each attempt gets a fresh deadline. Retries are sequential for one IP and
// throttled across the job; only the final confirmed sample is saved.
func readReliably(ctx context.Context, ip string, creds miner.Credentials, timeoutSeconds int, probe func(context.Context, string, miner.Credentials) (miner.Reading, error), slots chan struct{}, backoff time.Duration, known bool) (miner.Reading, error) {
	var last miner.Reading
	var lastErr error
	zeros := 0
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return miner.Reading{}, ctx.Err()
		}
		if attempt > 0 {
			timer := time.NewTimer(backoff * time.Duration(attempt))
			select {
			case <-ctx.Done():
				timer.Stop()
				return miner.Reading{}, ctx.Err()
			case <-timer.C:
			}
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return miner.Reading{}, ctx.Err()
			}
		}
		one, cancel := context.WithTimeout(ctx, max(65*time.Second, time.Duration(timeoutSeconds)*8*time.Second))
		r, e := probe(one, ip, creds)
		cancel()
		if attempt > 0 {
			<-slots
		}
		if ctx.Err() != nil {
			return miner.Reading{}, ctx.Err()
		}
		last, lastErr = r, e
		// A reachable port can belong to unrelated equipment. A new address
		// earns retries only after a collector has returned a valid sample.
		if e == nil {
			known = true
		} else if !known {
			return r, e
		}
		if e == nil && r.RealtimeTH != nil {
			if *r.RealtimeTH > 0 {
				return r, nil
			}
			if *r.RealtimeTH == 0 {
				zeros++
				if zeros >= 2 {
					return r, nil
				}
			}
		}
	}
	if lastErr != nil {
		return last, fmt.Errorf("连续复查后仍读取失败：%w", lastErr)
	}
	if last.RealtimeTH != nil && *last.RealtimeTH == 0 && zeros < 2 {
		last.RealtimeTH = nil
		last.Warnings = append(last.Warnings, "仅一次零算力读数，复核未确认；未判定为无算力")
	}
	return last, nil
}
