package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"image"
	"image/png"
	"math"
	"os"
	"strings"
	"testing"
)

// Optional design artifact, not a claim of running Win32 on this host.
// Uses the same geometry, palette, columns and pixel material renderer as the EXE.
func TestExportDashboardDesignPreview(t *testing.T) {
	path := os.Getenv("MINEWATCH_DASHBOARD_PREVIEW")
	if path == "" {
		t.Skip("optional design preview")
	}
	const w, h = 1500, 900
	theme := normalizedTheme(os.Getenv("MINEWATCH_PREVIEW_THEME"))
	lang := configuredLanguage(os.Getenv("MINEWATCH_PREVIEW_LANGUAGE"))
	p := paletteFor(theme)
	g := dashboardGeometry(w, h, false)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	paste := func(k glassTileKey, clipWidth int) {
		var data []byte
		if k.Surface {
			data = renderGlassLayer(k, k.Width, k.Height)
		} else {
			data = renderGlassTile(k)
		}
		for y := 0; y < k.Height; y++ {
			for x := 0; x < min(clipWidth, k.Width); x++ {
				dx, dy := k.X+x, k.Y+y
				if dx < 0 || dy < 0 || dx >= w || dy >= h {
					continue
				}
				i, j := (y*k.Width+x)*4, dy*img.Stride+dx*4
				inv := 255 - int(data[i+3])
				for c := 0; c < 3; c++ {
					img.Pix[j+c] = byte(min(255, int(data[i+2-c])+(int(img.Pix[j+c])*inv+127)/255))
				}
				img.Pix[j+3] = 255
			}
		}
	}
	paste(glassTileKey{Width: w, Height: h, CanvasWidth: w, CanvasHeight: h, Theme: theme}, w)
	surface := func(r uiBox, base uintptr, radius int, shadow bool, percent int) {
		pad := 0
		if shadow {
			pad = 8
		}
		width := int(r.W) + 2*pad
		paste(glassTileKey{Width: width, Height: int(r.H) + 2*pad, X: int(r.X) - pad, Y: int(r.Y) - pad, CanvasWidth: w, CanvasHeight: h, Radius: radius, Theme: theme, Base: base, Surface: true, Shadow: shadow}, width*percent/100)
	}
	for _, r := range g.Cards {
		surface(r, p.panel, 46, true, 100)
	}
	surface(g.ScanPanel, p.panel, 46, true, 100)
	surface(uiBox{g.Table.X - 8, g.Table.Y - 6, g.Table.W + 16, g.Table.H + 12}, p.panel, 46, true, 100)
	surface(g.MonitorState, p.panel, 34, false, 100)
	surface(g.Chart, p.panel, 46, false, 100)
	for id, r := range g.Controls {
		switch id {
		case 11, 12, 13, 26:
			surface(uiBox{r.X - 8, r.Y - 5, r.W + 16, r.H + 10}, p.edit, 25, false, 100)
		case 21, priorityCommand:
			surface(uiBox{r.X + 1, r.Y + 1, r.W - 2, r.H - 2}, p.accent, int(r.H-2), false, 100)
		default:
			surface(uiBox{r.X + 1, r.Y + 1, r.W - 2, r.H - 2}, p.button, int(r.H-2), false, 100)
		}
	}
	surface(g.Progress, p.edit, 30, false, 100)
	surface(g.Progress, p.accent, 30, false, 45)
	var pngData bytes.Buffer
	png.Encode(&pngData, img)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="1500" height="936" viewBox="0 0 1500 936"><image width="1500" height="900" href="data:image/png;base64,%s"/><style>text{font-family:'Microsoft YaHei UI','PingFang SC',sans-serif}</style>`, base64.StdEncoding.EncodeToString(pngData.Bytes()))
	color := func(c uintptr) string { return fmt.Sprintf("#%02x%02x%02x", c&255, c>>8&255, c>>16&255) }
	text := func(x, y int32, s string, size int, c uintptr) {
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="%d" fill="%s">%s</text>`, x, y, size, color(c), html.EscapeString(lang.text(s)))
	}
	box := func(r uiBox, rad int, fill, stroke uintptr) {
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="%s" stroke="%s"/>`, r.X, r.Y, r.W, r.H, rad, color(fill), color(stroke))
	}
	icon, e := os.ReadFile("assets/minewatch.png")
	if e != nil {
		t.Fatal(e)
	}
	fmt.Fprintf(&b, `<image x="24" y="22" width="38" height="38" href="data:image/png;base64,%s"/>`, base64.StdEncoding.EncodeToString(icon))
	text(g.BrandTitle.X, g.BrandTitle.Y+dashboardBrandFontSize, appDisplayName, dashboardBrandFontSize, p.ink)
	text(75, 64, "矿场运行总览", 13, p.muted)
	text(1250, 44, "●  "+lang.text("自动监控中"), 13, p.accent)
	rejectionPoints := make([]FleetPoint, 71)
	for i := range rejectionPoints {
		const shares = 100000
		rejected := math.Round(180 + 60*math.Sin(float64(i)*.31+2))
		rejectionPoints[i].RejectPercent = sharePercent(shares-rejected, rejected)
	}
	latestReject := fmt.Sprintf("%.2f", *rejectionPoints[len(rejectionPoints)-1].RejectPercent)
	titles := []string{"全场实时算力", "在线矿机", "24小时平均算力合计", "全场拒绝率"}
	values := []string{"8.462", "18 / 20", "8.317", latestReject}
	units := []string{"PH/s", "台", "PH/s", "%"}
	notes := []string{fmt.Sprintf(lang.text("%d / %d 台提供实时读数"), 18, 20), fmt.Sprintf(lang.text("需关注 %d 台 · 隐藏不计入"), 2), fmt.Sprintf(lang.text("%d / %d 台 · 覆盖 %.1f%%"), 20, 20, 96.3), fmt.Sprintf(lang.text("%d / %d 台提供有效份额"), 18, 20)}
	for i, r := range g.Cards[:profitCardIndex] {
		text(r.X+18, r.Y+27, titles[i], 13, p.muted)
		text(r.X+18, r.Y+67, values[i], 36, p.ink)
		text(r.X+r.W-52, r.Y+63, units[i], 13, p.muted)
		text(r.X+18, r.Y+91, notes[i], 13, p.muted)
	}
	for i := range len(g.Cards) - 1 {
		r := cardDivider(g.Cards, i)
		c := p.border
		box(uiBox{r.X + r.W/2 - 1, r.Y + r.H/2 - 12, 3, 24}, 1, c, c)
	}
	labels := map[int]string{38: fmt.Sprintf(lang.text("界面缩放 %d%% ▾"), 100), priorityCommand: "异常优先：开", phoneCommand: "手机提醒", 34: "选中重启", 11: "192.168.8.1-254", 12: "admin", 13: "••••••", 21: "保存并扫描", 22: "立即采样", 23: "暂停监控", 24: "导出24h CSV", 26: "搜索 IP、矿工名、备注", 27: lang.text("全部矿机") + " ▾", 28: "展开列表", 33: "列表设置 ▾", 29: "矿机详情", 32: "采集诊断", 25: lang.text("5 分钟") + " ▾", languageCommand: languageChoices[lang].label + " ▾"}
	for i, key := range themeChoices {
		if key == theme {
			labels[35] = lang.text(themeLabels[i]) + " ▾"
		}
	}
	labels[profitSettingsCommand] = "收益设置 ▾"
	labels[sourceCommand] = "软件来源"
	for id, r := range g.Controls {
		fg := p.ink
		if id == 11 || id == 12 || id == 13 || id == 26 {
			box(uiBox{r.X - 3, r.Y - 2, r.W + 6, r.H + 4}, 5, p.edit, p.edit)
		}
		if id == 21 || id == priorityCommand {
			fg = p.onAccent
		}
		size := 14
		if lang != chinese {
			size = 13
		}
		if id == profitSettingsCommand {
			size = profitDetailSize
		}
		text(r.X+8, r.Y+(r.H+int32(size))/2-1, labels[id], size, fg)
	}
	profit := g.Cards[profitCardIndex]
	text(profit.X+12, profit.Y+27, lang.profitCardCaption(profit.W), 13, p.muted)
	text(profit.X+12, profit.Y+65, "¥682.65", dashboardNumberSize, p.ink)
	text(profit.X+12, profit.Y+79, fmt.Sprintf(lang.text("实时功率 %.2f kW"), 128.90)+fmt.Sprintf(lang.text(" · %d/%d 台"), 18, 18)+fmt.Sprintf(lang.text(" · 手动 %d 台"), 1), profitDetailSize, p.blue)
	fee, rejected := lang.profitCardDetails(profitEstimate{PoolFee: num(50.82), PairedElectricity: num(1299.31), RejectPercent: num(0.42), RejectionLoss: num(8.58), RejectCoverage: 1}, g.Controls[profitSettingsCommand].X-profit.X-17)
	text(profit.X+12, profit.Y+92, fee, profitDetailSize, p.muted)
	text(profit.X+12, profit.Y+105, rejected, profitDetailSize, p.muted)
	for i, caption := range []string{"扫描范围 / IP 网段", "登录账号", "后台密码"} {
		field := g.Controls[11+i]
		text(field.X-4, field.Y-11, caption, 13, p.muted)
	}
	text(g.ListTitle.X, g.ListTitle.Y+22, "矿机列表", 16, p.ink)
	text(g.ListCount.X, g.ListCount.Y+17, lang.text("演示数据 · ")+fmt.Sprintf(lang.text("显示 %d / %d 台  ·  双击查看详情  ·  右键备注与时间线"), 20, 20), 13, p.muted)
	text(g.Controls[26].X-48, g.Controls[26].Y+16, "搜索", 13, p.muted)
	text(g.Progress.X+12, g.Progress.Y+21, lang.progressBarLabel(Job{Running: true, Done: 114, Total: 254}, g.Progress.W), 13, p.onAccent)
	fmt.Fprintf(&b, `<defs><clipPath id="table"><rect x="%d" y="%d" width="%d" height="%d" rx="10"/></clipPath></defs><g clip-path="url(#table)">`, g.Table.X, g.Table.Y, g.Table.W, g.Table.H)
	box(uiBox{g.Table.X, g.Table.Y, g.Table.W, 28}, 0, p.stripe, p.border)
	order, widths := nativeColumnOrder(true), nativeColumnWidths(true)
	x := g.Table.X
	for _, col := range order {
		width := widths[col]
		if width > 0 {
			text(x+10, g.Table.Y+19, nativeListColumns[col].Name, 13, p.muted)
			x += width
		}
	}
	for row := 0; row < 8; row++ {
		y := g.Table.Y + 28 + int32(row)*38
		c := p.panel
		if row%2 == 1 {
			c = p.stripe
		}
		box(uiBox{g.Table.X, y, g.Table.W, 38}, 0, c, c)
		x = g.Table.X
		vals := map[int]string{16: "打开后台", 0: fmt.Sprintf("192.168.8.%d", 100+row), 17: fmt.Sprintf("farm.%03d", row+1), 2: "在线", 3: fmt.Sprintf("%.2f", 560+float64(row)*2.7), 4: fmt.Sprintf("%.2f", 555+float64(row)*2.6), 5: "65.0", 13: "板1 66 · 板2 68 · 板3 63 · 板4 64", 15: "—", 8: "0.42"}
		vals[powerColumn] = "9912"
		if row == 1 {
			vals[powerColumn] += lang.text(" 手动")
		}
		if row == 0 {
			vals[powerColumn] = "—"
			vals[2] = "读取失败"
			vals[3] = "—"
			vals[4] = "—"
			vals[5] = "—"
			vals[13] = "—"
			vals[15] = "连接超时，无法判断矿机是否停机"
		}
		for _, col := range order {
			width := widths[col]
			if width == 0 {
				continue
			}
			fg := p.ink
			if col == 16 {
				fg = p.blue
			}
			if col == 3 {
				fg = p.accent
			}
			if col == 2 {
				fg = p.accent
				if row == 0 {
					fg = p.danger
				}
				box(uiBox{x + 5, y + 6, width - 10, 26}, 8, p.stripe, p.border)
			}
			fmt.Fprintf(&b, `<svg x="%d" y="%d" width="%d" height="38"><text x="10" y="25" font-size="14" fill="%s">%s</text></svg>`, x, y, width, color(fg), html.EscapeString(lang.text(vals[int(col)])))
			x += width
		}
	}
	b.WriteString(`</g>`)
	text(g.Chart.X+20, g.Chart.Y+18, "最近 24 小时", 16, p.ink)
	text(1240, g.Chart.Y+18, "点击标题切换单机曲线 →", 13, p.blue)
	for i, title := range []string{lang.text("总算力 · ") + "TH/s", "在线机器 · 台", "拒绝率 · % / 累计份额"} {
		width := (g.Chart.W - 40) / 3
		left := g.Chart.X + 20 + int32(i)*width
		right := left + width - 18
		top := g.Chart.Y + 82
		bottom := g.Chart.Y + g.Chart.H - 26
		text(left+10, g.Chart.Y+48, title, 13, p.muted)
		chartColor := []uintptr{p.accent, p.blue, p.warning}[i]
		text(left+10, g.Chart.Y+70, []string{"8462.00", "18", latestReject}[i], 16, chartColor)
		text(right-64, g.Chart.Y+70, "08:05", 13, p.muted)
		low, high := 0.0, 1.0
		if i == 2 {
			low, high = rejectRateAxis(rejectionPoints)
		}
		y := func(value float64) int32 {
			return bottom - int32((value-low)/(high-low)*float64(bottom-top))
		}
		ticks := []float64{1, .5, 0}
		if i == 2 {
			ticks = rejectRateTicks(low, high, bottom-top)
		}
		for _, v := range ticks {
			yy := y(v)
			if i == 2 {
				text(left+2, yy+4, fmt.Sprintf("%.2f", v), 12, p.muted)
			}
			fmt.Fprintf(&b, `<path d="M%d %dH%d" stroke="%s"/>`, left+45, yy, right, color(p.border))
		}
		var points strings.Builder
		for j := 0; j <= 70; j++ {
			xx := left + 45 + int32(j)*(right-left-48)/70
			value := .52 + .12*math.Sin(float64(j)*.31+float64(i))
			if i == 2 {
				value = *rejectionPoints[j].RejectPercent
			}
			fmt.Fprintf(&points, "%d,%d ", xx, y(value))
		}
		fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="2"/>`, points.String(), color(chartColor))
		text(left+45, bottom+20, "昨天 08:05", 13, p.muted)
		text(right-32, bottom+20, "现在", 13, p.muted)
	}
	text(24, g.Status.Y+16, "CPU / 内存显示区域     ·     自动监控     ·     异常优先", 13, p.muted)
	box(uiBox{0, 900, 1500, 36}, 0, p.panel, p.panel)
	text(24, 924, "界面排版预览 · 演示数据 · 使用程序同一套布局与玻璃材质绘制，并非 Windows 实机截图", 14, p.muted)
	b.WriteString(`</svg>`)
	if e := os.WriteFile(path, []byte(b.String()), 0644); e != nil {
		t.Fatal(e)
	}
}
