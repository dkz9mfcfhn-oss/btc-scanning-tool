package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestDisplayResolutionScaleMatrix(t *testing.T) {
	// Physical displays include small laptops, FHD, QHD, 4K and ultrawide.
	for _, screen := range [][2]int32{{1024, 768}, {1280, 720}, {1366, 768}, {1920, 1080}, {2560, 1440}, {3840, 2160}, {3440, 1440}, {1080, 1920}} {
		for _, dpi := range []int32{96, 120, 144, 168, 192, 240, 288} {
			t.Run(fmt.Sprintf("%dx%d_%d", screen[0], screen[1], dpi), func(t *testing.T) {
				// Reserve taskbar, borders and both native scrollbars conservatively.
				for _, zoom := range uiScaleChoices {
					content := contentDPI(dpi, zoom)
					vw, vh := unscaleUI(screen[0]-scaleUI(36, dpi), content), unscaleUI(screen[1]-scaleUI(85, dpi), content)
					for _, full := range []bool{false, true} {
						v := dashboardViewport(vw, vh, full, 0, 0)
						g := dashboardGeometry(v.CanvasW, v.CanvasH, full)
						lastCard := g.Cards[len(g.Cards)-1]
						if g.Cards[0].X != v.CanvasW-lastCard.X-lastCard.W || g.Table.X != v.CanvasW-g.Table.X-g.Table.W {
							t.Fatalf("uneven dashboard edge margins at %d%%", zoom)
						}
						if g.BrandTitle.W < 260 {
							t.Fatalf("full product name needs at least 260 logical pixels, got %d", g.BrandTitle.W)
						}
						boxes := []uiBox{g.Table, g.Status, g.Progress, g.ListTitle, g.MonitorState, g.ListCount, g.BrandTitle}
						boxes = append(boxes, g.Cards[:]...)
						if !full {
							boxes = append(boxes, g.Chart)
						}
						for id, b := range g.Controls {
							if id == profitSettingsCommand {
								if b.X < lastCard.X || b.Y < lastCard.Y || b.X+b.W > lastCard.X+lastCard.W || b.Y+b.H > lastCard.Y+lastCard.H {
									t.Fatalf("profit settings must stay inside the fifth card at %d%%: %+v", zoom, b)
								}
								continue
							}
							boxes = append(boxes, b)
						}
						for i, b := range boxes {
							if b.X < 0 || b.Y < 0 || b.W <= 0 || b.H <= 0 || b.X+b.W > v.CanvasW || b.Y+b.H > v.CanvasH {
								t.Fatalf("outside canvas at %d%%: %+v in %+v", zoom, b, v)
							}
							for _, other := range boxes[:i] {
								if b.X < other.X+other.W && other.X < b.X+b.W && b.Y < other.Y+other.H && other.Y < b.Y+b.H {
									t.Fatalf("overlap at %d%%: %+v and %+v in %+v", zoom, b, other, v)
								}
							}
							// Both ends of every control remain reachable, even if the table
							// itself is wider/taller than the physical viewport.
							for _, corner := range [][2]int32{{b.X, b.Y}, {b.X + b.W - 1, b.Y + b.H - 1}} {
								x := revealAxis(0, v.W, corner[0], 1, v.CanvasW)
								y := revealAxis(0, v.H, corner[1], 1, v.CanvasH)
								if corner[0] < x || corner[0] >= x+v.W || corner[1] < y || corner[1] >= y+v.H {
									t.Fatal("unreachable control", b, v)
								}
							}
						}
						if g.Table.Y+g.Table.H > g.Status.Y || (!full && g.Chart.Y+g.Chart.H > g.Status.Y) {
							t.Fatal("chart/status overlap")
						}
						if vw >= dashboardMinWidth && v.CanvasW != v.W {
							t.Fatal("dashboard must reflow without horizontal scroll", v)
						}
					}
					for _, minSize := range [][2]int32{{520, 420}, {640, 280}, {712, 390}, {630, 205}} {
						v := fitViewport(vw, vh, minSize[0], minSize[1], 99999, 99999)
						if v.X+v.W != v.CanvasW || v.Y+v.H != v.CanvasH {
							t.Fatal("dialog far edge unreachable", v)
						}
					}
				}
			})
		}
	}
}

func TestDisplayColumnZoomRetainsDraggedWidth(t *testing.T) {
	column := displayColumnWidth{logical: 115, lastPixels: 115}
	for range 12 {
		if pixels := column.rescale(115, 96, 48); pixels != 58 {
			t.Fatal("wrong physical width at 50%", pixels)
		}
		if pixels := column.rescale(58, 48, 96); pixels != 115 {
			t.Fatal("logical width drifted after zooming", pixels)
		}
	}
	column.rescale(115, 96, 48)
	if pixels := column.rescale(60, 48, 144); pixels != 180 {
		t.Fatal("dragged width must survive changing DPI", pixels)
	}
}

func TestMinerColumnsFillRemainingClientWidth(t *testing.T) {
	order := nativeColumnOrder(true)
	for _, systemDPI := range []int32{96, 120, 144, 192} {
		for _, zoom := range uiScaleChoices {
			dpi := contentDPI(systemDPI, zoom)
			for _, hideNote := range []bool{false, true} {
				config := Config{ColumnVisibility: map[string]bool{"备注": !hideNote}}
				widths := configuredColumnWidths(config)
				var total int32
				for i := range widths {
					widths[i] = scaleUI(widths[i], dpi)
					total += widths[i]
				}
				last := 19 // notes, before the trailing hidden columns
				if hideNote {
					last = 18 // uptime becomes the last visible column
				}
				for _, difference := range []int32{-17, 0, 1, 237} {
					got, extra := fitListColumns(widths, order, total+difference)
					if got != last || extra != max(0, difference) {
						t.Fatalf("DPI=%d zoom=%d hideNote=%t client=%d: got column %d plus %d px, want %d plus %d px", systemDPI, zoom, hideNote, total+difference, got, extra, last, max(0, difference))
					}
				}
			}
		}
	}
	if last, extra := fitListColumns([]int32{0, 0}, []int32{1, 0}, 100); last != -1 || extra != 0 {
		t.Fatalf("hidden columns became visible: column %d plus %d px", last, extra)
	}
}

func TestMinerColumnFillDoesNotBecomeUserWidth(t *testing.T) {
	filled := displayColumnWidth{logical: 115, lastPixels: 58 + 217, fillPixels: 217}
	if got := filled.remember(275, 48); got != 58 {
		t.Fatalf("expected original base pixels without fill, got %d", got)
	}
	if got := filled.rescale(275, 48, 96); got != 115 || filled.fillPixels != 0 {
		t.Fatalf("auto-filled width persisted through zoom: width=%d, column=%+v", got, filled)
	}
	dragged := displayColumnWidth{logical: 115, lastPixels: 58 + 217, fillPixels: 217}
	if got := dragged.remember(100, 48); got != 100 || dragged.fillPixels != 0 {
		t.Fatalf("manual width lost to auto fill: width=%d, column=%+v", got, dragged)
	}
	if got := dragged.rescale(100, 48, 96); got != 200 {
		t.Fatalf("manual width lost through zoom: got %d", got)
	}
}

func TestBackdropCoversOddPixelSizesThroughScrollEnd(t *testing.T) {
	// At 75% on a 125%-DPI monitor, separate logical rounding previously
	// covered only 126 of these 127 pixels at the far end of the viewport.
	dpi := contentDPI(120, 75)
	if got := backdropPixelSpan(154, 127, 19, dpi) - scaleUI(19, dpi); got != 127 {
		t.Fatalf("fractional scroll-end backing covers %d of 127 pixels", got)
	}

	for _, physical := range [][2]int32{{311, 461}, {519, 481}, {1281, 719}, {1919, 1079}, {3839, 2159}} {
		for _, systemDPI := range []int32{96, 120, 144, 168, 192, 240, 288} {
			for _, zoom := range uiScaleChoices {
				dpi := contentDPI(systemDPI, zoom)
				w, h := unscaleUI(physical[0], dpi), unscaleUI(physical[1], dpi)
				v := dashboardViewport(w, h, false, 100000, 100000)
				for i, axis := range [][2]int32{{v.CanvasW, v.X}, {v.CanvasH, v.Y}} {
					base := backdropPixelSpan(axis[0], physical[i], 0, dpi)
					for _, scroll := range []int32{0, axis[1] / 2, axis[1]} {
						span := backdropPixelSpan(axis[0], physical[i], scroll, dpi)
						covered := span - scaleUI(scroll, dpi)
						if span != base || covered < physical[i] {
							t.Fatalf("%v dpi=%d zoom=%d axis=%d scroll=%d: unstable span %d vs %d or covered %d of %d", physical, systemDPI, zoom, i, scroll, span, base, covered, physical[i])
						}
					}
				}
			}
		}
	}
}

func TestDisplayScalingNoDriftAndSignedCoordinates(t *testing.T) {
	for _, dpi := range []int32{96, 120, 144, 168, 192, 240, 288} {
		for n := int32(-4096); n < 4096; n++ {
			if got := unscaleUI(scaleUI(n, dpi), dpi); got != n {
				t.Fatalf("%d @%d -> %d", n, dpi, got)
			}
		}
	}
	// Moving from a small screen to a larger one must clear obsolete scroll offsets.
	v := fitViewport(700, 400, 1100, 640, 400, 240)
	v = fitViewport(1900, 1000, 1100, 640, v.X, v.Y)
	if v.X != 0 || v.Y != 0 {
		t.Fatal("stale scroll on large screen", v)
	}
	for _, wa := range []uiBox{{0, 0, 1366, 728}, {-1920, 0, 1920, 1040}, {0, -1440, 2560, 1400}} {
		for _, b := range []uiBox{{100, 100, 3000, 2000}, {-4000, -4000, 900, 600}, {5000, 5000, 100, 100}} {
			fit := fitWindowBox(b, wa)
			if fit.X < wa.X || fit.Y < wa.Y || fit.X+fit.W > wa.X+wa.W || fit.Y+fit.H > wa.Y+wa.H {
				t.Fatal("window outside monitor", fit, wa)
			}
		}
	}
}

func TestAllTopLevelWindowsHandleDisplayChanges(t *testing.T) {
	for file, proc := range map[string]string{"native_windows.go": "windowProc", "native_detail_windows.go": "detailProc", "push_windows.go": "phoneProc", "text_dialog_windows.go": "textDialogProc", "diagnostic_folder_windows.go": "diagnosticFolderProc"} {
		f, e := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Name.Name != proc {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "displayMessage" {
						found = true
					}
				}
				return true
			})
		}
		if !found {
			t.Error("missing per-window display handler", proc)
		}
	}
}
