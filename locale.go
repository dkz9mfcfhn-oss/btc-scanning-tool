package main

import (
	"errors"
	"strings"
	"sync/atomic"
)

type uiLanguage uint32

const (
	chinese uiLanguage = iota
	english
	russian
)

var languageChoices = [...]struct{ code, label string }{{"zh-CN", "中文"}, {"en", "English"}, {"ru", "Русский"}}
var activeUILanguage atomic.Uint32

func configuredLanguage(code string) uiLanguage {
	for i, choice := range languageChoices {
		if choice.code == code {
			return uiLanguage(i)
		}
	}
	return chinese
}

func currentLanguage() uiLanguage { return uiLanguage(activeUILanguage.Load()) }
func tr(s string) string          { return currentLanguage().text(s) }

func (l uiLanguage) text(s string) string {
	if l == english || l == russian {
		if labels, ok := translations[s]; ok {
			return labels[l-1]
		}
	}
	return s
}

// Localize app-generated diagnostic messages and leave unfamiliar firmware
// output intact. Known prefixes and suffixes can include an original error.
func (l uiLanguage) diagnostic(s string) string {
	if l == chinese || s == "" {
		return s
	}
	if value := l.text(s); value != s {
		return value
	}
	var lead, end string
	for _, prefix := range []string{"待确认：", "日志提示", "连接失败：", "读取失败：", "无效 IP：", "诊断失败：", "保存失败：", "无效 IPv4 CIDR：", "无效 IP 范围：", "无效 IP 末段：", "范围起始 IP 大于结束 IP：", "扫描范围包含非私有 IPv4 地址：", "阿瓦隆后台日志读取失败", "矿机报告错误码 ", "未检测到算力板插槽 "} {
		if tail, ok := strings.CutPrefix(s, prefix); ok {
			lead, s = l.text(prefix), tail
			break
		}
	}
	for _, suffix := range []string{"（设备报告）", "（待确认）", "（有效期未确认）"} {
		if head, ok := strings.CutSuffix(s, suffix); ok {
			s, end = head, l.text(suffix)
			break
		}
	}
	return lead + l.text(s) + end
}

func (a *App) setLanguage(lang uiLanguage) error {
	if lang >= uiLanguage(len(languageChoices)) {
		return errors.New("请选择有效的界面语言")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.config.Language
	a.config.Language = languageChoices[lang].code
	if err := a.saveLocked(); err != nil {
		a.config.Language = old
		return err
	}
	return nil
}
