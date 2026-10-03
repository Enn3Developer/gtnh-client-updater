package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// C4: the whole page of a full GTNH instance, top to bottom.
func TestC4GTNHPageTopToBottom(t *testing.T) {
	m, _ := oneFull(t)
	const w = 78

	got := pageLines(m, w)

	want := []string{
		"Home",
		"GTNH 2.8.1 · Java 17+ · played 2 days ago",
		"",
		hinted("▸ ", "▶ Play", "enter", w),
		hinted("  ", "Play and join play.example.org", "j", w),
		"",
		"Update",
		hinted("  ", "GTNH 2.8.4 is out · stable release · 5 days ago", "u", w),
		hinted("  ", "Choose another version…", "o", w),
		hinted("  ", "Last update 2.8.0 → 2.8.1, 3 days ago · undo", "b", w),
		"",
		"Settings",
		"  Memory         8192 MB (at least 4096 MB)",
		"  Java arguments Prism's default",
		"  Java           Prism's default",
		"  Window         Prism's default",
		"  Server         play.example.org",
		"  Server mods    mods.example.org",
		"",
		"Launcher",
	}
	if len(got) < len(want)+2 {
		t.Fatalf("page has %d lines, want at least %d:\n%s", len(got), len(want)+2, strings.Join(got, "\n"))
	}
	for i, l := range want {
		if got[i] != l {
			t.Errorf("page line %d = %q, want %q", i+1, got[i], l)
		}
	}
	after, prism := got[len(want)], got[len(want)+1]
	if !strings.HasPrefix(after, "  After I start the game") || !strings.HasSuffix(after, "stay open and show whether it's running") {
		t.Errorf("after row = %q", after)
	}
	if !strings.HasPrefix(prism, "  Prism Launcher") || !strings.HasSuffix(prism, "found automatically") {
		t.Errorf("prism row = %q", prism)
	}
}

// C4
func TestC4LauncherRowsShowTheAppSettings(t *testing.T) {
	m, _ := oneFull(t)
	m.app.AfterPlay = "quit"
	m.app.PrismExe = "/opt/prism/bin/prismlauncher"

	got := pageLines(m, 78)

	after, prism := lineWith(got, "After I start the game"), lineWith(got, "Prism Launcher")
	if !strings.HasSuffix(after, " quit") {
		t.Errorf("after row = %q, want value quit", after)
	}
	if !strings.HasSuffix(prism, "/opt/prism/bin/prismlauncher") {
		t.Errorf("prism row = %q, want the exe path", prism)
	}
}

// C4
func TestC4NonGTNHPageHasNoUpdateNoJoinAndFourSettings(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Vanilla", server: "play.example.org"})
	m, _ := loadedModel(root, 80, 24, in)
	m.showAll = true
	m.focus = focusPage
	const w = 78

	got := pageLines(m, w)

	want := []string{
		"Vanilla",
		"Not a GTNH instance · never played",
		"",
		hinted("▸ ", "▶ Play", "enter", w),
		"",
		"Settings",
		"  Memory         Prism's default",
		"  Java arguments Prism's default",
		"  Java           Prism's default",
		"  Window         Prism's default",
		"",
		"Launcher",
	}
	if len(got) < len(want) {
		t.Fatalf("page %q shorter than %q", got, want)
	}
	for i, l := range want {
		if got[i] != l {
			t.Errorf("page line %d = %q, want %q", i+1, got[i], l)
		}
	}
	if ids := strings.Join(rowIDs(m), ","); ids != "play,memory,jvm,java,window,after,prism" {
		t.Errorf("rows = %s", ids)
	}
}

// C4
func TestC4RowsOfAFullGTNHInstanceInOrder(t *testing.T) {
	m, _ := oneFull(t)

	ids := strings.Join(rowIDs(m), ",")

	if ids != "play,join,update,versions,undo,memory,jvm,java,window,server,mods,after,prism" {
		t.Errorf("rows = %s", ids)
	}
}

// C4
func TestC4RowsOfAPlainUpToDateInstance(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24, makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4"}))

	ids := strings.Join(rowIDs(m), ",")

	if ids != "play,versions,memory,jvm,java,window,server,mods,after,prism" {
		t.Errorf("rows = %s", ids)
	}
}

// C4
func TestC4PageWidthIsWhatTheMarginOrSidebarLeaves(t *testing.T) {
	alone, _ := oneFull(t)
	both, _ := twoGTNH(t) // "Home", "Second": sidebar 16 wide

	if got := alone.pageWidth(); got != 78 {
		t.Errorf("single instance pageWidth = %d, want 80-2", got)
	}
	if got := both.pageWidth(); got != 61 {
		t.Errorf("with sidebar pageWidth = %d, want 80-16-3", got)
	}
}

// C4
func TestC4RowLineSelectedWithHintIsExactlyWidth(t *testing.T) {
	got := ansi.Strip(rowLine(20, true, "Play", "enter"))

	if got != "▸ Play         enter" {
		t.Errorf("rowLine = %q", got)
	}
}

// C4
func TestC4RowLineUnselectedWithHint(t *testing.T) {
	got := ansi.Strip(rowLine(20, false, "Play", "enter"))

	if got != "  Play         enter" {
		t.Errorf("rowLine = %q", got)
	}
}

// C4
func TestC4RowLineTruncatesTextThatCrowdsTheHint(t *testing.T) {
	got := ansi.Strip(rowLine(20, false, "A rather long row text", "u"))

	if ansi.StringWidth(got) != 20 || !strings.HasPrefix(got, "  A rather") || !strings.HasSuffix(got, "u") || !strings.Contains(got, "…") {
		t.Errorf("rowLine = %q, want 20 columns, truncated text with … and hint u", got)
	}
}

// C4: with room for a single text column the hint still sits flush right.
// Kills page.go:186 (`room < 1` -> `<= 1`).
func TestC4RowLineKeepsTheHintWithOneColumnOfText(t *testing.T) {
	got := ansi.Strip(rowLine(5, false, "Play", "u"))

	if ansi.StringWidth(got) != 5 || !strings.HasPrefix(got, "  ") || !strings.HasSuffix(got, " u") {
		t.Errorf("rowLine = %q, want 5 columns ending in the hint u", got)
	}
}

// C4
func TestC4RowLineWithoutHintTruncatesToWidth(t *testing.T) {
	got := ansi.Strip(rowLine(10, false, "abcdefghijkl", ""))

	if got != "  abcdefg…" {
		t.Errorf("rowLine = %q, want %q", got, "  abcdefg…")
	}
}

// C4
func TestC4RowLineWithoutHintFitting(t *testing.T) {
	got := strings.TrimRight(ansi.Strip(rowLine(20, true, "Memory", "")), " ")

	if got != "▸ Memory" {
		t.Errorf("rowLine = %q, want %q", got, "▸ Memory")
	}
}

// C4
func TestC4HeadingStripsToItsText(t *testing.T) {
	if got := ansi.Strip(heading("Settings")); got != "Settings" {
		t.Errorf("heading = %q", got)
	}
}

// C4, kills K1 (scrollTo's bottom-edge adjustment returning the old offset, so moving
// down never scrolls): at height 14 the workspace is 11 lines; the last launcher row must
// scroll into view, with the "↑ N more" marker above it.
func TestC4LastLauncherRowScrollsIntoView(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 14, makeInst(t, root, fullSpec("Home")))
	m.focus = focusPage

	press(m, "down", "down", "down", "down", "down", "down", "down", "down", "down", "down", "down", "down")
	lines := screen(m)

	if m.rows()[m.row].id != "prism" {
		t.Fatalf("12 downs reached row %q, want prism", m.rows()[m.row].id)
	}
	if !strings.HasPrefix(strings.TrimSpace(lineWith(lines, "Prism Launcher")), "▸ Prism Launcher") {
		t.Errorf("selected Prism Launcher row not visible:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.HasSuffix(lineWith(lines, "↑ "), " more") {
		t.Errorf("no ↑ more marker:\n%s", strings.Join(lines, "\n"))
	}
}

// C4
func TestC4UnscrolledOverflowingPageEndsWithADownMarker(t *testing.T) {
	m, _ := oneFull(t)

	lines := trimLines(m.pageView(78, 8))

	if len(lines) > 8 || lines[0] != "Home" || !strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "↓ ") {
		t.Errorf("page window %q: want Home first and a ↓ more marker last", lines)
	}
}

// C4: every selected row is fully visible and never on a marker line.
func TestC4PageViewAlwaysShowsTheSelectedRow(t *testing.T) {
	for r := range 13 {
		t.Run(fmt.Sprint(r), func(t *testing.T) {
			m, _ := oneFull(t)
			m.row = r
			sel := strings.TrimSpace(ansi.Strip(m.rows()[r].lines(78, true)[0]))

			lines := trimLines(m.pageView(78, 8))

			if len(lines) > 8 {
				t.Fatalf("page window has %d lines, want ≤ 8", len(lines))
			}
			if !containsLine(lines, sel) {
				t.Errorf("selected row %q not visible in %q", sel, lines)
			}
		})
	}
}
