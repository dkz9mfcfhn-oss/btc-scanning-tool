package miner

import (
	"context"
	"time"
)

// Detailed Avalon temperature data gets its own bounded budget under the
// overall probe deadline. Never share the expired identification context.
func readAvalonDetail(ctx context.Context, address string, r Reading) Reading {
	detail, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	data, e := tcpJSON(detail, address, map[string]string{"command": "estats", "cmd": "estats"}, false)
	if e != nil {
		if r.Brand == "Avalon" {
			r.Warnings = append(r.Warnings, "阿瓦隆详细温度接口读取失败，分板温度可能不完整；请采集诊断")
		}
		return r
	}
	parsed, e := ParseCGMiner(data)
	if e != nil {
		return r
	}
	// Keep the selected hashrate snapshot but supplement every board's metrics.
	r = merge(r, parsed)
	if r.Brand == "Avalon" && !hasBoardMeans(r) {
		r.Warnings = append(r.Warnings, "已读阿瓦隆详细状态，但未取得完整分板均温；需核对本机MTavg/PVT_T日志字段")
	}
	return r
}
