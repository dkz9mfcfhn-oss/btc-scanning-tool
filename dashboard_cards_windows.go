//go:build windows

package main

import "unsafe"

type dashboardDrag struct {
	index int
	start int32
	cards [5]uiBox
	base  cardWidths
}

var activeCardDrag *dashboardDrag

func desktopGeometry(w, h int32) dashboardLayout {
	return dashboardGeometryWithWidths(w, h, desktop.fullList, desktop.cardWidths)
}

func dashboardPointer(h uintptr, p point) point {
	pan, dpi := windowScrollPixels(h), windowDPI(h)
	return point{unscaleUI(p.X+pan.X, dpi), unscaleUI(p.Y+pan.Y, dpi)}
}

func findCardDivider(cards [5]uiBox, p point) int {
	for i := range len(cards) - 1 {
		r := boxRect(cardDivider(cards, i))
		if p.X >= r.L && p.X < r.R && p.Y >= r.T && p.Y < r.B {
			return i
		}
	}
	return -1
}

func dashboardCardMouse(h uintptr, m uint32, l uintptr) bool {
	switch m {
	case 0x20: // WM_SETCURSOR
		if uint16(l) != 1 { // HTCLIENT
			return false
		}
		var p point
		call(user, "GetCursorPos", uintptr(unsafe.Pointer(&p)))
		call(user, "ScreenToClient", h, uintptr(unsafe.Pointer(&p)))
		cards := desktopGeometry(desktop.width, desktop.height).Cards
		if activeCardDrag == nil && findCardDivider(cards, dashboardPointer(h, p)) < 0 {
			return false
		}
		call(user, "SetCursor", call(user, "LoadCursorW", 0, 32644)) // IDC_SIZEWE
		return true
	case 0x200, 0x201, 0x202, 0x203: // WM_MOUSEMOVE, WM_LBUTTONDOWN/UP/DBLCLK
		if (m == 0x200 || m == 0x202) && activeCardDrag == nil {
			return false
		}
		p := dashboardPointer(h, point{int32(int16(l)), int32(int16(l >> 16))})
		if m == 0x200 || m == 0x202 {
			drag := activeCardDrag
			widths, changed := dragCardDivider(drag.cards, drag.index, p.X-drag.start)
			if !changed {
				widths = drag.base
			}
			if m == 0x202 {
				activeCardDrag = nil
				call(user, "ReleaseCapture")
				updateDashboardWidths(widths, drag.base)
			} else if widths != desktop.cardWidths {
				desktop.cardWidths = widths
				invalidateDashboardCards()
			}
			return true
		}
		cards := desktopGeometry(desktop.width, desktop.height).Cards
		index := findCardDivider(cards, p)
		if index < 0 {
			return false
		}
		if m == 0x203 {
			updateDashboardWidths(cardWidths{}, desktop.cardWidths)
			return true
		}
		call(user, "SetCapture", h)
		if call(user, "GetCapture") == h {
			activeCardDrag = &dashboardDrag{index: index, start: p.X, cards: cards, base: desktop.cardWidths}
			invalidateDashboardCards()
		}
		return true
	case 0x1f, 0x215: // WM_CANCELMODE / WM_CAPTURECHANGED
		cancelDashboardDrag(h)
	}
	return false
}

func cancelDashboardDrag(h uintptr) {
	if drag := activeCardDrag; drag != nil {
		activeCardDrag = nil
		desktop.cardWidths = drag.base
		if call(user, "GetCapture") == h {
			call(user, "ReleaseCapture")
		}
		invalidateDashboardCards()
	}
}

func updateDashboardWidths(widths, previous cardWidths) {
	var err error
	if widths != previous {
		err = desktop.app.setDashboardWidths(widths)
		if err != nil {
			widths = previous
		}
	}
	desktop.cardWidths = widths
	invalidateDashboardCards()
	if err != nil {
		message(tr("保存卡片宽度失败：") + err.Error())
	}
}

func invalidateDashboardCards() {
	h := desktop.h
	first := desktopGeometry(desktop.width, desktop.height).Cards[0]
	dpi, pan := windowDPI(h), windowScrollPixels(h)
	r := rect{0, scaleUI(first.Y-8, dpi) - pan.Y, windowClientRect(h).R, scaleUI(first.Y+first.H+8, dpi) - pan.Y}
	invalidate.Call(h, uintptr(unsafe.Pointer(&r)), 0)
}
