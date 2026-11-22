//go:build windows

package main

import (
	"context"
	"fmt"
	"minerwatch/internal/miner"
	"minerwatch/internal/store"
	"minerwatch/internal/telemetry"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unsafe"
)

type diagnosticResult struct{ message, path string }

func diagnoseNative() {
	if desktop.diagnosing {
		return
	}
	if desktop.selected == "" {
		message(tr("请先选择一台矿机。"))
		return
	}
	a := desktop.app
	if a.demo {
		message(tr("演示数据无法诊断，请在实际监控窗口选择矿机。"))
		return
	}
	a.mu.Lock()
	if a.job.Running {
		a.mu.Unlock()
		message(tr("正在采集矿机数据，请等本次采集结束后点击采集诊断。"))
		return
	}
	a.diagnosticRunning = true
	a.mu.Unlock()
	ip := desktop.selected
	creds := miner.Credentials{Username: value(desktop.username), Password: value(desktop.password)}
	desktop.diagnosing = true
	call(user, "EnableWindow", desktop.diagnose, 0)
	txt(desktop.diagnose, tr("诊断中…"))
	a.jobs.Add(1)
	go func() {
		defer a.jobs.Done()
		defer func() { a.mu.Lock(); a.diagnosticRunning = false; a.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(a.ctx, 65*time.Second)
		defer cancel()
		data, err := miner.Diagnose(ctx, ip, creds)
		result := ""
		savedPath := ""
		if err != nil {
			result = tr("诊断失败：") + err.Error()
		} else {
			dir := filepath.Join(a.dir, "diagnostics")
			path := filepath.Join(dir, "miner-"+strings.ReplaceAll(ip, ":", "_")+"-"+time.Now().Format("20060102-150405")+".json")
			err = os.MkdirAll(dir, 0700)
			if err == nil {
				err = os.WriteFile(path, data, 0600)
			}
			if err != nil {
				result = tr("保存诊断失败：") + err.Error()
			} else {
				savedPath = path
				result = tr("诊断已保存：\r\n") + path + tr("\r\n\r\n包含算力原因、温度、份额及只读接口的脱敏指标，不含密码、令牌或矿池账号。可将文件提供给开发者核对无算力原因、温度或拒绝份额字段。")
			}
		}
		desktop.diagResult <- diagnosticResult{message: result, path: savedPath}
	}()
}

func detailText() string {
	a := desktop.app
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.devices[desktop.selected]
	if d == nil {
		return tr("请选择矿机。")
	}
	var b strings.Builder
	fmt.Fprintf(&b, tr("备注：%s\r\n"), d.Note)
	fmt.Fprintf(&b, tr("矿机完整信息\r\n\r\nIP：%s\r\n机位：%s\r\n品牌 / 型号：%s %s\r\n采集协议：%s\r\n"), d.IP, d.Label, d.Brand, d.Model, d.Protocol)
	if d.Latest == nil {
		return b.String() + tr("\r\n尚无采样记录。")
	}
	s := d.Latest
	fmt.Fprintf(&b, tr("矿工名：%s\r\n"), currentLanguage().workerDisplay(d))
	if s.Online {
		fmt.Fprintf(&b, tr("连续运行时间（采样时）：%s\r\n"), currentLanguage().runtimeText(s))
		if s.ElapsedSeconds != nil && !s.At.IsZero() {
			start := s.At.Add(-time.Duration(int64(*s.ElapsedSeconds)) * time.Second)
			fmt.Fprintf(&b, tr("估算启动时间：%s\r\n"), start.Local().Format("2006-01-02 15:04:05"))
		}
	}
	b.WriteString(currentLanguage().healthDetailText(s, time.Now()))
	if !currentSampleTime(s, time.Now()) {
		b.WriteString(tr("以下为历史采集记录，不代表当前状态。\r\n"))
	}
	fmt.Fprintf(&b, tr("采样时间：%s\r\n采样间隔：%s\r\n\r\n"), s.At.Local().Format("2006-01-02 15:04:05"), sampleInterval(s))
	fmt.Fprintf(&b, tr("实时算力：%s TH/s\r\n平均算力：%s TH/s\r\n温度概览：%s °C\r\n进水温度：%s °C\r\n出水温度：%s °C\r\n"), format(s.RealtimeTH), format(s.AverageTH), format(temperatureHeadline(s.TempC, s.Boards)), format(s.InletC), format(s.OutletC))
	fmt.Fprintf(&b, tr("拒绝率：%s %%\r\n接受份额：%s    拒绝份额：%s\r\n口径：本次设备累计份额，不是24小时区间拒绝率。\r\n"), format(rejection(s)), format(s.Accepted), format(s.Rejected))
	if len(s.Warnings) > 0 {
		b.WriteString(tr("\r\n采集提示：\r\n") + strings.Join(displayWarnings(s.Warnings), "\r\n") + "\r\n")
	}
	if s.Error != "" {
		b.WriteString(tr("\r\n采集错误：\r\n") + s.Error + "\r\n")
	}
	b.WriteString("\r\n" + currentLanguage().temperatureReport(s.Boards, s.Temperatures))
	return b.String()
}

var detailColumnLabels = [...]string{"算力板", "芯片均温 °C", "芯片最低 °C", "芯片最高 °C", "PCB均温 °C", "PCB最低 °C", "PCB最高 °C"}

func refreshDetailLabels() {
	if desktop.detailWindow == 0 {
		return
	}
	for i, name := range detailColumnLabels {
		setColumnText(desktop.detailTable, i, tr(name))
	}
	history, sensors := tr("24h算力统计"), tr("查看测温点")
	if desktop.detailHistoryMode && !desktop.detailTimelineMode {
		history = tr("查看温度详情")
	}
	if desktop.detailExpanded {
		sensors = tr("收起测温点")
	}
	txt(desktop.detailHistory, history)
	txt(desktop.detailToggle, sensors)
	desktop.detailHistoryKey = ""
}

func openDetail() {
	if desktop.selected == "" {
		message(tr("请先选择一台矿机。"))
		return
	}
	if desktop.detailWindow == 0 {
		h := newNativeWindow(wide("MineWatchDetail"), tr("矿机完整详情"), 0x00cf0000, desktop.h, 0, 1100, 780, dashboardMinWidth, 420)
		if h == 0 {
			message(tr("无法打开详情窗口"))
			return
		}
		desktop.detailWindow = h
		desktop.detailExpanded = false
		desktop.detailHistoryMode = true
		desktop.detailTimelineMode = false
		child := func(class, label string, style uint32, id int) uintptr {
			v, _, _ := create.Call(0, uintptr(unsafe.Pointer(wide(class))), uintptr(unsafe.Pointer(wide(label))), uintptr(style|0x50010000), 0, 0, 0, 0, h, uintptr(id), 0, 0)
			setControlFont(v)
			return v
		}
		desktop.detailHeading = child("STATIC", "", 0, 40)
		desktop.detailToggle = child("BUTTON", tr("查看测温点"), 0x0b, 41)
		desktop.detailHistory = child("BUTTON", tr("查看温度详情"), 0x0b, 43)
		desktop.detailTimeline = child("BUTTON", tr("故障时间线"), 0x0b, 46)
		desktop.detailStats = child("STATIC", "", 0, 44)
		desktop.detailChart = child("MineWatchChart", "", 0, 45)
		desktop.detailTable = child("SysListView32", "", 0x0081004d, 42)
		setListRowHeight(desktop.detailTable)
		send.Call(desktop.detailTable, 0x1036, 0, 0x14421)
		send.Call(desktop.detailTable, 0x1001, 0, panel)
		send.Call(desktop.detailTable, 0x1026, 0, panel)
		send.Call(desktop.detailTable, 0x1024, 0, ink)
		widths := []int32{175, 125, 115, 115, 125, 115, 115}
		for i, name := range detailColumnLabels {
			c := column{Mask: 7, Text: wide(tr(name)), Sub: int32(i)}
			send.Call(desktop.detailTable, 0x1061, uintptr(i), uintptr(unsafe.Pointer(&c)))
		}
		setDisplayColumnWidths(desktop.detailTable, widths)
		edit, _, _ := create.Call(0, uintptr(unsafe.Pointer(wide("EDIT"))), 0, 0x50310844, 0, 0, 0, 0, h, 0, 0, 0) // multiline, readonly, vertical scroll, auto scroll
		desktop.detailEdit = edit
		setControlFont(edit)
		send.Call(edit, 0xc5, 4<<20, 0) // Allow complete sensor reports beyond the default edit limit.
		var r rect
		call(user, "GetClientRect", h, uintptr(unsafe.Pointer(&r)))
		layoutDetail(r.R, r.B)
	}
	refreshDetailWindow()
	applyNativeTheme()
	call(user, "ShowWindow", desktop.detailWindow, 5)
	call(user, "SetForegroundWindow", desktop.detailWindow)
}
func refreshDetailWindow() {
	if desktop.detailWindow == 0 {
		return
	}
	txt(desktop.detailTimeline, tr("故障时间线"))
	if desktop.detailTimelineMode {
		txt(desktop.detailTimeline, tr("返回详情"))
		samples := desktop.app.db.History(desktop.selected, time.Now().Add(-72*time.Hour))
		text := currentLanguage().faultTimelineText(samples, time.Now())
		if value(desktop.detailEdit) != text {
			txt(desktop.detailEdit, text)
		}
		txt(desktop.detailHeading, desktop.selected+tr(" · 故障时间线"))
		return
	}
	a := desktop.app
	a.mu.Lock()
	d := a.devices[desktop.selected]
	var boards []telemetry.BoardTemperature
	var sensors []telemetry.Sensor
	heading := tr("请选择矿机")
	nowText := ""
	healthText := ""
	if d != nil {
		heading = d.IP + " · " + d.Model + " · " + currentLanguage().workerDisplay(d)
		if d.Latest != nil {
			heading += " · " + currentLanguage().healthSummary(d.Latest, time.Now())
			healthText = currentLanguage().healthDetailText(d.Latest, time.Now())
			if watts, manual := store.ResolvePower(d.Latest.PowerW, d.Latest.PowerSource, d.ManualPowerW); watts != nil && fresh(d.Latest, time.Now()) {
				source := d.Latest.PowerSource
				if manual {
					source = tr("手动设定（估算）")
				}
				healthText += fmt.Sprintf(tr("\r\n功率 %.0f W · %s\r\n"), *watts, source)
			}
			for _, warning := range d.Latest.Warnings {
				if strings.Contains(warning, "温度接口仅返回0") || strings.Contains(warning, "阿瓦隆后台日志读取失败") {
					healthText += "\r\n" + currentLanguage().diagnostic(warning) + "\r\n"
				}
			}
			boards = telemetry.CloneBoards(d.Latest.Boards)
			sensors = telemetry.CloneSensors(d.Latest.Temperatures)
			nowText = tr("采样 ") + d.Latest.At.Local().Format("01-02 15:04:05")
			if !fresh(d.Latest, time.Now()) {
				nowText += tr("  ·  当前读取失败或待更新，以下为采集记录")
			}
		}
	}
	a.mu.Unlock()
	txt(desktop.detailWindow, tr("矿机详情 · ")+desktop.selected)
	txt(desktop.detailHeading, heading+"\r\n"+nowText+tr("  |  * 为采样点均值"))
	rows := currentLanguage().simpleTemperatureRows(displayTemperatureBoards(boards, sensors))
	if !reflect.DeepEqual(rows, desktop.detailRows) {
		sameCount := len(rows) == len(desktop.detailRows)
		send.Call(desktop.detailTable, 0xb, 0, 0)
		if !sameCount {
			send.Call(desktop.detailTable, 0x1009, 0, 0)
		}
		for i, row := range rows {
			for j, v := range row {
				it := item{Mask: 1, Index: int32(i), Sub: int32(j), Text: wide(v)}
				cmd := uintptr(0x1074)
				if !sameCount && j == 0 {
					cmd = 0x104d
				}
				send.Call(desktop.detailTable, cmd, uintptr(i), uintptr(unsafe.Pointer(&it)))
			}
		}
		desktop.detailRows = rows
		var r rect
		call(user, "GetClientRect", desktop.detailWindow, uintptr(unsafe.Pointer(&r)))
		layoutDetail(r.R, r.B)
		send.Call(desktop.detailTable, 0xb, 1, 0)
		invalidate.Call(desktop.detailTable, 0, 0)
	}
	if desktop.detailHistoryMode {
		now := time.Now()
		samples := historySamples24(a.db.History(desktop.selected, now.Add(-24*time.Hour)), now)
		txt(desktop.detailStats, currentLanguage().deviceHistorySummary(samples))
		key := fmt.Sprintf("%s/%d", desktop.selected, len(samples))
		if len(samples) > 0 {
			key += samples[0].At.String() + samples[len(samples)-1].At.String()
		}
		if desktop.detailHistoryKey != key {
			txt(desktop.detailEdit, currentLanguage().deviceHistoryReport(samples, now))
			desktop.detailHistoryKey = key
		}
		invalidate.Call(desktop.detailChart, 0, 0)
		return
	}
	text := healthText + "\r\n" + currentLanguage().simpleTemperatureReport(displayTemperatureBoards(boards, sensors), sensors)
	if original := btcTemperatureText(sensors); original != "" {
		text = tr("温度（BTC Tools顺序）：") + original + " °C\r\n" + text
	}
	if len(boards) == 0 {
		text = tr("尚未取得每板温度。请完成一次新采集；旧历史不会自动补出每板温度。\r\n\r\n") + text
	}
	if desktop.detailExpanded {
		text = detailText()
	}
	if value(desktop.detailEdit) != text {
		txt(desktop.detailEdit, text)
	}
}
func layoutDetail(w, h int32) {
	w, h = canvasSize(desktop.detailWindow)
	if w < 80 || h < 140 {
		return
	}
	bodyTop, actionsY, headingWidth := int32(82), int32(16), w-480
	if w < 790 {
		bodyTop, actionsY, headingWidth = 124, 78, w-32
	}
	place(desktop.detailHeading, 16, 14, headingWidth, 63)
	place(desktop.detailTimeline, w-458, actionsY, 146, 32)
	place(desktop.detailHistory, w-302, actionsY, 146, 32)
	place(desktop.detailToggle, w-148, actionsY, 130, 32)
	visible := uintptr(5)
	hidden := uintptr(0)
	if desktop.detailTimelineMode {
		for _, control := range []uintptr{desktop.detailTable, desktop.detailToggle, desktop.detailStats, desktop.detailChart} {
			call(user, "ShowWindow", control, hidden)
		}
		place(desktop.detailEdit, 16, bodyTop, w-32, h-bodyTop-16)
		return
	}
	if desktop.detailHistoryMode {
		call(user, "ShowWindow", desktop.detailTable, hidden)
		call(user, "ShowWindow", desktop.detailToggle, hidden)
		call(user, "ShowWindow", desktop.detailStats, visible)
		call(user, "ShowWindow", desktop.detailChart, visible)
		place(desktop.detailStats, 16, bodyTop, w-32, 75)
		chartH := min(int32(260), max(int32(120), h/3))
		place(desktop.detailChart, 16, bodyTop+80, w-32, chartH)
		editTop := bodyTop + 92 + chartH
		place(desktop.detailEdit, 16, editTop, w-32, h-editTop-16)
		return
	}
	call(user, "ShowWindow", desktop.detailTable, visible)
	call(user, "ShowWindow", desktop.detailToggle, visible)
	call(user, "ShowWindow", desktop.detailStats, hidden)
	call(user, "ShowWindow", desktop.detailChart, hidden)
	tableH := min(min(int32(235), h/3), max(int32(110), int32(len(desktop.detailRows))*34+36))
	place(desktop.detailTable, 16, bodyTop+6, w-32, tableH)
	roundTableWindow(desktop.detailTable)
	editTop := bodyTop + 20 + tableH
	place(desktop.detailEdit, 16, editTop, w-32, h-editTop-16)
}
func detailProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if result, handled := displayMessage(h, m, w, l); handled {
		return result
	}
	switch m {
	case 0x14:
		defer logicalDC(w, h, true)()
		paintGlassBackdrop(w, h)
		return 1
	case 5:
		if desktop.detailEdit != 0 {
			layoutDetail(int32(l&65535), int32(l>>16))
		}
		return 0
	case 0x2b:
		paintButton(l)
		return 1
	case 0x111:
		if int(w&65535) == 46 {
			desktop.detailTimelineMode = !desktop.detailTimelineMode
			var r rect
			call(user, "GetClientRect", h, uintptr(unsafe.Pointer(&r)))
			layoutDetail(r.R, r.B)
			refreshDetailWindow()
			return 0
		}
		if int(w&65535) == 43 {
			wasTimeline := desktop.detailTimelineMode
			desktop.detailTimelineMode = false
			desktop.detailHistoryMode = !desktop.detailHistoryMode || wasTimeline
			desktop.detailHistoryKey = ""
			caption := tr("24h算力统计")
			if desktop.detailHistoryMode {
				caption = tr("查看温度详情")
			}
			txt(desktop.detailHistory, caption)
			var r rect
			call(user, "GetClientRect", h, uintptr(unsafe.Pointer(&r)))
			layoutDetail(r.R, r.B)
			refreshDetailWindow()
			return 0
		}
		if int(w&65535) == 41 {
			desktop.detailExpanded = !desktop.detailExpanded
			caption := tr("查看测温点")
			if desktop.detailExpanded {
				caption = tr("收起测温点")
			}
			txt(desktop.detailToggle, caption)
			refreshDetailWindow()
		}
		return 0
	case 0x4e:
		if l != 0 {
			n := (*customDraw)(unsafe.Pointer(l))
			if n.Header.H == desktop.detailTable && n.Header.Code == -12 {
				switch n.Stage {
				case 1:
					return 0x20
				case 0x10001:
					return 0x20
				case 0x30001:
					n.Text = uint32(ink)
					n.Bg = uint32(panel)
					if n.Item%2 == 1 {
						n.Bg = uint32(stripeColor)
					}
					if n.Sub == 1 || n.Sub == 4 {
						n.Text = uint32(teal)
					}
					return 0
				}
			}
		}
		return 0
	case 0x138:
		if l == desktop.detailHeading || l == desktop.detailStats {
			return glassLabelColor(w, l)
		}
		return colorControl(w, true)
	case 0x133:
		return colorControl(w, true)
	case 0x10:
		call(user, "DestroyWindow", h)
		return 0
	case 2:
		desktop.detailWindow = 0
		desktop.detailEdit = 0
		desktop.detailTable = 0
		desktop.detailHeading = 0
		desktop.detailToggle = 0
		desktop.detailRows = nil
		desktop.detailHistory = 0
		desktop.detailChart = 0
		desktop.detailStats = 0
		desktop.detailHistoryKey = ""
		return 0
	}
	r, _, _ := def.Call(h, uintptr(m), w, l)
	return r
}
