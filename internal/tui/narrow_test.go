package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A narrow terminal keeps room for the page and still says how to quit.

func TestNarrowSidebarTakesAtMostAThird(t *testing.T) {
	root := t.TempDir()
	for _, c := range []struct{ width, want int }{{120, 25}, {80, 25}, {60, 20}, {40, 13}, {24, 10}} {
		m, _ := loadedModel(root, c.width, 24,
			makeInst(t, root, instSpec{name: "GT New Horizons 2.8.1", gtnh: true}),
			makeInst(t, root, instSpec{name: "GTNH with friends", gtnh: true}))
		if got := m.sidebarWidth(); got != c.want {
			t.Errorf("sidebarWidth at %d columns = %d, want %d", c.width, got, c.want)
		}
	}
}

func TestNarrowStatusBarKeepsQuit(t *testing.T) {
	pairs := []string{"↑↓", "instance", "enter", "play", "tab", "page", "n", "new instance", "a", "show all", "q", "quit"}

	for _, w := range []int{80, 60, 44, 30} {
		got := ansi.Strip(statusBar(w, pairs...))
		if ansi.StringWidth(got) != w || !strings.HasSuffix(strings.TrimRight(got, " "), "q quit") {
			t.Errorf("statusBar(%d) = %q, want it to end with q quit", w, got)
		}
	}
	if got := strings.TrimSpace(ansi.Strip(statusBar(60, pairs...))); got != "↑↓ instance   enter play   tab page   q quit" {
		t.Errorf("statusBar(60) = %q, want the pairs before q quit to give way from the end", got)
	}
	if got := strings.TrimSpace(ansi.Strip(statusBar(80, pairs...))); got != strings.Join([]string{
		"↑↓ instance", "enter play", "tab page", "n new instance", "a show all", "q quit"}, "   ") {
		t.Errorf("statusBar(80) = %q, want every pair", got)
	}
}
