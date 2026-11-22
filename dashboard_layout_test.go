package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDashboardCardDragAndResize(t *testing.T) {
	for _, width := range []int32{640, 800, 1100, 1500, 3440} {
		original := dashboardGeometry(width, 900, false).Cards
		for divider := range len(original) - 1 {
			for _, delta := range []int32{-5000, -6, 6, 5000} {
				widths, changed := dragCardDivider(original, divider, delta)
				g := dashboardGeometryWithWidths(width, 900, false, widths)
				last := g.Cards[profitCardIndex]
				for i, card := range g.Cards {
					if card.Y != last.Y || card.H != last.H || card.W < dashboardCardMinimums[i] {
						t.Fatalf("card does not fit on the row: %+v", g.Cards)
					}
					if i > 0 && card.X != g.Cards[i-1].X+g.Cards[i-1].W+dashboardCardGap {
						t.Fatal("invalid card gap", g.Cards)
					}
					if i != divider && i != divider+1 && card != original[i] {
						t.Fatal("drag moved an unrelated card", divider, i)
					}
				}
				if last.X+last.W != width-g.Cards[0].X || changed != (g.Cards != original) {
					t.Fatal("wrong width after drag", width, divider, delta, g.Cards)
				}
				wide := dashboardGeometryWithWidths(width+500, 900, false, widths)
				if wide.Cards[divider].W < g.Cards[divider].W || wide.Cards[divider+1].W < g.Cards[divider+1].W {
					t.Fatal("saved widths should grow with the window")
				}
			}
		}
	}
}

func TestDashboardCardWidthsPersistAndReset(t *testing.T) {
	dir := t.TempDir()
	a, err := newApp(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	want, _ := dragCardDivider(dashboardGeometry(1500, 900, false).Cards, 3, 42)
	if err := a.setDashboardWidths(want); err != nil {
		t.Fatal(err)
	}
	a.dir = filepath.Join(dir, "settings.json", "blocked")
	if err := a.setDashboardWidths(cardWidths{}); err == nil || *a.config.DashboardWidths != want {
		t.Fatal("failed save must keep previous widths")
	}
	b, err := newApp(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.db.Close()
	if b.config.DashboardWidths == nil || *b.config.DashboardWidths != want {
		t.Fatal("custom widths did not persist")
	}
	if err := b.setDashboardWidths(cardWidths{}); err != nil || b.config.DashboardWidths != nil {
		t.Fatal("reset should restore defaults", err)
	}
}

func TestDashboardKeepsTableChartAndStatusSeparate(t *testing.T) {
	for _, size := range [][2]int32{{1100, 640}, {1280, 680}, {1500, 900}, {1920, 1040}} {
		for _, full := range []bool{false, true} {
			g := dashboardGeometry(size[0], size[1], full)
			if g.Table.H < 80 || g.Table.Y+g.Table.H > g.Status.Y-10 {
				t.Fatalf("table clipped: %+v", g)
			}
			if !full && (g.Chart.Y < g.Table.Y+g.Table.H+10 || g.Chart.Y+g.Chart.H > g.Status.Y-10 || g.Chart.H < 124) {
				t.Fatalf("chart/status overlap: %+v", g)
			}
			for _, ids := range [][]int{{phoneCommand, 34, 35}, {priorityCommand, 38}, {11, 12, 13, 21, 22, 23, 24}, {27, 28, 33, 29, 32, 25}, {26}} {
				for i, id := range ids {
					r := g.Controls[id]
					if r.X < 0 || r.X+r.W > size[0] {
						t.Fatal("control clipped", id, r)
					}
					if i > 0 {
						prev := g.Controls[ids[i-1]]
						if prev.X+prev.W > r.X {
							t.Fatal("controls overlap", ids[i-1], id)
						}
					}
				}
			}
		}
	}
}

// An accidentally shared WM_COMMAND ID made the phone button toggle priority.
// Validate actual Win32 control declarations, including inactive build targets.
func TestNativeMainControlIDsAreUnique(t *testing.T) {
	f, e := parser.ParseFile(token.NewFileSet(), "native_windows.go", nil, 0)
	if e != nil {
		t.Fatal(e)
	}
	seen := map[int]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok || len(c.Args) != 4 {
			return true
		}
		fn, ok := c.Fun.(*ast.Ident)
		if !ok || fn.Name != "ctrl" {
			return true
		}
		id := -1
		switch v := c.Args[3].(type) {
		case *ast.BasicLit:
			id, _ = strconv.Atoi(v.Value)
		case *ast.Ident:
			switch v.Name {
			case "phoneCommand":
				id = phoneCommand
			case "priorityCommand":
				id = priorityCommand
			case "profitSettingsCommand":
				id = profitSettingsCommand
			case "languageCommand":
				id = languageCommand
			case "sourceCommand":
				id = sourceCommand
			}
		}
		if id < 0 {
			t.Fatal("unverified control identifier")
		}
		if seen[id] {
			t.Errorf("duplicate command ID %d", id)
		}
		seen[id] = true
		return true
	})
	if !seen[phoneCommand] || !seen[priorityCommand] {
		t.Fatal("missing phone/priority controls")
	}
}
