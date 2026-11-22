//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func showDiagnosticResult(result diagnosticResult) {
	if result.path == "" {
		message(result.message)
		return
	}
	if desktop.diagnosticWindow == 0 {
		instance := call(kernel, "GetModuleHandleW", 0)
		cls := wc{Size: uint32(unsafe.Sizeof(wc{})), Proc: syscall.NewCallback(diagnosticFolderProc), Instance: instance, Cursor: call(user, "LoadCursorW", 0, 32512), Name: wide("MineWatchDiagnosticResult")}
		call(user, "RegisterClassExW", uintptr(unsafe.Pointer(&cls)))
		h := newNativeWindow(cls.Name, tr("采集诊断已保存"), 0x00cf0000, desktop.h, instance, 700, 340, 640, 280)
		if h == 0 {
			message(result.message)
			return
		}
		desktop.diagnosticWindow = h
		child := func(class, text string, style uintptr, id uintptr) uintptr {
			handle := call(user, "CreateWindowExW", 0, uintptr(unsafe.Pointer(wide(class))), uintptr(unsafe.Pointer(wide(text))), style|0x50010000, 0, 0, 0, 0, h, id, instance, 0)
			setControlFont(handle)
			return handle
		}
		desktop.diagnosticMessage = child("EDIT", "", 0x00310844, 61)
		desktop.diagnosticOpen = child("BUTTON", tr("打开所在文件夹"), 0x0b, 62)
		desktop.diagnosticClose = child("BUTTON", tr("关闭"), 0x0b, 63)
	}
	desktop.diagnosticPath = result.path
	txt(desktop.diagnosticMessage, result.message)
	var bounds rect
	call(user, "GetClientRect", desktop.diagnosticWindow, uintptr(unsafe.Pointer(&bounds)))
	layoutDiagnosticResult(bounds.R, bounds.B)
	call(user, "ShowWindow", desktop.diagnosticWindow, 5)
	call(user, "SetForegroundWindow", desktop.diagnosticWindow)
}
func layoutDiagnosticResult(w, h int32) {
	w, h = canvasSize(desktop.diagnosticWindow)
	if w < 1 || h < 1 {
		return
	}
	place(desktop.diagnosticMessage, 16, 16, max(int32(1), w-32), max(int32(40), h-80))
	place(desktop.diagnosticOpen, 16, h-50, 180, 32)
	place(desktop.diagnosticClose, max(int32(206), w-106), h-50, 90, 32)
}
func diagnosticFolderProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if result, handled := displayMessage(h, m, w, l); handled {
		return result
	}
	switch m {
	case 5:
		if desktop.diagnosticMessage != 0 {
			layoutDiagnosticResult(int32(l&65535), int32(l>>16))
		}
		return 0
	case 0x14:
		defer logicalDC(w, h, true)()
		paintGlassBackdrop(w, h)
		return 1
	case 0x2b:
		paintButton(l)
		return 1
	case 0x133, 0x138:
		return colorControl(w, true)
	case 0x111:
		switch int(w & 65535) {
		case 62:
			revealDiagnosticFile(desktop.diagnosticPath)
		case 63:
			call(user, "DestroyWindow", h)
		}
		return 0
	case 0x10:
		call(user, "DestroyWindow", h)
		return 0
	case 2:
		desktop.diagnosticWindow = 0
		desktop.diagnosticMessage = 0
		desktop.diagnosticOpen = 0
		desktop.diagnosticClose = 0
		desktop.diagnosticPath = ""
		return 0
	}
	result, _, _ := def.Call(h, uintptr(m), w, l)
	return result
}
func revealDiagnosticFile(path string) {
	absolute, e := filepath.Abs(path)
	if e != nil {
		message(tr("无法定位诊断文件。"))
		return
	}
	info, e := os.Stat(absolute)
	if e != nil || info.IsDir() {
		message(tr("诊断文件不存在或已移动：\r\n") + absolute)
		return
	}
	ole := syscall.NewLazyDLL("ole32.dll")
	initialized := call(ole, "CoInitializeEx", 0, 2)
	if int32(initialized) >= 0 {
		defer call(ole, "CoUninitialize")
	}
	shell := syscall.NewLazyDLL("shell32.dll")
	var pidl uintptr
	hr := call(shell, "SHParseDisplayName", uintptr(unsafe.Pointer(wide(absolute))), 0, uintptr(unsafe.Pointer(&pidl)), 0, 0)
	if int32(hr) < 0 || pidl == 0 {
		message(fmt.Sprintf(tr("无法定位诊断文件（错误 0x%08X）：\r\n%s"), uint32(hr), absolute))
		return
	}
	defer call(ole, "CoTaskMemFree", pidl)
	hr = call(shell, "SHOpenFolderAndSelectItems", pidl, 0, 0, 0)
	if int32(hr) < 0 {
		message(fmt.Sprintf(tr("无法打开所在文件夹（错误 0x%08X）：\r\n%s"), uint32(hr), filepath.Dir(absolute)))
	}
}
