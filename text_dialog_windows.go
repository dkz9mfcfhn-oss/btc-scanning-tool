//go:build windows

package main

import (
	"fmt"
	"strconv"
	"syscall"
	"unsafe"
)

var textDialog struct {
	h, edit, help, confirm, cancel uintptr
	title, titleSuffix, helpText   string
	save                           func(string) error
}

func refreshTextDialogLabels() {
	if textDialog.h == 0 {
		return
	}
	txt(textDialog.h, tr(textDialog.title)+textDialog.titleSuffix)
	txt(textDialog.help, tr(textDialog.helpText))
	txt(textDialog.confirm, tr("保存"))
	txt(textDialog.cancel, tr("取消"))
}

func openTextDialog(title, titleSuffix, help, initial string, save func(string) error) {
	if textDialog.h != 0 {
		call(user, "SetForegroundWindow", textDialog.h)
		return
	}
	instance := call(kernel, "GetModuleHandleW", 0)
	cls := wc{Size: uint32(unsafe.Sizeof(wc{})), Proc: syscall.NewCallback(textDialogProc), Instance: instance, Cursor: call(user, "LoadCursorW", 0, 32512), Name: wide("MineWatchTextDialog"), Icon: tray.icon, SmallIcon: tray.smallIcon}
	call(user, "RegisterClassExW", uintptr(unsafe.Pointer(&cls)))
	h := newNativeWindow(cls.Name, tr(title)+titleSuffix, 0x00cf0000, desktop.h, instance, 660, 284, 630, 232)
	if h == 0 {
		return
	}
	textDialog.h = h
	textDialog.save = save
	textDialog.title, textDialog.titleSuffix, textDialog.helpText = title, titleSuffix, help
	child := func(class, text string, style, id uintptr, x, y, w, hg int32) uintptr {
		v := call(user, "CreateWindowExW", 0, uintptr(unsafe.Pointer(wide(class))), uintptr(unsafe.Pointer(wide(text))), style|0x50010000, uintptr(x), uintptr(y), uintptr(w), uintptr(hg), h, id, instance, 0)
		setControlFont(v)
		place(v, x, y, w, hg)
		return v
	}
	textDialog.help = child("STATIC", "", 0, 0, 18, 18, 590, 65)
	textDialog.edit = child("EDIT", initial, 0x00800080, 91, 18, 94, 590, 34)
	send.Call(textDialog.edit, 0xc5, 200, 0)
	textDialog.confirm = child("BUTTON", "", 0x0b, 92, 365, 162, 115, 34)
	textDialog.cancel = child("BUTTON", "", 0x0b, 93, 493, 162, 115, 34)
	refreshTextDialogLabels()
	applyNativeTheme()
	call(user, "ShowWindow", h, 5)
	call(user, "SetFocus", textDialog.edit)
}
func textDialogProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if result, handled := displayMessage(h, m, w, l); handled {
		return result
	}
	switch m {
	case 0x14:
		return 1
	case 15:
		paintUtilityWindow(h)
		return 0
	case 0x138:
		if l != textDialog.edit {
			return glassLabelColor(w, l)
		}
		return colorControl(w, true)
	case 0x133, 0x134:
		return colorControl(w, true)
	case 0x2b:
		paintButton(l)
		return 1

	case 0x111:
		if w&65535 == 92 {
			if err := textDialog.save(value(textDialog.edit)); err != nil {
				message(err.Error())
				return 0
			}
			refreshNative()
			call(user, "DestroyWindow", h)
			return 0
		}
		if w&65535 == 93 {
			call(user, "DestroyWindow", h)
			return 0
		}
	case 0x10:
		call(user, "DestroyWindow", h)
		return 0
	case 2:
		textDialog.h = 0
		textDialog.save = nil
		return 0
	}
	return call(user, "DefWindowProcW", h, uintptr(m), w, l)
}
func editMinerNote(ip string) {
	a := desktop.app
	a.mu.Lock()
	d := a.devices[ip]
	note := ""
	if d != nil {
		note = d.Note
	}
	a.mu.Unlock()
	openTextDialog("矿机备注 · ", ip, "可填写机架、位置或维修说明（最多200字）；留空可清除。", note, func(note string) error { return a.setMinerNote(ip, note) })
}

func editElectricityTariff() {
	a := desktop.app
	a.mu.Lock()
	tariff := a.config.ElectricityCNY
	a.mu.Unlock()
	initial := ""
	if tariff != nil {
		initial = strconv.FormatFloat(*tariff, 'f', -1, 64)
	}
	openTextDialog("电价设置 · 元/度", "", "例如 0.42 表示每度电 0.42 元。留空可清除；填 0 表示免电费。\r\n此电价用于最近 24 小时和每日晨报的估算。", initial, a.setElectricityTariff)
}

func openProfitSettings() {
	menu := call(user, "CreatePopupMenu")
	if menu == 0 {
		return
	}
	defer call(user, "DestroyMenu", menu)
	a := desktop.app
	a.mu.Lock()
	tariff, fee := a.config.ElectricityCNY, a.config.PoolFeePercent
	quote, demo := a.market, a.demo
	a.mu.Unlock()
	price := tr("未设置")
	if tariff != nil {
		price = fmt.Sprintf(tr("%.4g 元/度"), *tariff)
	}
	listMenuItem(menu, 0, 1, tr("设置电价… · ")+price)
	listMenuItem(menu, 0, 2, fmt.Sprintf(tr("设置矿池手续费… · %.4g%%"), fee))
	listMenuItem(menu, 0, 3, tr("查看最近 24 小时收益明细…"))
	var r rect
	call(user, "GetWindowRect", desktop.electricityButton, uintptr(unsafe.Pointer(&r)))
	switch trackListMenu(menu, point{X: r.L, Y: r.B}) {
	case 1:
		editElectricityTariff()
	case 2:
		initial := strconv.FormatFloat(fee, 'f', -1, 64)
		openTextDialog("矿池手续费 · %", "", "输入全场适用的手续费百分比，例如 2.5 表示 2.5%。\r\n净收益扣除该费用和电费。留空或填 0 表示免手续费。", initial, a.setPoolFee)
	case 3:
		history := desktop.average24
		message(tr("最近 24 小时收益明细\n\n") + currentLanguage().profitReportText(history.average, history.profit, dashboardQuote(quote, demo), tariff, fee))
	}
}

func editMinerPower(ip string) {
	a := desktop.app
	a.mu.Lock()
	d := a.devices[ip]
	initial := ""
	if d != nil && d.ManualPowerW != nil {
		initial = strconv.FormatFloat(*d.ManualPowerW, 'f', -1, 64)
	}
	a.mu.Unlock()
	openTextDialog("手动功率 · ", ip, "输入整机瓦数，例如 3200。留空可清除。\r\n仅在缺少可用功率时补用，从之后的采样计入历史。", initial, func(text string) error {
		return a.setManualPower(ip, text)
	})
}
