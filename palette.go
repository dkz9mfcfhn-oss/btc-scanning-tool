package main

import (
	"errors"
	"strings"
)

var themeChoices = [...]string{"glass", "dark", "light", "blue", "minimal-light", "minimal-dark", "minimal-sand"}
var themeLabels = [...]string{"液态玻璃", "玻璃 · 深色", "玻璃 · 浅色", "玻璃 · 深蓝", "极简 · 白色", "极简 · 石墨", "极简 · 暖砂"}

func minimalTheme(name string) bool { return strings.HasPrefix(name, "minimal-") }

func lightTheme(name string) bool {
	return name == "glass" || name == "light" || name == "minimal-light" || name == "minimal-sand"
}

func normalizedTheme(s string) string {
	for _, t := range themeChoices {
		if s == t {
			return s
		}
	}
	return "glass"
}
func (a *App) setTheme(s string) error {
	if normalizedTheme(s) != s {
		return errors.New("请选择有效的界面主题")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.config.Theme
	a.config.Theme = s
	if e := a.saveLocked(); e != nil {
		a.config.Theme = old
		return e
	}
	return nil
}

type palette struct{ background, panel, border, ink, muted, accent, blue, edit, button, stripe, selection, warning, danger, onAccent uintptr }

func paletteFor(name string) palette {
	c := func(r, g, b uintptr) uintptr { return r | g<<8 | b<<16 }
	switch normalizedTheme(name) {
	case "glass":
		return palette{c(233, 240, 251), c(247, 250, 255), c(198, 213, 234), c(28, 38, 58), c(66, 83, 109), c(0, 103, 94), c(36, 89, 183), c(232, 242, 253), c(235, 242, 253), c(241, 247, 254), c(210, 230, 252), c(139, 80, 7), c(179, 43, 64), c(255, 255, 255)}
	case "light":
		return palette{c(242, 246, 250), c(255, 255, 255), c(202, 213, 225), c(26, 42, 61), c(76, 97, 120), c(0, 107, 86), c(35, 84, 178), c(255, 255, 255), c(225, 234, 243), c(242, 246, 251), c(207, 232, 239), c(151, 85, 0), c(182, 36, 51), c(255, 255, 255)}
	case "blue":
		return palette{c(13, 27, 53), c(20, 39, 69), c(46, 70, 108), c(230, 240, 255), c(157, 180, 214), c(88, 210, 240), c(168, 184, 255), c(25, 47, 79), c(35, 61, 95), c(24, 45, 77), c(42, 78, 115), c(255, 198, 107), c(255, 153, 161), c(8, 31, 45)}
	case "minimal-light":
		return palette{c(244, 246, 248), c(255, 255, 255), c(218, 223, 230), c(22, 29, 40), c(73, 83, 97), c(0, 104, 91), c(29, 85, 159), c(255, 255, 255), c(247, 249, 252), c(249, 250, 252), c(220, 233, 247), c(134, 72, 10), c(164, 43, 57), c(255, 255, 255)}
	case "minimal-dark":
		return palette{c(23, 25, 29), c(32, 35, 40), c(58, 62, 70), c(236, 239, 243), c(170, 179, 191), c(106, 219, 194), c(149, 190, 246), c(29, 32, 37), c(41, 45, 51), c(36, 40, 46), c(49, 65, 82), c(251, 187, 111), c(255, 144, 152), c(19, 41, 37)}
	case "minimal-sand":
		return palette{c(242, 236, 225), c(255, 252, 245), c(220, 207, 188), c(49, 40, 32), c(103, 87, 70), c(51, 104, 77), c(81, 88, 147), c(255, 252, 245), c(246, 238, 225), c(250, 244, 233), c(234, 223, 208), c(141, 70, 4), c(167, 49, 59), c(255, 255, 255)}
	default:
		return palette{c(10, 17, 28), c(17, 28, 43), c(35, 52, 72), c(222, 234, 247), c(145, 165, 188), c(70, 220, 184), c(126, 168, 246), c(22, 36, 53), c(26, 43, 62), c(20, 32, 48), c(30, 65, 70), c(245, 177, 92), c(245, 137, 137), c(7, 34, 30)}
	}
}
