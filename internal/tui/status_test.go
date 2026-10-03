package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

var errNoNet = errors.New("no network")

// mixedModel is loaded with two GTNH instances and a hidden non-GTNH one.
func mixedModel(t *testing.T) (*model, *fakes) {
	root := t.TempDir()
	return loadedModel(root, 80, 24,
		makeInst(t, root, instSpec{name: "Alpha", gtnh: true, version: "2.8.4"}),
		makeInst(t, root, instSpec{name: "Vanilla"}),
		makeInst(t, root, instSpec{name: "Bravo", gtnh: true, version: "2.8.4"}))
}

// C7
func TestC7StatusBarIsPaddedToTheFullWidth(t *testing.T) {
	bar := statusBar(20, "q", "quit")

	if got := ansi.Strip(bar); got != " q quit"+strings.Repeat(" ", 13) {
		t.Errorf("status bar = %q", got)
	}
	if w := ansi.StringWidth(bar); w != 20 {
		t.Errorf("status bar is %d columns, want 20", w)
	}
}

// C7
func TestC7StatusBarJoinsPairsWithThreeSpaces(t *testing.T) {
	got := strings.TrimRight(ansi.Strip(statusBar(60, "↑↓", "move", "enter", "run", "q", "quit")), " ")

	if got != " ↑↓ move   enter run   q quit" {
		t.Errorf("status bar = %q", got)
	}
}

// C7, kills K2 (statusBar's fit check `> room` -> `>= room`, dropping a pair that fits
// exactly): " a one   b two" is exactly 14 columns.
func TestC7LastPairIsKeptWhenItFitsExactly(t *testing.T) {
	bar := statusBar(14, "a", "one", "b", "two")

	if got := ansi.Strip(bar); got != " a one   b two" {
		t.Errorf("status bar = %q, want both pairs", got)
	}
}

// C7, kills K2 (statusBar's fit check `> room` -> `>= room`): one column less drops the
// whole last pair.
func TestC7LastPairIsDroppedOneColumnShort(t *testing.T) {
	bar := statusBar(13, "a", "one", "b", "two")

	if got := ansi.Strip(bar); got != " a one       " {
		t.Errorf("status bar = %q, want only the first pair", got)
	}
	if w := ansi.StringWidth(bar); w != 13 {
		t.Errorf("status bar is %d columns, want 13", w)
	}
}

// C7
func TestC7AFirstPairThatDoesNotFitIsTruncated(t *testing.T) {
	bar := statusBar(8, "enter", "new instance", "q", "quit")
	got := ansi.Strip(bar)

	if ansi.StringWidth(bar) != 8 || !strings.HasPrefix(got, " ente") || !strings.HasSuffix(got, "…") {
		t.Errorf("status bar = %q, want 8 columns: the first pair truncated with …", got)
	}
}

// C7
func TestC7StatusBeforeLoadIsQuitOnly(t *testing.T) {
	m, _ := newTestModel(Config{}, 80, 24)

	if got := strings.Join(m.statusPairs(), ","); got != "q,quit" {
		t.Errorf("statusPairs = %s", got)
	}
}

// C7
func TestC7StatusAfterALoadErrorIsQuitOnly(t *testing.T) {
	m, _ := newTestModel(Config{}, 80, 24)
	m.Update(errMsg{errNoNet})

	if got := strings.Join(m.statusPairs(), ","); got != "q,quit" {
		t.Errorf("statusPairs = %s", got)
	}
}

// C7
func TestC7StatusOnThePageOfASingleGTNHInstance(t *testing.T) {
	m, _ := oneFull(t)

	got := strings.Join(m.statusPairs(), ",")

	if got != "↑↓,move,enter,run,n,new instance,q,quit" {
		t.Errorf("statusPairs = %s", got)
	}
}

// C7
func TestC7StatusOnASettingsOrLauncherRowSaysEdit(t *testing.T) {
	for _, id := range []string{"memory", "mods", "after", "prism"} {
		t.Run(id, func(t *testing.T) {
			m, _ := oneFull(t)
			m.row = indexOf(rowIDs(m), id)

			got := strings.Join(m.statusPairs(), ",")

			if got != "↑↓,move,enter,edit,n,new instance,q,quit" {
				t.Errorf("statusPairs = %s", got)
			}
		})
	}
}

// C7
func TestC7StatusOnThePageWithSidebarAndHiddenInstances(t *testing.T) {
	m, _ := mixedModel(t)
	m.focus = focusPage

	got := strings.Join(m.statusPairs(), ",")

	if got != "↑↓,move,enter,run,tab,instances,n,new instance,a,show all,q,quit" {
		t.Errorf("statusPairs = %s", got)
	}
}

// C7
func TestC7StatusInTheSidebar(t *testing.T) {
	m, _ := mixedModel(t)
	m.focus = focusSidebar

	got := strings.Join(m.statusPairs(), ",")

	if got != "↑↓,instance,enter,play,tab,page,n,new instance,a,show all,q,quit" {
		t.Errorf("statusPairs = %s", got)
	}
}

// C7: with showAll on, "a" stays offered.
func TestC7StatusKeepsShowAllWhileShowingAll(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24,
		makeInst(t, root, instSpec{name: "Alpha", gtnh: true, version: "2.8.4"}),
		makeInst(t, root, instSpec{name: "Bravo", gtnh: true, version: "2.8.4"}))
	m.showAll = true
	m.focus = focusSidebar

	got := strings.Join(m.statusPairs(), ",")

	if got != "↑↓,instance,enter,play,tab,page,n,new instance,a,show all,q,quit" {
		t.Errorf("statusPairs = %s", got)
	}
}

// C7: two GTNH instances and no other: no "a".
func TestC7StatusWithoutHiddenInstancesHasNoShowAll(t *testing.T) {
	m, _ := twoGTNH(t)
	m.focus = focusSidebar

	got := strings.Join(m.statusPairs(), ",")

	if got != "↑↓,instance,enter,play,tab,page,n,new instance,q,quit" {
		t.Errorf("statusPairs = %s", got)
	}
}

// C1/C7: the last line of the view is the status bar of statusPairs.
func TestC7TheViewEndsWithTheStatusBar(t *testing.T) {
	m, _ := mixedModel(t)
	m.focus = focusPage

	lines := screen(m)

	if got := lines[len(lines)-1]; got != " ↑↓ move   enter run   tab instances   n new instance   a show all   q quit" {
		t.Errorf("last line = %q", got)
	}
}
