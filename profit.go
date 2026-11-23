package main

import (
	"errors"
	"fmt"
	"math"
	"minerwatch/internal/store"
	"strconv"
	"strings"
	"time"
)

func validMeasurement(v *float64) bool { return v != nil && *v >= 0 && !math.IsInf(*v, 0) }

func validTariff(v float64) bool { return v >= 0 && !math.IsInf(v, 0) }

func validPoolFee(v float64) bool { return v >= 0 && v <= 100 }

func parseOptionalMeasurement(text string) (*float64, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, true
	}
	v, err := strconv.ParseFloat(text, 64)
	return &v, err == nil && validMeasurement(&v)
}

func (a *App) setElectricityTariff(text string) error {
	tariff, ok := parseOptionalMeasurement(text)
	if !ok {
		return errors.New("请输入非负电价（元/度），例如 0.42")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.config.ElectricityCNY
	a.config.ElectricityCNY = tariff
	if err := a.saveLocked(); err != nil {
		a.config.ElectricityCNY = old
		return err
	}
	return nil
}

func (a *App) setPoolFee(text string) error {
	text = strings.TrimSpace(text)
	feeText, ok := parseOptionalMeasurement(strings.TrimSuffix(text, "%"))
	fee := 0.0
	if feeText != nil {
		fee = *feeText
	}
	if !ok || (feeText == nil && text != "") || !validPoolFee(fee) {
		return errors.New("请输入 0–100 的手续费百分比，例如 2.5")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.config.PoolFeePercent
	a.config.PoolFeePercent = fee
	if err := a.saveLocked(); err != nil {
		a.config.PoolFeePercent = old
		return err
	}
	return nil
}

func (a *App) setManualPower(ip, text string) error {
	watts, ok := parseOptionalMeasurement(text)
	if !ok {
		return errors.New("请输入非负功率（W），例如 3200；留空可清除")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.devices[ip]
	if d == nil {
		return errors.New("矿机不存在")
	}
	old := d.ManualPowerW
	d.ManualPowerW = watts
	if err := a.saveLocked(); err != nil {
		d.ManualPowerW = old
		return err
	}
	return nil
}

type profitSamples struct {
	EnergyKWh, MatchedKWh, MatchedWorkEH float64
	PowerSeconds, MatchedSeconds         float64
	ManualKWh                            float64
	ManualDevices                        int
	RejectCoveredWorkEH                  float64 `json:"rejectCoveredWorkEH,omitempty"`
	RejectedWorkEH                       float64 `json:"rejectedWorkEH,omitempty"`
}

type earningsPeriod struct {
	start, asOf time.Time
	average     farmAverage
	profit      profitSamples
}

func earningsPeriodStart(now time.Time) time.Time {
	start, afterCutoff := dailyReportWindow(now)
	if !afterCutoff {
		start = start.Add(-farmAverageWindow)
	}
	return start
}

func (p *profitSamples) add(v profitSamples) {
	p.EnergyKWh += v.EnergyKWh
	p.MatchedKWh += v.MatchedKWh
	p.MatchedWorkEH += v.MatchedWorkEH
	p.PowerSeconds += v.PowerSeconds
	p.MatchedSeconds += v.MatchedSeconds
	p.ManualKWh += v.ManualKWh
	p.ManualDevices += v.ManualDevices
	p.RejectCoveredWorkEH += v.RejectCoveredWorkEH
	p.RejectedWorkEH += v.RejectedWorkEH
}

func sampleProfit(v store.HashrateAverage) profitSamples {
	p := profitSamples{
		EnergyKWh: v.EnergyKWh, MatchedKWh: v.MatchedKWh, MatchedWorkEH: v.MatchedWorkEH,
		PowerSeconds: v.PowerCovered.Seconds(), MatchedSeconds: v.Matched.Seconds(), ManualKWh: v.ManualKWh,
		RejectCoveredWorkEH: v.RejectCoveredWorkEH, RejectedWorkEH: v.RejectedWorkEH,
	}
	if v.ManualCovered > 0 {
		p.ManualDevices = 1
	}
	return p
}

type profitEstimate struct {
	Gross, PairedGross, PoolFee, Electricity, PairedElectricity, Net *float64
	RejectPercent, RejectionLoss                                     *float64
	PairedCoverage, RejectCoverage                                   float64
	Partial                                                          bool
}

func estimateProfit(farm farmAverage, samples profitSamples, quote marketQuote, tariff *float64, poolFeePercent float64, window time.Duration) profitEstimate {
	var p profitEstimate
	pricePerEH := quote.CNYPerEH()
	var payableRevenue float64
	if samples.RejectCoveredWorkEH > 0 && samples.MatchedWorkEH > 0 {
		p.RejectCoverage = min(1, samples.RejectCoveredWorkEH/samples.MatchedWorkEH)
		p.RejectPercent = num(100 * samples.RejectedWorkEH / samples.RejectCoveredWorkEH)
	}
	if farm.Valid > 0 && pricePerEH > 0 {
		p.Gross = num(farm.WorkEH * pricePerEH)
		if samples.MatchedSeconds > 0 {
			p.PairedGross = num(samples.MatchedWorkEH * pricePerEH)
			if p.RejectPercent != nil {
				p.RejectionLoss = num(samples.RejectedWorkEH * pricePerEH)
			}
			payableRevenue = max(0, samples.MatchedWorkEH-samples.RejectedWorkEH) * pricePerEH
			p.PoolFee = num(payableRevenue * (poolFeePercent / 100))
		}
	}
	if tariff != nil && samples.PowerSeconds > 0 {
		cost := samples.EnergyKWh * *tariff
		if !math.IsInf(cost, 0) {
			p.Electricity = &cost
			if samples.MatchedSeconds > 0 {
				p.PairedElectricity = num(samples.MatchedKWh * *tariff)
			}
		}
	}
	if farm.Devices > 0 && window > 0 {
		p.PairedCoverage = min(1, samples.MatchedSeconds/(window.Seconds()*float64(farm.Devices)))
	}
	if p.PairedGross != nil && p.PairedElectricity != nil {
		p.Net = num(payableRevenue - *p.PoolFee - *p.PairedElectricity)
		p.Partial = samples.MatchedSeconds+0.001 < max(samples.PowerSeconds, farm.DeviceHours*3600)
	}
	return p
}

func (p profitEstimate) rejectRateText() string {
	if p.RejectPercent == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f%%", *p.RejectPercent)
}

func (l uiLanguage) profitCardDetails(p profitEstimate, width int32) (fee, rejected string) {
	fee = fmt.Sprintf(l.text("矿池费 %s"), money(p.PoolFee))
	rejected = fmt.Sprintf(l.text("拒绝率 %s"), p.rejectRateText())
	if p.RejectPercent != nil && p.RejectCoverage < 0.999999 {
		rejected += l.text(" · 部分")
	}
	if width >= 216 {
		fee += fmt.Sprintf(l.text(" · 电费 %s"), money(p.PairedElectricity))
		rejected += fmt.Sprintf(l.text(" · 损失 %s"), money(p.RejectionLoss))
	}
	return fee, rejected
}

func money(v *float64) string {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return "—"
	}
	return fmt.Sprintf("¥%.2f", *v)
}

func (p profitEstimate) netCaption() string {
	if p.Partial {
		return "配对净收益"
	}
	return "预估净收益"
}

func (l uiLanguage) profitCardCaption(width int32) string {
	caption := l.text("累计配对净收益")
	if width >= 245 {
		caption += l.text(" · 08:00 起")
	}
	return caption
}

func (l uiLanguage) earningsPeriodDetails(period earningsPeriod, quote marketQuote, tariff *float64, poolFeePercent float64) string {
	const dateTime = "01-02 15:04"
	end := period.start.Add(farmAverageWindow)
	header := fmt.Sprintf(l.text("本期收益明细\n周期：%s — %s（北京时间）\n统计截至：%s\n\n"), period.start.Format(dateTime), end.Format(dateTime), period.asOf.In(reportZone).Format(dateTime))
	return header + l.profitReportText(period.average, period.profit, quote, tariff, poolFeePercent, period.asOf.Sub(period.start))
}

func (l uiLanguage) profitReportText(farm farmAverage, samples profitSamples, quote marketQuote, tariff *float64, poolFeePercent float64, window time.Duration) string {
	p := estimateProfit(farm, samples, quote, tariff, poolFeePercent, window)
	energy := "—"
	if samples.PowerSeconds > 0 {
		energy = fmt.Sprintf(l.text("%.2f 度"), samples.EnergyKWh)
	}
	text := fmt.Sprintf(l.text("%s：%s"), l.text(p.netCaption()), money(p.Net))
	if p.PairedGross == nil {
		text += "\n" + fmt.Sprintf(l.text("已采样预估毛收入：%s"), money(p.Gross))
	} else {
		text += "\n" + fmt.Sprintf(l.text("配对预估毛收入：%s"), money(p.PairedGross))
	}
	text += "\n" + fmt.Sprintf(l.text("拒绝率：%s · 收入扣减：%s"), p.rejectRateText(), money(p.RejectionLoss))
	text += "\n" + fmt.Sprintf(l.text("配对矿池手续费（%.4g%%）：%s\n配对电费：%s\n已采样电量：%s\n算力与功率配对覆盖：%.1f%%"), poolFeePercent, money(p.PoolFee), money(p.PairedElectricity), energy, p.PairedCoverage*100)
	if p.PairedElectricity == nil && p.Electricity != nil {
		text += "\n" + fmt.Sprintf(l.text("已采样电费：%s"), money(p.Electricity))
	}
	if samples.MatchedWorkEH > 0 && p.RejectCoverage < 0.999999 {
		text += "\n" + fmt.Sprintf(l.text("拒绝率覆盖：%.1f%%（仅对有数据的算力扣减）"), p.RejectCoverage*100)
	}
	if samples.ManualDevices > 0 {
		text += fmt.Sprintf(l.text("\n其中手动功率估算：%d 台 · %.2f 度"), samples.ManualDevices, samples.ManualKWh)
	}
	if tariff == nil {
		text += l.text("\n电价未设置")
	} else {
		text += fmt.Sprintf(l.text("\n电价 %.4g 元/度"), *tariff)
	}
	if quote.CNYPerEH() > 0 {
		text += fmt.Sprintf(" · BTC ¥%.0f（%s）", quote.BTCCNY, quote.At.In(reportZone).Format("01-02 15:04"))
	} else {
		text += l.text(" · 暂无有效行情")
	}
	return text + l.text("\n按区块补贴估算，未计链上交易费收入。")
}
