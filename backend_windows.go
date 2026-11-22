//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

type listHitTest struct {
	Point            point
	Flags            uint32
	Item, Sub, Group int32
}

// Hit-test the clicked cell, not the previously selected row. Physical subitem
// indexes remain stable after horizontal scrolling or column reordering.
func backendClick(open bool) bool {
	pos := call(user, "GetMessagePos")
	hit := listHitTest{Point: point{int32(int16(pos & 0xffff)), int32(int16((pos >> 16) & 0xffff))}, Item: -1}
	call(user, "ScreenToClient", desktop.list, uintptr(unsafe.Pointer(&hit.Point)))
	send.Call(desktop.list, 0x1039, 0, uintptr(unsafe.Pointer(&hit))) // LVM_SUBITEMHITTEST
	if hit.Sub != backendColumn || hit.Item < 0 || int(hit.Item) >= len(desktop.ips) {
		return false
	}
	if open {
		openDefaultBackend(desktop.ips[hit.Item])
	}
	return true
}

func openDefaultBackend(ip string) {
	url, e := backendURL(ip)
	if e != nil {
		message(e.Error())
		return
	}
	// ShellExecuteW routes an HTTP URL to the user's default browser. No command
	// interpreter, credentials or automatic firmware login are involved.
	if result := launchBrowser(url); result <= 32 {
		message(fmt.Sprintf("无法打开后台（系统错误 %d）。请检查默认浏览器，或手动访问：\r\n%s", result, url))
	}
}

func openProjectSource() {
	if result := launchBrowser(appSourceURL); result <= 32 {
		message(fmt.Sprintf(tr("无法打开软件来源（系统错误 %d）。\r\n%s"), result, appSourceURL))
	}
}

func launchBrowser(url string) uintptr {
	return call(syscall.NewLazyDLL("shell32.dll"), "ShellExecuteW", desktop.h, uintptr(unsafe.Pointer(wide("open"))), uintptr(unsafe.Pointer(wide(url))), 0, 0, 1)
}
