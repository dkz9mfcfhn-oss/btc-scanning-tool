//go:build windows

package main

import (
	"maps"
	"slices"
	"unsafe"
)

// The leading fields of NMITEMACTIVATE include the clicked row index.
type listItemClick struct {
	Header nmhdr
	Index  int32
	Sub    int32
}

func listMenuItem(menu, flags, id uintptr, label string) {
	call(user, "AppendMenuW", menu, flags, id, uintptr(unsafe.Pointer(wide(label))))
}

func trackListMenu(menu uintptr, p point) uintptr {
	return call(user, "TrackPopupMenu", menu, 0x100|0x2, uintptr(p.X), uintptr(p.Y), 0, desktop.h, 0)
}

func openListSettings() {
	menu := call(user, "CreatePopupMenu")
	if menu == 0 {
		return
	}
	defer call(user, "DestroyMenu", menu)
	a := desktop.app
	a.mu.Lock()
	config := a.config
	a.mu.Unlock()

	listMenuItem(menu, 0, 1, tr("恢复精简列"))
	listMenuItem(menu, 0, 2, tr("显示全部列"))
	listMenuItem(menu, 0, 3, tr("查看已隐藏矿机"))
	var priorityFlags uintptr
	if config.ExceptionFirst {
		priorityFlags = 8
	}
	listMenuItem(menu, priorityFlags, 5, tr("异常优先显示"))
	selection := selectedRebootIPs()
	filter, _, _ := send.Call(desktop.filter, 0x147, 0, 0)
	hide := filter != hiddenMinersFilter
	if len(selection) > 0 {
		label := tr("隐藏所选矿机")
		if !hide {
			label = tr("恢复显示所选矿机")
		}
		listMenuItem(menu, 0, 4, label)
	}
	call(user, "AppendMenuW", menu, 0x800, 0, 0)
	appendSortMenu(menu, config)
	call(user, "AppendMenuW", menu, 0x800, 0, 0)
	widths := configuredColumnWidths(config)
	for _, index := range nativeColumnOrder(!config.FullColumns) {
		if index == 0 {
			continue
		}
		var flags uintptr
		if widths[index] > 0 {
			flags = 8 // MF_CHECKED
		}
		listMenuItem(menu, flags, uintptr(100+index), tr(nativeListColumns[index].Name))
	}
	var r rect
	call(user, "GetWindowRect", desktop.autoColumns, uintptr(unsafe.Pointer(&r)))
	switch selected := trackListMenu(menu, point{X: r.L, Y: r.B}); {
	case selected == 0:
		return
	case selected == 3:
		txt(desktop.search, "")
		send.Call(desktop.filter, 0x14e, hiddenMinersFilter, 0)
		return
	case selected == 5:
		if err := a.setExceptionFirst(!config.ExceptionFirst); err != nil {
			message(err.Error())
		}
		refreshNative()
		return
	case selected == 4:
		setMinersHidden(selection, hide)
		return
	case selected >= 10 && selected < 10+uintptr(len(listSortChoices)):
		setNativeListSort(listSortChoices[selected-10].Mode)
		return
	case selected == 1 || selected == 2:
		config.FullColumns = selected == 2
		config.ColumnVisibility = nil
	case selected > 100 && selected < 100+uintptr(len(widths)):
		i := selected - 100
		config.ColumnVisibility = maps.Clone(config.ColumnVisibility)
		if config.ColumnVisibility == nil {
			config.ColumnVisibility = map[string]bool{}
		}
		config.ColumnVisibility[nativeListColumns[i].Name] = widths[i] == 0
	default:
		return
	}

	a.mu.Lock()
	old := a.config
	a.config.FullColumns, a.config.ColumnVisibility = config.FullColumns, config.ColumnVisibility
	err := a.saveLocked()
	if err != nil {
		a.config = old
	}
	a.mu.Unlock()
	if err != nil {
		message(tr("保存列表设置失败：") + err.Error())
		return
	}
	applyColumnLayout()
}

func openMinerVisibilityMenu(clicked string) {
	ips := selectedRebootIPs()
	if !slices.Contains(ips, clicked) {
		ips = []string{clicked}
	}
	filter, _, _ := send.Call(desktop.filter, 0x147, 0, 0)
	hide := filter != hiddenMinersFilter
	menu := call(user, "CreatePopupMenu")
	if menu == 0 {
		return
	}
	defer call(user, "DestroyMenu", menu)
	label := tr("隐藏矿机")
	if !hide {
		label = tr("恢复显示矿机")
	}
	if len(ips) > 1 {
		label += tr("（所选）")
	}
	listMenuItem(menu, 0, 1, label)
	listMenuItem(menu, 0, 30, tr("编辑此矿机备注"))
	listMenuItem(menu, 0, 31, tr("查看故障时间线"))
	listMenuItem(menu, 0, 32, tr("设置此矿机的手动功率…"))
	call(user, "AppendMenuW", menu, 0x800, 0, 0)
	for i, label := range []string{tr("上移"), tr("下移"), tr("置顶"), tr("置底")} {
		listMenuItem(menu, 0, uintptr(20+i), label+tr("（手动排序）"))
	}
	a := desktop.app
	a.mu.Lock()
	config := a.config
	a.mu.Unlock()
	call(user, "AppendMenuW", menu, 0x800, 0, 0)
	appendSortMenu(menu, config)
	var p point
	call(user, "GetCursorPos", uintptr(unsafe.Pointer(&p)))
	switch choice := trackListMenu(menu, p); {
	case choice == 32:
		editMinerPower(clicked)
	case choice == 30:
		editMinerNote(clicked)
	case choice == 31:
		desktop.selected = clicked
		openDetail()
		desktop.detailTimelineMode = true
		var r rect
		call(user, "GetClientRect", desktop.detailWindow, uintptr(unsafe.Pointer(&r)))
		layoutDetail(r.R, r.B)
		refreshDetailWindow()
	case choice == 1:
		setMinersHidden(ips, hide)
		refreshNative()
	case choice >= 10 && choice < 10+uintptr(len(listSortChoices)):
		setNativeListSort(listSortChoices[choice-10].Mode)
	case choice >= 20 && choice <= 23:
		actions := []string{"up", "down", "top", "bottom"}
		if err := a.moveListDevices(desktop.ips, ips, actions[choice-20]); err != nil {
			message(tr("保存排序失败：") + err.Error())
			return
		}
		refreshNative()
		// Keep the moved miners selected and the clicked miner in view.
		invalidateListHeader()
		for i, ip := range desktop.ips {
			it := item{StateMask: 3}
			if slices.Contains(ips, ip) {
				it.State = 2
			}
			if ip == clicked {
				it.State |= 1
			}
			send.Call(desktop.list, 0x102b, uintptr(i), uintptr(unsafe.Pointer(&it)))
		}
		desktop.selected = clicked
		if index := slices.Index(desktop.ips, clicked); index >= 0 {
			send.Call(desktop.list, 0x1013, uintptr(index), 0)
		}
		updateDetail()
	}
}

func appendSortMenu(menu uintptr, config Config) {
	for i, choice := range listSortChoices {
		var flags uintptr
		if listSortMode(config) == choice.Mode {
			flags = 8
		}
		listMenuItem(menu, flags, uintptr(10+i), tr(choice.Label))
	}
}

func setNativeListSort(mode string) {
	if err := desktop.app.setListSort(mode); err != nil {
		message(tr("保存排序失败：") + err.Error())
		return
	}
	refreshNative()
	invalidateListHeader()
}

func invalidateListHeader() {
	header, _, _ := send.Call(desktop.list, 0x101f, 0, 0)
	if header != 0 {
		invalidate.Call(header, 0, 1)
	}
}

func toggleNativeListSort(column int32) {
	base := "ip"
	if column == 17 {
		base = "name"
	}
	a := desktop.app
	a.mu.Lock()
	mode := listSortMode(a.config)
	a.mu.Unlock()
	next := base + "_asc"
	if mode == next {
		next = base + "_desc"
	}
	setNativeListSort(next)
}

func setMinersHidden(ips []string, hide bool) {
	a := desktop.app
	a.mu.Lock()
	var changed []*Device
	for _, ip := range ips {
		if d := a.devices[ip]; d != nil && d.Hidden != hide {
			d.Hidden = hide
			changed = append(changed, d)
		}
	}
	var err error
	if len(changed) > 0 {
		err = a.saveLocked()
		if err != nil {
			for _, d := range changed {
				d.Hidden = !hide
			}
		} else {
			for _, d := range changed {
				delete(a.pushStates, d.IP)
			}
		}
	}
	a.mu.Unlock()
	if err != nil {
		message(tr("保存矿机显示设置失败：") + err.Error())
	}
}
