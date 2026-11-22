package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"minerwatch/internal/store"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var reportZone = time.FixedZone("北京时间", 8*60*60)

type dailyReportRow struct {
	IP, Text     string
	Mean         *float64       `json:"mean,omitempty"`
	CoveredHours float64        `json:"coveredHours,omitempty"`
	Profit       *profitSamples `json:"profit,omitempty"`
}
type dailyReportState struct {
	WindowVersion  int                `json:"windowVersion,omitempty"`
	SummaryOnly    bool               `json:"summaryOnly,omitempty"`
	Day            string             `json:"day"`
	EndpointID     string             `json:"endpointID"`
	End            time.Time          `json:"end"`
	Parts          [][]dailyReportRow `json:"parts"`
	Next           int                `json:"next"`
	Market         marketQuote        `json:"market,omitempty"`
	Tariff         *float64           `json:"electricityCnyPerKwh,omitempty"`
	PoolFeePercent float64            `json:"poolFeePercent,omitempty"`
}

const dailyWindowVersion = 3

func dailyReportWindow(now time.Time) (time.Time, bool) {
	local := now.In(reportZone)
	end := time.Date(local.Year(), local.Month(), local.Day(), 8, 0, 0, 0, reportZone)
	return end, !now.Before(end)
}

func buildDailyReport(ds []*Device, averages map[string]store.HashrateAverage, end time.Time, endpoint string) dailyReportState {
	sort.Slice(ds, func(i, j int) bool { return ipLess(ds[i].IP, ds[j].IP) })
	state := dailyReportState{WindowVersion: dailyWindowVersion, Day: end.In(reportZone).Format("2006-01-02"), EndpointID: fmt.Sprintf("%x", sha256.Sum256([]byte(endpoint))), End: end, SummaryOnly: true}
	var part []dailyReportRow
	for _, d := range ds {
		if d.Hidden {
			continue
		}
		sample := averages[d.IP]
		row := dailyReportRow{IP: d.IP, CoveredHours: sample.Covered.Hours()}
		profit := sampleProfit(sample)
		if profit.PowerSeconds > 0 {
			row.Profit = &profit
		}
		if sample.Covered > 0 {
			row.Mean = num(sample.MeanTH)
		}
		part = append(part, row)
	}
	if len(part) > 0 {
		state.Parts = append(state.Parts, part)
	}
	return state
}

func dailyReportBody(state dailyReportState, index int, hidden map[string]bool) string {
	if state.SummaryOnly {
		var summary farmAverage
		var profit profitSamples
		for _, r := range state.Parts[index] {
			if hidden[r.IP] {
				continue
			}
			if r.Mean != nil {
				summary.add(*r.Mean, r.CoveredHours)
			} else {
				summary.add(0, 0)
			}
			if r.Profit != nil {
				profit.add(*r.Profit)
			}
		}
		if summary.Devices == 0 {
			return ""
		}
		rate := "暂无有效样本"
		if summary.Valid > 0 {
			rate = fmt.Sprintf("%.3f PH/s（%.2f TH/s）", summary.MeanTH/1000, summary.MeanTH)
		}
		body := fmt.Sprintf("%s — %s（北京时间）\n\n24小时平均算力合计：%s\n已采集累计运算量：%.3f EH\n有效算力：%d / %d 台\n采样覆盖：%.1f%%\n\n按各机有效时长计算均值后合计，实测零计入。", state.End.Add(-farmAverageWindow).In(reportZone).Format("01-02 15:04"), state.End.In(reportZone).Format("01-02 15:04"), rate, summary.WorkEH, summary.Valid, summary.Devices, summary.coverage()*100)
		if state.WindowVersion >= 2 {
			body += "\n\n" + profitReportText(summary, profit, state.Market, state.Tariff, state.PoolFeePercent)
		}
		return body
	}

	var rows []string
	for _, row := range state.Parts[index] {
		if !hidden[row.IP] {
			rows = append(rows, row.Text)
		}
	}
	if len(rows) == 0 {
		return ""
	}
	return fmt.Sprintf("%s — %s（北京时间）\n24h实时算力时长加权均值；缺测不计零。\n\n%s\n\n%d/%d 页", state.End.Add(-24*time.Hour).In(reportZone).Format("01-02 15:04"), state.End.In(reportZone).Format("01-02 15:04"), strings.Join(rows, "\n\n"), index+1, len(state.Parts))
}

func (a *App) loadDailyReport() {
	data, err := os.ReadFile(filepath.Join(a.dir, "daily-report.json"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil || len(data) > 4<<20 {
		a.dailyError = "日报发送记录无法读取"
		return
	}
	var state dailyReportState
	if json.Unmarshal(data, &state) != nil || state.Next < 0 || state.Next > len(state.Parts) {
		a.dailyError = "日报发送记录无效"
		return
	}
	a.dailyState = state
}
func (a *App) saveDailyReportLocked() error {
	data, err := json.Marshal(a.dailyState)
	if err != nil {
		return err
	}
	path := filepath.Join(a.dir, "daily-report.json")
	if err = os.WriteFile(path+".tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func (a *App) maybeDailyReport(ctx context.Context, now time.Time) {
	end, due := dailyReportWindow(now)
	if !due || ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	endpoint := a.pushURL
	endpointID := fmt.Sprintf("%x", sha256.Sum256([]byte(endpoint)))
	done := a.dailyState.Day == end.Format("2006-01-02") && a.dailyState.EndpointID == endpointID && a.dailyState.Next >= len(a.dailyState.Parts)
	if a.demo || endpoint == "" || a.dailyRunning || done || now.Before(a.dailyNextAttempt) {
		a.mu.Unlock()
		return
	}
	visible := false
	for _, d := range a.devices {
		if !d.Hidden {
			visible = true
			break
		}
	}
	if !visible {
		a.mu.Unlock()
		return
	}
	a.dailyRunning = true
	a.dailyNextAttempt = now.Add(5 * time.Minute)
	a.jobs.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.jobs.Done()
		defer func() { a.mu.Lock(); a.dailyRunning = false; a.mu.Unlock() }()
		err := a.sendDailyReport(ctx, end, endpoint)
		a.mu.Lock()
		if err != nil {
			a.dailyError = "算力日报：" + err.Error()
		} else {
			a.dailyError = ""
		}
		a.mu.Unlock()
	}()
}

func (a *App) sendDailyReport(ctx context.Context, end time.Time, endpoint string) error {
	endpointID := fmt.Sprintf("%x", sha256.Sum256([]byte(endpoint)))
	a.mu.Lock()
	current := a.dailyState
	prepare := current.Day != end.Format("2006-01-02") || current.EndpointID != endpointID ||
		(current.WindowVersion < dailyWindowVersion && current.Next == 0 && len(current.Parts) > 0)
	tariff, poolFee, quote := a.config.ElectricityCNY, a.config.PoolFeePercent, a.market
	var ds []*Device
	if prepare {
		for _, d := range a.devices {
			if !d.Hidden {
				ds = append(ds, &Device{IP: d.IP})
			}
		}
	}
	a.mu.Unlock()
	if prepare {
		ips := make([]string, 0, len(ds))
		for _, d := range ds {
			ips = append(ips, d.IP)
		}
		averages, err := a.db.RetainedHashrateAverages(ctx, ips, end.Add(-farmAverageWindow), end)
		if err != nil {
			return err
		}
		state := buildDailyReport(ds, averages, end, endpoint)
		if !quote.fresh(time.Now()) {
			quote = marketQuote{}
			if tariff != nil {
				quote = a.refreshMarket(ctx)
			}
		}
		state.Market, state.Tariff, state.PoolFeePercent = quote, tariff, poolFee
		a.mu.Lock()
		old := a.dailyState
		a.dailyState = state
		err = a.saveDailyReportLocked()
		if err != nil {
			a.dailyState = old
		}
		a.mu.Unlock()
		if err != nil {
			return fmt.Errorf("无法保存发送进度")
		}
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.mu.Lock()
		state := a.dailyState
		if a.pushURL != endpoint || state.EndpointID != endpointID {
			a.mu.Unlock()
			return nil
		}
		if state.Next >= len(state.Parts) {
			a.mu.Unlock()
			return nil
		}
		hidden := map[string]bool{}
		for _, row := range state.Parts[state.Next] {
			d := a.devices[row.IP]
			hidden[row.IP] = d == nil || d.Hidden
		}
		body := dailyReportBody(state, state.Next, hidden)
		sender := a.dailySender
		if sender == nil {
			sender = sendPush
		}
		a.mu.Unlock()
		if body != "" {
			if err := sender(ctx, endpoint, appDisplayName+" · 24小时算力与收益", body); err != nil {
				return err
			}
		}
		a.mu.Lock()
		a.dailyState.Next++
		err := a.saveDailyReportLocked()
		a.mu.Unlock()
		if err != nil {
			return fmt.Errorf("发送成功，但进度保存失败")
		}
	}
}
