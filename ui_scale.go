package main

import "fmt"

var uiScaleChoices = []int{50, 60, 75, 90, 100, 110, 125, 150, 175, 200}

func normalizedUIScale(percent int) int {
	for _, choice := range uiScaleChoices {
		if percent == choice {
			return percent
		}
	}
	return 100
}

// User zoom multiplies the monitor's Windows scaling. 100% preserves system DPI.
func contentDPI(systemDPI int32, percent int) int32 {
	if systemDPI <= 0 {
		systemDPI = 96
	}
	return int32((int64(systemDPI)*int64(normalizedUIScale(percent)) + 50) / 100)
}

func adjacentUIScale(percent, direction int) int {
	percent = normalizedUIScale(percent)
	for i, choice := range uiScaleChoices {
		if percent == choice {
			return uiScaleChoices[min(max(0, i+direction), len(uiScaleChoices)-1)]
		}
	}
	return 100
}

func (a *App) setUIScale(percent int) error {
	if normalizedUIScale(percent) != percent {
		return fmt.Errorf("界面缩放比例无效")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.config.UIScalePercent
	a.config.UIScalePercent = percent
	if err := a.saveLocked(); err != nil {
		a.config.UIScalePercent = old
		return err
	}
	return nil
}
