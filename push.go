package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"minerwatch/internal/store"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type pushState struct {
	ZeroFailures int
	ZeroNotified bool
	Failures     int
	Notified     bool
	Seen         bool
}
type pushEvent struct {
	IP, Title, Body string
	Offline         bool
	Zero            bool
}

func pushTransition(s pushState, online bool) (pushState, string) {
	if online {
		recovery := s.Notified
		s.Seen = true
		s.Failures = 0
		if recovery {
			return s, "恢复在线"
		}
		return s, ""
	}
	if !s.Seen {
		return s, ""
	}
	s.Failures++
	if s.Failures >= 2 && !s.Notified {
		return s, "设备掉线"
	}
	return s, ""
}
func validatePushURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("请填写 HTTPS 推送地址，不含 title/body 和问号参数")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 || len(parts[len(parts)-1]) < 8 {
		return "", errors.New("推送地址需要包含 Bark 设备密钥")
	}
	return strings.TrimRight(raw, "/"), nil
}
func sendPush(ctx context.Context, endpoint, title, body string) error {
	if _, e := validatePushURL(endpoint); e != nil {
		return e
	}
	form := url.Values{"title": {title}, "body": {body}, "group": {"矿场视窗"}}
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(form.Encode()))
	if e != nil {
		return errors.New("无法建立推送请求")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("拒绝推送重定向") }}
	resp, e := client.Do(req)
	if e != nil {
		return errors.New("推送连接失败，请检查网络和服务器地址")
	}
	defer resp.Body.Close()
	var result struct {
		Code int `json:"code"`
	}
	e = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&result)
	if e != nil || resp.StatusCode != 200 || result.Code != 200 {
		return errors.New("推送服务器未确认成功，请检查设备密钥")
	}
	return nil
}
func (a *App) setPushURL(raw string) error {
	a.cloudIO.Lock()
	defer a.cloudIO.Unlock()
	if raw != "" {
		var e error
		raw, e = validatePushURL(raw)
		if e != nil {
			return e
		}
	}
	a.mu.Lock()
	oldEndpoint, cloudID, disabled := a.pushURL, a.cloudID, a.config.CloudWatchDisabled
	a.mu.Unlock()
	if oldEndpoint != "" && oldEndpoint != raw && !disabled && cloudID != "" {
		if _, _, err := cloudTarget(oldEndpoint); err == nil {
			if err = sendCloudHeartbeat(context.Background(), oldEndpoint, cloudID, false, false); err != nil {
				return errors.New("未能停用旧地址的云端看护，请恢复网络后重试")
			}
		}
	}
	encrypted, e := protectPush([]byte(raw))
	if e != nil {
		return errors.New("无法保护手机推送配置")
	}
	path := filepath.Join(a.dir, "phone-push.dat")
	tmp := path + ".tmp"
	if e = os.WriteFile(tmp, encrypted, 0600); e != nil {
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pushURL = raw
	a.pushStates = map[string]pushState{}
	a.pushError = ""
	return nil
}
func (a *App) loadPush() {
	data, e := os.ReadFile(filepath.Join(a.dir, "phone-push.dat"))
	if os.IsNotExist(e) {
		return
	}
	if e != nil {
		a.pushError = "无法读取手机提醒设置"
		return
	}
	raw, e := unprotectPush(data)
	if e != nil {
		a.pushError = "手机提醒配置无法解密，请重新设置"
		return
	}
	if len(raw) > 0 {
		if _, e = validatePushURL(string(raw)); e != nil {
			a.pushError = "手机提醒地址无效"
			return
		}
	}
	a.pushURL = string(raw)
}

// Called once per completed scan, outside the worker pool. Only known devices
// that have previously succeeded in this session are eligible for an outage.
func (a *App) notifyScan(ctx context.Context, ips []string) {
	a.mu.Lock()
	endpoint := a.pushURL
	if endpoint == "" || a.demo || ctx.Err() != nil {
		a.mu.Unlock()
		return
	}
	if a.pushStates == nil {
		a.pushStates = map[string]pushState{}
	}
	events := []pushEvent{}
	for _, ip := range ips {
		d := a.devices[ip]
		if d == nil || d.Hidden || d.Latest == nil {
			continue
		}
		s, title := pushSampleTransition(a.pushStates[ip], d.Latest)
		a.pushStates[ip] = s
		if title != "" {
			body := phoneAlertBody(d, time.Now())
			events = append(events, pushEvent{IP: ip, Title: title, Body: body, Offline: !d.Latest.Online, Zero: title == "无算力"})
		}
	}
	a.mu.Unlock()
	for _, event := range events {
		if ctx.Err() != nil {
			return
		}
		a.mu.Lock()
		d := a.devices[event.IP]
		send := a.pushURL == endpoint && d != nil && !d.Hidden
		a.mu.Unlock()
		if !send {
			continue
		}
		err := sendPush(ctx, endpoint, appDisplayName+" · "+event.Title, event.Body)
		a.mu.Lock()
		if d := a.devices[event.IP]; a.pushURL == endpoint && d != nil && !d.Hidden {
			if err != nil {
				a.pushError = err.Error()
			} else {
				a.pushError = ""
				s := a.pushStates[event.IP]
				if event.Title == "无算力" || event.Title == "算力恢复" {
					s.ZeroNotified = event.Zero
				} else {
					s.Notified = event.Offline
				}
				a.pushStates[event.IP] = s
			}
		}
		a.mu.Unlock()
	}
}

func pushSampleTransition(s pushState, sample *store.Sample) (pushState, string) {
	var title string
	s, title = pushTransition(s, sample.Online)
	if !sample.Online {
		s.ZeroFailures = 0
		return s, title
	}
	if sample.RealtimeTH == nil {
		s.ZeroFailures = 0
		return s, title
	}
	if *sample.RealtimeTH > 0 {
		s.ZeroFailures = 0
		if s.ZeroNotified && title == "" {
			title = "算力恢复"
		}
	} else if *sample.RealtimeTH == 0 {
		s.ZeroFailures++
		if s.ZeroFailures >= 2 && !s.ZeroNotified && title == "" {
			title = "无算力"
		}
	} else {
		s.ZeroFailures = 0
	}
	return s, title
}
func phoneAlertBody(d *Device, now time.Time) string {
	worker := "未读取到矿工名"
	if len(d.WorkerNames) > 0 {
		worker = strings.Join(d.WorkerNames, ", ")
		if d.Latest == nil || !d.Latest.Online || d.WorkerReadAt.Before(d.Latest.At) {
			worker += "（上次读取）"
		}
	}
	status, reason := "待更新", "原因未确认"
	restart := ""
	if d.Latest != nil {
		h := sampleHealthView(d.Latest, now)
		status = h.Status
		reason = h.Reason
		if h.State == "zero" {
			if codes := h.avalonRestartCodes(); codes != "" {
				restart = "\n上次重启记录：" + codes
			}
		}
		if reason == "" {
			if h.State == "normal" {
				reason = "当前算力正常"
			} else {
				reason = "原因未确认"
			}
		}
	}
	return fmt.Sprintf("矿工：%s\nIP：%s\n型号：%s\n状态：%s\n原因：%s%s\n时间：%s", healthShort(healthOneLine(worker), 240), d.IP, d.Model, status, healthShort(healthOneLine(reason), 240), restart, now.Format("01-02 15:04"))
}
