package main

import (
	"context"
	"net"
	"sync"
	"time"
)

// These are the ports used by the existing collectors. A connection only
// identifies a candidate; the collector still verifies that it is a miner.
func minerPortReachable(ctx context.Context, ip string) bool {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	results := make(chan bool, 3)
	for _, port := range []string{"4028", "4433", "80"} {
		go func() {
			dialer := net.Dialer{}
			conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, port))
			if err == nil {
				conn.Close()
			}
			results <- err == nil
		}()
	}
	reachable := false
	for range 3 {
		if <-results {
			reachable = true
			cancel()
		}
	}
	return reachable
}

type scanTarget struct {
	ip    string
	known bool
}

func (a *App) enqueueScanTargets(ctx context.Context, ips []string, concurrency int, queue chan<- scanTarget) {
	defer close(queue)
	var candidates []string
	a.mu.Lock()
	for _, ip := range ips {
		if _, known := a.devices[ip]; known {
			queue <- scanTarget{ip: ip, known: true}
		} else {
			candidates = append(candidates, ip)
		}
	}
	a.mu.Unlock()

	var workers sync.WaitGroup
	pending := make(chan string, len(candidates))
	for _, ip := range candidates {
		pending <- ip
	}
	close(pending)
	for range min(concurrency*2, 64, len(candidates)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for ip := range pending {
				if ctx.Err() != nil {
					return
				}
				if a.discover(ctx, ip) {
					queue <- scanTarget{ip: ip}
				} else if ctx.Err() == nil {
					a.mu.Lock()
					a.job.Done++
					a.mu.Unlock()
				}
			}
		}()
	}
	workers.Wait()
}
