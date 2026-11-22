package main

import (
	"errors"
	"fmt"
	"math"
	"minerwatch/internal/store"
	"strconv"
	"strings"
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
}

func (p *profitSamples) add(v profitSamples) {
	p.EnergyKWh += v.EnergyKWh
	p.MatchedKWh += v.MatchedKWh
	p.MatchedWorkEH += v.MatchedWorkEH
	p.PowerSeconds += v.PowerSeconds
	p.MatchedSeconds += v.MatchedSeconds
	p.ManualKWh += v.ManualKWh
	p.ManualDevices += v.ManualDevices
}

func sampleProfit(v store.HashrateAverage) profitSamples {
	p := profitSamples{
		EnergyKWh: v.EnergyKWh, MatchedKWh: v.MatchedKWh, MatchedWorkEH: v.MatchedWorkEH,
		PowerSeconds: v.PowerCovered.Seconds(), MatchedSeconds: v.Matched.Seconds(), ManualKWh: v.ManualKWh,
	}
	if v.ManualCovered > 0 {
		p.ManualDevices = 1
	}
	return p
}

type profitEstimate struct {
	Gross, PoolFee, Electricity, Net *float64
	PairedCoverage                   float64
	Partial                          bool
}

func estimateProfit(farm farmAverage, samples profitSamples, quote marketQuote, tariff *float64, poolFeePercent float64) profitEstimate {
	var p profitEstimate
	pricePerEH := quote.CNYPerEH()
	fee := poolFeePercent / 100
	if farm.Valid > 0 && pricePerEH > 0 {
		p.Gross = num(farm.WorkEH * pricePerEH)
		p.PoolFee = num(*p.Gross * fee)
	}
	if tariff != nil && samples.PowerSeconds > 0 {
		cost := samples.EnergyKWh * *tariff
		if !math.IsInf(cost, 0) {
			p.Electricity = &cost
		}
	}
	if farm.Devices > 0 {
		p.PairedCoverage = min(1, samples.MatchedSeconds/(farmAverageWindow.Seconds()*float64(farm.Devices)))
	}
	if p.Gross != nil && p.Electricity != nil && samples.MatchedSeconds > 0 {
		pairedGross := samples.MatchedWorkEH * pricePerEH
		p.Net = num(pairedGross*(1-fee) - samples.MatchedKWh**tariff)
		p.Partial = samples.MatchedSeconds+0.001 < max(samples.PowerSeconds, farm.DeviceHours*3600)
	}
	return p
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

func profitReportText(farm farmAverage, samples profitSamples, quote marketQuote, tariff *float64, poolFeePercent float64) string {
	return chinese.profitReportText(farm, samples, quote, tariff, poolFeePercent)
}

func (l uiLanguage) profitReportText(farm farmAverage, samples profitSamples, quote marketQuote, tariff *float64, poolFeePercent float64) string {
	p := estimateProfit(farm, samples, quote, tariff, poolFeePercent)
	energy := "—"
	if samples.PowerSeconds > 0 {
		energy = fmt.Sprintf(l.text("%.2f 度"), samples.EnergyKWh)
	}
	text := fmt.Sprintf(l.text("预估毛收入：%s\n矿池手续费（%.4g%%）：%s\n已采样电量：%s\n电费：%s\n%s：%s\n算力与功率配对覆盖：%.1f%%"), money(p.Gross), poolFeePercent, money(p.PoolFee), energy, money(p.Electricity), l.text(p.netCaption()), money(p.Net), p.PairedCoverage*100)
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
