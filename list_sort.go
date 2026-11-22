package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

var listSortChoices = []struct{ Mode, Label string }{
	{"ip_asc", "IP：从小到大"},
	{"ip_desc", "IP：从大到小"},
	{"name_asc", "名称：升序（矿工名 / 机位）"},
	{"name_desc", "名称：降序（矿工名 / 机位）"},
	{"manual", "手动排序（右键上移 / 下移）"},
}

func listSortMode(c Config) string {
	for _, choice := range listSortChoices {
		if choice.Mode == c.ListSort {
			return choice.Mode
		}
	}
	return "ip_asc"
}

func deviceSortName(d *Device) string {
	for _, name := range d.WorkerNames {
		if name = strings.TrimSpace(name); name != "" {
			return strings.ToLower(name)
		}
	}
	return strings.ToLower(strings.TrimSpace(d.Label))
}

// Compare digit runs numerically without integer overflow: worker2 precedes worker10.
func naturalNameCompare(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		if a[0] >= '0' && a[0] <= '9' && b[0] >= '0' && b[0] <= '9' {
			i, j := 0, 0
			for i < len(a) && a[i] >= '0' && a[i] <= '9' {
				i++
			}
			for j < len(b) && b[j] >= '0' && b[j] <= '9' {
				j++
			}
			x, y := strings.TrimLeft(a[:i], "0"), strings.TrimLeft(b[:j], "0")
			if len(x) < len(y) {
				return -1
			}
			if len(x) > len(y) {
				return 1
			}
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
			a, b = a[i:], b[j:]
			continue
		}
		if a[0] < b[0] {
			return -1
		}
		if a[0] > b[0] {
			return 1
		}
		a, b = a[1:], b[1:]
	}
	return strings.Compare(a, b)
}

func sortDeviceList(ds []*Device, c Config) {
	mode := listSortMode(c)
	now := time.Now()
	rank := make(map[string]int, len(c.ManualOrder))
	for i, ip := range c.ManualOrder {
		if _, exists := rank[ip]; !exists {
			rank[ip] = i
		}
	}
	sort.Slice(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if c.ExceptionFirst && exceptionRank(a, now) != exceptionRank(b, now) {
			return exceptionRank(a, now) < exceptionRank(b, now)
		}
		switch mode {
		case "manual":
			x, ax := rank[a.IP]
			y, by := rank[b.IP]
			if ax != by {
				return ax
			}
			if ax && x != y {
				return x < y
			}
		case "name_asc", "name_desc":
			x, y := deviceSortName(a), deviceSortName(b)
			if (x == "") != (y == "") {
				return x != ""
			}
			if cmp := naturalNameCompare(x, y); cmp != 0 {
				if mode == "name_desc" {
					return cmp > 0
				}
				return cmp < 0
			}
		case "ip_desc":
			return ipLess(b.IP, a.IP)
		}
		return ipLess(a.IP, b.IP)
	})
}

func (a *App) orderedDeviceIPsLocked() []string {
	ds := make([]*Device, 0, len(a.devices))
	for _, d := range a.devices {
		ds = append(ds, d)
	}
	sortDeviceList(ds, a.config)
	ips := make([]string, 0, len(ds))
	for _, d := range ds {
		ips = append(ips, d.IP)
	}
	return ips
}

func (a *App) setListSort(mode string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if listSortMode(Config{ListSort: mode}) != mode {
		return fmt.Errorf("未知排序方式")
	}
	old := a.config
	if mode == "manual" && len(a.config.ManualOrder) == 0 {
		a.config.ManualOrder = a.orderedDeviceIPsLocked()
	}
	a.config.ListSort = mode
	if err := a.saveLocked(); err != nil {
		a.config = old
		return err
	}
	return nil
}

// Move within the displayed subset, retaining filtered/hidden devices in their slots.
// The first manual move captures the current automatic order; new devices append.
func (a *App) moveListDevices(visible, selected []string, action string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if action != "up" && action != "down" && action != "top" && action != "bottom" {
		return fmt.Errorf("未知移动方式")
	}
	order := a.orderedDeviceIPsLocked()
	shown, chosen := map[string]bool{}, map[string]bool{}
	for _, ip := range visible {
		shown[ip] = true
	}
	for _, ip := range selected {
		chosen[ip] = true
	}
	var subset []string
	var slots []int
	for i, ip := range order {
		if shown[ip] {
			slots = append(slots, i)
			subset = append(subset, ip)
		}
	}
	if !slices.ContainsFunc(subset, func(ip string) bool { return chosen[ip] }) {
		return nil
	}
	switch action {
	case "up":
		for i := 1; i < len(subset); i++ {
			if chosen[subset[i]] && !chosen[subset[i-1]] {
				subset[i-1], subset[i] = subset[i], subset[i-1]
			}
		}
	case "down":
		for i := len(subset) - 2; i >= 0; i-- {
			if chosen[subset[i]] && !chosen[subset[i+1]] {
				subset[i], subset[i+1] = subset[i+1], subset[i]
			}
		}
	case "top", "bottom":
		sort.SliceStable(subset, func(i, j int) bool {
			if action == "top" {
				return chosen[subset[i]] && !chosen[subset[j]]
			}
			return !chosen[subset[i]] && chosen[subset[j]]
		})
	}
	for i, slot := range slots {
		order[slot] = subset[i]
	}
	old := a.config
	a.config.ListSort, a.config.ManualOrder = "manual", order
	a.config.ExceptionFirst = false
	if err := a.saveLocked(); err != nil {
		a.config = old
		return err
	}
	return nil
}

func exceptionRank(d *Device, now time.Time) int {
	switch sampleHealthView(d.Latest, now).State {
	case "unavailable":
		return 0
	case "zero":
		return 1
	case "missing":
		return 2
	case "stale":
		return 3
	default:
		return 4
	}
}
func (a *App) setExceptionFirst(enabled bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.config.ExceptionFirst
	a.config.ExceptionFirst = enabled
	if err := a.saveLocked(); err != nil {
		a.config.ExceptionFirst = old
		return err
	}
	return nil
}
func (a *App) setMinerNote(ip, note string) error {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 200 {
		return fmt.Errorf("备注最多200字")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.devices[ip]
	if d == nil {
		return fmt.Errorf("矿机不存在")
	}
	old := d.Note
	d.Note = note
	if err := a.saveLocked(); err != nil {
		d.Note = old
		return err
	}
	return nil
}
