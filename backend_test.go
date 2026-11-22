package main

import (
	"minerwatch/internal/store"
	"testing"
	"time"
)

func TestBackendURLUsesOnlyIPv4(t *testing.T) {
	for _, ip := range []string{"192.168.1.2", "10.20.30.40", "172.16.1.10"} {
		u, e := backendURL(ip)
		if e != nil || u != "http://"+ip+"/" {
			t.Fatal(u, e)
		}
	}
	for _, ip := range []string{"", "example.com", "https://192.168.1.2", "192.168.1.2/", "192.168.1.2:80", "admin@192.168.1.2", "192.168.1.2\n", "::1", "::ffff:192.168.1.2", "192.168.1.2 & start cmd"} {
		if _, e := backendURL(ip); e == nil {
			t.Fatal("invalid address accepted", ip)
		}
	}
}
func TestBackendActionIsBeforeIPAndWorksForUnavailableMachines(t *testing.T) {
	for _, simple := range []bool{true, false} {
		order, widths := nativeColumnOrder(simple), nativeColumnWidths(simple)
		if order[0] != backendColumn || order[1] != 0 || widths[backendColumn] < 80 {
			t.Fatal(order, widths)
		}
	}
	for _, sample := range []*store.Sample{nil, {At: time.Now(), Online: false}, {At: time.Now(), Online: true}} {
		d := &Device{IP: "192.168.1.8", Label: "unsafe label http://other", Latest: sample}
		row := nativeRow(d, time.Now())
		if len(row) != len(nativeListColumns) || row[backendColumn] != "打开后台" {
			t.Fatal(row)
		}
	}
	if nativeRow(nil, time.Now())[backendColumn] != "" {
		t.Fatal("nil row has an action")
	}
}
