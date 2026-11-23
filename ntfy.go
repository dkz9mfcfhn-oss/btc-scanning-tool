package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var ntfyTopic = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var ntfyToken = regexp.MustCompile(`^tk_[A-Za-z0-9_-]{16,100}$`)

func parseNTFY(raw string) (string, string, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 || len(fields) > 2 || !ntfyTopic.MatchString(fields[0]) {
		return "", "", errors.New("请输入 ntfy.sh 的主题名，可在空格后填写访问令牌")
	}
	if len(fields) == 2 {
		if !ntfyToken.MatchString(fields[1]) {
			return "", "", errors.New("ntfy.sh 访问令牌格式无效")
		}
		return fields[0], fields[1], nil
	}
	if len(fields[0]) < 16 {
		return "", "", errors.New("免费主题请使用至少 16 位的随机主题名")
	}
	return fields[0], "", nil
}

func sendNTFY(ctx context.Context, settings, title, body string) error {
	return publishNTFY(ctx, &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, settings, title, body)
}

func publishNTFY(ctx context.Context, client *http.Client, settings, title, body string) error {
	topic, token, err := parseNTFY(settings)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"topic": topic, "title": title, "message": body})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://ntfy.sh/", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("无法连接 ntfy.sh，请检查网络")
	}
	defer resp.Body.Close()
	var result struct {
		ID    string `json:"id"`
		Topic string `json:"topic"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&result) != nil || result.ID == "" || result.Topic != topic {
		return errors.New("ntfy.sh 未确认发送，请检查主题、访问令牌或使用额度")
	}
	return nil
}

func (a *App) setNTFY(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		topic, token, err := parseNTFY(raw)
		if err != nil {
			return err
		}
		raw = strings.TrimSpace(topic + " " + token)
	}
	data, err := protectPush([]byte(raw))
	if err != nil {
		return errors.New("无法保护安卓推送配置")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	path := filepath.Join(a.dir, "android-push.dat")
	if err := os.WriteFile(path+".tmp", data, 0600); err != nil {
		return err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return err
	}
	if a.ntfySettings != raw {
		a.ntfySettings = raw
		a.ntfyPushStates = nil
		a.ntfyDailyError = ""
		a.dailyNextAttempt = time.Time{}
	}
	a.ntfyError = ""
	return nil
}

func (a *App) loadNTFY() {
	data, err := os.ReadFile(filepath.Join(a.dir, "android-push.dat"))
	if os.IsNotExist(err) {
		return
	}
	if err == nil {
		data, err = unprotectPush(data)
		if err == nil && len(data) > 0 {
			_, _, err = parseNTFY(string(data))
		}
	}
	if err != nil {
		a.ntfyError = "无法读取安卓推送设置，请重新配置"
		return
	}
	a.ntfySettings = string(data)
}
