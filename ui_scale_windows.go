//go:build windows

package main

import (
	"fmt"
	"unsafe"
)

// Accessed only on the native UI thread; background workers read saved config.
var uiScalePercent = 100

func uiScaleCaption() string { return fmt.Sprintf(tr("界面缩放 %d%% ▾"), uiScalePercent) }

func openUIScaleMenu() {
	menu := call(user, "CreatePopupMenu")
	if menu == 0 {
		return
	}
	defer call(user, "DestroyMenu", menu)
	for i, percent := range uiScaleChoices {
		label := fmt.Sprintf("%d%%", percent)
		if percent == 100 {
			label += tr("（默认，跟随系统）")
		}
		flags := uintptr(0)
		if percent == uiScalePercent {
			flags = 8
		}
		listMenuItem(menu, flags, uintptr(i+1), label)
	}
	var r rect
	call(user, "GetWindowRect", desktop.zoomButton, uintptr(unsafe.Pointer(&r)))
	choice := trackListMenu(menu, point{r.L, r.B})
	if choice > 0 && int(choice) <= len(uiScaleChoices) {
		applyUIScale(uiScaleChoices[choice-1])
	}
}

func applyUIScale(percent int) {
	if percent == uiScalePercent {
		return
	}
	if err := desktop.app.setUIScale(percent); err != nil {
		message(tr("无法保存界面大小：") + err.Error())
		return
	}
	oldDPI := map[uintptr]int32{}
	for h := range displays {
		oldDPI[h] = windowDPI(h)
	}
	uiScalePercent = percent
	txt(desktop.zoomButton, uiScaleCaption())
	for h, s := range displays {
		refreshDisplayResources(h, oldDPI[h])
		// Reset stale scroll offsets; Ctrl+0 remains available at every zoom level.
		s.v.X, s.v.Y = 0, 0
		relayoutDisplay(h)
	}
}

func handleZoomKey(m *msg) bool {
	if m.ID != 0x100 || displays[rootWindow(m.H)] == nil || call(user, "GetKeyState", 0x11)&0x8000 == 0 {
		return false
	}
	if call(user, "GetKeyState", 0x12)&0x8000 != 0 {
		return false
	} // leave AltGr input alone
	switch m.W {
	case 0x30, 0x60:
		applyUIScale(100)
	case 0xbb, 0x6b:
		applyUIScale(adjacentUIScale(uiScalePercent, 1))
	case 0xbd, 0x6d:
		applyUIScale(adjacentUIScale(uiScalePercent, -1))
	default:
		return false
	}
	return true
}
