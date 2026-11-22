package miner

import (
	"context"
	"errors"
	"strings"
	"time"
)

func is1566(r Reading) bool { return strings.Contains(strings.ToLower(r.Model), "1566") }

// FMS 3.3.2 app.so contains this exact read-only request and bracket-field
// patterns. This is an independent implementation, not decompiled FMS control flow.
func fmsRequest() map[string]any { return map[string]any{"command": "stats", "new_api": true} }

func parseAvalonFMS(data []byte) (Reading, error) {
	// Extract fields from the entire response, independent of the STATS/MM ID
	// wrapper. Both JSON string values and the legacy response retain brackets.
	fields := map[string]string{}
	health := Reading{}
	healthAvalonModule(&health, "FMS stats new_api", string(data))
	for _, match := range bracket.FindAllStringSubmatch(string(data), -1) {
		key, value := match[1], match[2]
		if old, exists := fields[key]; exists && old != value && (key == "MTavg" || key == "TMax" || key == "TAvg") {
			return health, errors.New("FMS响应包含多组冲突温度，未合并不同模块")
		}
		fields[key] = value
	}
	if fields["TMax"] == "" && fields["TAvg"] == "" && fields["MTavg"] == "" {
		return health, errors.New("FMS状态未返回TMax/TAvg/MTavg温度字段")
	}
	r := avalonTemperatures(map[string]any{avalonFlatKey: string(data)})
	r = merge(r, health)
	r.Brand = "Avalon"
	r.ElapsedSeconds = elapsedSeconds(numeric(fields["Elapsed"]))
	r.Protocol = "Avalon FMS stats new_api"
	// Overview is the reported maximum, then reported mean; never DEVS zero.
	r.TempC = arrayMax(fields["TMax"])
	if r.TempC == nil {
		r.TempC = arrayMax(fields["TAvg"])
	}
	if r.TempC == nil {
		r.TempC = arrayMax(fields["MTavg"])
	}
	if r.TempC == nil {
		return health, errors.New("FMS温度字段没有有效数值")
	}
	if !hasBoardMeans(r) {
		r.Warnings = append(r.Warnings, "FMS状态已返回整机温度，但未返回分板均温")
	}
	return r, nil
}

func applyAvalonFMS(current, fresh Reading, err error) Reading {
	// Discard the old 1566 temperature route even when the new request fails.
	current.TempC, current.InletC, current.OutletC = nil, nil, nil
	current.Boards, current.Temperatures = nil, nil
	if err != nil {
		current = merge(current, Reading{Faults: fresh.Faults, StatusKnown: fresh.StatusKnown})
		current.Warnings = append(current.Warnings, "1566HA FMS温度读取失败："+err.Error())
		return current
	}
	current = merge(current, fresh)
	current.TempC = fresh.TempC
	return current
}

func readAvalonFMS(ctx context.Context, address string, r Reading) Reading {
	one, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	data, err := tcpJSON(one, address, fmsRequest(), false)
	fresh := Reading{}
	if err == nil {
		fresh, err = parseAvalonFMS(data)
		if power, e := ParseAvalonLog(data); e == nil {
			r = merge(r, power)
		}
	}
	return applyAvalonFMS(r, fresh, err)
}
