//go:build !windows

// Package lock provides an exclusive process lifetime lock for a data directory.
// The marker file remains on disk; the operating system releases the actual lock
// when its handle closes or the process exits, including unexpected termination.
package lock

import (
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
)

type fileLock struct {
	mu   sync.Mutex
	file *os.File
}

func Acquire(path string) (io.Closer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("数据目录正在被另一进程使用：%w", err)
	}
	return &fileLock{file: f}, nil
}

func (l *fileLock) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	closeErr := f.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
