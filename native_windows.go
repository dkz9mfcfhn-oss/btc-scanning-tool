//go:build windows

package main

import (
	"flag"
	"fmt"
	"minerwatch/internal/lock"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var user = syscall.NewLazyDLL("user32.dll")
var gdi = syscall.NewLazyDLL("gdi32.dll")
var comctl = syscall.NewLazyDLL("comctl32.dll")
var kernel = syscall.NewLazyDLL("kernel32.dll")
var create = user.NewProc("CreateWindowExW")
var send = user.NewProc("SendMessageW")
var def = user.NewProc("DefWindowProcW")
var setText = user.NewProc("SetWindowTextW")
var getText = user.NewProc("GetWindowTextW")
var move = user.NewProc("MoveWindow")
var invalidate = user.NewProc("InvalidateRect")

type point struct{ X, Y int32 }
type rect struct{ L, T, R, B int32 }
type msg struct {
	H       uintptr
	ID      uint32
	W, L    uintptr
	Time    uint32
	P       point
	Private uint32
}
type wc struct {
	Size, Style                   uint32
	Proc                          uintptr
	ClsExtra, WndExtra            int32
	Instance, Icon, Cursor, Brush uintptr
	Menu, Name                    *uint16
	SmallIcon                     uintptr
}
type paint struct {
	DC           uintptr
	Erase        int32
	Rect         rect
	Restore, Inc int32
	Reserved     [32]byte
}
type column struct {
	Mask                              uint32
	Fmt, Width                        int32
	Text                              *uint16
	Max, Sub                          int32
	Image, Order, Min, Default, Ideal int32
}
type item struct {
	Mask             uint32
	Index, Sub       int32
	State, StateMask uint32
	Text             *uint16
	Max, Image       int32
	Param            uintptr
	Indent, Group    int32
	Columns          uint32
	ColPtr, ColFmt   uintptr
	GroupIndex       int32
}

var desktop struct {
	startup                                                               *nativeStartup
	dashboardReady                                                        bool
	average24                                                             rollingFarmAverage
	cardWidths                                                            cardWidths
	lastProgress                                                          Job
	phoneButton                                                           uintptr
	priorityButton                                                        uintptr
	electricityButton                                                     uintptr
	app                                                                   *App
	h, list, chart, ranges, username, password, scan, poll, pause, export uintptr
	statusText                                                            string
	detailHistory, detailChart, detailStats                               uintptr
	detailHistoryKey                                                      string
	detailHistoryMode                                                     bool
	detailTimelineMode                                                    bool
	detailTimeline                                                        uintptr
	reboot                                                                uintptr
	rebooting                                                             bool
	rebootResult                                                          chan string
	diagnosticWindow, diagnosticMessage, diagnosticOpen, diagnosticClose  uintptr
	diagnosticPath                                                        string
	font                                                                  uintptr
	themeBox, zoomButton, languageButton, sourceButton                    uintptr
	intervalBox                                                           uintptr
	ips                                                                   []string
	selected                                                              string
	width, height                                                         int32
	lastRefresh                                                           time.Time
	search, filter, expand, details, diagnose, autoColumns                uintptr
	fullList, refreshing, diagnosing                                      bool
	visibleCount, totalCount                                              int
	rows                                                                  [][]string
	trends                                                                map[string]trendCache
	diagResult                                                            chan diagnosticResult
	detailWindow, detailEdit                                              uintptr
	detailTable, detailHeading, detailToggle                              uintptr
	detailExpanded                                                        bool
	detailRows                                                            [][]string
	singleChart                                                           bool
}

func wide(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

//go:uintptrescapes
func call(d *syscall.LazyDLL, name string, args ...uintptr) uintptr {
	r, _, _ := d.NewProc(name).Call(args...)
	return r
}
func message(s string) {
	call(user, "MessageBoxW", desktop.h, uintptr(unsafe.Pointer(wide(currentLanguage().diagnostic(s)))), uintptr(unsafe.Pointer(wide(appDisplayName))), 0)
}
func ctrl(class, label string, style uint32, id int) uintptr {
	h, _, _ := create.Call(0, uintptr(unsafe.Pointer(wide(class))), uintptr(unsafe.Pointer(wide(label))), uintptr(style|0x50010000), 0, 0, 0, 0, desktop.h, uintptr(id), 0, 0)
	setControlFont(h)
	return h
}
func txt(h uintptr, s string) { setText.Call(h, uintptr(unsafe.Pointer(wide(s)))) }
func value(h uintptr) string {
	length := min(call(user, "GetWindowTextLengthW", h), uintptr(4<<20))
	b := make([]uint16, int(length)+1)
	getText.Call(h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b)
}
func place(h uintptr, x, y, w, hg int32) {
	placeScaled(h, uiBox{x, y, w, hg}, true)
}
func main() {
	runtime.LockOSThread()
	enableDisplayScaling()
	defer releaseDisplayFonts()
	demo := flag.Bool("demo", false, tr("演示"))
	dir := flag.String("data-dir", "", tr("数据目录"))
	flag.Parse()
	if *dir == "" {
		base, e := os.UserConfigDir()
		if e != nil {
			message(e.Error())
			return
		}
		*dir = filepath.Join(base, "MineWatch")
	}
	if *demo {
		*dir = filepath.Join(*dir, "demo")
	}
	if e := os.MkdirAll(*dir, 0700); e != nil {
		message(e.Error())
		return
	}
	lk, e := lock.Acquire(filepath.Join(*dir, "running.lock"))
	if e != nil {
		existing := call(user, "FindWindowW", uintptr(unsafe.Pointer(wide("MineWatchNative"))), 0)
		if existing != 0 {
			call(user, "ShowWindow", existing, 9)
			call(user, "SetForegroundWindow", existing)
			return
		}
		message(tr("程序已运行或数据目录无法锁定：") + e.Error())
		return
	}
	defer lk.Close()
	init := struct{ Size, ICC uint32 }{8, 1}
	call(comctl, "InitCommonControlsEx", uintptr(unsafe.Pointer(&init)))
	instance := call(kernel, "GetModuleHandleW", 0)
	loadAppIcons(instance)
	defer releaseAppIcons()
	themeValues("glass")
	brush := call(gdi, "CreateSolidBrush", bg)
	defer call(gdi, "DeleteObject", brush)
	cursor := call(user, "LoadCursorW", 0, 32512)
	desktop.font = call(gdi, "CreateFontW", uintptr(^uint32(15)), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(wide("Microsoft YaHei UI"))))
	defer call(gdi, "DeleteObject", desktop.font)
	themeStart()
	defer themeStop()
	cls := wc{Size: uint32(unsafe.Sizeof(wc{})), Style: 8, Proc: syscall.NewCallback(windowProc), Instance: instance, Cursor: cursor, Brush: brush, Name: wide("MineWatchNative"), Icon: tray.icon, SmallIcon: tray.smallIcon} // CS_DBLCLKS
	if call(user, "RegisterClassExW", uintptr(unsafe.Pointer(&cls))) == 0 {
		message(tr("窗口注册失败"))
		return
	}
	chartcls := wc{Size: uint32(unsafe.Sizeof(wc{})), Proc: syscall.NewCallback(chartProc), Instance: instance, Cursor: cursor, Brush: brush, Name: wide("MineWatchChart")}
	call(user, "RegisterClassExW", uintptr(unsafe.Pointer(&chartcls)))
	title := appDisplayName + " " + appVersion
	if *demo {
		title += tr(" · 演示数据（非真实矿机）")
	}
	detailcls := wc{Size: uint32(unsafe.Sizeof(wc{})), Proc: syscall.NewCallback(detailProc), Instance: instance, Cursor: cursor, Brush: brush, Name: wide("MineWatchDetail"), Icon: tray.icon, SmallIcon: tray.smallIcon}
	call(user, "RegisterClassExW", uintptr(unsafe.Pointer(&detailcls)))
	h := newNativeWindow(cls.Name, title, 0x02cf0000, 0, instance, 1500, 960, dashboardMinWidth, 644)
	if h == 0 {
		message(tr("窗口创建失败"))
		return
	}
	desktop.h = h
	// The first paint is deliberately lightweight. Display it before loading
	// history or building anything that may wait on a slow drive.
	call(user, "ShowWindow", h, 5)
	call(user, "UpdateWindow", h)
	desktop.startup = beginNativeStartup(h, *dir, *demo)
	defer desktop.startup.close()
	defer removeTray()
	var m msg
	for {
		v := call(user, "GetMessageW", uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(v) <= 0 {
			break
		}
		// Route keyboard navigation through the window that owns the focused
		// control, including dialogs on another monitor.
		if desktop.dashboardReady && handleZoomKey(&m) {
			continue
		}
		root := rootWindow(m.H)
		if displays[root] != nil && call(user, "IsDialogMessageW", root, uintptr(unsafe.Pointer(&m))) != 0 {
			continue
		}
		call(user, "TranslateMessage", uintptr(unsafe.Pointer(&m)))
		call(user, "DispatchMessageW", uintptr(unsafe.Pointer(&m)))
	}
}

func openNativeDashboard(h uintptr, loaded nativeStartupResult) {
	a := loaded.app
	desktop.app = a
	activeUILanguage.Store(uint32(configuredLanguage(a.config.Language)))
	if a.demo {
		txt(h, appDisplayName+" "+appVersion+tr(" · 演示数据（非真实矿机）"))
	}
	desktop.average24 = loaded.average
	if widths := a.config.DashboardWidths; widths != nil && widths.valid() {
		desktop.cardWidths = *widths
	}
	uiScalePercent = normalizedUIScale(a.config.UIScalePercent)
	themeValues(a.config.Theme)
	desktop.diagResult = make(chan diagnosticResult, 1)
	desktop.rebootResult = make(chan string, 1)
	desktop.phoneButton = ctrl("BUTTON", tr("手机提醒"), 0x0b, phoneCommand)
	desktop.reboot = ctrl("BUTTON", tr("选中重启"), 0x0b, 34)
	desktop.trends = make(map[string]trendCache)
	desktop.ranges = ctrl("EDIT", loaded.ranges, 0x00001080, 11)
	desktop.username = ctrl("EDIT", a.config.Username, 0x00000080, 12)
	desktop.password = ctrl("EDIT", a.password, 0x000000a0, 13)
	desktop.scan = ctrl("BUTTON", tr("保存并扫描"), 0x0b, 21)
	desktop.poll = ctrl("BUTTON", tr("立即采样"), 0x0b, 22)
	desktop.pause = ctrl("BUTTON", tr("暂停监控"), 0x0b, 23)
	desktop.export = ctrl("BUTTON", tr("导出24h CSV"), 0x0b, 24)
	desktop.list = ctrl("SysListView32", "", 0x00010049, 30)
	call(comctl, "SetWindowSubclass", desktop.list, glassListCallback, 1, 0)
	setListRowHeight(desktop.list)
	send.Call(desktop.list, 0x1036, 0, 0x14420) // double buffer, full row, infotips, label tips
	send.Call(desktop.list, 0x2005, 0, 1)       // Unicode common-control notifications
	send.Call(desktop.list, 0x1001, 0, panel)
	send.Call(desktop.list, 0x1026, 0, panel)
	send.Call(desktop.list, 0x1024, 0, ink)
	call(syscall.NewLazyDLL("uxtheme.dll"), "SetWindowTheme", desktop.list, uintptr(unsafe.Pointer(wide("DarkMode_Explorer"))), 0)

	for i, col := range nativeListColumns {
		c := column{Mask: 7, Width: scaleUI(col.Width, windowDPI(desktop.list)), Text: wide(tr(col.Name)), Sub: int32(i)}
		send.Call(desktop.list, 0x1061, uintptr(i), uintptr(unsafe.Pointer(&c)))
	}
	desktop.priorityButton = ctrl("BUTTON", tr("异常优先"), 0x0b, priorityCommand)
	desktop.search = ctrl("EDIT", "", 0x00000080, 26)
	send.Call(desktop.search, 0x1501, 1, uintptr(unsafe.Pointer(wide(tr("搜索 IP、矿工名、备注")))))
	desktop.filter = ctrl("COMBOBOX", "", 0x00200203, 27)
	setComboLabels(desktop.filter, filterLabels[:], 0)
	desktop.expand = ctrl("BUTTON", tr("展开列表"), 0x0b, 28)
	desktop.details = ctrl("BUTTON", tr("矿机详情"), 0x0b, 29)
	desktop.diagnose = ctrl("BUTTON", tr("采集诊断"), 0x0b, 32)
	desktop.autoColumns = ctrl("BUTTON", tr("列表设置 ▾"), 0x0b, 33)
	desktop.chart = ctrl("MineWatchChart", "", 0, 31)
	desktop.intervalBox = ctrl("COMBOBOX", "", 0x00200203, 25)
	chosen := 3
	for i, v := range intervalChoices {
		if time.Duration(v)*time.Second == a.config.pollInterval() {
			chosen = i
		}
	}
	setComboLabels(desktop.intervalBox, intervalLabels[:], uintptr(chosen))
	desktop.themeBox = ctrl("COMBOBOX", "", 0x00200203, 35)
	for i, name := range themeChoices {
		if normalizedTheme(a.config.Theme) == name {
			setComboLabels(desktop.themeBox, themeLabels[:], uintptr(i))
			break
		}
	}
	for _, combo := range []uintptr{desktop.filter, desktop.intervalBox, desktop.themeBox} {
		installComboGlass(combo)
	}
	desktop.zoomButton = ctrl("BUTTON", uiScaleCaption(), 0x0b, 38)
	desktop.languageButton = ctrl("BUTTON", languageCaption(), 0x0b, languageCommand)
	desktop.sourceButton = ctrl("BUTTON", tr("软件来源"), 0x0b, sourceCommand)
	desktop.electricityButton = ctrl("BUTTON", tr("收益设置 ▾"), 0x0b, profitSettingsCommand)
	applyNativeTheme()
	applyColumnLayout()
	layout(0, 0)
	desktop.statusText = tr("正在显示设备列表…")
	desktop.dashboardReady = true
	call(user, "RedrawWindow", h, 0, 0, 0x85) // RDW_INVALIDATE | RDW_ERASE | RDW_ALLCHILDREN
	// The first list update follows the complete dashboard's initial paint.
	call(user, "SetTimer", h, 1, 1000, 0)
	call(user, "SetTimer", h, nativeFirstListTimer, 100, 0)
	addTray()
	desktop.startup.startMonitor(a)
}

func layout(w, h int32) {
	cancelDashboardDrag(desktop.h)
	w, h = canvasSize(desktop.h)
	if w < 1 || h < 1 {
		return
	}
	desktop.width, desktop.height = w, h
	g := desktopGeometry(w, h)
	controls := map[int]uintptr{38: desktop.zoomButton, priorityCommand: desktop.priorityButton, phoneCommand: desktop.phoneButton, 34: desktop.reboot, 35: desktop.themeBox, 11: desktop.ranges, 12: desktop.username, 13: desktop.password, 21: desktop.scan, 22: desktop.poll, 23: desktop.pause, 24: desktop.export, 26: desktop.search, 27: desktop.filter, 28: desktop.expand, 33: desktop.autoColumns, 29: desktop.details, 32: desktop.diagnose, 25: desktop.intervalBox}
	controls[profitSettingsCommand] = desktop.electricityButton
	controls[languageCommand] = desktop.languageButton
	controls[sourceCommand] = desktop.sourceButton
	for id, handle := range controls {
		r := g.Controls[id]
		height := r.H
		if id == 25 || id == 27 || id == 35 {
			height = 230
		}
		place(handle, r.X, r.Y, r.W, height)
	}
	ui.tableY, ui.tableH = g.Table.Y, g.Table.H
	place(desktop.list, g.Table.X, g.Table.Y, g.Table.W, g.Table.H)
	roundTableWindow(desktop.list)
	scheduleMinerListFit(desktop.list)
	show := uintptr(5)
	if desktop.fullList {
		show = 0
	}
	call(user, "ShowWindow", desktop.chart, show)
	if !desktop.fullList {
		place(desktop.chart, g.Chart.X, g.Chart.Y, g.Chart.W, g.Chart.H)
	}
	invalidate.Call(desktop.h, 0, 1)
}
func windowProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if result, handled := displayMessage(h, m, w, l); handled {
		return result
	}
	if desktop.dashboardReady && tray.taskbarCreated != 0 && m == tray.taskbarCreated {
		tray.added = false
		if !addTray() {
			showMainWindow()
		}
		return 0
	}
	if desktop.dashboardReady && m == trayMessage {
		trayEvent(uint32(l))
		return 0
	}
	if !desktop.dashboardReady {
		switch m {
		case nativeStartupMessage:
			desktop.startup.finish(h)
			return 0
		case 15:
			paintStartupWindow(h)
			return 0
		case 0x10:
			call(user, "DestroyWindow", h)
			return 0
		case 0x111, 0x4e, 0x113:
			// Native controls can send notifications during construction.
			return 0
		case 0x2b:
			return 1
		}
	}
	if desktop.dashboardReady && dashboardCardMouse(h, m, l) {
		if m == 0x20 {
			return 1
		}
		return 0
	}
	switch m {
	case 0x14: // WM_ERASEBKGND: the buffered painter covers the background.
		return 1
	case 15:
		paintDashboard(h)
		return 0
	case 0x138:
		return colorControl(w, false)
	case 0x134:
		return colorControl(w, true)
	case 0x133:
		return colorControl(w, true)
	case 0x2b:
		paintButton(l)
		return 1
	case 0x113:
		if w == nativeFirstListTimer {
			call(user, "KillTimer", h, nativeFirstListTimer)
		}
		select {
		case result := <-desktop.rebootResult:
			desktop.rebooting = false
			call(user, "EnableWindow", desktop.reboot, 1)
			txt(desktop.reboot, tr("选中重启"))
			message(result)
		default:
		}
		select {
		case result := <-desktop.diagResult:
			desktop.diagnosing = false
			call(user, "EnableWindow", desktop.diagnose, 1)
			txt(desktop.diagnose, tr("采集诊断"))
			showDiagnosticResult(result)
		default:
		}
		if call(user, "IsWindowVisible", desktop.h) != 0 && call(user, "IsIconic", desktop.h) == 0 {
			refreshProgressIndicator()
		}
		if call(user, "IsWindowVisible", desktop.h) != 0 && call(user, "IsIconic", desktop.h) == 0 && time.Since(desktop.lastRefresh) > 4*time.Second {
			refreshNative()
		}
		return 0
	case 0x111:
		id, code := int(w&65535), (w>>16)&65535
		if id == 26 {
			if code == 0x300 {
				refreshNative()
			}
			return 0
		}
		if id == 25 || id == 27 || id == 35 {
			if l != 0 {
				invalidate.Call(l, 0, 0)
			}
			if code != 1 { // CBN_SELCHANGE
				return 0
			}
		}
		nativeCommand(id)
		return 0
	case 0x4e:
		if l == 0 {
			return 0
		}
		if r, ok := tableDraw(l); ok {
			applyHealthDrawColor(l)
			return r
		}
		if l == 0 || desktop.refreshing {
			return 0
		}
		n := (*nmhdr)(unsafe.Pointer(l))
		if n.H != desktop.list {
			return 0
		}
		if n.Code == -5 {
			click := (*listItemClick)(unsafe.Pointer(l))
			if click.Index >= 0 && int(click.Index) < len(desktop.ips) {
				openMinerVisibilityMenu(desktop.ips[click.Index])
			}
			return 0
		}
		if n.Code == -108 { // LVN_COLUMNCLICK: IP and worker-name headers toggle order.
			click := (*listItemClick)(unsafe.Pointer(l))
			if click.Sub == 0 || click.Sub == 17 {
				toggleNativeListSort(click.Sub)
			}
			return 0
		}
		if n.Code == -2 && backendClick(true) {
			return 0
		}
		if n.Code == -3 && backendClick(false) {
			return 0
		}
		if n.Code == -101 || n.Code == -3 {
			idx, _, _ := send.Call(desktop.list, 0x100c, ^uintptr(0), 1)
			if int32(idx) < 0 {
				idx, _, _ = send.Call(desktop.list, 0x100c, ^uintptr(0), 2)
			}
			if int32(idx) >= 0 && int(idx) < len(desktop.ips) {
				desktop.selected = desktop.ips[idx]
				updateDetail()
				if n.Code == -3 {
					openDetail()
				}
			}
		}
		return 0
	case 0x10:
		hideToTray()
		return 0
	case 2:
		removeTray()
		call(user, "PostQuitMessage", 0)
		return 0
	}
	r, _, _ := def.Call(h, uintptr(m), w, l)
	return r
}
func nativeCommand(id int) {
	if id == priorityCommand {
		a := desktop.app
		a.mu.Lock()
		enabled := !a.config.ExceptionFirst
		a.mu.Unlock()
		if err := a.setExceptionFirst(enabled); err != nil {
			message(err.Error())
		}
		refreshNative()
		return
	}
	a := desktop.app
	switch id {
	case profitSettingsCommand:
		openProfitSettings()
	case languageCommand:
		openLanguageMenu()
	case sourceCommand:
		openProjectSource()
	case 38:
		openUIScaleMenu()
	case 35:
		idx, _, _ := send.Call(desktop.themeBox, 0x147, 0, 0)
		if int(idx) < len(themeChoices) {
			if e := a.setTheme(themeChoices[idx]); e != nil {
				message(e.Error())
			} else {
				applyNativeTheme()
			}
		}
	case 21:
		a.mu.Lock()
		if a.job.Running {
			a.mu.Unlock()
			message(tr("请等待当前采集结束"))
			return
		}
		old := a.config
		c := old
		c.Ranges = strings.TrimSpace(value(desktop.ranges))
		c.Username = value(desktop.username)
		if e := validateConfig(c); e != nil {
			a.mu.Unlock()
			message(e.Error())
			return
		}
		a.config = c
		if e := a.saveLocked(); e != nil {
			a.config = old
			a.mu.Unlock()
			message(e.Error())
			return
		}
		a.password = value(desktop.password)
		a.mu.Unlock()
		if e := a.startJob("scan"); e != nil {
			message(e.Error())
		}
	case 22:
		if e := a.startJob("poll"); e != nil {
			message(e.Error())
		}
	case 23:
		a.mu.Lock()
		a.paused = !a.paused
		if !a.paused {
			a.nextPoll = time.Now()
		}
		a.mu.Unlock()
	case 25:
		selected, _, _ := send.Call(desktop.intervalBox, 0x147, 0, 0)
		if int(selected) < len(intervalChoices) {
			if err := a.setInterval(intervalChoices[selected]); err != nil {
				message(err.Error())
			}
		}
	case 24:
		exportNative()
	case 27:
		// Filter is applied by refreshNative below.
	case 28:
		desktop.fullList = !desktop.fullList
		caption := tr("展开列表")
		if desktop.fullList {
			caption = tr("显示趋势")
		}
		txt(desktop.expand, caption)
		layout(desktop.width, desktop.height)
	case 29:
		openDetail()
	case 32:
		diagnoseNative()
	case phoneCommand:
		openPhone()
	case 34:
		rebootSelectedNative()
	case 33:
		openListSettings()

	}
	refreshNative()
}
func format(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f", *v)
}
func refreshNative() {
	if desktop.list == 0 {
		return
	}
	desktop.lastRefresh = time.Now()
	a := desktop.app
	a.mu.Lock()
	ds := []*Device{}
	for _, d := range a.devices {
		v := *d
		ds = append(ds, &v)
	}
	sortDeviceList(ds, a.config)
	sortMode := listSortMode(a.config)
	priority := a.config.ExceptionFirst
	job := a.job
	paused := a.paused
	next := a.nextPoll
	period := a.config.pollInterval()
	err := a.storageError
	a.mu.Unlock()
	desktop.average24.refresh(desktop.lastRefresh, ds, a.db)
	query := value(desktop.search)
	filter, _, _ := send.Call(desktop.filter, 0x147, 0, 0)
	filtered := make([]*Device, 0, len(ds))
	ips := make([]string, 0, len(ds))
	for _, d := range ds {
		if listIncludesDevice(d, query, int(filter), time.Now()) {
			filtered = append(filtered, d)
			ips = append(ips, d.IP)
		}
	}
	desktop.visibleCount, desktop.totalCount = len(filtered), len(ds)
	selectedSet := map[string]bool{}
	for _, ip := range selectedRebootIPs() {
		selectedSet[ip] = true
	}
	changed := !slices.Equal(ips, desktop.ips)
	top, _, _ := send.Call(desktop.list, 0x1027, 0, 0)
	topIP := ""
	if int(top) < len(desktop.ips) {
		topIP = desktop.ips[top]
	}
	hscroll := call(user, "GetScrollPos", desktop.list, 0)
	rowRect := rect{}
	rowHeight := int32(34)
	if len(desktop.ips) > 0 && call(user, "SendMessageW", desktop.list, 0x100e, top, uintptr(unsafe.Pointer(&rowRect))) != 0 && rowRect.B > rowRect.T {
		rowHeight = rowRect.B - rowRect.T
	}
	desktop.refreshing = true
	send.Call(desktop.list, 0xb, 0, 0)
	if changed {
		send.Call(desktop.list, 0x1009, 0, 0)
	}
	if len(ips) == 0 {
		desktop.selected = ""
	}
	oldRows := desktop.rows
	desktop.ips = ips
	desktop.rows = make([][]string, len(filtered))
	if slices.Index(ips, desktop.selected) < 0 && len(ips) > 0 {
		desktop.selected = ips[0]
	}
	for i, d := range filtered {
		vals := currentLanguage().nativeRow(d, time.Now())
		desktop.rows[i] = vals
		for j, v := range vals {
			if !changed && i < len(oldRows) && j < len(oldRows[i]) && oldRows[i][j] == v {
				continue
			}
			it := item{Mask: 1, Index: int32(i), Sub: int32(j), Text: wide(v)}
			cmd := uintptr(0x1074)
			if changed && j == 0 {
				cmd = 0x104d
			}
			send.Call(desktop.list, cmd, uintptr(i), uintptr(unsafe.Pointer(&it)))
		}
		if changed && (selectedSet[d.IP] || len(selectedSet) == 0 && d.IP == desktop.selected) {
			it := item{State: 3, StateMask: 3}
			send.Call(desktop.list, 0x102b, uintptr(i), uintptr(unsafe.Pointer(&it)))
		}
	}
	if changed {
		index := slices.Index(ips, topIP)
		if index < 0 {
			index = min(int(top), len(ips)-1)
		}
		if index >= 0 {
			send.Call(desktop.list, 0x1013, uintptr(index), 0)
			current, _, _ := send.Call(desktop.list, 0x1027, 0, 0)
			send.Call(desktop.list, 0x1014, 0, uintptr((int64(index)-int64(current))*int64(rowHeight)))
		}
		current := call(user, "GetScrollPos", desktop.list, 0)
		send.Call(desktop.list, 0x1014, uintptr(int64(hscroll)-int64(current)), 0)
	}
	send.Call(desktop.list, 0xb, 1, 0)
	desktop.refreshing = false
	if changed {
		scheduleMinerListFit(desktop.list)
	}
	invalidate.Call(desktop.list, 0, 0)
	button := tr("暂停监控")
	if paused {
		button = tr("恢复监控")
	}
	txt(desktop.pause, button)
	status := fmt.Sprintf(tr("显示 %d / %d 台 · 每%s采样 · 下次 %s · 双击查看原因和温度 · * 为采样均值 · 右键可隐藏矿机"), desktop.visibleCount, desktop.totalCount, period.String(), next.Local().Format("15:04:05"))
	sortCaption := map[string]string{"ip_asc": "IP ↑", "ip_desc": "IP ↓", "name_asc": tr("名称 ↑"), "name_desc": tr("名称 ↓"), "manual": tr("手动")}[sortMode]
	if priority {
		sortCaption = tr("异常优先 · ") + sortCaption
	}
	status = tr("排序 ") + sortCaption + " · " + status
	if paused {
		status = tr("监控已暂停 | ") + status
	}
	if job.Running {
		status = fmt.Sprintf(tr("采集中 %d/%d，读取成功 %d 台 | "), job.Done, job.Total, job.Found) + status
	}
	if err != "" {
		status = currentLanguage().diagnostic(err)
	} else if job.Error != "" {
		status = currentLanguage().diagnostic(job.Error) + " | " + status
	}
	priorityLabel := tr("异常优先：关")
	if priority {
		priorityLabel = tr("异常优先：开")
	}
	txt(desktop.priorityButton, priorityLabel)
	desktop.statusText = resourceLabel(time.Now()) + "  |  " + status
	invalidate.Call(desktop.h, 0, 0)
	updateDetail()
}
func updateDetail() {
	refreshDetailWindow()
	invalidate.Call(desktop.chart, 0, 1)
}

type fileResponse struct {
	*os.File
	headers http.Header
}

func (f fileResponse) Header() http.Header { return f.headers }
func (f fileResponse) WriteHeader(int)     {}
func exportNative() {
	a := desktop.app
	path := filepath.Join(a.dir, "BTC-Scanning-tool-24h-"+time.Now().Format("20060102-150405")+".csv")
	f, e := os.Create(path)
	if e != nil {
		message(e.Error())
		return
	}
	r, _ := http.NewRequest("GET", "http://local/api/export", nil)
	a.export(fileResponse{f, make(http.Header)}, r)
	e = f.Close()
	if e != nil {
		message(e.Error())
		return
	}
	message(tr("已导出最近24小时所有矿机记录：\r\n") + path)
}
func chartProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if h != desktop.detailChart && m == 514 && unscaleUI(int32((l>>16)&0xffff), windowDPI(h)) < 28 {
		desktop.singleChart = !desktop.singleChart
		invalidate.Call(h, 0, 1)
		return 0
	}
	if m == 0x14 { // The chart painter includes its glass backplate.
		return 1
	}
	if m != 15 {
		r, _, _ := def.Call(h, uintptr(m), w, l)
		return r
	}
	var ps paint
	dc := call(user, "BeginPaint", h, uintptr(unsafe.Pointer(&ps)))
	defer call(user, "EndPaint", h, uintptr(unsafe.Pointer(&ps)))
	var r rect
	call(user, "GetClientRect", h, uintptr(unsafe.Pointer(&r)))
	r = logicalRect(r, h)
	defer logicalDC(dc, h, false)()
	call(gdi, "SelectObject", dc, desktop.font)
	if r.R > 4 && r.B > 4 {
		paintControlSurface(dc, h, r, rect{r.L + 1, r.T + 1, r.R - 1, r.B - 1}, panel, panelCornerDiameter)
	}
	call(gdi, "SetTextColor", dc, muted)
	call(gdi, "SetBkMode", dc, 1)
	drawText := func(x, y int32, s string) {
		u, _ := syscall.UTF16FromString(s)
		call(gdi, "TextOutW", dc, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1))
	}
	a := desktop.app
	if a == nil {
		return 0
	}
	if h != desktop.detailChart && !desktop.singleChart {
		drawFleetChart(dc, r)
		return 0
	}
	a.mu.Lock()
	ip := desktop.selected
	a.mu.Unlock()
	now := time.Now()
	samples := historySamples24(a.db.MetricHistory(ip, now.Add(-24*time.Hour)), now)
	if len(samples) == 0 {
		drawText(20, 20, tr("选择矿机后显示最近24小时曲线；首次监控后逐步积累历史。"))
		return 0
	}
	max := 1.
	for _, s := range samples {
		if s.Online {
			for _, v := range []*float64{s.RealtimeTH, s.AverageTH} {
				if historyValue(v) && *v > max {
					max = *v
				}
			}
		}
	}
	max *= 1.08
	start := now.Add(-24 * time.Hour)
	x := func(t time.Time) int32 { return 60 + int32(t.Sub(start).Seconds()/86400*float64(r.R-85)) }
	y := func(v float64) int32 { return r.B - 35 - int32(v/max*float64(r.B-65)) }
	if h == desktop.detailChart {
		drawText(60, 3, tr("24h算力 · TH/s | 实时绿 / 矿机平均蓝"))
	} else {
		drawText(60, 3, tr("单机趋势 · 点击这里返回全场  |  实时绿 / 平均蓝"))
	}
	for i := 0; i < 4; i++ {
		v := max * float64(i) / 3
		line(dc, 60, y(v), r.R-25, y(v), border)
		drawText(6, y(v)-8, fmt.Sprintf("%.0f", v))
	}
	drawText(60, r.B-25, start.Local().Format("01-02 15:04"))
	drawText(r.R-110, r.B-25, tr("现在"))
	for i := 1; i < 4; i++ {
		tick := start.Add(time.Duration(i) * 6 * time.Hour)
		xx := x(tick)
		line(dc, xx, 30, xx, r.B-35, border)
		drawText(xx-18, r.B-25, tick.Local().Format("15:04"))
	}
	for k, color := range []uintptr{teal, blue} {
		pen := call(gdi, "CreatePen", 0, 2, color)
		old := call(gdi, "SelectObject", dc, pen)
		var prev time.Time
		var prevPeriod time.Duration
		for _, s := range samples {
			v := s.RealtimeTH
			if k == 1 {
				v = s.AverageTH
			}
			if !s.Online || !historyValue(v) {
				prev = time.Time{}
				continue
			}
			xx, yy := x(s.At), y(*v)
			if prev.IsZero() || s.At.Sub(prev) > prevPeriod*3/2 {
				call(gdi, "MoveToEx", dc, uintptr(xx), uintptr(yy), 0)
				fill(dc, rect{xx - 2, yy - 2, xx + 2, yy + 2}, color)
			} else {
				call(gdi, "LineTo", dc, uintptr(xx), uintptr(yy))
			}

			prev = s.At
			prevPeriod = sampleInterval(&s)
		}
		call(gdi, "SelectObject", dc, old)
		call(gdi, "DeleteObject", pen)
	}
	return 0
}

var intervalChoices = []int{30, 60, 120, 300, 600, 900, 1800}

func applyColumnLayout() {
	desktop.app.mu.Lock()
	c := desktop.app.config
	widths := configuredColumnWidths(c)
	desktop.app.mu.Unlock()
	order := nativeColumnOrder(!c.FullColumns)
	send.Call(desktop.list, 0x103a, uintptr(len(order)), uintptr(unsafe.Pointer(&order[0])))
	setDisplayColumnWidths(desktop.list, widths)
	scheduleMinerListFit(desktop.list)
}

// Extend the existing custom draw without changing trend rendering or column
// indexes. Use the displayed row's state so its color and wording always agree.
func applyHealthDrawColor(ptr uintptr) {
	n := (*customDraw)(unsafe.Pointer(ptr))
	if n.Header.H != desktop.list || n.Header.Code != -12 || n.Stage != 0x30001 || int(n.Item) >= len(desktop.rows) {
		return
	}
	if n.Sub != 2 && n.Sub != 15 && n.Sub != 3 {
		return
	}
	row := desktop.rows[n.Item]
	if len(row) < 3 {
		return
	}
	switch row[2] {
	case tr("无算力"), tr("算力未报"):
		n.Text = uint32(warningColor)
	case tr("读取失败"):
		n.Text = uint32(dangerColor)
	case tr("待更新"):
		n.Text = uint32(muted)
	case tr("在线"):
		n.Text = uint32(teal)
	}
}
