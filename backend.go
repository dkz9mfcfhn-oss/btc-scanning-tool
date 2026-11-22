package main

import (
	"errors"
	"net"
	"strings"
)

const backendColumn = 16

// Only the stored IPv4 address enters the URL, never a label or shell command.
func backendURL(ip string) (string, error) {
	addr := net.ParseIP(ip)
	if addr == nil || addr.To4() == nil || strings.Contains(ip, ":") {
		return "", errors.New("矿机 IP 地址无效")
	}
	return "http://" + addr.To4().String() + "/", nil
}
