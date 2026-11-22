//go:build windows

package main

import (
	"strings"
	"syscall"
	"unsafe"
)

// UI-thread owned. Each top-level window keeps its own DPI; a detail dialog may
// live on a different monitor from the dashboard.
type displayState struct {
	dpi, minW, minH int32
	v               displayViewport
	children        map[uintptr]uiBox
	rows            map[uintptr]uintptr
	columns         map[uintptr][]displayColumnWidth
	busy            bool
}

var displays = map[uintptr]*displayState{}
var displayFonts = map[int32]uintptr{}
var displayIcons = map[int32]uintptr{}

func enableDisplayScaling() {
	p := user.NewProc("SetProcessDpiAwarenessContext")
	if p.Find() == nil {
		if ok, _, _ := p.Call(^uintptr(3)); ok != 0 {
			return
		}
	} // PER_MONITOR_AWARE_V2
	p = syscall.NewLazyDLL("shcore.dll").NewProc("SetProcessDpiAwareness")
	if p.Find() == nil {
		if hr, _, _ := p.Call(2); int32(hr) >= 0 {
			return
		}
	}
	call(user, "SetProcessDPIAware")
}
func rootWindow(h uintptr) uintptr { return call(user, "GetAncestor", h, 2) }
func windowDPI(h uintptr) int32    { return contentDPI(systemWindowDPI(h), uiScalePercent) }

func systemWindowDPI(h uintptr) int32 {
	if s := displays[rootWindow(h)]; s != nil {
		return s.dpi
	}
	p := user.NewProc("GetDpiForWindow")
	if p.Find() == nil {
		if dpi, _, _ := p.Call(h); dpi != 0 {
			return int32(dpi)
		}
	}
	dc := call(user, "GetDC", h)
	dpi := int32(call(gdi, "GetDeviceCaps", dc, 88))
	call(user, "ReleaseDC", h, dc)
	if dpi <= 0 {
		return 96
	}
	return dpi
}
func nativeFont(h uintptr) uintptr {
	dpi := windowDPI(h)
	if f := displayFonts[dpi]; f != 0 {
		return f
	}
	f := call(gdi, "CreateFontW", uintptr(-int64(scaleUI(16, dpi))), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(wide("Microsoft YaHei UI"))))
	displayFonts[dpi] = f
	return f
}
func releaseDisplayFonts() {
	for _, f := range displayFonts {
		call(gdi, "DeleteObject", f)
	}
	for _, icon := range displayIcons {
		call(user, "DestroyIcon", icon)
	}
}
func displayIcon(size int32) uintptr {
	if icon := displayIcons[size]; icon != 0 {
		return icon
	}
	instance := call(kernel, "GetModuleHandleW", 0)
	icon := call(user, "LoadImageW", instance, 1, 1, uintptr(size), uintptr(size), 0)
	if icon == 0 {
		return tray.icon
	}
	displayIcons[size] = icon
	return icon
}
func applyWindowIcons(h uintptr) {
	dpi := systemWindowDPI(h)
	send.Call(h, 0x80, 0, displayIcon(scaleUI(16, dpi)))
	send.Call(h, 0x80, 1, displayIcon(scaleUI(32, dpi)))
}
func setControlFont(h uintptr) {
	send.Call(h, 0x30, nativeFont(h), 1)
	call(comctl, "SetWindowSubclass", h, displayChildCallback, 79, supportsGlassHover(h))
}

var displayChildCallback uintptr

func init() { displayChildCallback = syscall.NewCallback(displayChildProc) }
func displayChildProc(h uintptr, m uint32, w, l, id, data uintptr) uintptr {
	if data != 0 {
		trackGlassHover(h, m)
	}
	if m == 7 { // Keyboard navigation also reveals controls in a small viewport.
		root := rootWindow(h)
		if s := displays[root]; s != nil {
			if b, ok := s.children[h]; ok {
				// Combo box stored height includes the drop-down, not just the field.
				height := b.H
				var cls [32]uint16
				call(user, "GetClassNameW", h, uintptr(unsafe.Pointer(&cls[0])), 32)
				if strings.EqualFold(syscall.UTF16ToString(cls[:]), "COMBOBOX") {
					height = 32
				}
				s.v.X = revealAxis(s.v.X, s.v.W, b.X, b.W, s.v.CanvasW)
				s.v.Y = revealAxis(s.v.Y, s.v.H, b.Y, height, s.v.CanvasH)
				positionDisplay(root)
			}
		}
	}
	if m == 0x82 {
		call(comctl, "RemoveWindowSubclass", h, displayChildCallback, id)
	}
	return call(comctl, "DefSubclassProc", h, uintptr(m), w, l)
}

func monitorWork(h uintptr) uiBox {
	m := call(user, "MonitorFromWindow", h, 2)
	info := struct {
		Size          uint32
		Monitor, Work rect
		Flags         uint32
	}{}
	info.Size = uint32(unsafe.Sizeof(info))
	if call(user, "GetMonitorInfoW", m, uintptr(unsafe.Pointer(&info))) != 0 {
		r := info.Work
		return uiBox{r.L, r.T, r.R - r.L, r.B - r.T}
	}
	var r rect
	call(user, "SystemParametersInfoW", 0x30, 0, uintptr(unsafe.Pointer(&r)), 0)
	return uiBox{r.L, r.T, r.R - r.L, r.B - r.T}
}
func newNativeWindow(class *uint16, title string, style, owner, instance uintptr, prefW, prefH, minW, minH int32) uintptr {
	wa := monitorWork(owner)
	h := call(user, "CreateWindowExW", 0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(wide(title))), style|0x00300000, uintptr(wa.X+20), uintptr(wa.Y+20), 640, 480, owner, 0, instance, 0)
	if h == 0 {
		return 0
	}
	dpi := systemWindowDPI(h)
	displays[h] = &displayState{dpi: dpi, minW: minW, minH: minH, children: map[uintptr]uiBox{}, rows: map[uintptr]uintptr{}, columns: map[uintptr][]displayColumnWidth{}}
	applyWindowIcons(h)
	b := fitWindowBox(uiBox{wa.X + (wa.W-scaleUI(prefW, dpi))/2, wa.Y + (wa.H-scaleUI(prefH, dpi))/2, scaleUI(prefW, dpi), scaleUI(prefH, dpi)}, wa)
	call(user, "SetWindowPos", h, 0, uintptr(b.X), uintptr(b.Y), uintptr(b.W), uintptr(b.H), 0x14)
	canvasSize(h)
	return h
}

type scrollInfo struct {
	Size, Mask uint32
	Min, Max   int32
	Page       uint32
	Pos, Track int32
}

func updateScroll(h uintptr, bar int32, total, page, pos int32) {
	i := scrollInfo{Size: uint32(unsafe.Sizeof(scrollInfo{})), Mask: 7, Max: max(0, total-1), Page: uint32(max(1, page)), Pos: pos}
	call(user, "SetScrollInfo", h, uintptr(bar), uintptr(unsafe.Pointer(&i)), 1)
}
func canvasSize(h uintptr) (int32, int32) {
	s := displays[h]
	if s == nil {
		return 0, 0
	}
	if s.busy {
		return s.v.CanvasW, s.v.CanvasH
	}
	s.busy = true
	defer func() { s.busy = false }()
	// Both scrollbars influence client size. Iterate to stabilize their visibility.
	for i := 0; i < 3; i++ {
		var r rect
		call(user, "GetClientRect", h, uintptr(unsafe.Pointer(&r)))
		dpi := windowDPI(h)
		w, height := unscaleUI(r.R, dpi), unscaleUI(r.B, dpi)
		if h == desktop.h {
			s.v = dashboardViewport(w, height, desktop.fullList, s.v.X, s.v.Y)
		} else {
			s.v = fitViewport(w, height, s.minW, s.minH, s.v.X, s.v.Y)
		}
		updateScroll(h, 0, s.v.CanvasW, s.v.W, s.v.X)
		updateScroll(h, 1, s.v.CanvasH, s.v.H, s.v.Y)
	}
	return s.v.CanvasW, s.v.CanvasH
}
func placeScaled(h uintptr, b uiBox, save bool) {
	if h == 0 {
		return
	}
	dpi := windowDPI(h)
	x, y := b.X, b.Y
	parent := call(user, "GetParent", h)
	if s := displays[parent]; s != nil {
		if save {
			s.children[h] = b
		}
		x -= s.v.X
		y -= s.v.Y
	}
	// Scale edges rather than accumulating rounded widths across adjacent controls.
	repaint := uintptr(1)
	if parent == desktop.h && !desktop.dashboardReady {
		repaint = 0
	}
	move.Call(h, uintptr(scaleUI(x, dpi)), uintptr(scaleUI(y, dpi)), uintptr(scaleUI(x+b.W, dpi)-scaleUI(x, dpi)), uintptr(scaleUI(y+b.H, dpi)-scaleUI(y, dpi)), repaint)
}
func positionDisplay(h uintptr) {
	if s := displays[h]; s != nil {
		updateScroll(h, 0, s.v.CanvasW, s.v.W, s.v.X)
		updateScroll(h, 1, s.v.CanvasH, s.v.H, s.v.Y)
		for child, b := range s.children {
			placeScaled(child, b, false)
		}
		call(user, "RedrawWindow", h, 0, 0, 0x85) // invalidate, erase, all children
	}
}
func relayoutDisplay(h uintptr) {
	canvasSize(h)
	switch h {
	case desktop.h:
		if desktop.list != 0 {
			layout(0, 0)
		}
	case desktop.detailWindow:
		if desktop.detailEdit != 0 {
			layoutDetail(0, 0)
		}
	case desktop.diagnosticWindow:
		if desktop.diagnosticMessage != 0 {
			layoutDiagnosticResult(0, 0)
		}
	}
	positionDisplay(h)
}
func setListRowHeight(h uintptr) {
	s := displays[rootWindow(h)]
	if s == nil {
		return
	}
	img := call(comctl, "ImageList_Create", 1, uintptr(scaleUI(38, windowDPI(h))), 0, 1, 1)
	if img == 0 {
		return
	}
	send.Call(h, 0x1003, 1, img)
	if old := s.rows[h]; old != 0 {
		call(comctl, "ImageList_Destroy", old)
	}
	s.rows[h] = img
}

func setDisplayColumnWidths(list uintptr, widths []int32) {
	s := displays[rootWindow(list)]
	if s == nil {
		return
	}
	dpi := windowDPI(list)
	columns := make([]displayColumnWidth, len(widths))
	for i, width := range widths {
		pixels := scaleUI(width, dpi)
		send.Call(list, 0x101e, uintptr(i), uintptr(pixels))
		columns[i] = displayColumnWidth{logical: width, lastPixels: pixels}
	}
	s.columns[list] = columns
}

const fitMinerListColumnsMessage = 0x8000 + 83 // WM_APP

var minerListFitPending bool

func scheduleMinerListFit(list uintptr) {
	if !minerListFitPending {
		// ListView updates its scrollbars inside its own message handlers.
		// Measure after those handlers have finished and coalesce nested updates.
		minerListFitPending = call(user, "PostMessageW", list, fitMinerListColumnsMessage, 0, 0) != 0
	}
}

func fitMinerListToClient(list uintptr) {
	s := displays[desktop.h]
	if s == nil {
		return
	}
	columns := s.columns[list]
	clientWidth := windowClientRect(list).R
	if clientWidth <= 0 || len(columns) == 0 {
		return
	}
	dpi := windowDPI(list)
	widths := make([]int32, len(columns))
	for i := range columns {
		actual, _, _ := send.Call(list, 0x101d, uintptr(i), 0) // LVM_GETCOLUMNWIDTH
		widths[i] = columns[i].remember(int32(actual), dpi)
	}
	last, extra := fitListColumns(widths, nativeColumnOrder(true), clientWidth)
	for i, width := range widths {
		add := int32(0)
		if i == last {
			add = extra
		}
		if target := width + add; target != columns[i].lastPixels {
			if ok, _, _ := send.Call(list, 0x101e, uintptr(i), uintptr(target)); ok != 0 { // LVM_SETCOLUMNWIDTH
				columns[i].lastPixels = target
				columns[i].fillPixels = add
			}
		}
	}
	if windowClientRect(list).R != clientWidth {
		scheduleMinerListFit(list)
	}
}

var updateFontCallback = syscall.NewCallback(func(h, l uintptr) uintptr { send.Call(h, 0x30, nativeFont(h), 1); return 1 })

func displayMessage(h uintptr, m uint32, w, l uintptr) (uintptr, bool) {
	s := displays[h]
	if m == 0x24 && l != 0 {
		p := (*[10]int32)(unsafe.Pointer(l))
		wa := monitorWork(h)
		dpi := systemWindowDPI(h)
		p[6] = min(scaleUI(600, dpi), wa.W)
		p[7] = min(scaleUI(320, dpi), wa.H)
		return 0, true
	}
	if s == nil {
		return 0, false
	}
	switch m {
	case 0x2e0: // WM_DPICHANGED: use this window's new DPI and suggested physical bounds.
		old := windowDPI(h)
		s.dpi = int32(w & 65535)
		if s.dpi < 48 {
			s.dpi = 96
		}
		applyWindowIcons(h)
		refreshDisplayResources(h, old)
		if l != 0 {
			r := *(*rect)(unsafe.Pointer(l))
			// The suggested rect can exceed a small monitor's work area.
			mi := call(user, "MonitorFromRect", l, 2)
			info := struct {
				Size          uint32
				Monitor, Work rect
				Flags         uint32
			}{}
			info.Size = uint32(unsafe.Sizeof(info))
			b := uiBox{r.L, r.T, r.R - r.L, r.B - r.T}
			if call(user, "GetMonitorInfoW", mi, uintptr(unsafe.Pointer(&info))) != 0 {
				a := info.Work
				b = fitWindowBox(b, uiBox{a.L, a.T, a.R - a.L, a.B - a.T})
			}
			call(user, "SetWindowPos", h, 0, uintptr(b.X), uintptr(b.Y), uintptr(b.W), uintptr(b.H), 0x14)
		}
		relayoutDisplay(h)
		return 0, true
	case 5:
		if w != 1 && !s.busy {
			relayoutDisplay(h)
		}
		return 0, true
	case 0x114, 0x115:
		if l != 0 {
			return 0, false
		}
		bar := int32(0)
		pos, page, total := s.v.X, s.v.W, s.v.CanvasW
		if m == 0x115 {
			bar = 1
			pos, page, total = s.v.Y, s.v.H, s.v.CanvasH
		}
		switch w & 65535 {
		case 0:
			pos -= 32
		case 1:
			pos += 32
		case 2:
			pos -= page - 32
		case 3:
			pos += page - 32
		case 4, 5:
			i := scrollInfo{Size: uint32(unsafe.Sizeof(scrollInfo{})), Mask: 0x10}
			call(user, "GetScrollInfo", h, uintptr(bar), uintptr(unsafe.Pointer(&i)))
			pos = i.Track
		case 6:
			pos = 0
		case 7:
			pos = total
		}
		pos = min(max(0, pos), max(0, total-page))
		if bar == 0 {
			s.v.X = pos
		} else {
			s.v.Y = pos
		}
		positionDisplay(h)
		return 0, true
	case 0x20a:
		delta := int32(int16(w >> 16))
		if w&4 != 0 {
			s.v.X = min(max(0, s.v.X-delta*72/120), max(0, s.v.CanvasW-s.v.W))
		} else {
			s.v.Y = min(max(0, s.v.Y-delta*72/120), max(0, s.v.CanvasH-s.v.H))
		}
		positionDisplay(h)
		return 0, true
	case 0x7e, 0x1a: // Display removed/resolution or work-area changed.
		if m == 0x7e || w == 0x2f {
			var r rect
			call(user, "GetWindowRect", h, uintptr(unsafe.Pointer(&r)))
			b := fitWindowBox(uiBox{r.L, r.T, r.R - r.L, r.B - r.T}, monitorWork(h))
			call(user, "SetWindowPos", h, 0, uintptr(b.X), uintptr(b.Y), uintptr(b.W), uintptr(b.H), 0x14)
			relayoutDisplay(h)
		}
	case 0x82:
		for _, img := range s.rows {
			call(comctl, "ImageList_Destroy", img)
		}
		delete(displays, h)
	}
	return 0, false
}

// DrawIconEx sizes are device pixels even when the surrounding GDI mapping is
// logical. Draw the icon in an explicitly physical DC to match adjacent text.
func drawDashboardIcon(dc, h uintptr) {
	saved := call(gdi, "SaveDC", dc)
	defer call(gdi, "RestoreDC", dc, saved)
	dpi := windowDPI(h)
	p := point{24, 22}
	call(gdi, "LPtoDP", dc, uintptr(unsafe.Pointer(&p)), 1)
	call(gdi, "SetMapMode", dc, 1)
	call(gdi, "SetWindowOrgEx", dc, 0, 0, 0)
	call(gdi, "SetViewportOrgEx", dc, 0, 0, 0)
	size := scaleUI(38, dpi)
	call(user, "DrawIconEx", dc, uintptr(p.X), uintptr(p.Y), displayIcon(size), uintptr(size), uintptr(size), 0, 0, 3)
}

// Custom GDI drawing uses logical coordinates and 96-DPI fonts. Native controls
// use physical fonts. Save/restore prevents leaking the mapping into default draw.
func logicalDC(dc, h uintptr, scroll bool) func() {
	saved := call(gdi, "SaveDC", dc)
	dpi := windowDPI(h)
	call(gdi, "SetMapMode", dc, 8)
	call(gdi, "SetWindowExtEx", dc, 96, 96, 0)
	call(gdi, "SetViewportExtEx", dc, uintptr(dpi), uintptr(dpi), 0)
	if scroll {
		if s := displays[h]; s != nil {
			call(gdi, "SetViewportOrgEx", dc, uintptr(-scaleUI(s.v.X, dpi)), uintptr(-scaleUI(s.v.Y, dpi)), 0)
		}
	}
	return func() { call(gdi, "RestoreDC", dc, saved) }
}
func logicalRect(r rect, h uintptr) rect {
	d := windowDPI(h)
	return rect{unscaleUI(r.L, d), unscaleUI(r.T, d), unscaleUI(r.R, d), unscaleUI(r.B, d)}
}

func windowClientRect(h uintptr) rect {
	var r rect
	call(user, "GetClientRect", h, uintptr(unsafe.Pointer(&r)))
	return r
}

func canvasRect(h uintptr) rect {
	if s := displays[h]; s != nil {
		return rect{0, 0, s.v.CanvasW, s.v.CanvasH}
	}
	return logicalRect(windowClientRect(h), h)
}

func windowScrollPixels(h uintptr) point {
	if s := displays[h]; s != nil {
		dpi := windowDPI(h)
		return point{scaleUI(s.v.X, dpi), scaleUI(s.v.Y, dpi)}
	}
	return point{}
}

// Begin a pixel-aligned paint pass while keeping the original native clip.
// The returned logical-zero point includes any scroll/dirty-buffer translation.
func pixelPaintDC(dc uintptr) (point, func()) {
	var origin point
	call(gdi, "LPtoDP", dc, uintptr(unsafe.Pointer(&origin)), 1)
	saved := call(gdi, "SaveDC", dc)
	call(gdi, "SetMapMode", dc, 1) // MM_TEXT
	call(gdi, "SetWindowOrgEx", dc, 0, 0, 0)
	call(gdi, "SetViewportOrgEx", dc, 0, 0, 0)
	return origin, func() { call(gdi, "RestoreDC", dc, saved) }
}

// Shared by monitor DPI changes and the user's independent content zoom.
func refreshDisplayResources(h uintptr, old int32) {
	call(user, "EnumChildWindows", h, updateFontCallback, 0)
	s := displays[h]
	dpi := windowDPI(h)
	for list := range s.rows {
		columns := s.columns[list]
		for i := range columns {
			width := int32(call(user, "SendMessageW", list, 0x101d, uintptr(i), 0))
			pixels := columns[i].rescale(width, old, dpi)
			send.Call(list, 0x101e, uintptr(i), uintptr(pixels))
		}
		setListRowHeight(list)
	}
	resetGlassCache()
}
