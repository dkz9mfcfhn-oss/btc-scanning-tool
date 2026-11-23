//go:build windows

package main

import (
	"fmt"
	"time"
	"unsafe"
)

func dashboardQuote(quote marketQuote, demo bool) marketQuote {
	now := time.Now()
	if demo {
		return marketQuote{At: now, BTCCNY: 400000, Difficulty: 135e12, Height: 976000}
	}
	if !quote.fresh(now) {
		return marketQuote{}
	}
	return quote
}

func paintEconomics(dc uintptr, g dashboardLayout, totals Totals, period earningsPeriod, quote marketQuote, tariff *float64, poolFee float64) {
	r := g.Cards[profitCardIndex]
	panelRect(dc, boxRect(r), 0)
	left, right := r.X+12, r.X+r.W-12
	line := func(top, bottom int32, text string, font, color uintptr) {
		labelBox(dc, rect{left, r.Y + top, right, r.Y + bottom}, text, font, color, 0)
	}
	p := estimateProfit(period.average, period.profit, quote, tariff, poolFee, period.asOf.Sub(period.start))
	line(13, 32, currentLanguage().profitCardCaption(r.W), ui.smallFont, muted)
	amount := money(p.Net)
	line(30, 68, amount, profitAmountFont(dc, amount, right-left), ink)

	power := tr("实时功率 —")
	if valid := totals.PowerReported + totals.PowerManual; valid > 0 {
		power = fmt.Sprintf(tr("实时功率 %.2f kW"), totals.PowerW/1000)
		if r.W >= 218 {
			power += fmt.Sprintf(tr(" · %d/%d 台"), valid, totals.Online)
		}
		if r.W >= 310 && totals.PowerManual > 0 {
			power += fmt.Sprintf(tr(" · 手动 %d 台"), totals.PowerManual)
		}
	}
	line(68, 81, power, ui.profitDetailFont, blue)
	right = g.Controls[profitSettingsCommand].X - 5
	fee, rejected := currentLanguage().profitCardDetails(p, right-left)
	line(81, 94, fee, ui.profitDetailFont, muted)
	line(94, 107, rejected, ui.profitDetailFont, muted)
}

func profitAmountFont(dc uintptr, amount string, width int32) uintptr {
	for _, font := range []uintptr{ui.numberFont, ui.compactNumberFont} {
		call(gdi, "SelectObject", dc, font)
		var extent point
		if call(gdi, "GetTextExtentPoint32W", dc, uintptr(unsafe.Pointer(wide(amount))), uintptr(lenUTF16(amount)), uintptr(unsafe.Pointer(&extent))) != 0 && extent.X <= width {
			return font
		}
	}
	return desktop.font
}
