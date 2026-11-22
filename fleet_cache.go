package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type fleetHistoryCache struct {
	sync.Mutex
	key    string
	points []FleetPoint
}

func (a *App) fleetPoints(now time.Time) []FleetPoint {
	a.mu.Lock()
	known := map[string]*Device{}
	ips := []string{}
	for ip, d := range a.devices {
		known[ip] = &Device{Hidden: d.Hidden}
		if !d.Hidden {
			ips = append(ips, ip)
		}
	}
	a.mu.Unlock()
	sort.Strings(ips)
	key := fmt.Sprintf("%d/%d/%s", a.db.Revision(), now.Unix()/60, strings.Join(ips, ","))
	c := &a.fleetCache
	c.Lock()
	defer c.Unlock()
	if c.key != key {
		c.points = fleetMetricHistory(a.db.Metrics(now.Add(-24*time.Hour)), known)
		c.key = key
	}
	return c.points
}
