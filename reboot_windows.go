//go:build windows

package main

import (
	"context"
	"fmt"
	"minerwatch/internal/miner"
	"strings"
	"time"
	"unsafe"
)

func selectedRebootIPs() []string {
	var ips []string
	index := ^uintptr(0)
	for {
		next, _, _ := send.Call(desktop.list, 0x100c, index, 2)
		if int32(next) < 0 || int(next) >= len(desktop.ips) {
			break
		}
		ips = append(ips, desktop.ips[next])
		index = next
	}
	return ips
}
func rebootSelectedNative() {
	if desktop.rebooting {
		return
	}
	a := desktop.app
	if a.demo {
		message(tr("演示模式不能重启矿机。"))
		return
	}
	ips := selectedRebootIPs()
	if len(ips) == 0 {
		message(tr("请先选择矿机；按住 Ctrl 或 Shift 可多选。"))
		return
	}
	a.mu.Lock()
	busy := a.job.Running || a.diagnosticRunning
	a.mu.Unlock()
	if busy {
		message(tr("正在采集或诊断，请等本轮结束后重启。"))
		return
	}
	prompt := fmt.Sprintf(tr("重启以下 %d 台矿机？重启期间算力会暂时中断。\r\n\r\n%s"), len(ips), strings.Join(ips, "\r\n"))
	answer := call(user, "MessageBoxW", desktop.h, uintptr(unsafe.Pointer(wide(prompt))), uintptr(unsafe.Pointer(wide(tr("确认选中重启")))), 0x134)
	if answer != 6 {
		return
	}
	type target struct{ ip, brand, model, protocol string }
	var targets []target
	a.mu.Lock()
	if a.job.Running || a.diagnosticRunning {
		a.mu.Unlock()
		message(tr("采集已开始，请等本轮结束。"))
		return
	}
	for _, ip := range ips {
		if d := a.devices[ip]; d != nil {
			targets = append(targets, target{ip, d.Brand, d.Model, d.Protocol})
		}
	}
	a.diagnosticRunning = true
	a.jobs.Add(1)
	a.mu.Unlock()
	creds := miner.Credentials{Username: value(desktop.username), Password: value(desktop.password)}
	desktop.rebooting = true
	call(user, "EnableWindow", desktop.reboot, 0)
	txt(desktop.reboot, tr("重启处理中…"))
	go func() {
		defer a.jobs.Done()
		defer func() { a.mu.Lock(); a.diagnosticRunning = false; a.mu.Unlock() }()
		var lines []string
		for _, t := range targets {
			if a.ctx.Err() != nil {
				lines = append(lines, t.ip+tr("：已取消，未继续发送"))
				continue
			}
			ctx, cancel := context.WithTimeout(a.ctx, 35*time.Second)
			result := miner.Reboot(ctx, t.ip, t.brand, t.model, t.protocol, creds)
			cancel()
			lines = append(lines, t.ip+"："+currentLanguage().diagnostic(result.Message))
		}
		desktop.rebootResult <- strings.Join(lines, "\r\n")
	}()
}
