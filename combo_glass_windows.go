//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

const comboGlassSubclassID = 80

var comboGlassCallback uintptr

func init() { comboGlassCallback = syscall.NewCallback(comboGlassProc) }

func installComboGlass(h uintptr) {
	if h != 0 {
		call(comctl, "SetWindowSubclass", h, comboGlassCallback, comboGlassSubclassID, 0)
	}
}

// The collapsed CBS_DROPDOWNLIST is painted as glass. The native combo window
// still owns keyboard navigation, accessibility, selection and its separate popup.
func comboGlassProc(h uintptr, m uint32, w, l, id, data uintptr) uintptr {
	switch m {
	case 0x14: // WM_ERASEBKGND: paint the surrounding material as part of the face.
		return 1
	case 0x0f: // WM_PAINT
		if w != 0 {
			paintComboGlass(h, w)
			return 0
		}
		var ps paint
		dc := call(user, "BeginPaint", h, uintptr(unsafe.Pointer(&ps)))
		if dc != 0 {
			paintComboGlass(h, dc)
		}
		call(user, "EndPaint", h, uintptr(unsafe.Pointer(&ps)))
		return 0
	case 0x318: // WM_PRINTCLIENT
		if w != 0 && (l&1 == 0 || call(user, "IsWindowVisible", h) != 0) {
			paintComboGlass(h, w)
		}
		return 0
	case 0x82: // WM_NCDESTROY
		call(comctl, "RemoveWindowSubclass", h, comboGlassCallback, id)
	}
	result := call(comctl, "DefSubclassProc", h, uintptr(m), w, l)
	switch m {
	case 7, 8, 0x0a, 0x30, 0x14e, 0x14f: // focus, enable, font, selection, dropdown state
		invalidate.Call(h, 0, 0)
	}
	return result
}

func paintComboGlass(h, dc uintptr) {
	var pixels rect
	call(user, "GetClientRect", h, uintptr(unsafe.Pointer(&pixels)))
	bounds := logicalRect(pixels, h)
	if bounds.R-bounds.L < 12 || bounds.B-bounds.T < 8 {
		return
	}
	defer logicalDC(dc, h, false)()

	face := rect{bounds.L + 1, bounds.T + 1, bounds.R - 1, bounds.B - 1}
	tint, foreground := buttonColor, ink
	if call(user, "IsWindowEnabled", h) == 0 {
		tint, foreground = panel, muted
	} else if call(user, "SendMessageW", h, 0x157, 0, 0) != 0 { // CB_GETDROPPEDSTATE
		tint = selectionColor
	} else {
		tint = interactiveGlassTint(h, tint, false)
	}
	paintControlSurface(dc, h, bounds, face, tint, face.B-face.T)
	if call(user, "GetFocus") == h {
		glassOutline(dc, rect{face.L + 2, face.T + 2, face.R - 2, face.B - 2}, 16, blue)
	}
	font := desktop.font
	if currentLanguage() != chinese && rootWindow(h) == desktop.h {
		font = ui.smallFont
	}
	labelBox(dc, rect{face.L + 9, face.T, face.R - 23, face.B}, value(h), font, foreground, 0)
	centerX, centerY := face.R-13, (face.T+face.B)/2
	paintInterfaceIcon(dc, uiBox{centerX - 6, centerY - 6, 12, 12}, iconChevron, foreground)
}
