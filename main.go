//go:build !windows

package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"minerwatch/internal/lock"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"
)

//go:embed web/*
var assets embed.FS

func main() {
	demo := flag.Bool("demo", false, "Use isolated demonstration data; no network probes")
	noBrowser := flag.Bool("no-browser", false, "Do not open browser automatically")
	dir := flag.String("data-dir", "", "Directory for local settings and history")
	port := flag.Int("port", 17828, "Local dashboard port")
	flag.Parse()
	if *demo && *port == 17828 {
		*port = 17829
	}
	if *dir != "" && *demo {
		*dir = filepath.Join(*dir, "demo")
	}
	if *dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			log.Fatal(err)
		}
		*dir = filepath.Join(base, "MineWatch")
		if *demo {
			*dir = filepath.Join(*dir, "demo")
		}
	}
	// Bind before opening persistent state; a second process must not write it.
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		log.Fatalf("Cannot listen on %s. MineWatch may already be running: %v", addr, err)
	}
	if err = os.MkdirAll(*dir, 0700); err != nil {
		log.Fatal(err)
	}
	// An exclusive lock also protects an explicitly shared --data-dir across ports.
	processLock, err := lock.Acquire(filepath.Join(*dir, "running.lock"))
	if err != nil {
		log.Fatal(err)
	}
	defer processLock.Close()
	app, err := newApp(*dir, *demo)
	if err != nil {
		log.Print(err)
		return
	}
	defer app.db.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	app.cancel = cancel
	app.ctx = ctx
	mux := http.NewServeMux()
	app.routes(mux)
	sub, _ := fs.Sub(assets, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	srv := &http.Server{Handler: guard(addr, mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	scheduleDone := make(chan struct{})
	go func() { defer close(scheduleDone); app.schedule(ctx) }()
	go func() {
		<-ctx.Done()
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		srv.Shutdown(c)
	}()
	url := "http://" + addr
	log.Printf("MineWatch 1.0.0 — %s\nData: %s\nKeep this process running for 5-minute monitoring.", url, *dir)
	if !*noBrowser {
		go func() { time.Sleep(250 * time.Millisecond); openBrowser(url) }()
	}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Print(err)
	}
	cancel()
	<-scheduleDone
	app.jobs.Wait()
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("Open browser manually: %s", url)
	} else {
		go cmd.Wait()
	}
}
