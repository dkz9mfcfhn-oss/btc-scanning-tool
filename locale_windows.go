//go:build windows

package main

import "unsafe"

var filterLabels = [...]string{"全部矿机", "在线矿机", "读取失败 / 待更新", "拒绝率未上报", "无算力", "已隐藏矿机"}
var intervalLabels = [...]string{"30 秒", "1 分钟", "2 分钟", "5 分钟", "10 分钟", "15 分钟", "30 分钟"}

func setComboLabels(combo uintptr, labels []string, selected uintptr) {
	send.Call(combo, 0x14b, 0, 0) // CB_RESETCONTENT
	for _, label := range labels {
		send.Call(combo, 0x143, 0, uintptr(unsafe.Pointer(wide(tr(label)))))
	}
	send.Call(combo, 0x160, uintptr(scaleUI(216, windowDPI(combo))), 0) // CB_SETDROPPEDWIDTH
	send.Call(combo, 0x14e, selected, 0)
}

func languageCaption() string { return languageChoices[currentLanguage()].label + " ▾" }

func openLanguageMenu() {
	menu := call(user, "CreatePopupMenu")
	if menu == 0 {
		return
	}
	defer call(user, "DestroyMenu", menu)
	for i, option := range languageChoices {
		flags := uintptr(0)
		if uiLanguage(i) == currentLanguage() {
			flags = 8
		}
		listMenuItem(menu, flags, uintptr(i+1), option.label)
	}
	var r rect
	call(user, "GetWindowRect", desktop.languageButton, uintptr(unsafe.Pointer(&r)))
	if selected := trackListMenu(menu, point{r.L, r.B}); selected > 0 && selected <= uintptr(len(languageChoices)) {
		applyNativeLanguage(uiLanguage(selected - 1))
	}
}

func applyNativeLanguage(lang uiLanguage) {
	if lang == currentLanguage() {
		return
	}
	if err := desktop.app.setLanguage(lang); err != nil {
		message(tr("无法保存界面语言：") + currentLanguage().diagnostic(err.Error()))
		return
	}
	activeUILanguage.Store(uint32(lang))
	txt(desktop.languageButton, languageCaption())
	txt(desktop.sourceButton, tr("软件来源"))
	txt(desktop.zoomButton, uiScaleCaption())
	txt(desktop.phoneButton, tr("手机提醒"))
	txt(desktop.scan, tr("保存并扫描"))
	txt(desktop.poll, tr("立即采样"))
	txt(desktop.export, tr("导出24h CSV"))
	txt(desktop.details, tr("矿机详情"))
	txt(desktop.autoColumns, tr("列表设置 ▾"))
	txt(desktop.electricityButton, tr("收益设置 ▾"))
	if desktop.fullList {
		txt(desktop.expand, tr("显示趋势"))
	} else {
		txt(desktop.expand, tr("展开列表"))
	}
	if desktop.rebooting {
		txt(desktop.reboot, tr("重启处理中…"))
	} else {
		txt(desktop.reboot, tr("选中重启"))
	}
	if desktop.diagnosing {
		txt(desktop.diagnose, tr("诊断中…"))
	} else {
		txt(desktop.diagnose, tr("采集诊断"))
	}
	send.Call(desktop.search, 0x1501, 1, uintptr(unsafe.Pointer(wide(tr("搜索 IP、矿工名、备注")))))
	for _, field := range []struct {
		h      uintptr
		labels []string
	}{{desktop.filter, filterLabels[:]}, {desktop.intervalBox, intervalLabels[:]}, {desktop.themeBox, themeLabels[:]}} {
		index, _, _ := send.Call(field.h, 0x147, 0, 0)
		setComboLabels(field.h, field.labels, index)
	}
	for i, col := range nativeListColumns {
		setColumnText(desktop.list, i, tr(col.Name))
	}
	refreshDetailLabels()
	refreshPhoneLabels()
	refreshTextDialogLabels()
	if desktop.diagnosticWindow != 0 {
		txt(desktop.diagnosticWindow, tr("采集诊断已保存"))
		txt(desktop.diagnosticOpen, tr("打开所在文件夹"))
		txt(desktop.diagnosticClose, tr("关闭"))
	}
	if desktop.app.demo {
		txt(desktop.h, appDisplayName+" "+appVersion+tr(" · 演示数据（非真实矿机）"))
	}
	if tray.added {
		updateTrayTip()
	}
	processStats.label = ""
	refreshNative()
	call(user, "RedrawWindow", desktop.h, 0, 0, 0x85)
}

func setColumnText(list uintptr, index int, text string) {
	c := column{Mask: 4, Text: wide(text)}                               // LVCF_TEXT
	send.Call(list, 0x1060, uintptr(index), uintptr(unsafe.Pointer(&c))) // LVM_SETCOLUMNW
}
