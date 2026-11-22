package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDeviceListSorting(t *testing.T) {
	ds := []*Device{
		{IP: "192.168.1.100"},
		{IP: "192.168.1.10", WorkerNames: []string{"farm.10"}},
		{IP: "192.168.1.2", WorkerNames: []string{"Farm.2"}},
		{IP: "192.168.1.3", Label: "farm.2"},
	}
	for _, tc := range []struct {
		mode string
		want []string
	}{
		{"ip_asc", []string{"192.168.1.2", "192.168.1.3", "192.168.1.10", "192.168.1.100"}},
		{"ip_desc", []string{"192.168.1.100", "192.168.1.10", "192.168.1.3", "192.168.1.2"}},
		{"name_asc", []string{"192.168.1.2", "192.168.1.3", "192.168.1.10", "192.168.1.100"}},
		{"name_desc", []string{"192.168.1.10", "192.168.1.2", "192.168.1.3", "192.168.1.100"}},
		{"manual", []string{"192.168.1.10", "192.168.1.3", "192.168.1.2", "192.168.1.100"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			sortDeviceList(ds, Config{ListSort: tc.mode, ManualOrder: []string{"192.168.1.10", "192.168.1.3"}})
			var got []string
			for _, d := range ds {
				got = append(got, d.IP)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestManualOrderFilteredMultiSelectionAndRestart(t *testing.T) {
	dir := t.TempDir()
	a, err := newApp(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	ips := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3", "192.168.1.4", "192.168.1.5"}
	for _, ip := range ips {
		a.devices[ip] = &Device{IP: ip}
	}
	a.devices[ips[1]].Hidden = true
	visible := []string{ips[0], ips[2], ips[3], ips[4]}
	chosen := []string{ips[2], ips[3]}
	for _, tc := range []struct {
		action string
		want   []string
	}{
		{"up", []string{ips[2], ips[1], ips[3], ips[0], ips[4]}},
		{"down", ips},
		{"bottom", []string{ips[0], ips[1], ips[4], ips[2], ips[3]}},
		{"top", []string{ips[2], ips[1], ips[3], ips[0], ips[4]}},
		{"up", []string{ips[2], ips[1], ips[3], ips[0], ips[4]}},
	} {
		if err = a.moveListDevices(visible, chosen, tc.action); err != nil {
			t.Fatal(err)
		}
		if got := a.orderedDeviceIPsLocked(); !slices.Equal(got, tc.want) {
			t.Fatalf("%s: %v", tc.action, got)
		}
	}
	a.db.Close()
	b, err := newApp(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.db.Close()
	if b.config.ListSort != "manual" || !slices.Equal(b.config.ManualOrder, a.config.ManualOrder) {
		t.Fatal("manual order not persisted")
	}
	b.devices["192.168.1.6"] = &Device{IP: "192.168.1.6"}
	if got := b.orderedDeviceIPsLocked(); got[len(got)-1] != "192.168.1.6" {
		t.Fatal("new device did not append")
	}
	saved := slices.Clone(b.config.ManualOrder)
	if err = b.setListSort("ip_desc"); err != nil {
		t.Fatal(err)
	}
	if err = b.setListSort("manual"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(saved, b.config.ManualOrder) {
		t.Fatal("switching modes lost manual order")
	}
	if err = os.Mkdir(filepath.Join(dir, "settings.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = b.moveListDevices(visible, chosen, "bottom"); err == nil {
		t.Fatal("expected save failure")
	}
	if !slices.Equal(saved, b.config.ManualOrder) {
		t.Fatal("failed write changed order")
	}
	if err = b.setListSort("ip_asc"); err == nil || b.config.ListSort != "manual" {
		t.Fatal("failed mode change not rolled back")
	}
}

func TestFirstManualMoveUsesCurrentSort(t *testing.T) {
	a, err := newApp(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	for _, ip := range []string{"192.168.1.2", "192.168.1.10", "192.168.1.100"} {
		a.devices[ip] = &Device{IP: ip}
	}
	if err = a.setListSort("ip_desc"); err != nil {
		t.Fatal(err)
	}
	visible := a.orderedDeviceIPsLocked()
	if err = a.moveListDevices(visible, []string{"192.168.1.2"}, "up"); err != nil {
		t.Fatal(err)
	}
	if got := a.orderedDeviceIPsLocked(); !slices.Equal(got, []string{"192.168.1.100", "192.168.1.2", "192.168.1.10"}) {
		t.Fatal(got)
	}
}
