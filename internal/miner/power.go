package miner

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const avalonWallSource = "Avalon WALLPOWER（输入 W）"

var stockPowerLine = regexp.MustCompile(`(?im)^\s*miner power\s*:\s*([0-9]+)\s*$`)

func parseStockPower(data []byte) *float64 {
	var envelope struct {
		Response string `json:"response"`
	}
	if json.Unmarshal(data, &envelope) == nil && envelope.Response != "" {
		data = []byte(envelope.Response)
	}
	match := stockPowerLine.FindSubmatch(data)
	if match == nil {
		return nil
	}
	n, err := strconv.ParseUint(string(match[1]), 10, 32)
	if err != nil {
		return nil
	}
	return ptr(float64(n))
}

func stockPower(ctx context.Context, base string, creds Credentials) Reading {
	one, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	data, err := httpGetWithChallenge(one, base, "/miner_power", creds, nil, "*/*")
	if err != nil {
		return Reading{}
	}
	if watts := parseStockPower(data); watts != nil {
		return Reading{PowerW: watts, PowerSource: "蚂蚁 6060/miner_power（固件报告 W）"}
	}
	return Reading{}
}

var whatsPowerInput = regexp.MustCompile(`(?is)<input\b[^>]*\bid\s*=\s*["']cbid\.table\.1\.power["'][^>]*>`)
var whatsPowerValue = regexp.MustCompile(`(?is)\bvalue\s*=\s*["']([^"']*)["']`)
var whatsPowerNumber = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?|[0-9]{1,3}(?:,[0-9]{3})+(?:\.[0-9]+)?)\s*W?$`)

func parseWhatsPowerPage(data []byte) *float64 {
	input := whatsPowerInput.Find(data)
	if match := whatsPowerValue.FindSubmatch(input); match != nil {
		if v := whatsPowerNumber.FindStringSubmatch(strings.TrimSpace(string(match[1]))); v != nil {
			return nonnegative(numeric(strings.ReplaceAll(v[1], ",", "")))
		}
	}
	return nil
}

func whatsPowerPage(ctx context.Context, base string, creds Credentials) Reading {
	one, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	client := newBTCHTTP(base, nil)
	defer client.client.CloseIdleConnections()
	loggedIn := false
	for _, path := range []string{"/cgi-bin/luci/admin/status/cgminerstatus", "/cgi-bin/luci/admin/status/btminerstatus"} {
		reply, err := client.request(one, path, "GET", "")
		if err != nil {
			continue
		}
		if !loggedIn && creds.Username != "" && strings.Contains(string(reply.body), "luci_username") {
			loggedIn = true
			form := url.Values{"luci_username": {creds.Username}, "luci_password": {creds.Password}}
			if _, err = client.request(one, "/cgi-bin/luci/", "POST", form.Encode()); err != nil {
				break
			}
			reply, err = client.request(one, path, "GET", "")
		}
		if err == nil && reply.code == 200 {
			if watts := parseWhatsPowerPage(reply.body); watts != nil {
				return Reading{PowerW: watts, PowerSource: "神马后台状态页（W）"}
			}
		}
	}
	return Reading{}
}

func parseBraiinsPower(data []byte) Reading {
	if m, err := decode(data); err == nil {
		for _, row := range section(m, "TUNERSTATUS") {
			if p := nonnegative(number(row, "ApproximateMinerPowerConsumption")); p != nil {
				return Reading{PowerW: p, PowerSource: "Braiins tunerstatus（固件估算 W）"}
			}
		}
	}
	return Reading{}
}
