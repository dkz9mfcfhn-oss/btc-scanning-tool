//go:build windows

package main

import "unsafe"

func boxRect(b uiBox) rect { return rect{b.X, b.Y, b.X + b.W, b.Y + b.H} }

// Owner-draw controls receive a client-local DC, which can be buffered by
// Windows. Use the HWND instead of inferring its origin from that DC.
func paintControlSurface(dc, control uintptr, bounds, surface rect, tint uintptr, radius int32) {
	origin, canvas := paintControlBackdrop(dc, control, bounds)
	paintGlassTileAt(dc, surface, origin, canvas, tint, radius, true, false)
}

func paintControlBackdrop(dc, control uintptr, bounds rect) (point, rect) {
	root := rootWindow(control)
	canvas := canvasRect(root)
	physicalAt := point{}
	call(user, "MapWindowPoints", control, root, uintptr(unsafe.Pointer(&physicalAt)), 1)
	dpi := windowDPI(root)
	origin := point{unscaleUI(physicalAt.X, dpi), unscaleUI(physicalAt.Y, dpi)}
	if display := displays[root]; display != nil {
		origin.X += display.v.X
		origin.Y += display.v.Y
		if box, ok := display.children[control]; ok {
			origin = point{box.X, box.Y}
		}
	}
	paintSceneBackdrop(dc, control, root, physicalAt)
	if root == desktop.h {
		layout := desktopGeometry(canvas.R, canvas.B)
		for _, surface := range []uiBox{layout.ScanPanel, layout.Cards[profitCardIndex]} {
			panelRect := offsetRect(boxRect(surface), -origin.X, -origin.Y)
			if bounds.L >= panelRect.R+8 || panelRect.L-8 >= bounds.R || bounds.T >= panelRect.B+8 || panelRect.T-8 >= bounds.B {
				continue
			}
			// Draw the same panel in root logical coordinates, translated by
			// the child's real pixel offset. This avoids independent rounding
			// at the edge between the parent and its owner-drawn controls.
			var dcOrigin point
			call(gdi, "LPtoDP", dc, uintptr(unsafe.Pointer(&dcOrigin)), 1)
			pan := windowScrollPixels(root)
			saved := call(gdi, "SaveDC", dc)
			call(gdi, "SetViewportOrgEx", dc, uintptr(dcOrigin.X-physicalAt.X-pan.X), uintptr(dcOrigin.Y-physicalAt.Y-pan.Y), 0)
			paintGlassTileAt(dc, boxRect(surface), point{}, canvas, panel, panelCornerDiameter, true, true)
			call(gdi, "RestoreDC", dc, saved)
		}
	}
	return origin, canvas
}

// WM_CTLCOLORSTATIC runs before the native label draws its current text.
// Repaint its backdrop, then keep the label's background clear.
func glassLabelColor(dc, control uintptr) uintptr {
	var pixels rect
	call(user, "GetClientRect", control, uintptr(unsafe.Pointer(&pixels)))
	finish := logicalDC(dc, control, false)
	paintControlBackdrop(dc, control, logicalRect(pixels, control))
	finish()
	call(gdi, "SetTextColor", dc, ink)
	call(gdi, "SetBkMode", dc, 1)
	return call(gdi, "GetStockObject", 5) // NULL_BRUSH
}

func offsetRect(r rect, x, y int32) rect { return rect{r.L + x, r.T + y, r.R + x, r.B + y} }

// Keep native table rendering and scrollbars, but draw no pixels in its four
// outer corners. Windows owns the region once SetWindowRgn succeeds.
func roundTableWindow(h uintptr) {
	var bounds rect
	if call(user, "GetWindowRect", h, uintptr(unsafe.Pointer(&bounds))) == 0 {
		return
	}
	diameter := max(2, scaleUI(20, windowDPI(h)))
	rgn := call(gdi, "CreateRoundRectRgn", 0, 0, uintptr(bounds.R-bounds.L), uintptr(bounds.B-bounds.T), uintptr(diameter), uintptr(diameter))
	if rgn != 0 && call(user, "SetWindowRgn", h, rgn, 1) == 0 {
		call(gdi, "DeleteObject", rgn)
	}
}
