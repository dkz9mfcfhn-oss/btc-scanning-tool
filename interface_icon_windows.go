//go:build windows

package main

import "unsafe"

type interfaceIconBitmapKey struct {
	kind  interfaceIcon
	size  int32
	color uintptr
}

// Cache small premultiplied icons on the UI thread. Drawing rows and hover
// states reuses their bitmaps without doing any per-icon raster work.
var interfaceIconBitmaps = map[interfaceIconBitmapKey]uintptr{}
var interfaceIconBytes int

func resetInterfaceIconCache() {
	for _, bitmap := range interfaceIconBitmaps {
		call(gdi, "DeleteObject", bitmap)
	}
	clear(interfaceIconBitmaps)
	interfaceIconBytes = 0
}

func interfaceIconBitmap(dc uintptr, key interfaceIconBitmapKey) uintptr {
	if bitmap := interfaceIconBitmaps[key]; bitmap != 0 {
		return bitmap
	}
	bytes := int(key.size * key.size * 4)
	if interfaceIconBytes+bytes > 512<<10 || len(interfaceIconBitmaps) >= 128 {
		resetInterfaceIconCache()
	}
	info := glassBitmapInfo{Size: 40, Width: key.size, Height: -key.size, Planes: 1, Bits: 32}
	var data unsafe.Pointer
	bitmap := call(gdi, "CreateDIBSection", dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&data)), 0, 0)
	if bitmap == 0 || data == nil {
		if bitmap != 0 {
			call(gdi, "DeleteObject", bitmap)
		}
		return 0
	}
	rasterInterfaceIcon(key.kind, int(key.size), key.color, unsafe.Slice((*byte)(data), bytes))
	interfaceIconBitmaps[key] = bitmap
	interfaceIconBytes += bytes
	return bitmap
}

func paintInterfaceIcon(dc uintptr, box uiBox, kind interfaceIcon, color uintptr) bool {
	if kind == iconNone || box.W <= 0 || box.H <= 0 {
		return false
	}
	// Render at the destination's native pixel size, including fractional user
	// zoom and the buffered dashboard's translated paint origin.
	points := [2]point{{box.X, box.Y}, {box.X + box.W, box.Y + box.H}}
	if call(gdi, "LPtoDP", dc, uintptr(unsafe.Pointer(&points[0])), 2) == 0 {
		return false
	}
	p, end := points[0], points[1]
	size := min(end.X-p.X, end.Y-p.Y)
	minSize := int32(minInterfaceIconPixels)
	if kind == iconChevron {
		minSize = 5 // The simple dropdown indicator remains readable when very small.
	}
	if size < minSize {
		return false
	}
	rasterSize := min(size, 128)
	bitmap := interfaceIconBitmap(dc, interfaceIconBitmapKey{kind, rasterSize, color})
	if bitmap == 0 {
		return false
	}
	if glassSourceDC == 0 {
		glassSourceDC = call(gdi, "CreateCompatibleDC", 0)
	}
	if glassSourceDC == 0 {
		return false
	}
	old := call(gdi, "SelectObject", glassSourceDC, bitmap)
	if old == 0 {
		return false
	}
	defer call(gdi, "SelectObject", glassSourceDC, old)
	_, restore := pixelPaintDC(dc)
	defer restore()
	x, y := p.X+(end.X-p.X-size)/2, p.Y+(end.Y-p.Y-size)/2
	const blendSourceOver = 0x01ff0000 // Fully opaque source constant, per-pixel alpha.
	ok, _, _ := alphaGlass.Call(dc, uintptr(x), uintptr(y), uintptr(size), uintptr(size), glassSourceDC, 0, 0, uintptr(rasterSize), uintptr(rasterSize), blendSourceOver)
	return ok != 0
}

func paintIconLabel(dc uintptr, bounds rect, title string, font, foreground, iconColor uintptr, kind interfaceIcon, size int32) {
	text := wide(title)
	call(gdi, "SelectObject", dc, font)
	call(gdi, "SetTextColor", dc, foreground)
	call(gdi, "SetBkMode", dc, 1)
	const singleLine = 0x8824 // Single line, vertically centered, no mnemonics, end ellipsis.
	var dimensions point
	alignment := uintptr(1) // DT_CENTER
	if kind != iconNone && call(gdi, "GetTextExtentPoint32W", dc, uintptr(unsafe.Pointer(text)), uintptr(lenUTF16(title)), uintptr(unsafe.Pointer(&dimensions))) != 0 {
		icon, caption := iconTextPlacement(uiBox{bounds.L, bounds.T, bounds.R - bounds.L, bounds.B - bounds.T}, dimensions.X, size)
		if paintInterfaceIcon(dc, icon, kind, iconColor) {
			bounds.L = caption.X
			alignment = 0 // DT_LEFT
		}
	}
	call(user, "DrawTextW", dc, uintptr(unsafe.Pointer(text)), ^uintptr(0), uintptr(unsafe.Pointer(&bounds)), singleLine|alignment)
}
