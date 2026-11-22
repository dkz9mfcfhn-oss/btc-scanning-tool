//go:build windows

package main

import (
	"context"
	"minerwatch/internal/network"
	"time"
	"unsafe"
)

const nativeStartupMessage = 0x8000 + 82
const nativeFirstListTimer = 2

type nativeStartupResult struct {
	app     *App
	average rollingFarmAverage
	ranges  string
	err     error
}

type nativeStartup struct {
	ctx                       context.Context
	cancel                    context.CancelFunc
	result                    chan nativeStartupResult
	loaderDone, schedulerDone chan struct{}
}

func beginNativeStartup(h uintptr, dir string, demo bool) *nativeStartup {
	ctx, cancel := context.WithCancel(context.Background())
	s := &nativeStartup{ctx: ctx, cancel: cancel, result: make(chan nativeStartupResult, 1), loaderDone: make(chan struct{})}
	go func() {
		defer close(s.loaderDone)
		s.result <- loadNativeStartup(ctx, dir, demo)
		call(user, "PostMessageW", h, nativeStartupMessage, 0, 0)
	}()
	return s
}

func loadNativeStartup(ctx context.Context, dir string, demo bool) nativeStartupResult {
	a, err := newAppContext(ctx, dir, demo)
	if err != nil {
		return nativeStartupResult{err: err}
	}
	r := nativeStartupResult{app: a, ranges: a.config.Ranges}
	if r.ranges == "" && ctx.Err() == nil {
		if ranges := network.LocalRanges(); len(ranges) > 0 {
			r.ranges = ranges[0]
		}
	}
	if ctx.Err() != nil {
		r.err = ctx.Err()
		return r
	}
	now := time.Now()
	devices := make([]*Device, 0, len(a.devices))
	for _, d := range a.devices {
		devices = append(devices, d)
	}
	r.average.refresh(now, devices, a.db)
	if ctx.Err() == nil {
		a.fleetPoints(now)
	}
	r.err = ctx.Err()
	return r
}

func (s *nativeStartup) finish(h uintptr) {
	if s == nil {
		return
	}
	select {
	case loaded := <-s.result:
		if loaded.err != nil {
			if loaded.app != nil {
				loaded.app.db.Close()
			}
			message(tr("无法载入监控数据：") + loaded.err.Error())
			call(user, "DestroyWindow", h)
			return
		}
		openNativeDashboard(h, loaded)
	default:
	}
}

func (s *nativeStartup) startMonitor(a *App) {
	a.ctx, a.cancel = s.ctx, s.cancel
	// Reserve the first full frame before starting network collection.
	first := time.Now().Add(time.Second)
	if a.nextPoll.Before(first) {
		a.nextPoll = first
	}
	s.schedulerDone = make(chan struct{})
	go func() { defer close(s.schedulerDone); a.schedule(s.ctx) }()
}

func (s *nativeStartup) close() {
	s.cancel()
	<-s.loaderDone
	a := desktop.app
	if a == nil {
		// The window can close while the background loader still owns the data.
		select {
		case pending := <-s.result:
			a = pending.app
		default:
		}
	}
	if a != nil {
		if s.schedulerDone != nil {
			<-s.schedulerDone
			a.jobs.Wait()
		}
		a.db.Close()
	}
}

func paintStartupWindow(h uintptr) {
	var ps paint
	dc := call(user, "BeginPaint", h, uintptr(unsafe.Pointer(&ps)))
	defer call(user, "EndPaint", h, uintptr(unsafe.Pointer(&ps)))
	if dc == 0 {
		return
	}
	fill(dc, ps.Rect, bg)
	defer logicalDC(dc, h, false)()
	drawDashboardIcon(dc, h)
	label(dc, 74, 17, appDisplayName, ui.titleFont, ink)
	label(dc, 76, 51, tr("正在读取设备与历史记录…"), ui.smallFont, muted)
}
