//go:build windows

package main

import (
	"context"
	"syscall"
	"unsafe"
)

var phone struct {
	window, edit, status, cloudLabel, cloudButton uintptr
	result                                        chan string
}

var phoneLabels = [...]struct {
	id   uintptr
	text string
}{
	{70, "粘贴 Bark 推送地址（到设备密钥为止，不含 /title/body）"},
	{71, "连续两轮掉线或零算力提醒一次，恢复再提醒；附矿工名和原因。"},
	{72, "每天 08:00（北京时间）发送前24小时算力、预估净收益、矿池费和电费。"},
	{73, "隐藏矿机不进入日报；需保持程序运行。当天晚启动会补发。"},
	{74, "每10分钟心跳，超过25分钟失联提醒。检修或主动退出前可暂停看护。"},
	{82, "保存并启用"}, {83, "发送测试"}, {84, "关闭全部提醒"},
}

func refreshPhoneLabels() {
	if phone.window == 0 {
		return
	}
	txt(phone.window, tr("iPhone 手机提醒"))
	for _, label := range phoneLabels {
		txt(call(user, "GetDlgItem", phone.window, label.id), tr(label.text))
	}
}

func openPhone() {
	if phone.window != 0 {
		call(user, "ShowWindow", phone.window, 5)
		call(user, "SetForegroundWindow", phone.window)
		return
	}
	instance := call(kernel, "GetModuleHandleW", 0)
	cls := wc{Size: uint32(unsafe.Sizeof(wc{})), Proc: syscall.NewCallback(phoneProc), Instance: instance, Cursor: call(user, "LoadCursorW", 0, 32512), Name: wide("MineWatchPhone"), Icon: tray.icon, SmallIcon: tray.smallIcon}
	call(user, "RegisterClassExW", uintptr(unsafe.Pointer(&cls)))
	h := newNativeWindow(cls.Name, tr("iPhone 手机提醒"), 0x00cf0000, desktop.h, instance, 740, 440, 712, 390)
	if h == 0 {
		return
	}
	phone.window = h
	phone.result = make(chan string, 1)
	child := func(class, text string, style, id uintptr, x, y, w, h int32) uintptr {
		v := call(user, "CreateWindowExW", 0, uintptr(unsafe.Pointer(wide(class))), uintptr(unsafe.Pointer(wide(text))), style|0x50010000, uintptr(x), uintptr(y), uintptr(w), uintptr(h), phone.window, id, instance, 0)
		setControlFont(v)
		place(v, x, y, w, h)
		return v
	}
	child("STATIC", "", 0, 70, 18, 18, 680, 25)
	phone.edit = child("EDIT", "", 0x00800080, 81, 18, 53, 675, 32)
	desktop.app.mu.Lock()
	endpoint := desktop.app.pushURL
	note := desktop.app.pushError
	cloudStatus := desktop.app.cloudStatus
	cloudDisabled := desktop.app.config.CloudWatchDisabled
	if desktop.app.dailyError != "" {
		note += " " + desktop.app.dailyError
	}
	desktop.app.mu.Unlock()
	txt(phone.edit, endpoint)
	child("STATIC", "", 0, 71, 18, 99, 675, 25)
	child("STATIC", "", 0, 72, 18, 130, 675, 25)
	child("STATIC", "", 0, 73, 18, 160, 675, 25)
	phone.cloudLabel = child("STATIC", tr("云端看护：")+currentLanguage().diagnostic(cloudStatus), 0, 0, 18, 193, 675, 25)
	cloudLabel := tr("暂停云端看护")
	if cloudDisabled {
		cloudLabel = tr("启用云端看护")
	}
	phone.cloudButton = child("BUTTON", cloudLabel, 0x0b, 85, 18, 300, 150, 34)
	child("STATIC", "", 0, 74, 18, 344, 675, 25)
	phone.status = child("STATIC", currentLanguage().diagnostic(note), 0, 0, 18, 224, 675, 30)
	child("BUTTON", "", 0x0b, 82, 18, 258, 140, 34)
	child("BUTTON", "", 0x0b, 83, 173, 258, 125, 34)
	child("BUTTON", "", 0x0b, 84, 313, 258, 145, 34)
	refreshPhoneLabels()
	call(user, "SetTimer", h, 1, 500, 0)
	applyNativeTheme()
	call(user, "ShowWindow", h, 5)
}
func phoneProc(h uintptr, m uint32, w, l uintptr) uintptr {
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
		if l != phone.edit {
			return glassLabelColor(w, l)
		}
		return colorControl(w, true)
	case 0x133, 0x134:
		return colorControl(w, true)
	case 0x2b:
		paintButton(l)
		return 1

	case 0x113:
		desktop.app.mu.Lock()
		cloudText := desktop.app.cloudStatus
		disabled := desktop.app.config.CloudWatchDisabled
		desktop.app.mu.Unlock()
		if cloudText == "" {
			cloudText = "等待首次连接"
		}
		label := tr("暂停云端看护")
		if disabled {
			label = tr("启用云端看护")
			cloudText = "已暂停"
		}
		cloudText = currentLanguage().diagnostic(cloudText)
		if value(phone.cloudLabel) != tr("云端看护：")+cloudText {
			txt(phone.cloudLabel, tr("云端看护：")+cloudText)
		}
		if value(phone.cloudButton) != label {
			txt(phone.cloudButton, label)
		}
		select {
		case note := <-phone.result:
			txt(phone.status, currentLanguage().diagnostic(note))
		default:
		}
		return 0
	case 0x111:
		switch int(w & 65535) {
		case 82:
			if e := desktop.app.setPushURL(value(phone.edit)); e != nil {
				txt(phone.status, currentLanguage().diagnostic(e.Error()))
			} else {
				txt(phone.status, tr("已保存；掉线、零算力提醒和每天08:00日报均已启用。"))
			}
		case 85:
			a := desktop.app
			a.mu.Lock()
			enabled := a.config.CloudWatchDisabled
			a.mu.Unlock()
			result := phone.result
			txt(phone.status, tr("正在更新云端看护…"))
			go func() {
				note := tr("云端看护设置已更新")
				if e := a.setCloudWatch(enabled); e != nil {
					note = e.Error()
				}
				select {
				case result <- note:
				default:
				}
			}()
		case 84:
			if e := desktop.app.setPushURL(""); e != nil {
				txt(phone.status, currentLanguage().diagnostic(e.Error()))
			} else {
				txt(phone.status, tr("手机提醒已关闭"))
			}
		case 83:
			endpoint, e := validatePushURL(value(phone.edit))
			if e != nil {
				txt(phone.status, currentLanguage().diagnostic(e.Error()))
				break
			}
			result := phone.result
			txt(phone.status, tr("正在发送测试…"))
			go func() {
				note := tr("服务器已接受测试通知，请确认手机是否收到")
				if e := sendPush(context.Background(), endpoint, appDisplayName+tr(" · 测试"), tr("手机提醒连接测试")); e != nil {
					note = e.Error()
				}
				select {
				case result <- note:
				default:
				}
			}()
		}
		return 0
	case 0x10:
		call(user, "DestroyWindow", h)
		return 0
	case 2:
		phone.window = 0
		return 0
	}
	r, _, _ := def.Call(h, uintptr(m), w, l)
	return r
}
