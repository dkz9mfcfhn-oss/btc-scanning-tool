package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestInterfaceLanguagePersistsAndPreservesMinerData(t *testing.T) {
	dir := t.TempDir()
	a, err := newApp(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	if configuredLanguage(a.config.Language) != chinese || configuredLanguage("unknown") != chinese {
		t.Fatal("older or unknown preferences should use Chinese")
	}
	if err := a.setLanguage(russian); err != nil {
		t.Fatal(err)
	}
	b, err := newApp(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.db.Close()
	if b.config.Language != "ru" {
		t.Fatal("language did not persist")
	}
	b.dir = filepath.Join(dir, "settings.json", "blocked")
	if err = b.setLanguage(english); err == nil || b.config.Language != "ru" {
		t.Fatal("failed save changed the language")
	}
	now := time.Now()
	d := &Device{IP: "192.168.1.4", WorkerReadAt: now, WorkerNames: []string{"在线"}, Note: "在线"}
	for _, lang := range []uiLanguage{english, russian} {
		row := lang.nativeRow(d, now)
		if row[2] == "待更新" || row[backendColumn] == "打开后台" || !strings.HasPrefix(row[17], "在线") || row[19] != "在线" {
			t.Fatal("incorrectly translated row", row)
		}
		if got := lang.progressBarLabel(Job{Running: true, Done: 1, Total: 2}, 120); strings.Contains(got, "扫描中") || !strings.Contains(got, "50%") {
			t.Fatal("incorrectly translated scan progress", got)
		}
	}
}

func TestInterfaceTranslationsCoverLabelsAndFormats(t *testing.T) {
	check := func(value string) {
		t.Helper()
		if !strings.ContainsFunc(value, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			return
		}
		if pair, ok := translations[value]; !ok || pair[0] == "" || pair[1] == "" {
			t.Errorf("missing translation: %q", value)
		}
	}
	for _, label := range themeLabels {
		check(label)
	}
	for _, column := range nativeListColumns {
		check(column.Name)
	}
	for _, order := range listSortChoices {
		check(order.Label)
	}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") || file.Name() == "locale_catalog.go" {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				if fn.Name != "tr" {
					return true
				}
			case *ast.SelectorExpr:
				if fn.Sel.Name != "text" {
					return true
				}
			default:
				return true
			}
			if v, ok := call.Args[0].(*ast.BasicLit); ok && v.Kind == token.STRING {
				value, err := strconv.Unquote(v.Value)
				if err != nil {
					t.Fatal(err)
				}
				check(value)
			}
			return true
		})
	}
	format := regexp.MustCompile(`%[-+0-9.]*[dsfgxX%]`)
	for label, pair := range translations {
		for _, translated := range pair {
			if !slices.Equal(format.FindAllString(label, -1), format.FindAllString(translated, -1)) {
				t.Errorf("inconsistent format: %q and %q", label, translated)
			}
		}
	}
}
