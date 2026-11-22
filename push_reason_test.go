package main

import (
	"minerwatch/internal/store"
	"minerwatch/internal/telemetry"
	"strings"
	"testing"
	"time"
)

func TestZeroPushAndReason(t *testing.T) {
	now := time.Now()
	sample := &store.Sample{At: now, Online: true, RealtimeTH: num(0), Health: telemetry.HashrateHealth{State: "zero", Reason: "矿池连接异常（设备报告）"}}
	s, title := pushSampleTransition(pushState{}, sample)
	if title != "" {
		t.Fatal(title)
	}
	s, title = pushSampleTransition(s, sample)
	if title != "无算力" {
		t.Fatal(title)
	}
	s.ZeroNotified = true
	s, title = pushSampleTransition(s, sample)
	if title != "" {
		t.Fatal("duplicate", title)
	}
	missing := *sample
	missing.RealtimeTH = nil
	s, title = pushSampleTransition(s, &missing)
	if title != "" {
		t.Fatal("missing treated as recovery")
	}
	healthy := *sample
	healthy.RealtimeTH = num(100)
	_, title = pushSampleTransition(s, &healthy)
	if title != "算力恢复" {
		t.Fatal(title)
	}
	body := phoneAlertBody(&Device{IP: "192.168.8.100", Model: "1566HA", WorkerNames: []string{"farm.100"}, WorkerReadAt: now, Latest: sample}, now)
	for _, want := range []string{"farm.100", "矿池连接异常", "无算力", "192.168.8.100"} {
		if !strings.Contains(body, want) {
			t.Fatal(body)
		}
	}
}
func TestOfflineReasonAndWorkerFallback(t *testing.T) {
	now := time.Now()
	s := &store.Sample{At: now, Error: "连接超时"}
	body := phoneAlertBody(&Device{Latest: s}, now)
	if !strings.Contains(body, "未读取到矿工名") || !strings.Contains(body, "无法判断矿机是否停机") {
		t.Fatal(body)
	}
}
