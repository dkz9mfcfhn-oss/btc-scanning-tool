package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const cloudHeartbeatInterval = 10 * time.Minute

func (a *App) loadCloudIdentity() {
	path := filepath.Join(a.dir, "monitor-id")
	data, _ := os.ReadFile(path)
	if len(data) == 32 {
		a.cloudID = string(data)
		return
	}
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return
	}
	a.cloudID = hex.EncodeToString(b)
	_ = os.WriteFile(path, []byte(a.cloudID), 0600)
}
func cloudTarget(endpoint string) (string, string, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", errors.New("无有效Bark地址")
	}
	// Only the user's self-hosted Bark prefix opts into the paired watchdog.
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "minewatch-bark" || len(parts[1]) < 16 {
		return "", "", errors.New("云端看护需要配套自建Bark服务")
	}
	return "https://" + u.Host + "/minewatch-watchdog/heartbeat", parts[1], nil
}
func sendCloudHeartbeat(ctx context.Context, endpoint, id string, paused, enabled bool) error {
	target, key, e := cloudTarget(endpoint)
	if e != nil {
		return e
	}
	data, _ := json.Marshal(map[string]any{"id": id, "name": "矿场监控电脑", "paused": paused, "enabled": enabled})
	req, e := http.NewRequestWithContext(ctx, "POST", target, strings.NewReader(string(data)))
	if e != nil {
		return errors.New("无法创建看护请求")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("不接受重定向") }}
	resp, e := c.Do(req)
	if e != nil {
		return errors.New("云端看护连接失败")
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != 200 {
		return errors.New("云端看护尚未就绪")
	}
	return nil
}
func (a *App) maybeCloudHeartbeat(ctx context.Context, now time.Time) {
	a.mu.Lock()
	endpoint := a.pushURL
	if a.demo || a.cloudRunning || endpoint == "" || a.cloudID == "" || a.config.CloudWatchDisabled || now.Before(a.cloudNext) || ctx.Err() != nil {
		a.mu.Unlock()
		return
	}
	a.cloudNext = now.Add(cloudHeartbeatInterval)
	a.cloudRunning = true
	id, paused := a.cloudID, a.paused
	a.jobs.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.jobs.Done()
		a.cloudIO.Lock()
		defer a.cloudIO.Unlock()
		a.mu.Lock()
		if a.config.CloudWatchDisabled || a.pushURL != endpoint {
			a.cloudRunning = false
			a.mu.Unlock()
			return
		}
		a.mu.Unlock()
		e := sendCloudHeartbeat(ctx, endpoint, id, paused, true)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.cloudRunning = false
		if e != nil {
			a.cloudStatus = e.Error()
		} else {
			a.cloudStatus = "云端看护已连接 · 超过25分钟无心跳提醒"
		}
	}()
}

// Explicitly disarm before disabling; normal application exit deliberately keeps
// the remote watchdog armed so crashes, power loss and exit can be detected.
func (a *App) setCloudWatch(enabled bool) error {
	a.cloudIO.Lock()
	defer a.cloudIO.Unlock()
	a.mu.Lock()
	endpoint, id, paused := a.pushURL, a.cloudID, a.paused
	a.mu.Unlock()
	if !enabled && endpoint != "" {
		if e := sendCloudHeartbeat(context.Background(), endpoint, id, paused, false); e != nil {
			return e
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.config.CloudWatchDisabled
	a.config.CloudWatchDisabled = !enabled
	if e := a.saveLocked(); e != nil {
		a.config.CloudWatchDisabled = old
		return e
	}
	a.cloudNext = time.Time{}
	if enabled {
		a.cloudStatus = "等待连接云端看护"
	} else {
		a.cloudStatus = "云端看护已暂停"
	}
	return nil
}
