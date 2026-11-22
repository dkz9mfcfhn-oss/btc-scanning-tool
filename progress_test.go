package main

import (
	"strings"
	"testing"
)

func TestScanProgress(t *testing.T) {
	for _, c := range []struct {
		job     Job
		percent int
		label   string
	}{
		{Job{}, 0, "等待扫描"}, {Job{Running: true, Total: 100, Done: 25}, 25, "25 / 100"},
		{Job{Total: 100, Done: 100}, 100, "扫描结束"}, {Job{Total: 100, Done: 25}, 25, "扫描已停止"},
		{Job{Running: true, Total: 4, Done: 99}, 100, "4 / 4"},
		{Job{Running: true}, 0, "准备扫描"},
		{Job{Error: "failed"}, 0, "扫描失败"},
		{Job{Total: 10, Done: -1}, 0, "0 / 10"},
		{Job{Total: 10, Done: 10, Error: "failed"}, 100, "扫描已停止"},
	} {
		p, s := scanProgress(c.job)
		if p != c.percent || !strings.Contains(s, c.label) {
			t.Fatal(p, s)
		}
	}
}

func TestProgressUsesFormerBlankFieldAndKeepsSearchSeparate(t *testing.T) {
	for _, w := range []int32{520, 700, 880, 1099, 1100, 1280, 1500, 1920, 3840} {
		g := dashboardGeometry(w, 640, false)
		p, s := g.Progress, g.Controls[26]
		if p.X != 150 || p.Y != g.ListTitle.Y || p.H != 32 || p.X < g.ListTitle.X+g.ListTitle.W {
			t.Fatal("progress is not beside list heading", g)
		}
		if s.Y < p.Y+p.H || s.Y+s.H+13 >= g.Table.Y {
			t.Fatal("search overlaps progress/table", g)
		}
		for id, b := range g.Controls {
			if p.X < b.X+b.W && b.X < p.X+p.W && p.Y < b.Y+b.H && b.Y < p.Y+p.H {
				t.Fatal("native control covers progress", id)
			}
		}
	}
	for _, width := range []int32{160, 560} {
		if !strings.Contains(progressBarLabel(Job{Running: true, Total: 254, Done: 127}, width), "50%") {
			t.Fatal("missing actual percentage")
		}
		if !strings.Contains(progressBarLabel(Job{Total: 254, Done: 254}, width), "100%") {
			t.Fatal("completion not retained")
		}
		if strings.Contains(progressBarLabel(Job{}, width), "100%") {
			t.Fatal("idle shown complete")
		}
	}
}
