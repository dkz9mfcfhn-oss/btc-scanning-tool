//go:build windows

// Package lock provides an exclusive process lifetime lock for a data directory.
// LockFileEx locks one byte even for an empty file. Windows releases the lock on
// process exit, so a leftover marker never prevents recovery after a crash.
package lock

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var lockFileEx = kernel32.NewProc("LockFileEx")
var unlockFileEx = kernel32.NewProc("UnlockFileEx")

type fileLock struct {
	mu         sync.Mutex
	file       *os.File
	overlapped syscall.Overlapped
}

func Acquire(path string) (io.Closer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	l := &fileLock{file: f}
	// LOCKFILE_FAIL_IMMEDIATELY | LOCKFILE_EXCLUSIVE_LOCK, one byte at offset 0.
	ok, _, callErr := lockFileEx.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&l.overlapped)))
	runtime.KeepAlive(l)
	if ok == 0 {
		f.Close()
		return nil, fmt.Errorf("数据目录正在被另一进程使用或无法锁定：%w", callErr)
	}
	return l, nil
}

func (l *fileLock) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	ok, _, callErr := unlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&l.overlapped)))
	runtime.KeepAlive(l)
	closeErr := f.Close()
	if ok == 0 {
		return fmt.Errorf("释放目录锁失败：%w", callErr)
	}
	return closeErr
}
