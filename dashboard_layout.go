package main

import "fmt"

// One geometry model drives native controls, painting and design previews.
// Coordinates are logical 96-DPI units, so a short window never pushes charts below status.
type uiBox struct{ X, Y, W, H int32 }
type dashboardLayout struct {
	Cards                                        [5]uiBox
	ScanPanel, Table, Chart, Status              uiBox
	Progress, MonitorState, ListTitle, ListCount uiBox
	BrandTitle                                   uiBox
	Controls                                     map[int]uiBox
	Height                                       int32
}

const phoneCommand = 36
const priorityCommand = 37
const profitSettingsCommand = 39
const languageCommand = 40
const sourceCommand = 47
const dashboardMinWidth int32 = 640
const dashboardBrandFontSize = 25
const dashboardCardGap int32 = 8
const dashboardCardHeight int32 = 112
const profitCardIndex = 4
const profitDetailSize = 11
const compactProfitButtonWidth = 34
const dashboardNumberSize = 36

type cardWidths [5]int32

var dashboardCardMinimums = cardWidths{102, 102, 102, 102, 142}
var defaultCardWidths = cardWidths{18, 15, 20, 15, 32}

func dashboardViewport(w, h int32, full bool, x, y int32) displayViewport {
	geometry := dashboardGeometry(w, h, full)
	return fitViewport(w, h, dashboardMinWidth, geometry.Height, x, y)
}

func dashboardGeometry(w, h int32, full bool) dashboardLayout {
	return dashboardGeometryWithWidths(w, h, full, cardWidths{})
}

func dashboardGeometryWithWidths(w, h int32, full bool, widths cardWidths) dashboardLayout {
	w = max(w, dashboardMinWidth)
	var g dashboardLayout
	cardY := int32(82)
	if w < 1100 {
		g = compactDashboard(w)
		cardY = 130
	} else {
		g = wideDashboard(w)
	}
	inset := g.Table.X
	g.Cards = dashboardCards(inset, cardY, w-2*inset, widths)
	g.Controls[sourceCommand] = uiBox{203, 50, 116, 24}
	profit := g.Cards[profitCardIndex]
	buttonWidth := int32(94)
	if profit.W < 250 {
		buttonWidth = compactProfitButtonWidth
	}
	g.Controls[profitSettingsCommand] = uiBox{profit.X + profit.W - buttonWidth - 10, profit.Y + profit.H - 31, buttonWidth, 23}
	// Keep enough room for the list header, rows, and chart labels even in a
	// very short window. Only the overflow goes into the vertical scroll range.
	const minTable, minChart, gap = 96, 124, 14
	minContent := int32(minTable)
	if !full {
		minContent += minChart + gap
	}
	g.Height = max(h, g.Table.Y+minContent+40)
	g.Status = uiBox{inset, g.Height - 28, w - 2*inset, 22}
	available := g.Status.Y - 12 - g.Table.Y
	if full {
		g.Table.H = available
		return g
	}
	chartH := min(int32(222), max(int32(minChart), (available-gap)*2/5))
	g.Table.H = available - chartH - gap
	g.Chart = uiBox{inset, g.Table.Y + g.Table.H + gap, w - 2*inset, chartH}
	return g
}

func wideDashboard(w int32) dashboardLayout {
	const inset = 16
	g := dashboardLayout{
		ScanPanel: uiBox{8, 206, w - 16, 66}, Progress: uiBox{150, 284, w - 940, 32},
		MonitorState: uiBox{w - 264, 22, 240, 34}, ListTitle: uiBox{24, 284, 112, 32},
		ListCount: uiBox{inset, 325, w - 460, 22}, Table: uiBox{inset, 370, w - 2*inset, 0},
		BrandTitle: uiBox{74, 17, w - 760, 32},
	}
	g.Controls = map[int]uiBox{
		38:              {w - 208, 57, 184, 24},
		languageCommand: {w - 487, 57, 116, 24},
		priorityCommand: {w - 358, 57, 138, 24}, phoneCommand: {w - 674, 23, 112, 32}, 34: {w - 550, 23, 112, 32}, 35: {w - 426, 23, 140, 32},
		11: {28, 233, max(120, w-735), 24}, 12: {w - 669, 233, 73, 24}, 13: {w - 574, 233, 73, 24},
		21: {w - 480, 227, 112, 34}, 22: {w - 358, 227, 94, 34}, 23: {w - 254, 227, 100, 34}, 24: {w - 144, 227, 116, 34},
		26: {w - 354, 325, 322, 22}, 27: {w - 768, 284, 148, 32}, 28: {w - 610, 284, 108, 32}, 33: {w - 492, 284, 108, 32}, 29: {w - 374, 284, 108, 32}, 32: {w - 256, 284, 108, 32}, 25: {w - 138, 284, 118, 32},
	}
	return g
}

func compactDashboard(w int32) dashboardLayout {
	const inset = 12
	g := dashboardLayout{
		MonitorState: uiBox{w - 172, 14, 152, 32},
		BrandTitle:   uiBox{74, 17, w - 258, 32},
		Controls:     map[int]uiBox{38: {w - 192, 50, 172, 27}, languageCommand: {w - 312, 50, 108, 27}},
	}
	placeDashboardRow(g.Controls, inset, 86, w-2*inset, 32, priorityCommand, phoneCommand, 34, 35)
	scanY := int32(130 + dashboardCardHeight + 12)
	g.ScanPanel = uiBox{6, scanY, w - 12, 101}
	g.Controls[11] = uiBox{28, scanY + 26, w - 236, 24}
	g.Controls[12] = uiBox{w - 186, scanY + 26, 71, 24}
	g.Controls[13] = uiBox{w - 98, scanY + 26, 70, 24}
	placeDashboardRow(g.Controls, inset, scanY+58, w-2*inset, 34, 21, 22, 23, 24)
	listY := scanY + 112
	g.ListTitle = uiBox{inset, listY, 112, 32}
	g.Progress = uiBox{150, listY, w - 170, 32}
	g.Controls[27] = uiBox{inset, listY + 40, 148, 32}
	g.Controls[26] = uiBox{226, listY + 45, w - 254, 22}
	placeDashboardRow(g.Controls, inset, listY+79, w-2*inset, 32, 28, 33, 29, 32, 25)
	g.ListCount = uiBox{inset, listY + 115, w - 2*inset, 20}
	g.Table = uiBox{inset, listY + 143, w - 2*inset, 0}
	return g
}

func dashboardCards(x, y, width int32, widths cardWidths) [5]uiBox {
	extra := width - int32(len(widths)-1)*dashboardCardGap
	for _, minimum := range dashboardCardMinimums {
		extra -= minimum
	}
	if !widths.valid() || widths == (cardWidths{}) {
		widths = defaultCardWidths
	}
	var total int64
	for _, weight := range widths {
		total += int64(weight)
	}
	var cards [5]uiBox
	var weight int64
	var assigned int32
	for i, minimum := range dashboardCardMinimums {
		weight += int64(widths[i])
		share := int32(int64(extra) * weight / total)
		cardWidth := minimum + share - assigned
		cards[i] = uiBox{x, y, cardWidth, dashboardCardHeight}
		x += cardWidth + dashboardCardGap
		assigned = share
	}
	return cards
}

func (widths cardWidths) valid() bool {
	for _, width := range widths {
		if width < 0 {
			return false
		}
	}
	return true
}

func (a *App) setDashboardWidths(widths cardWidths) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.config.DashboardWidths
	a.config.DashboardWidths = nil
	if widths != (cardWidths{}) {
		a.config.DashboardWidths = &widths
	}
	if err := a.saveLocked(); err != nil {
		a.config.DashboardWidths = old
		return err
	}
	return nil
}

func cardDivider(cards [5]uiBox, i int) uiBox {
	card := cards[i]
	return uiBox{card.X + card.W, card.Y, dashboardCardGap, card.H}
}

func dragCardDivider(cards [5]uiBox, i int, delta int32) (cardWidths, bool) {
	var widths cardWidths
	for j, r := range cards {
		widths[j] = r.W - dashboardCardMinimums[j]
	}
	delta = max(-widths[i], min(delta, widths[i+1]))
	widths[i] += delta
	widths[i+1] -= delta
	return widths, delta != 0
}

func placeDashboardRow(controls map[int]uiBox, x, y, width, height int32, ids ...int) {
	const gap = 8
	width -= gap * int32(len(ids)-1)
	for i, id := range ids {
		left, right := width*int32(i)/int32(len(ids)), width*int32(i+1)/int32(len(ids))
		controls[id] = uiBox{x + left + gap*int32(i), y, right - left, height}
	}
}
func dashboardRate(value float64, reported int) (string, string) {
	if reported == 0 {
		return "—", "TH/s"
	}
	if value >= 1000 {
		return fmt.Sprintf("%.3f", value/1000), "PH/s"
	}
	return fmt.Sprintf("%.2f", value), "TH/s"
}
