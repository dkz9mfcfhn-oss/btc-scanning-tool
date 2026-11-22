package lock

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestExclusiveLockAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "running.lock")
	a, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := Acquire(path); err == nil {
		b.Close()
		t.Fatal("second process handle acquired held lock")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal("Close must be idempotent", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("marker should remain on disk", err)
	}
	b, err := Acquire(path)
	if err != nil {
		t.Fatal("leftover marker blocked reacquisition", err)
	}
	b.Close()
}

func TestLockReleasedAfterProcessDeath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "running.lock")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessLockHelper$")
	cmd.Env = append(os.Environ(), "MINERWATCH_LOCK_HELPER="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "LOCKED\n" {
		t.Fatalf("helper lock failed: %q %v", line, err)
	}
	if l, err := Acquire(path); err == nil {
		l.Close()
		t.Fatal("acquired lock held by child")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	l, err := Acquire(path)
	if err != nil {
		t.Fatal("process death did not release lock", err)
	}
	l.Close()
}

func TestProcessLockHelper(t *testing.T) {
	path := os.Getenv("MINERWATCH_LOCK_HELPER")
	if path == "" {
		return
	}
	l, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	fmt.Println("LOCKED")
	for {
		time.Sleep(time.Hour)
	}
}
