//go:build windows

package main

import (
	"fmt"
	"runtime"
	"time"
	"unsafe"
)

var processStats struct {
	at    time.Time
	cpu   uint64
	label string
}

func resourceLabel(now time.Time) string {
	if processStats.label != "" && now.Sub(processStats.at) < 5*time.Second {
		return processStats.label
	}
	var creation, exit, kernelTime, userTime uint64
	handle := call(kernel, "GetCurrentProcess")
	ok := call(kernel, "GetProcessTimes", handle, uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kernelTime)), uintptr(unsafe.Pointer(&userTime)))
	cpu := 0.0
	current := kernelTime + userTime
	if ok != 0 && !processStats.at.IsZero() && current >= processStats.cpu {
		cpu = float64(current-processStats.cpu) * 1e-7 / now.Sub(processStats.at).Seconds() * 100 / float64(runtime.NumCPU())
	}
	mem := struct {
		Size, PageFaults                                                                                uint32
		PeakWorking, Working, PeakPaged, Paged, PeakNonpaged, Nonpaged, Pagefile, PeakPagefile, Private uintptr
	}{}
	mem.Size = uint32(unsafe.Sizeof(mem))
	call(kernel, "K32GetProcessMemoryInfo", handle, uintptr(unsafe.Pointer(&mem)), uintptr(mem.Size))
	processStats.at, processStats.cpu = now, current
	processStats.label = fmt.Sprintf(tr("本程序 CPU %.1f%% · 内存 %.0f MB"), cpu, float64(mem.Working)/(1<<20))
	return processStats.label
}
