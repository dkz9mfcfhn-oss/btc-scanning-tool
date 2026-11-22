//go:build windows

package main

import "unsafe"

func paintScanProgress(dc uintptr, box uiBox, job Job) {
	r := rect{box.X, box.Y, box.X + box.W, box.Y + box.H}
	percent, _ := currentLanguage().scanProgress(job)
	accent := teal
	if !job.Running && (job.Error != "" || job.Done < job.Total) {
		accent = warningColor
	}
	glassSurface(dc, r, editColor, 30, false)
	label := currentLanguage().progressBarLabel(job, box.W)
	if percent <= 0 {
		labelBox(dc, r, label, ui.smallFont, ink, 1)
		return
	}
	end := r.L + int32(int64(box.W)*int64(percent)/100)
	saved := call(gdi, "SaveDC", dc)
	// Clip the full rounded shape at the true progress boundary, including tiny
	// percentages, rather than drawing a minimum-width decorative segment.
	call(gdi, "IntersectClipRect", dc, uintptr(r.L), uintptr(r.T), uintptr(end), uintptr(r.B))
	glassSurface(dc, r, accent, 30, false)
	labelBox(dc, r, label, ui.smallFont, onAccent, 1)
	call(gdi, "RestoreDC", dc, saved)
	saved = call(gdi, "SaveDC", dc)
	call(gdi, "IntersectClipRect", dc, uintptr(end), uintptr(r.T), uintptr(r.R), uintptr(r.B))
	labelBox(dc, r, label, ui.smallFont, ink, 1)
	call(gdi, "RestoreDC", dc, saved)
}

// The existing 1-second UI timer updates only this dirty region when job counts
// change. It does not rebuild the miner table or increase scan/network frequency.
func refreshProgressIndicator() {
	a := desktop.app
	a.mu.Lock()
	job := a.job
	a.mu.Unlock()
	if job == desktop.lastProgress {
		return
	}
	desktop.lastProgress = job
	g := dashboardGeometry(desktop.width, desktop.height, desktop.fullList)
	b := g.Progress
	x, y := b.X, b.Y
	if s := displays[desktop.h]; s != nil {
		x -= s.v.X
		y -= s.v.Y
	}
	dpi := windowDPI(desktop.h)
	r := rect{scaleUI(x-1, dpi), scaleUI(y-1, dpi), scaleUI(x+b.W+1, dpi), scaleUI(y+b.H+1, dpi)}
	invalidate.Call(desktop.h, uintptr(unsafe.Pointer(&r)), 0)
}
