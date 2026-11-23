//go:build windows

package main

import (
	"fmt"
	"minerwatch/internal/store"
	"syscall"
	"time"
	"unsafe"
)

var ui struct {
	background, panel, edit, titleFont, numberFont, smallFont uintptr
	compactNumberFont                                         uintptr
	profitDetailFont                                          uintptr
	tableY, tableH                                            int32
}

func rgb(r, g, b uintptr) uintptr { return r | g<<8 | b<<16 }

var bg, panel, border, ink, muted, teal, blue uintptr
var editColor, buttonColor, stripeColor, selectionColor, warningColor, dangerColor, onAccent uintptr

func themeValues(name string) {
	glassTheme = normalizedTheme(name)
	resetGlassCache()
	resetInterfaceIconCache()
	p := paletteFor(name)
	bg, panel, border, ink, muted, teal, blue = p.background, p.panel, p.border, p.ink, p.muted, p.accent, p.blue
	editColor, buttonColor, stripeColor, selectionColor, warningColor, dangerColor, onAccent = p.edit, p.button, p.stripe, p.selection, p.warning, p.danger, p.onAccent
}

func themeStart() {
	ui.background = call(gdi, "CreateSolidBrush", bg)
	ui.panel = call(gdi, "CreateSolidBrush", panel)
	ui.edit = call(gdi, "CreateSolidBrush", editColor)
	font := func(face string, size int32, weight uintptr) uintptr {
		return call(gdi, "CreateFontW", uintptr(-int64(size)), 0, 0, 0, weight, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(wide(face))))
	}
	ui.titleFont = font("Segoe UI", dashboardBrandFontSize, 600)
	ui.numberFont = font("Segoe UI", dashboardNumberSize, 600)
	ui.compactNumberFont = font("Segoe UI", 22, 600)
	ui.smallFont = font("Microsoft YaHei UI", 13, 400)
	ui.profitDetailFont = font("Microsoft YaHei UI", profitDetailSize, 400)
}
func themeStop() {
	resetInterfaceIconCache()
	releaseGlassCache()
	for _, o := range []uintptr{ui.background, ui.panel, ui.edit, ui.titleFont, ui.numberFont, ui.compactNumberFont, ui.smallFont, ui.profitDetailFont} {
		call(gdi, "DeleteObject", o)
	}
}
func fill(dc uintptr, r rect, color uintptr) {
	b := call(gdi, "CreateSolidBrush", color)
	call(user, "FillRect", dc, uintptr(unsafe.Pointer(&r)), b)
	call(gdi, "DeleteObject", b)
}
func label(dc uintptr, x, y int32, s string, font, color uintptr) {
	call(gdi, "SelectObject", dc, font)
	call(gdi, "SetTextColor", dc, color)
	call(gdi, "SetBkMode", dc, 1)
	u := wide(s)
	call(gdi, "TextOutW", dc, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(u)), uintptr(lenUTF16(s)))
}
func lenUTF16(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 65535 {
			n++
		}
	}
	return n
}
func line(dc uintptr, x1, y1, x2, y2 int32, color uintptr) {
	p := call(gdi, "CreatePen", 0, 1, color)
	old := call(gdi, "SelectObject", dc, p)
	call(gdi, "MoveToEx", dc, uintptr(x1), uintptr(y1), 0)
	call(gdi, "LineTo", dc, uintptr(x2), uintptr(y2))
	call(gdi, "SelectObject", dc, old)
	call(gdi, "DeleteObject", p)
}

const panelCornerDiameter int32 = 46

func panelRect(dc uintptr, r rect, accent uintptr) {
	glassSurface(dc, r, panel, panelCornerDiameter, true)
	if accent != 0 {
		fill(dc, rect{r.L + 17, r.T + 17, r.L + 21, r.T + 34}, accent)
	}
}
func paintDashboard(h uintptr) {
	var ps paint
	target := call(user, "BeginPaint", h, uintptr(unsafe.Pointer(&ps)))
	defer call(user, "EndPaint", h, uintptr(unsafe.Pointer(&ps)))
	dirty := ps.Rect
	width, height := dirty.R-dirty.L, dirty.B-dirty.T
	if width <= 0 || height <= 0 {
		return
	}
	// Buffer only the invalid area. A progress update should not allocate a
	// full-screen bitmap on a 4K monitor.
	dc := call(gdi, "CreateCompatibleDC", target)
	bitmap := call(gdi, "CreateCompatibleBitmap", target, uintptr(width), uintptr(height))
	buffered := dc != 0 && bitmap != 0
	if !buffered {
		if dc != 0 {
			call(gdi, "DeleteDC", dc)
		}
		if bitmap != 0 {
			call(gdi, "DeleteObject", bitmap)
		}
		dc = target
	} else {
		old := call(gdi, "SelectObject", dc, bitmap)
		// Initialize the paint buffer in physical pixels before any scaled
		// drawing. A driver failure can no longer expose unpainted edges.
		fill(dc, rect{0, 0, width, height}, bg)
		defer func() {
			call(gdi, "BitBlt", target, uintptr(dirty.L), uintptr(dirty.T), uintptr(width), uintptr(height), dc, 0, 0, 0x00cc0020)
			call(gdi, "SelectObject", dc, old)
			call(gdi, "DeleteObject", bitmap)
			call(gdi, "DeleteDC", dc)
		}()
	}
	defer logicalDC(dc, h, true)()
	if buffered {
		var origin point
		call(gdi, "GetViewportOrgEx", dc, uintptr(unsafe.Pointer(&origin)))
		call(gdi, "SetViewportOrgEx", dc, uintptr(origin.X-dirty.L), uintptr(origin.Y-dirty.T), 0)
	}
	bounds := canvasRect(h)
	paintGlassBackdrop(dc, h)
	g := desktopGeometry(bounds.R, bounds.B)
	drawDashboardIcon(dc, h)
	brand := g.BrandTitle
	label(dc, brand.X, brand.Y, appDisplayName, ui.titleFont, ink)
	label(dc, 75, 49, tr("矿场运行总览"), ui.smallFont, muted)
	if desktop.app == nil {
		return
	}
	a := desktop.app
	a.mu.Lock()
	ds := make([]*Device, 0, len(a.devices))
	attention := 0
	for _, d := range a.devices {
		ds = append(ds, d)
		if !d.Hidden && sampleHealthView(d.Latest, time.Now()).State != "normal" {
			attention++
		}
	}
	t := totals(ds, time.Now())
	tariff, poolFee, quote := a.config.ElectricityCNY, a.config.PoolFeePercent, a.market
	job, demo, paused := a.job, a.demo, a.paused
	a.mu.Unlock()
	state := tr("自动监控中")
	stateColor := teal
	if paused {
		state = tr("监控已暂停")
		stateColor = warningColor
	}
	if job.Running {
		state = tr("正在采集数据")
		stateColor = blue
	}
	stateBox := g.MonitorState
	stateRect := rect{stateBox.X, stateBox.Y, stateBox.X + stateBox.W, stateBox.Y + stateBox.H}
	glassSurface(dc, stateRect, panel, 34, false)
	stateMid := stateBox.Y + stateBox.H/2
	roundedFill(dc, rect{stateBox.X + 14, stateMid - 3, stateBox.X + 20, stateMid + 3}, stateColor, stateColor)
	labelBox(dc, rect{stateBox.X + 30, stateRect.T, stateRect.R - 8, stateRect.B}, state, ui.smallFont, stateColor, 0)
	rate, unit := dashboardRate(t.RealtimeTH, t.RealtimeReported)
	farm24 := desktop.average24.average
	average, avgUnit := dashboardRate(farm24.MeanTH, farm24.Valid)
	reject := "—"
	if t.RejectPercent != nil {
		reject = fmt.Sprintf("%.2f", *t.RejectPercent)
	}
	titles := []string{tr("全场实时算力"), tr("在线矿机"), tr("24小时平均算力合计"), tr("全场拒绝率")}
	values := []string{rate, fmt.Sprintf("%d / %d", t.Online, t.Count), average, reject}
	units := []string{unit, tr("台"), avgUnit, "%"}
	notes := []string{fmt.Sprintf(tr("%d / %d 台提供实时读数"), t.RealtimeReported, t.Count), fmt.Sprintf(tr("需关注 %d 台 · 隐藏不计入"), attention), fmt.Sprintf(tr("%d / %d 台 · 覆盖 %.1f%%"), farm24.Valid, farm24.Devices, farm24.coverage()*100), fmt.Sprintf(tr("%d / %d 台提供有效份额"), t.RejectReported, t.Count)}
	colors := []uintptr{teal, blue, blue, warningColor}
	coverage := []float64{float64(t.RealtimeReported), float64(t.Online), farm24.coverage() * float64(t.Count), float64(t.RejectReported)}
	for i, r := range g.Cards[:profitCardIndex] {
		x, y := r.X, r.Y
		panelRect(dc, rect{x, y, x + r.W, y + r.H}, 0)
		if r.W < 185 {
			shortTitles := [...]string{tr("实时算力"), tr("在线矿机"), tr("24h 平均算力"), tr("全场拒绝率")}
			labelBox(dc, rect{x + 10, y + 13, x + r.W - 10, y + 32}, shortTitles[i], ui.smallFont, muted, 0)
			labelBox(dc, rect{x + 10, y + 34, x + r.W - 10, y + 64}, values[i], ui.compactNumberFont, ink, 0)
			labelBox(dc, rect{x + 10, y + 65, x + r.W - 10, y + 83}, units[i], ui.smallFont, muted, 0)
			labelBox(dc, rect{x + 10, y + 84, x + r.W - 10, y + 102}, notes[i], ui.smallFont, muted, 0)
		} else {
			labelBox(dc, rect{x + 18, y + 13, x + r.W - 48, y + 32}, titles[i], ui.smallFont, muted, 0)
			badge := rect{x + r.W - 40, y + 11, x + r.W - 12, y + 39}
			glassSurface(dc, badge, mixColor(panel, colors[i], 13), 28, false)
			paintInterfaceIcon(dc, uiBox{x + r.W - 34, y + 17, 16, 16}, metricIcons[i], colors[i])
			labelBox(dc, rect{x + 18, y + 33, x + r.W - 56, y + 76}, values[i], ui.numberFont, ink, 0)
			labelBox(dc, rect{x + r.W - 58, y + 48, x + r.W - 18, y + 69}, units[i], ui.smallFont, muted, 2)
			labelBox(dc, rect{x + 18, y + 80, x + r.W - 18, y + 98}, notes[i], ui.smallFont, muted, 0)
		}
		if t.Count > 0 {
			track := rect{x + 18, y + r.H - 5, x + r.W - 18, y + r.H - 3}
			fill(dc, track, mixColor(panel, colors[i], 12))
			fill(dc, rect{track.L, track.T, track.L + int32(float64(track.R-track.L)*coverage[i]/float64(t.Count)), track.B}, mixColor(panel, colors[i], 62))
		}
	}
	paintEconomics(dc, g, t, desktop.average24.earnings, dashboardQuote(quote, demo), tariff, poolFee)
	for i := range len(g.Cards) - 1 {
		d := cardDivider(g.Cards, i)
		color := mixColor(panel, muted, 46)
		if activeCardDrag != nil && activeCardDrag.index == i {
			color = teal
		}
		x, y := d.X+d.W/2, d.Y+d.H/2
		roundedFill(dc, rect{x - 1, y - 12, x + 2, y + 12}, color, color)
	}
	r := g.ScanPanel
	panelRect(dc, rect{r.X, r.Y, r.X + r.W, r.Y + r.H}, 0)
	for _, id := range []int{11, 12, 13, 26} {
		f := g.Controls[id]
		glassSurface(dc, rect{f.X - 8, f.Y - 5, f.X + f.W + 8, f.Y + f.H + 5}, editColor, 25, false)
		// EDIT keeps its native caret, selection and password behavior. Match
		// its inner backing to the surrounding rounded recess.
		roundedFill(dc, rect{f.X - 3, f.Y - 2, f.X + f.W + 3, f.Y + f.H + 2}, editColor, editColor)
	}
	fields := []struct {
		id    int
		label string
	}{{11, tr("扫描范围 / IP 网段")}, {12, tr("登录账号")}, {13, tr("后台密码")}}
	for _, field := range fields {
		f := g.Controls[field.id]
		label(dc, f.X-4, f.Y-23, field.label, ui.smallFont, muted)
	}
	listTitle := g.ListTitle
	labelBox(dc, rect{listTitle.X, listTitle.Y, listTitle.X + listTitle.W, listTitle.Y + listTitle.H}, tr("矿机列表"), desktop.font, ink, 0)
	caption := fmt.Sprintf(tr("显示 %d / %d 台  ·  双击查看详情  ·  右键备注与时间线"), desktop.visibleCount, desktop.totalCount)
	if demo {
		caption = tr("演示数据 · ") + caption
	}
	count := g.ListCount
	labelBox(dc, rect{count.X, count.Y, count.X + count.W, count.Y + count.H}, caption, ui.smallFont, muted, 0)
	search := g.Controls[26]
	label(dc, search.X-48, search.Y+3, tr("搜索"), ui.smallFont, muted)
	paintScanProgress(dc, g.Progress, job)
	if g.Table.H > 0 {
		r = g.Table
		panelRect(dc, rect{r.X - 8, r.Y - 6, r.X + r.W + 8, r.Y + r.H + 6}, 0)
	}
	status := g.Status
	labelBox(dc, boxRect(status), desktop.statusText, ui.smallFont, ink, 0)
}
func labelBox(dc uintptr, r rect, s string, font, color uintptr, align uintptr) {
	call(gdi, "SelectObject", dc, font)
	call(gdi, "SetTextColor", dc, color)
	call(gdi, "SetBkMode", dc, 1)
	call(user, "DrawTextW", dc, uintptr(unsafe.Pointer(wide(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x8824|align)
}
func colorControl(dc uintptr, edit bool) uintptr {
	call(gdi, "SetTextColor", dc, ink)
	color, b := bg, ui.background
	if edit {
		color = editColor
		b = ui.edit
	}
	call(gdi, "SetBkColor", dc, color)
	return b
}

type drawItem struct {
	Type, ID, Item, Action, State uint32
	H, DC                         uintptr
	R                             rect
	Data                          uintptr
}

func paintButton(ptr uintptr) {
	original := (*drawItem)(unsafe.Pointer(ptr))
	copy := *original
	d := &copy
	d.R = logicalRect(d.R, d.H)
	defer logicalDC(d.DC, d.H, false)()
	color := buttonColor
	fg := ink
	title := value(d.H)
	activePriority := d.ID == priorityCommand && title == tr("异常优先：开")
	accented := d.ID == 21 || d.ID == 82 || d.ID == 92 || activePriority
	if accented {
		color = teal
		fg = onAccent
	}
	if d.State&4 != 0 {
		color = panel
		fg = muted
	} else if d.State&1 != 0 {
		color = selectionColor
		fg = ink
	} else {
		color = interactiveGlassTint(d.H, color, accented)
	}
	buttonRect := d.R
	buttonRect.L++
	buttonRect.T++
	buttonRect.R--
	buttonRect.B--
	paintControlSurface(d.DC, d.H, d.R, buttonRect, color, buttonRect.B-buttonRect.T)
	r := d.R
	r.L += 6
	r.R -= 6
	iconColor := fg
	if !accented && d.State&4 == 0 {
		iconColor = mixColor(fg, blue, 75)
	}
	font := desktop.font
	if currentLanguage() != chinese && rootWindow(d.H) == desktop.h {
		font = ui.smallFont
	}
	if d.ID == profitSettingsCommand {
		font = ui.profitDetailFont
	}
	icon := commandIcon(d.ID, title)
	if d.ID == profitSettingsCommand && d.R.R-d.R.L <= compactProfitButtonWidth+2 {
		const size = 16
		paintInterfaceIcon(d.DC, uiBox{(r.L + r.R - size) / 2, (r.T + r.B - size) / 2, size, size}, icon, iconColor)
	} else {
		paintIconLabel(d.DC, r, title, font, fg, iconColor, icon, 16)
	}
	if d.State&0x10 != 0 {
		r.L += 3
		r.T += 3
		r.R -= 3
		r.B -= 3
		glassOutline(d.DC, r, 12, fg)
	}
}

type nmhdr struct {
	H, ID uintptr
	Code  int32
}
type customDraw struct {
	Header   nmhdr
	Stage    uint32
	DC       uintptr
	R        rect
	Item     uintptr
	State    uint32
	Param    uintptr
	Text, Bg uint32
	Sub      int32
}

func tableDraw(ptr uintptr) (uintptr, bool) {
	n := (*customDraw)(unsafe.Pointer(ptr))
	if result, ok := glassHeaderDraw(n); ok {
		return result, true
	}
	if n.Header.H != desktop.list || n.Header.Code != -12 {
		return 0, false
	}
	switch n.Stage {
	case 1:
		return 0x20, true
	case 0x10001:
		return 0x20, true
	case 0x30001:
		n.Text = uint32(ink)
		n.Bg = uint32(panel)
		if n.Item%2 == 1 {
			n.Bg = uint32(stripeColor)
		}
		if n.State&1 != 0 {
			n.Bg = uint32(selectionColor)
		}
		if (n.Sub == 2 || n.Sub == backendColumn) && drawStatusBadge(n) {
			return 4, true
		}
		if n.Sub == 10 && int(n.Item) < len(desktop.ips) {
			drawRowTrend(n)
			return 4, true
		}
		if n.Sub == backendColumn {
			n.Text = uint32(blue)
		}
		if n.Sub == 3 {
			n.Text = uint32(teal)
		}
		if n.Sub == 2 && int(n.Item) < len(desktop.ips) {
			a := desktop.app
			a.mu.Lock()
			d := a.devices[desktop.ips[n.Item]]
			if d != nil {
				if fresh(d.Latest, time.Now()) {
					n.Text = uint32(teal)
				} else {
					n.Text = uint32(dangerColor)
				}
			}
			a.mu.Unlock()
		}
		return 0, true
	}
	return 0, false
}

// Cache history only for rows that have actually been painted.
type trendCache struct {
	at   time.Time
	data []store.Sample
}

func drawRowTrend(n *customDraw) {
	r := rect{L: 0, T: 10}
	send.Call(desktop.list, 0x1038, n.Item, uintptr(unsafe.Pointer(&r)))
	r = logicalRect(r, desktop.list)
	defer logicalDC(n.DC, desktop.list, false)()
	fill(n.DC, r, uintptr(n.Bg))
	ip := desktop.ips[n.Item]
	a := desktop.app
	a.mu.Lock()
	d := a.devices[ip]
	var at time.Time
	if d != nil && d.Latest != nil {
		at = d.Latest.At
	}
	a.mu.Unlock()
	cached, ok := desktop.trends[ip]
	if !ok || !at.Equal(cached.at) {
		cached = trendCache{at, a.db.MetricHistory(ip, time.Now().Add(-24*time.Hour))}
		desktop.trends[ip] = cached
	}
	maxRate := 1.0
	count := 0
	for _, s := range cached.data {
		if s.Online && s.RealtimeTH != nil {
			count++
			maxRate = max(maxRate, *s.RealtimeTH)
		}
	}
	if count == 0 {
		label(n.DC, r.L+10, r.T+8, tr("暂无采样"), ui.smallFont, muted)
		return
	}
	left, right, top, bottom := r.L+8, r.R-8, r.T+5, r.B-5
	if right <= left || bottom <= top {
		return
	}
	line(n.DC, left, bottom, right, bottom, border)
	start := time.Now().Add(-24 * time.Hour)
	prev := time.Time{}
	var prevPeriod time.Duration
	var px, py int32
	for _, s := range cached.data {
		if s.At.Before(start) || !s.Online || s.RealtimeTH == nil {
			prev = time.Time{}
			continue
		}
		x := left + int32(s.At.Sub(start).Seconds()/86400*float64(right-left))
		y := bottom - int32(*s.RealtimeTH/maxRate*float64(bottom-top))
		if x > right {
			continue
		}
		if !prev.IsZero() && s.At.Sub(prev) <= prevPeriod*3/2 {
			line(n.DC, px, py, x, y, teal)
		} else {
			fill(n.DC, rect{x, y, x + 2, y + 2}, teal)
		}
		prev, prevPeriod, px, py = s.At, sampleInterval(&s), x, y
	}
}

// Rebuild only brushes. Controls retain their font handles across theme changes.
func applyNativeTheme() {
	a := desktop.app
	if a == nil {
		return
	}
	a.mu.Lock()
	name := normalizedTheme(a.config.Theme)
	a.mu.Unlock()
	themeValues(name)
	old := []uintptr{ui.background, ui.panel, ui.edit}
	ui.background = call(gdi, "CreateSolidBrush", bg)
	ui.panel = call(gdi, "CreateSolidBrush", panel)
	ui.edit = call(gdi, "CreateSolidBrush", editColor)
	for _, b := range old {
		if b != 0 {
			call(gdi, "DeleteObject", b)
		}
	}
	visual := "DarkMode_Explorer"
	dark := int32(1)
	if lightTheme(name) {
		visual = "Explorer"
		dark = 0
	}
	for _, h := range []uintptr{desktop.list, desktop.detailTable} {
		if h != 0 {
			send.Call(h, 0x1001, 0, panel)
			send.Call(h, 0x1026, 0, panel)
			send.Call(h, 0x1024, 0, ink)
			call(syscall.NewLazyDLL("uxtheme.dll"), "SetWindowTheme", h, uintptr(unsafe.Pointer(wide(visual))), 0)
		}
	}
	for _, h := range []uintptr{desktop.h, desktop.detailWindow, desktop.diagnosticWindow, phone.window, textDialog.h} {
		if h != 0 {
			call(syscall.NewLazyDLL("dwmapi.dll"), "DwmSetWindowAttribute", h, 20, uintptr(unsafe.Pointer(&dark)), 4)
			call(user, "RedrawWindow", h, 0, 0, 0x85)
		}
	}
}

func roundedFill(dc uintptr, r rect, color, edge uintptr) {
	pen := call(gdi, "CreatePen", 0, 1, edge)
	brush := call(gdi, "CreateSolidBrush", color)
	op := call(gdi, "SelectObject", dc, pen)
	ob := call(gdi, "SelectObject", dc, brush)
	call(gdi, "RoundRect", dc, uintptr(r.L), uintptr(r.T), uintptr(r.R), uintptr(r.B), 10, 10)
	call(gdi, "SelectObject", dc, op)
	call(gdi, "SelectObject", dc, ob)
	call(gdi, "DeleteObject", pen)
	call(gdi, "DeleteObject", brush)
}
func drawStatusBadge(n *customDraw) bool {
	if int(n.Item) >= len(desktop.rows) {
		return false
	}
	row := desktop.rows[n.Item]
	if int(n.Sub) >= len(row) {
		return false
	}
	r := rect{T: n.Sub}
	if sendResult := call(user, "SendMessageW", desktop.list, 0x1038, n.Item, uintptr(unsafe.Pointer(&r))); sendResult == 0 {
		return false
	}
	r = logicalRect(r, desktop.list)
	defer logicalDC(n.DC, desktop.list, false)()
	fill(n.DC, r, uintptr(n.Bg))
	fg := muted
	if n.Sub == backendColumn {
		fg = blue
	} else {
		switch row[n.Sub] {
		case tr("在线"):
			fg = teal
		case tr("无算力"), tr("算力未报"):
			fg = warningColor
		case tr("读取失败"):
			fg = dangerColor
		}
	}
	r.L += 5
	r.R -= 5
	r.T += 6
	r.B -= 6
	roundedFill(n.DC, r, mixColor(panel, fg, 9), mixColor(panel, fg, 19))
	icon := iconNone
	if n.Sub == backendColumn {
		icon = iconExternal
		r.L += 2
		r.R -= 2
	}
	paintIconLabel(n.DC, r, row[n.Sub], ui.smallFont, fg, fg, icon, 11)
	return true
}

// Shared calm background and glass buttons for small utility windows.
func paintUtilityWindow(h uintptr) {
	var ps paint
	dc := call(user, "BeginPaint", h, uintptr(unsafe.Pointer(&ps)))
	defer call(user, "EndPaint", h, uintptr(unsafe.Pointer(&ps)))
	defer logicalDC(dc, h, true)()
	paintGlassBackdrop(dc, h)
}
