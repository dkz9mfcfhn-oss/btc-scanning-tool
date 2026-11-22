package main

import (
	"minerwatch/internal/store"
	"strings"
	"testing"
	"time"
)

func TestFaultTimelineRecoveryAndMonitoringGap(t *testing.T) {
	now := time.Now()
	mk := func(m int, online bool, rate float64) store.Sample {
		return store.Sample{At: now.Add(time.Duration(m) * time.Minute), Online: online, RealtimeTH: num(rate), IntervalSeconds: 300}
	}
	p := faultTimeline([]store.Sample{mk(-40, false, 0), mk(-35, false, 0), mk(-30, true, 100), mk(-25, true, 0), mk(-5, true, 100)}, now)
	if len(p) != 2 || p[0].EndReason != "已恢复正常" || !strings.Contains(p[1].EndReason, "未知") || p[1].Open {
		t.Fatalf("bad periods %+v", p)
	}
	if p[1].End.Sub(p[1].Start) != 5*time.Minute {
		t.Fatal("gap counted as outage")
	}
}
func TestExceptionFirstAndNotesPersistence(t *testing.T) {
	a, e := newApp(t.TempDir(), false)
	if e != nil {
		t.Fatal(e)
	}
	defer a.db.Close()
	now := time.Now()
	a.devices["10.0.0.1"] = &Device{IP: "10.0.0.1", Latest: &store.Sample{At: now, Online: true, RealtimeTH: num(100)}}
	a.devices["10.0.0.2"] = &Device{IP: "10.0.0.2", Latest: &store.Sample{At: now, Online: false}}
	if e = a.setExceptionFirst(true); e != nil {
		t.Fatal(e)
	}
	ds := []*Device{a.devices["10.0.0.1"], a.devices["10.0.0.2"]}
	sortDeviceList(ds, a.config)
	if ds[0].IP != "10.0.0.2" {
		t.Fatal("outage not first")
	}
	if e = a.setMinerNote(ds[0].IP, "  北排 2 号  "); e != nil {
		t.Fatal(e)
	}
	b, e := newApp(a.dir, false)
	if e != nil {
		t.Fatal(e)
	}
	defer b.db.Close()
	if b.devices[ds[0].IP].Note != "北排 2 号" || !b.config.ExceptionFirst {
		t.Fatal("preferences not restored")
	}
	if !matchesDevice(b.devices[ds[0].IP], "北排", 0, now) {
		t.Fatal("note not searchable")
	}
}
func TestCloudTargetScope(t *testing.T) {
	target, key, e := cloudTarget("https://example.com/minewatch-bark/abcdefghijklmnop")
	if e != nil || target != "https://example.com/minewatch-watchdog/heartbeat" || key != "abcdefghijklmnop" {
		t.Fatal(target, key, e)
	}
	for _, u := range []string{"http://example.com/minewatch-bark/abcdefghijklmnop", "https://example.com/key", "https://example.com/minewatch-bark/abcdefghijklmnop?secret=yes", "https://user@example.com/minewatch-bark/abcdefghijklmnop"} {
		if _, _, e := cloudTarget(u); e == nil {
			t.Fatal("accepted", u)
		}
	}
}
func TestFleetCacheVisibilityAndRevision(t *testing.T) {
	a, e := newApp(t.TempDir(), false)
	if e != nil {
		t.Fatal(e)
	}
	defer a.db.Close()
	now := time.Now()
	a.devices["10.0.0.1"] = &Device{IP: "10.0.0.1"}
	if e = a.db.Append([]store.Record{{IP: "10.0.0.1", Sample: store.Sample{At: now, Online: true, RealtimeTH: num(100)}}}); e != nil {
		t.Fatal(e)
	}
	p := a.fleetPoints(now)
	if len(p) == 0 {
		t.Fatal("no chart")
	}
	key := a.fleetCache.key
	a.fleetPoints(now)
	if a.fleetCache.key != key {
		t.Fatal("cache changed")
	}
	a.devices["10.0.0.1"].Hidden = true
	a.fleetPoints(now)
	if a.fleetCache.key == key {
		t.Fatal("hidden not invalidating")
	}
}

func TestExceptionPriorityOffSurvivesRestart(t *testing.T) {
	a, e := newApp(t.TempDir(), false)
	if e != nil {
		t.Fatal(e)
	}
	defer a.db.Close()
	if !a.config.ExceptionFirst {
		t.Fatal("new default disabled")
	}
	if e = a.setExceptionFirst(false); e != nil {
		t.Fatal(e)
	}
	b, e := newApp(a.dir, false)
	if e != nil {
		t.Fatal(e)
	}
	defer b.db.Close()
	if b.config.ExceptionFirst {
		t.Fatal("explicit off was lost")
	}
	if cloudHeartbeatInterval != 10*time.Minute {
		t.Fatal("wrong heartbeat interval")
	}
}
