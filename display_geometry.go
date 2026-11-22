package main

// All UI geometry is expressed in 96-DPI units. Always scale from the original
// value, never from the previous monitor's rounded physical size.
func scaleUI(n, dpi int32) int32 {
	if dpi <= 0 {
		dpi = 96
	}
	if n < 0 {
		return -scaleUI(-n, dpi)
	}
	return int32((int64(n)*int64(dpi) + 48) / 96)
}
func unscaleUI(n, dpi int32) int32 {
	if dpi <= 0 {
		dpi = 96
	}
	if n < 0 {
		return -unscaleUI(-n, dpi)
	}
	return int32((int64(n)*96 + int64(dpi)/2) / int64(dpi))
}

// The backing raster must cover the physical viewport all the way to the end
// of its scroll range. Rounding the canvas and scroll separately can otherwise
// leave an uncovered pixel, even when the logical viewport fits exactly.
func backdropPixelSpan(canvas, physical, scroll, dpi int32) int32 {
	logicalPage := unscaleUI(physical, dpi)
	maxScroll := max(0, scroll, canvas-logicalPage)
	return max(scaleUI(canvas, dpi), physical+scaleUI(maxScroll, dpi))
}

// Preserve the logical width through repeated zoom changes. At zoom below
// 100%, different logical widths can round to the same physical width.
type displayColumnWidth struct{ logical, lastPixels, fillPixels int32 }

func (c *displayColumnWidth) remember(actual, dpi int32) int32 {
	if actual != c.lastPixels {
		c.logical = unscaleUI(actual, dpi) // user dragged the header
		c.lastPixels = actual
		c.fillPixels = 0
	}
	return c.lastPixels - c.fillPixels
}

func (c *displayColumnWidth) rescale(actual, oldDPI, dpi int32) int32 {
	c.remember(actual, oldDPI)
	c.lastPixels = scaleUI(c.logical, dpi)
	c.fillPixels = 0
	return c.lastPixels
}

// In report view the rightmost visible column can use all spare space. Keep
// its extra pixels separate so a smaller viewport restores its original width.
func fitListColumns(widths []int32, order []int32, clientWidth int32) (lastVisible int, extraPixels int32) {
	lastVisible = -1
	for _, i := range order {
		if width := widths[i]; width > 0 {
			lastVisible = int(i)
			clientWidth -= width
		}
	}
	if lastVisible < 0 {
		return -1, 0
	}
	return lastVisible, max(0, clientWidth)
}

type displayViewport struct{ W, H, CanvasW, CanvasH, X, Y int32 }

func fitViewport(w, h, minW, minH, x, y int32) displayViewport {
	v := displayViewport{W: max(1, w), H: max(1, h), CanvasW: max(w, minW), CanvasH: max(h, minH)}
	v.X = min(max(0, x), max(0, v.CanvasW-v.W))
	v.Y = min(max(0, y), max(0, v.CanvasH-v.H))
	return v
}
func revealAxis(pos, page, start, length, total int32) int32 {
	if start < pos {
		pos = start
	} else if start+length > pos+page {
		pos = min(start, start+length-page)
	}
	return min(max(0, pos), max(0, total-page))
}
func fitWindowBox(b, work uiBox) uiBox {
	b.W = min(max(1, b.W), max(1, work.W))
	b.H = min(max(1, b.H), max(1, work.H))
	b.X = min(max(b.X, work.X), work.X+work.W-b.W)
	b.Y = min(max(b.Y, work.Y), work.Y+work.H-b.H)
	return b
}
