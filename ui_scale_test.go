package main

import (
	"path/filepath"
	"testing"
)

func TestUIScalePersistenceAndRollback(t *testing.T) {
	dir := t.TempDir()
	a, err := newApp(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if normalizedUIScale(a.config.UIScalePercent) != 100 {
		t.Fatal("old/default settings must use system scaling")
	}
	if err = a.setUIScale(125); err != nil {
		t.Fatal(err)
	}
	if err = a.setUIScale(300); err == nil || a.config.UIScalePercent != 125 {
		t.Fatal("invalid zoom changed settings")
	}
	// The existing settings file cannot be used as a parent directory.
	a.dir = filepath.Join(dir, "settings.json", "blocked")
	if err = a.setUIScale(150); err == nil || a.config.UIScalePercent != 125 {
		t.Fatal("failed save did not roll back")
	}
	a.db.Close()
	b, err := newApp(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.db.Close()
	if b.config.UIScalePercent != 125 {
		t.Fatal("zoom not restored on reopen")
	}
}

func TestUIScaleSystemCompositionAndBounds(t *testing.T) {
	if contentDPI(144, 125) != 180 || contentDPI(192, 75) != 144 || contentDPI(144, 100) != 144 {
		t.Fatal("zoom must multiply system scaling")
	}
	for _, invalid := range []int{-1, 0, 49, 74, 99, 201, 10000} {
		if normalizedUIScale(invalid) != 100 {
			t.Fatal("bad stored scale must fall back safely")
		}
	}
	if adjacentUIScale(50, -1) != 50 || adjacentUIScale(50, 1) != 60 || adjacentUIScale(200, 1) != 200 || adjacentUIScale(100, 1) != 110 || adjacentUIScale(100, -1) != 90 {
		t.Fatal("incorrect zoom stepping")
	}
	for _, dpi := range []int32{96, 120, 144, 192, 288} {
		for _, zoom := range uiScaleChoices {
			content := contentDPI(dpi, zoom)
			v := dashboardViewport(unscaleUI(1280, content), unscaleUI(680, content), false, 0, 0)
			g := dashboardGeometry(v.CanvasW, v.CanvasH, false)
			button := g.Controls[38]
			if button.W < 170 || button.Y+button.H > g.Cards[0].Y {
				t.Fatal("zoom menu overlaps cards")
			}
			// Reach the zoom menu on the smallest viewport as well as the table.
			for _, b := range []uiBox{button, g.Table, g.Status} {
				x := revealAxis(0, v.W, b.X, 1, v.CanvasW)
				y := revealAxis(0, v.H, b.Y, 1, v.CanvasH)
				if b.X < x || b.X >= x+v.W || b.Y < y || b.Y >= y+v.H {
					t.Fatal("unreachable control", dpi, zoom, b)
				}
			}
		}
	}
}
