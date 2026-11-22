package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestThemePersistsAndInvalidChoiceDoesNotChangeConfig(t *testing.T) {
	dir := t.TempDir()
	a, e := newApp(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	defer a.db.Close()
	if normalizedTheme(a.config.Theme) != "glass" {
		t.Fatal("unspecified theme should default to glass")
	}
	for _, name := range themeChoices {
		if e = a.setTheme(name); e != nil {
			t.Fatal(e)
		}
		b, e := newApp(dir, false)
		if e != nil {
			t.Fatal(e)
		}
		if b.config.Theme != name {
			t.Fatal(b.config.Theme)
		}
		b.db.Close()
	}
	previous := a.config.Theme
	if e = a.setTheme("invalid"); e == nil || a.config.Theme != previous {
		t.Fatal("invalid palette changed config")
	}
	// A failed settings write must roll the preference back.
	if e = os.Mkdir(filepath.Join(dir, "settings.tmp"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = a.setTheme("light"); e == nil || a.config.Theme != previous {
		t.Fatal("failed save lost old theme")
	}
}
func TestPaletteReadability(t *testing.T) {
	lum := func(c uintptr) float64 {
		a := []float64{float64(c&255) / 255, float64((c>>8)&255) / 255, float64((c>>16)&255) / 255}
		for i, v := range a {
			if v <= 0.04045 {
				a[i] = v / 12.92
			} else {
				a[i] = math.Pow((v+0.055)/1.055, 2.4)
			}
		}
		return a[0]*0.2126 + a[1]*0.7152 + a[2]*0.0722
	}
	for _, name := range themeChoices {
		p := paletteFor(name)
		for _, pair := range [][2]uintptr{{p.ink, p.panel}, {p.ink, p.background}, {p.muted, p.panel}, {p.ink, p.selection}, {p.onAccent, p.accent}, {p.warning, p.panel}, {p.danger, p.panel}} {
			a, b := lum(pair[0]), lum(pair[1])
			if a < b {
				a, b = b, a
			}
			if (a+0.05)/(b+0.05) < 4.5 {
				t.Fatalf("theme %s has insufficient text contrast: %.2f", name, (a+0.05)/(b+0.05))
			}
		}
	}
}

func TestMinimalThemesHaveSolidBackdropsAndFlatCards(t *testing.T) {
	for _, name := range themeChoices {
		if !minimalTheme(name) {
			continue
		}
		p := paletteFor(name)
		key := glassTileKey{Width: 72, Height: 48, CanvasWidth: 144, CanvasHeight: 96, Theme: name}
		background := renderGlassTile(key)
		for i := 0; i < len(background); i += 4 {
			if got := uintptr(background[i+2]) | uintptr(background[i+1])<<8 | uintptr(background[i])<<16; got != p.background || background[i+3] != 255 {
				t.Fatalf("%s rendered a gradient or transparent background", name)
			}
		}
		key.Base, key.Surface, key.Shadow, key.Radius = p.panel, true, true, 46
		layer := renderGlassLayer(key, key.Width, key.Height)
		mid := (key.Height/2*key.Width + key.Width/2) * 4
		if got := uintptr(layer[mid+2]) | uintptr(layer[mid+1])<<8 | uintptr(layer[mid])<<16; got != p.panel || layer[mid+3] != 255 || layer[3] != 0 {
			t.Fatalf("%s card is not solid with a transparent outer edge", name)
		}
	}
}
