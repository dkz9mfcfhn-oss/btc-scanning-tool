package main

import (
	"minerwatch/internal/store"
	"slices"
	"time"
)

const farmAverageWindow = 24 * time.Hour

type farmAverage struct {
	MeanTH, WorkEH, DeviceHours float64
	Valid, Devices              int
}

// Each device contributes its own mean weighted by its valid sample time.
// Report coverage separately because devices may have different missing periods.
func (f *farmAverage) add(meanTH, hours float64) {
	f.Devices++
	if hours <= 0 {
		return
	}
	f.Valid++
	f.MeanTH += meanTH
	f.DeviceHours += hours
	f.WorkEH += meanTH * hours * 3600 / 1e6
}

func (f farmAverage) coverage() float64 {
	if f.Devices == 0 {
		return 0
	}
	return f.DeviceHours / (farmAverageWindow.Hours() * float64(f.Devices))
}

type rollingFarmAverage struct {
	ips      []string
	at       time.Time
	average  farmAverage
	earnings earningsPeriod
}

// The dashboard refresh loop owns this cache, and paint only consumes its
// result. Repeated progress paints do not touch the history store.
func (c *rollingFarmAverage) refresh(now time.Time, devices []*Device, db *store.Store) {
	ips := make([]string, 0, len(devices))
	for _, d := range devices {
		if !d.Hidden {
			ips = append(ips, d.IP)
		}
	}
	slices.Sort(ips)
	if c.at.Equal(now.Truncate(15*time.Second)) && slices.Equal(c.ips, ips) {
		return
	}
	summary := farmAverage{}
	earnings := earningsPeriod{start: earningsPeriodStart(now), asOf: now}
	averages, period := db.RecentHashrateAndProfit(ips, now.Add(-farmAverageWindow), earnings.start, now)
	for _, ip := range ips {
		v := averages[ip]
		summary.add(v.MeanTH, v.Covered.Hours())
		v = period[ip]
		earnings.average.add(v.MeanTH, v.Covered.Hours())
		earnings.profit.add(sampleProfit(v))
	}
	c.average = summary
	c.earnings = earnings
	c.ips = ips
	c.at = now.Truncate(15 * time.Second)
}
