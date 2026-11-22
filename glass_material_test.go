package main

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"os"
	"testing"
)

func TestGlassLayerLeavesRoundedCornerPixelsUntouched(t *testing.T) {
	for _, theme := range themeChoices {
		for _, size := range [][2]int{{132, 46}, {66, 23}, {198, 69}} {
			for _, shadow := range []bool{false, true} {
				key := glassTileKey{Width: 132, Height: 46, X: 260, Y: 82, CanvasWidth: 1100, CanvasHeight: 680, Theme: theme, Radius: 42, Base: paletteFor(theme).panel, Surface: true, Shadow: shadow}
				w, h := size[0], size[1]
				pixels := renderGlassLayer(key, w, h)
				composite := renderGlassRaster(key, w, h)
				key.Surface = false
				background := renderGlassRaster(key, w, h)
				for _, corner := range []int{0, w - 1, (h - 1) * w, w*h - 1} {
					i := corner * 4
					if !bytes.Equal(pixels[i:i+4], []byte{0, 0, 0, 0}) || !bytes.Equal(composite[i:i+4], background[i:i+4]) {
						t.Fatalf("%s %dx%d shadow=%t: corner %d changes parent", theme, w, h, shadow, corner)
					}
				}
				center := pixels[(h/2*w+w/2)*4+3]
				if minimalTheme(theme) && center != 255 {
					t.Fatalf("%s flat surface center alpha: %d", theme, center)
				}
				if !minimalTheme(theme) && (center < 175 || center >= 255) {
					t.Fatalf("%s center alpha out of translucent range: %d", theme, center)
				}
				partial := false
				for i := 0; i < len(pixels); i += 4 {
					alpha := pixels[i+3]
					if alpha != 0 && alpha < center {
						partial = true
					}
					if max(pixels[i], pixels[i+1], pixels[i+2]) > alpha {
						t.Fatalf("%s %dx%d: source pixel is not premultiplied", theme, w, h)
					}
				}
				if !partial {
					t.Fatalf("%s %dx%d: no smooth transition at the bevel", theme, w, h)
				}
			}
		}
	}
}

func TestGlassSurfaceKeepsTextContrast(t *testing.T) {
	luma := func(v materialColor) float64 {
		for i, n := range v {
			n /= 255
			if n <= 0.04045 {
				v[i] = n / 12.92
			} else {
				v[i] = math.Pow((n+0.055)/1.055, 2.4)
			}
		}
		return 0.2126*v[0] + 0.7152*v[1] + 0.0722*v[2]
	}
	for _, theme := range themeChoices {
		p := paletteFor(theme)
		for _, pair := range [][2]uintptr{{p.panel, p.muted}, {p.button, p.ink}, {p.accent, p.onAccent}} {
			key := glassTileKey{Width: 140, Height: 32, X: 80, Y: 30, CanvasWidth: 1100, CanvasHeight: 680, Theme: theme, Radius: 30, Base: pair[0], Surface: true}
			pixels := renderGlassTile(key)
			i := (16*key.Width + 70) * 4
			a := luma(materialColor{float64(pixels[i+2]), float64(pixels[i+1]), float64(pixels[i])})
			b := luma(materialRGB(pair[1]))
			if ratio := (max(a, b) + 0.05) / (min(a, b) + 0.05); ratio < 4.5 {
				t.Fatalf("%s: text contrast against glass is %.2f", theme, ratio)
			}
		}
	}
}

func TestGlassRimRemainsVisibleAtHalfZoom(t *testing.T) {
	key := glassTileKey{Width: 160, Height: 42, X: 35, Y: 31, CanvasWidth: 1100, CanvasHeight: 680, Theme: "glass", Radius: 42, Base: paletteFor("glass").button, Surface: true}
	for _, scale := range []int{1, 2} {
		w, h := key.Width/scale, key.Height/scale
		pixels := renderGlassRaster(key, w, h)
		brightness := func(y int) int {
			i := (y*w + w/2) * 4
			return (int(pixels[i+2])*21 + int(pixels[i+1])*72 + int(pixels[i])*7) / 100
		}
		top := max(brightness(0), brightness(1))
		face := brightness(h / 2)
		bottom := min(brightness(h-1), brightness(h-2))
		if top-face < 16 || top-bottom < 24 {
			t.Fatalf("%d%% zoom lost contrast between the specular rim and lens body: top=%d face=%d bottom=%d", 100/scale, top, face, bottom)
		}
	}
}

func TestGlassRasterStaysBoundedAtSmallZoom(t *testing.T) {
	for _, surface := range []bool{false, true} {
		key := glassTileKey{Width: 7680, Height: 4140, CanvasWidth: 7680, CanvasHeight: 4140, Theme: "glass", Surface: surface}
		w, h := glassRasterSize(key)
		if w*h*4 > 6<<20 || w < 720 || h < 400 {
			t.Fatalf("unexpected raster budget at 4K and 50%% zoom: %dx%d", w, h)
		}
	}
}

func TestCachedGlassLightMatchesAnalyticScene(t *testing.T) {
	for _, theme := range themeChoices {
		scene := newMaterialScene(glassTileKey{CanvasWidth: 1100, CanvasHeight: 680, Theme: theme})
		for y := range 140 {
			for x := range 140 {
				gx, gy := float64(x)/100-0.2, float64(y)/100-0.2
				got, want := scene.at(gx*scene.width, gy*scene.height), scene.analyticAt(gx, gy)
				for c := range got {
					if delta := math.Abs(got[c] - want[c]); delta >= .25 {
						t.Fatalf("%s lighting changed by %.3f at %g, %g", theme, delta, gx, gy)
					}
				}
			}
		}
	}
}

func TestGlassLayerOverwritesDestinationPixelsOutsideShape(t *testing.T) {
	const w, h = 132, 46
	key := glassTileKey{Width: w, Height: h, X: 260, Y: 82, CanvasWidth: 1100, CanvasHeight: 680, Theme: "glass", Radius: 42, Base: paletteFor("glass").panel, Surface: true}
	destination := bytes.Repeat([]byte{255}, w*h*4)
	paintGlassLayerPixels(key, w, h, destination)
	if !bytes.Equal(destination, renderGlassLayer(key, w, h)) || !bytes.Equal(destination[:4], []byte{0, 0, 0, 0}) {
		t.Fatal("direct pixel painting inherited previous pixels")
	}
}

func BenchmarkGlassColdDashboard(b *testing.B) {
	const w, h = 1500, 900
	layout := dashboardGeometry(w, h, false)
	panels := []uiBox{layout.ScanPanel, layout.Table, layout.Chart}
	panels = append(panels, layout.Cards[:]...)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		key := glassTileKey{Width: w, Height: h, CanvasWidth: w, CanvasHeight: h, Theme: "glass"}
		width, height := glassRasterSize(key)
		_ = renderGlassRaster(key, width, height)
		for _, p := range panels {
			key.Width, key.Height = int(p.W)+16, int(p.H)+16
			key.X, key.Y = int(p.X)-8, int(p.Y)-8
			key.Surface, key.Shadow, key.Radius, key.Base = true, true, 40, paletteFor("glass").panel
			width, height = glassRasterSize(key)
			_ = renderGlassLayer(key, width, height)
		}
	}
}

func TestRenderGlassMaterialPreview(t *testing.T) {
	path := os.Getenv("MINEWATCH_GLASS_PREVIEW")
	if path == "" {
		t.Skip("optional renderer preview")
	}
	const w, h = 1100, 680
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	paste := func(k glassTileKey) {
		data := renderGlassTile(k)
		for y := 0; y < k.Height; y++ {
			for x := 0; x < k.Width; x++ {
				dx, dy := k.X+x, k.Y+y
				if dx < 0 || dy < 0 || dx >= w || dy >= h {
					continue
				}
				i, j := (y*k.Width+x)*4, dy*img.Stride+dx*4
				img.Pix[j], img.Pix[j+1], img.Pix[j+2], img.Pix[j+3] = data[i+2], data[i+1], data[i], 255
			}
		}
	}
	k := glassTileKey{Width: w, Height: h, CanvasWidth: w, CanvasHeight: h, Theme: "glass"}
	paste(k)
	k.Surface = true
	k.Shadow = true
	k.Radius = 30
	k.Base = paletteFor("glass").panel
	for i := 0; i < 4; i++ {
		k.X = 16 + i*270
		k.Y = 70
		k.Width = 260
		k.Height = 112
		paste(k)
	}
	k.X = 16
	k.Y = 190
	k.Width = 1068
	k.Height = 64
	paste(k)
	k.X = 16
	k.Y = 280
	k.Width = 1068
	k.Height = 210
	paste(k)
	for i := 0; i < 3; i++ {
		k.X = 16 + i*360
		k.Y = 510
		k.Width = 348
		k.Height = 148
		paste(k)
	}
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = png.Encode(f, img); e != nil {
		t.Fatal(e)
	}
}
