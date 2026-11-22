//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var glassListCallback uintptr

func init() { glassListCallback = syscall.NewCallback(glassListProc) }

// Header WM_NOTIFY messages go to its parent list control, not the main window.
func glassListProc(h uintptr, m uint32, w, l, id, data uintptr) uintptr {
	switch m {
	case fitMinerListColumnsMessage:
		minerListFitPending = false
		header, _, _ := send.Call(h, 0x101f, 0, 0)
		if header == 0 || call(user, "GetCapture") != header {
			fitMinerListToClient(h)
		}
		return 0
	case 0x4e:
		if l != 0 {
			n := (*customDraw)(unsafe.Pointer(l))
			if result, ok := glassHeaderDraw(n); ok {
				return result
			}
			switch n.Header.Code {
			case -301, -321, -307, -327, -16: // HDN_ITEMCHANGED, HDN_ENDTRACK, NM_RELEASEDCAPTURE
				scheduleMinerListFit(h)
			}
		}
	case 5: // WM_SIZE
		scheduleMinerListFit(h)
	case 0x82: // WM_NCDESTROY
		minerListFitPending = false
		call(comctl, "RemoveWindowSubclass", h, glassListCallback, id)
	}
	return call(comctl, "DefSubclassProc", h, uintptr(m), w, l)
}

// Glass samples the app's procedurally diffused scene. Native text is painted
// separately so it stays crisp as the mist and reflections are scaled.
func mixColor(a, b uintptr, amount int) uintptr {
	amount = max(0, min(100, amount))
	var c uintptr
	for shift := uint(0); shift < 24; shift += 8 {
		x, y := int((a>>shift)&255), int((b>>shift)&255)
		c |= uintptr((x*(100-amount)+y*amount)/100) << shift
	}
	return c
}

func gradientFill(dc uintptr, r rect, top, bottom uintptr) {
	if r.B <= r.T || r.R <= r.L {
		return
	}
	const steps = int32(48)
	for i := int32(0); i < steps; i++ {
		y1, y2 := r.T+(r.B-r.T)*i/steps, r.T+(r.B-r.T)*(i+1)/steps
		if y2 > y1 {
			fill(dc, rect{r.L, y1, r.R, y2}, mixColor(top, bottom, int(i)*100/int(steps-1)))
		}
	}
}

func glassOutline(dc uintptr, r rect, radius int32, color uintptr) {
	pen := call(gdi, "CreatePen", 0, 1, color)
	oldPen := call(gdi, "SelectObject", dc, pen)
	oldBrush := call(gdi, "SelectObject", dc, call(gdi, "GetStockObject", 5)) // NULL_BRUSH
	call(gdi, "RoundRect", dc, uintptr(r.L), uintptr(r.T), uintptr(r.R), uintptr(r.B), uintptr(radius), uintptr(radius))
	call(gdi, "SelectObject", dc, oldBrush)
	call(gdi, "SelectObject", dc, oldPen)
	call(gdi, "DeleteObject", pen)
}

type cachedGlassTile struct {
	backdrop []byte
	bitmap   uintptr
}

// Only the UI thread accesses this bounded cache. Surface DIBs are reusable
// and released on eviction or a theme change.
var glassTiles = map[glassTileKey]cachedGlassTile{}
var glassTileBytes int
var glassTheme string
var glassSourceDC uintptr
var alphaGlass = syscall.NewLazyDLL("msimg32.dll").NewProc("AlphaBlend")

func resetGlassCache() {
	for _, tile := range glassTiles {
		if tile.bitmap != 0 {
			call(gdi, "DeleteObject", tile.bitmap)
		}
	}
	clear(glassTiles)
	glassTileBytes = 0
}

func releaseGlassCache() {
	resetGlassCache()
	if glassSourceDC != 0 {
		call(gdi, "DeleteDC", glassSourceDC)
		glassSourceDC = 0
	}
}

type glassBitmapInfo struct {
	Size                   uint32
	Width, Height          int32
	Planes, Bits           uint16
	Compression, SizeImage uint32
	XPels, YPels           int32
	Used, Important        uint32
}

func paintGlassTile(dc uintptr, r rect, base uintptr, radius int32, surface, shadow bool) {
	// Child controls paint in local coordinates; sample the shared backdrop at
	// their actual position so chart and button materials line up with the window.
	origin := point{}
	canvas := rect{R: desktop.width, B: desktop.height}
	if window := call(user, "WindowFromDC", dc); window != 0 {
		root := rootWindow(window)
		canvas = canvasRect(root)
		if window != root {
			call(user, "MapWindowPoints", window, root, uintptr(unsafe.Pointer(&origin)), 1)
			origin.X = unscaleUI(origin.X, windowDPI(root))
			origin.Y = unscaleUI(origin.Y, windowDPI(root))
			if s := displays[root]; s != nil {
				origin.X += s.v.X
				origin.Y += s.v.Y
			}
		}
	}
	paintGlassTileAt(dc, r, origin, canvas, base, radius, surface, shadow)
}

// paintGlassTileAt takes the logical position of the DC origin explicitly.
// A child can reconstruct its parent's backdrop with the identical scene and
// tile grid before stamping a glass surface over it.
func paintGlassTileAt(dc uintptr, r rect, origin point, canvas rect, base uintptr, radius int32, surface, shadow bool) {
	face := r
	if shadow {
		r = rect{r.L - 8, r.T - 8, r.R + 8, r.B + 8}
	}
	k := glassTileKey{Width: int(r.R - r.L), Height: int(r.B - r.T), X: int(r.L + origin.X), Y: int(r.T + origin.Y), CanvasWidth: int(canvas.R), CanvasHeight: int(canvas.B), Radius: int(radius), Theme: glassTheme, Base: base, Surface: surface, Shadow: shadow}
	if !drawGlassTile(dc, r, k) && surface {
		roundedFill(dc, face, base, base)
	}
}

// The destination can be a logical box (glass surface) or exact device pixels
// (opaque backing). In either case the tile key describes the logical scene.
func drawGlassTile(dc uintptr, dst rect, k glassTileKey) bool {
	if k.Width <= 0 || k.Height <= 0 {
		return false
	}
	if call(gdi, "RectVisible", dc, uintptr(unsafe.Pointer(&dst))) == 0 {
		return true // Nothing in the target's native clip needs painting.
	}
	width, height := uintptr(dst.R-dst.L), uintptr(dst.B-dst.T)
	rasterW, rasterH := glassRasterSize(k)
	info := glassBitmapInfo{Size: 40, Width: int32(rasterW), Height: -int32(rasterH), Planes: 1, Bits: 32}
	tile, ok := glassTiles[k]
	if !ok {
		bytes := rasterW * rasterH * 4
		if glassTileBytes+bytes > 32<<20 || len(glassTiles) >= 128 {
			resetGlassCache()
		}
		if k.Surface {
			var bits unsafe.Pointer
			tile.bitmap = call(gdi, "CreateDIBSection", dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
			if tile.bitmap == 0 || bits == nil {
				if tile.bitmap != 0 {
					call(gdi, "DeleteObject", tile.bitmap)
				}
				return false
			}
			paintGlassLayerPixels(k, rasterW, rasterH, unsafe.Slice((*byte)(bits), bytes))
		} else {
			tile.backdrop = renderGlassRaster(k, rasterW, rasterH)
			if len(tile.backdrop) == 0 {
				return false
			}
		}
		glassTiles[k] = tile
		glassTileBytes += bytes
	}
	if !k.Surface {
		copied := call(gdi, "StretchDIBits", dc, uintptr(dst.L), uintptr(dst.T), width, height, 0, 0, uintptr(rasterW), uintptr(rasterH), uintptr(unsafe.Pointer(&tile.backdrop[0])), uintptr(unsafe.Pointer(&info)), 0, 0x00cc0020)
		return int32(copied) > 0
	}
	if glassSourceDC == 0 {
		glassSourceDC = call(gdi, "CreateCompatibleDC", 0)
	}
	if glassSourceDC != 0 {
		old := call(gdi, "SelectObject", glassSourceDC, tile.bitmap)
		if old != 0 {
			const blendSourceOver = 0x01ff0000 // AC_SRC_OVER, flags 0, constant alpha 255, AC_SRC_ALPHA
			ok, _, _ := alphaGlass.Call(dc, uintptr(dst.L), uintptr(dst.T), width, height, glassSourceDC, 0, 0, uintptr(rasterW), uintptr(rasterH), blendSourceOver)
			call(gdi, "SelectObject", glassSourceDC, old)
			return ok != 0
		}
	}
	return false
}

func paintGlassBackdrop(dc, window uintptr) {
	paintSceneBackdrop(dc, window, window, point{})
}

// Child controls use their actual native position in the parent, and both
// paint from the same raster. No round-trip through logical child offsets.
func paintSceneBackdrop(dc, window, root uintptr, childAt point) {
	canvas, rootClient := canvasRect(root), windowClientRect(root)
	dpi := windowDPI(root)
	pan := windowScrollPixels(root)
	var scroll point
	if s := displays[root]; s != nil {
		scroll = point{s.v.X, s.v.Y}
	}
	dcOrigin, restore := pixelPaintDC(dc)
	defer restore()
	// The root's logical DC already includes its viewport pan. For a child
	// DC translate the scene by the actual native offset and viewport pan.
	scene, visible, clip := dcOrigin, dcOrigin, rootClient
	if window == root {
		visible.X += pan.X
		visible.Y += pan.Y
	} else {
		scene.X -= childAt.X + pan.X
		scene.Y -= childAt.Y + pan.Y
		clip = windowClientRect(window)
	}
	clip = offsetRect(clip, visible.X, visible.Y)
	call(gdi, "IntersectClipRect", dc, uintptr(clip.L), uintptr(clip.T), uintptr(clip.R), uintptr(clip.B))
	w := backdropPixelSpan(canvas.R, rootClient.R, scroll.X, dpi)
	h := backdropPixelSpan(canvas.B, rootClient.B, scroll.Y, dpi)
	dst := rect{scene.X, scene.Y, scene.X + w, scene.Y + h}
	k := glassTileKey{Width: int(canvas.R), Height: int(canvas.B), CanvasWidth: int(canvas.R), CanvasHeight: int(canvas.B), Theme: glassTheme, Base: bg}
	if !drawGlassTile(dc, dst, k) {
		fill(dc, clip, bg)
	}
}

func glassSurface(dc uintptr, r rect, base uintptr, radius int32, shadow bool) {
	paintGlassTile(dc, r, base, radius, true, shadow)
}

// Native header drawing matches the frosted table and keeps sorting discoverable.
func glassHeaderDraw(n *customDraw) (uintptr, bool) {
	header, _, _ := send.Call(desktop.list, 0x101f, 0, 0)
	if n.Header.H != header || n.Header.Code != -12 {
		return 0, false
	}
	if n.Stage == 1 {
		return 0x20, true
	}
	if n.Stage != 0x10001 {
		return 0, false
	}
	// Cover the exact device-pixel bounds before scaling the label.
	if minimalTheme(glassTheme) {
		fill(n.DC, n.R, stripeColor)
	} else {
		gradientFill(n.DC, n.R, mixColor(panel, 0xffffff, 22), mixColor(panel, blue, 6))
	}
	line(n.DC, n.R.L, n.R.B-1, n.R.R, n.R.B-1, border)
	copy := *n
	n = &copy
	n.R = logicalRect(n.R, n.Header.H)
	defer logicalDC(n.DC, n.Header.H, false)()
	index := int(n.Item)
	if index < 0 || index >= len(nativeListColumns) {
		return 0, false
	}
	title := tr(nativeListColumns[index].Name)
	if index == 0 || index == 17 {
		a := desktop.app
		a.mu.Lock()
		mode := listSortMode(a.config)
		a.mu.Unlock()
		switch {
		case index == 0 && mode == "ip_asc", index == 17 && mode == "name_asc":
			title += " ↑"
		case index == 0 && mode == "ip_desc", index == 17 && mode == "name_desc":
			title += " ↓"
		default:
			title += " ↕"
		}
	}
	r := n.R
	r.L += 10
	r.R -= 6
	call(gdi, "SelectObject", n.DC, ui.smallFont)
	call(gdi, "SetTextColor", n.DC, muted)
	call(gdi, "SetBkMode", n.DC, 1)
	call(user, "DrawTextW", n.DC, uintptr(unsafe.Pointer(wide(title))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x8824)
	return 4, true
}
