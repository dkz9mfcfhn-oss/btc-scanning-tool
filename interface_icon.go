package main

import (
	"math"
)

type interfaceIcon uint8

const minInterfaceIconPixels = 7

const (
	iconNone interfaceIcon = iota
	iconScan
	iconPulse
	iconPause
	iconPlay
	iconDownload
	iconExpand
	iconDetails
	iconDiagnostic
	iconSliders
	iconRestart
	iconBell
	iconWarning
	iconZoom
	iconThermometer
	iconHistory
	iconTimeline
	iconFolder
	iconClose
	iconCheck
	iconSend
	iconCloud
	iconExternal
	iconChevron
	iconTrend
	iconServers
	iconPercent
)

type iconPoint struct{ x, y float64 }

// Each path is drawn in the same 16-unit grid with rounded stroke ends.
var interfaceIconPaths = [...][][]iconPoint{
	iconScan: {
		{{2, 6}, {2, 2}, {6, 2}}, {{10, 2}, {14, 2}, {14, 6}},
		{{14, 10}, {14, 14}, {10, 14}}, {{6, 14}, {2, 14}, {2, 10}},
		{{1.8, 8}, {14.2, 8}},
	},
	iconPulse: {{
		{1.8, 8}, {4.5, 8}, {6.6, 3}, {9.1, 13}, {11.5, 8}, {14.2, 8},
	}},
	iconPause: {{{5.5, 3.2}, {5.5, 12.8}}, {{10.5, 3.2}, {10.5, 12.8}}},
	iconPlay:  {{{5, 3}, {12, 8}, {5, 13}, {5, 3}}},
	iconDownload: {
		{{8, 2}, {8, 10}}, {{5.2, 7.2}, {8, 10}, {10.8, 7.2}},
		{{2, 10.5}, {2, 13.2}, {3, 14}, {13, 14}, {14, 13.2}, {14, 10.5}},
	},
	iconExpand: {
		{{2.4, 6}, {2.4, 2.4}, {6, 2.4}}, {{10, 2.4}, {13.6, 2.4}, {13.6, 6}},
		{{13.6, 10}, {13.6, 13.6}, {10, 13.6}}, {{6, 13.6}, {2.4, 13.6}, {2.4, 10}},
	},
	iconDetails: {
		{{3, 2.1}, {12.1, 2.1}, {13, 3}, {13, 13}, {12.1, 13.9}, {3, 13.9}, {2.1, 13}, {2.1, 3}, {3, 2.1}},
		{{5, 5.7}, {5, 5.7}}, {{8, 5.7}, {10.5, 5.7}}, {{5, 9.5}, {5, 9.5}}, {{8, 9.5}, {10.5, 9.5}},
	},
	iconDiagnostic: {
		{{6, 3.1}, {3.2, 3.1}, {3.2, 14}, {12.8, 14}, {12.8, 3.1}, {10, 3.1}},
		{{6, 2}, {10, 2}, {10, 4.8}, {6, 4.8}, {6, 2}}, {{5.8, 9.1}, {7.4, 10.7}, {10.5, 7.6}},
	},
	iconSliders: {
		{{2, 4}, {6, 4}}, {{9, 4}, {14, 4}}, {{7.5, 2.3}, {7.5, 5.7}},
		{{2, 8}, {9, 8}}, {{12, 8}, {14, 8}}, {{10.5, 6.3}, {10.5, 9.7}},
		{{2, 12}, {4, 12}}, {{7, 12}, {14, 12}}, {{5.5, 10.3}, {5.5, 13.7}},
	},
	iconRestart: {
		{{13.2, 8.4}, {12.6, 10.9}, {10.7, 12.9}, {8, 13.7}, {5.3, 13}, {3.3, 11}, {2.5, 8}, {3.3, 5.3}, {5.3, 3.2}, {8, 2.4}, {10.7, 3.2}, {13, 5.5}},
		{{10.1, 5.5}, {13, 5.5}, {13, 2.6}},
	},
	iconBell: {
		{{2.1, 11.2}, {3.8, 9.4}, {4.3, 5.6}, {5.2, 3.7}, {6.4, 2.7}, {8, 2.4}, {9.6, 2.7}, {10.8, 3.7}, {11.7, 5.6}, {12.2, 9.4}, {13.9, 11.2}, {2.1, 11.2}},
		{{6.5, 13.4}, {7.2, 14}, {8.8, 14}, {9.5, 13.4}},
	},
	iconWarning: {
		{{8, 2.1}, {14, 13}, {2, 13}, {8, 2.1}}, {{8, 6.2}, {8, 8.9}}, {{8, 10.9}, {8, 10.9}},
	},
	iconZoom: {
		{{11.8, 7}, {11.2, 9.4}, {9.4, 11.2}, {7, 11.8}, {4.6, 11.2}, {2.8, 9.4}, {2.2, 7}, {2.8, 4.6}, {4.6, 2.8}, {7, 2.2}, {9.4, 2.8}, {11.2, 4.6}, {11.8, 7}},
		{{10.5, 10.6}, {14.1, 14.1}}, {{7, 4.9}, {7, 9.1}}, {{4.9, 7}, {9.1, 7}},
	},
	iconThermometer: {
		{{6.2, 8.7}, {6.2, 3.3}, {7, 2}, {9, 2}, {9.8, 3.3}, {9.8, 8.7}, {11.2, 10.2}, {11.2, 12.2}, {9.8, 13.8}, {8, 14.2}, {6.2, 13.8}, {4.8, 12.2}, {4.8, 10.2}, {6.2, 8.7}},
		{{8, 6.2}, {8, 11.6}},
	},
	iconHistory: {
		{{14, 8}, {13.2, 11}, {11, 13.2}, {8, 14}, {5, 13.2}, {2.8, 11}, {2, 8}, {2.8, 5}, {5, 2.8}, {8, 2}, {11, 2.8}, {13.2, 5}, {14, 8}},
		{{8, 4.7}, {8, 8}, {10.5, 9.5}},
	},
	iconTimeline: {
		{{4, 2.5}, {4, 2.5}}, {{4, 5.2}, {4, 6.7}}, {{4, 8}, {4, 8}}, {{4, 9.5}, {4, 11}}, {{4, 13.5}, {4, 13.5}},
		{{7.5, 2.5}, {12.5, 2.5}}, {{7.5, 8}, {11.4, 8}}, {{7.5, 13.5}, {12.5, 13.5}},
	},
	iconFolder: {{{2, 13}, {2, 4}, {3, 3}, {6.5, 3}, {8.2, 5}, {13, 5}, {14, 6}, {14, 13}, {2, 13}}},
	iconClose:  {{{4.3, 4.3}, {11.7, 11.7}}, {{11.7, 4.3}, {4.3, 11.7}}},
	iconCheck:  {{{2.9, 8.4}, {6.3, 11.8}, {13.1, 4.8}}},
	iconSend:   {{{2.1, 6.7}, {13.9, 2.1}, {9.3, 13.9}, {7, 9}, {2.1, 6.7}}, {{7, 9}, {10.6, 5.4}}},
	iconCloud:  {{{4.5, 12.6}, {2.7, 11.9}, {1.8, 10}, {2.5, 8.2}, {4.1, 7.4}, {4.4, 5.6}, {5.7, 4}, {7.8, 3.4}, {10, 4}, {11.4, 5.8}, {11.7, 7.5}, {13.5, 8.2}, {14.2, 10}, {13.4, 11.9}, {11.6, 12.6}, {4.5, 12.6}}},
	iconExternal: {
		{{6, 3.8}, {2.5, 3.8}, {2.5, 13.5}, {12.2, 13.5}, {12.2, 10}},
		{{8.5, 2.5}, {13.5, 2.5}, {13.5, 7.5}}, {{6.6, 9.4}, {13.5, 2.5}},
	},
	iconChevron: {{{3.4, 6}, {8, 10.4}, {12.6, 6}}},
	iconTrend: {
		{{2.2, 12.5}, {5.9, 8.9}, {8.8, 10.4}, {13.8, 4.1}}, {{10.2, 4.1}, {13.8, 4.1}, {13.8, 7.7}},
	},
	iconServers: {
		{{3, 2.5}, {13, 2.5}, {14, 3.5}, {14, 6}, {13, 7}, {3, 7}, {2, 6}, {2, 3.5}, {3, 2.5}},
		{{3, 9}, {13, 9}, {14, 10}, {14, 12.5}, {13, 13.5}, {3, 13.5}, {2, 12.5}, {2, 10}, {3, 9}},
		{{5.2, 4.8}, {5.2, 4.8}}, {{5.2, 11.2}, {5.2, 11.2}},
	},
	iconPercent: {
		{{3.2, 12.8}, {12.8, 3.2}},
		{{6.1, 4.5}, {5.6, 5.6}, {4.5, 6.1}, {3.4, 5.6}, {2.9, 4.5}, {3.4, 3.4}, {4.5, 2.9}, {5.6, 3.4}, {6.1, 4.5}},
		{{13.1, 11.5}, {12.6, 12.6}, {11.5, 13.1}, {10.4, 12.6}, {9.9, 11.5}, {10.4, 10.4}, {11.5, 9.9}, {12.6, 10.4}, {13.1, 11.5}},
	},
}

func commandIcon(id uint32, title string) interfaceIcon {
	switch id {
	case 21:
		return iconScan
	case 22:
		return iconPulse
	case 23:
		if title == tr("恢复监控") {
			return iconPlay
		}
		return iconPause
	case 24:
		return iconDownload
	case 28:
		if title == tr("显示趋势") {
			return iconTrend
		}
		return iconExpand
	case 29:
		return iconDetails
	case 32:
		return iconDiagnostic
	case 33:
		return iconSliders
	case 34:
		return iconRestart
	case phoneCommand:
		return iconBell
	case priorityCommand:
		return iconWarning
	case 38:
		return iconZoom
	case profitSettingsCommand:
		return iconSliders
	case 41:
		return iconThermometer
	case 43:
		if title == tr("查看温度详情") {
			return iconThermometer
		}
		return iconHistory
	case 46:
		return iconTimeline
	case 62:
		return iconFolder
	case 63, 84, 93:
		return iconClose
	case 82, 92:
		return iconCheck
	case 83:
		return iconSend
	case 85:
		return iconCloud
	default:
		return iconNone
	}
}

var metricIcons = [...]interfaceIcon{iconTrend, iconServers, iconHistory, iconPercent}

// Center the icon and measured caption as one group. Keep full-width text
// available on narrow controls and on systems with wider fallback fonts.
func iconTextPlacement(bounds uiBox, textWidth, iconSize int32) (icon, text uiBox) {
	const gap = 5
	text = bounds
	groupWidth := iconSize + gap + textWidth
	if iconSize <= 0 || textWidth <= 0 || groupWidth > bounds.W || iconSize > bounds.H-3 {
		return icon, text
	}
	icon = uiBox{bounds.X + (bounds.W-groupWidth)/2, bounds.Y + (bounds.H-iconSize)/2, iconSize, iconSize}
	text.X = icon.X + iconSize + gap
	text.W -= text.X - bounds.X
	return icon, text
}

// GDI AlphaBlend takes premultiplied BGRA. Rasterize each segment over its
// tight bounds and use maximum coverage so stroke joints stay even.
func rasterInterfaceIcon(kind interfaceIcon, size int, color uintptr, out []byte) {
	clear(out)
	if kind == iconNone || int(kind) >= len(interfaceIconPaths) || size <= 0 || len(out)/4/size < size {
		return
	}
	scale := float64(size) / 16
	radius := max(0.62, scale*0.84)
	red, green, blue := int(color&255), int(color>>8&255), int(color>>16&255)
	for _, path := range interfaceIconPaths[kind] {
		for i := 1; i < len(path); i++ {
			a, b := path[i-1], path[i]
			x0, y0, x1, y1 := a.x*scale, a.y*scale, b.x*scale, b.y*scale
			dx, dy := x1-x0, y1-y0
			lengthSquared := dx*dx + dy*dy
			reach := radius + 0.5
			left := max(0, int(math.Floor(min(x0, x1)-reach)))
			top := max(0, int(math.Floor(min(y0, y1)-reach)))
			right := min(size, int(math.Ceil(max(x0, x1)+reach)))
			bottom := min(size, int(math.Ceil(max(y0, y1)+reach)))
			for y := top; y < bottom; y++ {
				for x := left; x < right; x++ {
					px, py := float64(x)+0.5-x0, float64(y)+0.5-y0
					if lengthSquared > 0 {
						position := min(1, max(0, (px*dx+py*dy)/lengthSquared))
						px, py = px-position*dx, py-position*dy
					}
					coverage := min(1, max(0, reach-math.Sqrt(px*px+py*py)))
					alpha := int(coverage*255 + 0.5)
					offset := (y*size + x) * 4
					if alpha > int(out[offset+3]) {
						out[offset], out[offset+1], out[offset+2], out[offset+3] = byte((blue*alpha+127)/255), byte((green*alpha+127)/255), byte((red*alpha+127)/255), byte(alpha)
					}
				}
			}
		}
	}
}
