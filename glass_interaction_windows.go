//go:build windows

package main

import (
	"strings"
	"syscall"
	"unsafe"
)

var glassHoverControl uintptr

func supportsGlassHover(h uintptr) uintptr {
	var class [24]uint16
	call(user, "GetClassNameW", h, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
	switch strings.ToUpper(syscall.UTF16ToString(class[:])) {
	case "COMBOBOX":
		return 1
	case "BUTTON":
		style := call(user, "GetWindowLongPtrW", h, ^uintptr(15)) // GWL_STYLE
		if style&0xf == 0xb {
			return 1
		} // BS_OWNERDRAW
	}
	return 0
}

// Update the material on enter/leave only. Moving across a control does not
// continuously invalidate the window or render another cached bitmap.
func trackGlassHover(h uintptr, m uint32) {
	switch m {
	case 0x200: // WM_MOUSEMOVE
		if glassHoverControl == h {
			return
		}
		tracking := struct {
			Size, Flags uint32
			Window      uintptr
			HoverTime   uint32
		}{Flags: 2, Window: h} // TME_LEAVE
		tracking.Size = uint32(unsafe.Sizeof(tracking))
		if call(user, "TrackMouseEvent", uintptr(unsafe.Pointer(&tracking))) == 0 {
			return
		}
		previous := glassHoverControl
		glassHoverControl = h
		if previous != 0 {
			invalidate.Call(previous, 0, 0)
		}
		invalidate.Call(h, 0, 0)
	case 0x2a3, 0x82: // WM_MOUSELEAVE / WM_NCDESTROY
		if glassHoverControl == h {
			glassHoverControl = 0
			if m == 0x2a3 {
				invalidate.Call(h, 0, 0)
			}
		}
	}
}

func interactiveGlassTint(h, tint uintptr, accented bool) uintptr {
	if glassHoverControl != h {
		return tint
	}
	if accented && (glassTheme == "glass" || glassTheme == "light") {
		return mixColor(tint, 0, 9)
	}
	return mixColor(tint, blue, 15)
}
