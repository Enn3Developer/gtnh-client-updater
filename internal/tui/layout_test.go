package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// overlayBase is 10 lines of 20 columns, line i made of letter 'a'+i; line(i) styles it.
func overlayBase(line func(i int, s string) string) []string {
	var base []string
	for i := range 10 {
		base = append(base, line(i, strings.Repeat(string(rune('a'+i)), 20)))
	}
	return base
}

const overlayBox = "[XY]\n[ZW]"

// C8: a 4x2 box on a 20x10 base sits at x = (20-4)/2 = 8 on lines 5 and 6 (1-based),
// the middle of the workspace lines; other lines are untouched.
func TestC8OverlayCentresTheBoxAndKeepsTheRest(t *testing.T) {
	base := overlayBase(func(_ int, s string) string { return s })

	got := strings.Split(overlay(strings.Join(base, "\n"), overlayBox, 20, 10), "\n")

	want := append([]string{}, base...)
	want[4] = "eeeeeeee[XY]eeeeeeee"
	want[5] = "ffffffff[ZW]ffffffff"
	if len(got) != 10 {
		t.Fatalf("overlay has %d lines, want 10", len(got))
	}
	for i := range want {
		if ansi.Strip(got[i]) != want[i] {
			t.Errorf("line %d = %q, want %q", i+1, ansi.Strip(got[i]), want[i])
		}
	}
}

// C8: a box as tall as the workspace (lines 3..height-2 of a 10-line base: 6 lines)
// covers all of it, down to its last line. Kills layout.go:80 (`n > last` -> `>=`).
func TestC8OverlayBoxFillingTheWorkspaceDrawsEveryLine(t *testing.T) {
	base := overlayBase(func(_ int, s string) string { return s })
	box := strings.Repeat("[XY]\n", 5) + "[ZW]"

	got := strings.Split(overlay(strings.Join(base, "\n"), box, 20, 10), "\n")

	if len(got) != 10 {
		t.Fatalf("overlay has %d lines, want 10", len(got))
	}
	if g := ansi.Strip(got[2]); g != "cccccccc[XY]cccccccc" {
		t.Errorf("line 3 = %q", g)
	}
	if g := ansi.Strip(got[7]); g != "hhhhhhhh[ZW]hhhhhhhh" {
		t.Errorf("line 8 = %q, want the box's last line", g)
	}
	if got[1] != base[1] || got[8] != base[8] || got[9] != base[9] {
		t.Errorf("lines 2, 9 or 10 changed: %q", got)
	}
}

// C8: ANSI-coloured base lines keep their visible text left and right of the box.
func TestC8OverlaySplicesStyledLines(t *testing.T) {
	base := overlayBase(func(_ int, s string) string { return "\x1b[31m" + s[:10] + "\x1b[0m\x1b[1;34m" + s[10:] + "\x1b[0m" })

	got := strings.Split(overlay(strings.Join(base, "\n"), overlayBox, 20, 10), "\n")

	if len(got) != 10 {
		t.Fatalf("overlay has %d lines, want 10", len(got))
	}
	if g := ansi.Strip(got[4]); g != "eeeeeeee[XY]eeeeeeee" {
		t.Errorf("styled line 5 = %q", g)
	}
	if g := ansi.Strip(got[5]); g != "ffffffff[ZW]ffffffff" {
		t.Errorf("styled line 6 = %q", g)
	}
	for _, i := range []int{0, 1, 9} {
		if got[i] != base[i] {
			t.Errorf("line %d changed: %q", i+1, got[i])
		}
	}
	for i, l := range got {
		if w := ansi.StringWidth(l); w > 20 {
			t.Errorf("line %d is %d columns", i+1, w)
		}
	}
}

// C8: a wide rune straddling the splice point doesn't push the line past the width.
func TestC8OverlaySplicesThroughWideRunes(t *testing.T) {
	wide := "abcdefg日本語hijklmn" // 日 spans columns 7-8: the box starts at 8
	base := overlayBase(func(i int, s string) string {
		if i == 4 || i == 5 {
			return wide
		}
		return s
	})

	got := strings.Split(overlay(strings.Join(base, "\n"), overlayBox, 20, 10), "\n")

	want4 := ansi.Truncate(wide, 8, "") + "[XY]" + ansi.TruncateLeft(wide, 12, "")
	want5 := ansi.Truncate(wide, 8, "") + "[ZW]" + ansi.TruncateLeft(wide, 12, "")
	if len(got) != 10 {
		t.Fatalf("overlay has %d lines, want 10", len(got))
	}
	if g := ansi.Strip(got[4]); g != want4 {
		t.Errorf("line 5 = %q, want %q", g, want4)
	}
	if g := ansi.Strip(got[5]); g != want5 {
		t.Errorf("line 6 = %q, want %q", g, want5)
	}
	for i, l := range got {
		if w := ansi.StringWidth(l); w > 20 {
			t.Errorf("line %d is %d columns", i+1, w)
		}
	}
}

// C8: the dialog is drawn over the view, which keeps its title bar and status bar.
func TestC8DialogIsOverlaidOnTheView(t *testing.T) {
	m, _ := oneFull(t)
	before := screen(m)
	m.notify("Heads up", "Something to know.")

	lines := screen(m)

	if len(lines) != 24 || lines[0] != before[0] || lines[1] != before[1] {
		t.Errorf("title lines changed under the dialog:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lineWith(lines, "Heads up"), "╭─ Heads up ") {
		t.Errorf("no dialog border with the title:\n%s", strings.Join(lines, "\n"))
	}
	if got := lines[23]; got != " ←→ choose   enter ok   esc cancel" {
		t.Errorf("status bar = %q", got)
	}
}
