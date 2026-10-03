package tui

import (
	"strings"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
)

func TestDefaultTargetNeverDowngrades(t *testing.T) {
	m, err := manifest.Parse([]byte(`{
	  "2.8.4": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/a.zip"}},
	  "2.9.0-RC-1": {"title":"Beta release","releaseDate":"2026/09/24","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/b.zip"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for installed, want := range map[string]string{
		"2.8.1":      "2.8.4",      // behind the stable: take the stable
		"2.8.4":      "2.8.4",      // on the stable: stay (repair / custom-mods sync)
		"2.9.0-RC-1": "2.9.0-RC-1", // ahead of the stable: never preselect a downgrade
	} {
		if got := defaultTarget(m, installed); got != want {
			t.Errorf("installed %s: default %s, want %s", installed, got, want)
		}
	}
}

// oneFull is a loaded 80x24 model with one full GTNH instance (13 rows), page focused.
func oneFull(t *testing.T) (*model, *fakes) {
	root := t.TempDir()
	m, f := loadedModel(root, 80, 24, makeInst(t, root, fullSpec("Home")))
	m.focus = focusPage
	return m, f
}

// twoGTNH is a loaded model with two GTNH instances (sidebar shown).
func twoGTNH(t *testing.T) (*model, *fakes) {
	root := t.TempDir()
	m, f := loadedModel(root, 80, 24,
		makeInst(t, root, fullSpec("Home")),
		makeInst(t, root, instSpec{name: "Second", gtnh: true, version: "2.8.4"}))
	return m, f
}

// C5
func TestC5DownAndUpMoveTheRowOnThePage(t *testing.T) {
	m, _ := oneFull(t)

	press(m, "down", "down")
	afterDown := m.row
	press(m, "up")

	if afterDown != 2 || m.row != 1 {
		t.Errorf("rows after down,down = %d, after up = %d; want 2, 1", afterDown, m.row)
	}
}

// C5
func TestC5RowMovesAreClampedWithoutWrap(t *testing.T) {
	m, _ := oneFull(t)
	last := len(m.rows()) - 1

	press(m, "up")
	atTop := m.row
	m.row = last
	press(m, "down")

	if atTop != 0 || m.row != last {
		t.Errorf("up at top -> %d, down at bottom -> %d; want 0, %d", atTop, m.row, last)
	}
}

// C5
func TestC5SidebarMovesSelectAndResetThePage(t *testing.T) {
	m, _ := twoGTNH(t)
	m.focus = focusPage
	press(m, "down", "down", "down")
	m.focus = focusSidebar

	press(m, "down")
	in, _ := m.current()

	if in.Name != "Second" || m.sel != 1 {
		t.Errorf("selected %q (sel %d), want Second (1)", in.Name, m.sel)
	}
	if m.row != 0 || m.pageScroll != 0 {
		t.Errorf("row %d scroll %d after moving instance, want 0 0", m.row, m.pageScroll)
	}
}

// C5
func TestC5SidebarMovesAreClamped(t *testing.T) {
	m, _ := twoGTNH(t)
	m.focus = focusSidebar

	press(m, "up")
	atTop := m.sel
	press(m, "down", "down", "down")

	if atTop != 0 || m.sel != 1 {
		t.Errorf("sel after up at top %d, after downs %d; want 0, 1", atTop, m.sel)
	}
}

// C5
func TestC5TabAndArrowsToggleFocusWithASidebar(t *testing.T) {
	cases := []string{"tab", "shift+tab", "left", "right"}
	for _, k := range cases {
		t.Run(k, func(t *testing.T) {
			m, _ := twoGTNH(t)
			m.focus = focusSidebar

			press(m, k)
			first := m.focus
			press(m, k)

			if first != focusPage || m.focus != focusSidebar {
				t.Errorf("%s: focus %v then %v, want page then sidebar", k, first, m.focus)
			}
		})
	}
}

// C5
func TestC5TabDoesNothingWithASingleInstance(t *testing.T) {
	m, _ := oneFull(t)

	press(m, "tab")

	if m.focus != focusPage {
		t.Errorf("focus changed to %v without a sidebar", m.focus)
	}
}

// C5
func TestC5EnterOnThePagePlayRowLaunches(t *testing.T) {
	m, f := oneFull(t)

	runCmd(press(m, "enter"))

	if len(f.launches) != 1 || f.launches[0].server != "" {
		t.Errorf("launches %+v, want one plain play", f.launches)
	}
}

// C5
func TestC5EnterInTheSidebarPlays(t *testing.T) {
	m, f := twoGTNH(t)
	m.focus = focusSidebar
	press(m, "down")

	runCmd(press(m, "enter"))

	if len(f.launches) != 1 || f.launches[0].inst.Name != "Second" {
		t.Errorf("launches %+v, want one of Second", f.launches)
	}
}

// C5
func TestC5PPlaysFromAnyRow(t *testing.T) {
	m, f := oneFull(t)
	m.row = 7

	runCmd(press(m, "p"))

	if len(f.launches) != 1 || f.launches[0].inst.Name != "Home" {
		t.Errorf("launches %+v, want one of Home", f.launches)
	}
}

// C5
func TestC5JJoinsTheSavedServer(t *testing.T) {
	m, f := oneFull(t)

	runCmd(press(m, "j"))

	if len(f.launches) != 1 || f.launches[0].server != "play.example.org" {
		t.Errorf("launches %+v, want one joining play.example.org", f.launches)
	}
}

// C5
func TestC5JWithoutAServerDoesNotLaunch(t *testing.T) {
	root := t.TempDir()
	m, f := loadedModel(root, 80, 24, makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4"}))

	runCmd(press(m, "j"))

	if len(f.launches) != 0 {
		t.Errorf("j launched without a server: %+v", f.launches)
	}
}

// C5
func TestC5SJumpsToTheFirstSettingsRow(t *testing.T) {
	m, _ := twoGTNH(t)
	m.focus = focusSidebar

	press(m, "s")

	if m.focus != focusPage || m.rows()[m.row].id != "memory" {
		t.Errorf("after s: focus %v row %d, want page on memory", m.focus, m.row)
	}
}

// C5
func TestC5SWithUnreadableSettingsJumpsToTheLauncherRows(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24, makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4", noCfg: true}))
	m.focus = focusSidebar

	press(m, "s")

	if m.focus != focusPage || m.rows()[m.row].id != "after" {
		t.Errorf("after s: focus %v row %d, want page on after", m.focus, m.row)
	}
}

// C4/C5: the undo, settings and launcher rows do nothing yet (update and versions are
// the update flow's, see flow_update_test.go).
func TestC5RowsOfLaterSlicesChangeNothing(t *testing.T) {
	for _, id := range []string{"undo", "memory", "server", "after", "prism"} {
		t.Run(id, func(t *testing.T) {
			m, f := oneFull(t)
			m.row = indexOf(rowIDs(m), id)
			before := strings.Join(screen(m), "\n")

			runCmd(press(m, "enter"))

			if after := strings.Join(screen(m), "\n"); after != before || m.dialog != nil || len(f.launches) != 0 {
				t.Errorf("enter on %s changed the view:\n%s\n---\n%s", id, before, after)
			}
		})
	}
}

// C5
func TestC5ShortcutsOfLaterSlicesChangeNothing(t *testing.T) {
	for _, k := range []string{"b", "n", "v", "esc"} {
		t.Run(k, func(t *testing.T) {
			m, _ := oneFull(t)
			m.Update(newerMsg{r: &selfupdate.Release{Version: "9.9.9"}})
			before := strings.Join(screen(m), "\n")

			runCmd(press(m, k))

			if after := strings.Join(screen(m), "\n"); after != before || m.dialog != nil || m.quitting {
				t.Errorf("%s changed the view:\n%s\n---\n%s", k, before, after)
			}
		})
	}
}

// C5
func TestC5ATogglesShowAll(t *testing.T) {
	m, _ := oneFull(t)

	press(m, "a")
	first := m.showAll
	press(m, "a")

	if !first || m.showAll {
		t.Errorf("showAll after a: %v, after a again: %v; want true, false", first, m.showAll)
	}
}

// C5
func TestC5QAndCtrlCQuit(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		t.Run(k, func(t *testing.T) {
			m, _ := oneFull(t)

			cmd := press(m, k)

			if !m.quitting || !hasQuit(runCmd(cmd)) {
				t.Errorf("%s: quitting %v, want true and tea.Quit", k, m.quitting)
			}
		})
	}
}

// C3: visible keeps the order of m.insts and hides non-GTNH instances unless showAll.
func TestC3VisibleIsGTNHOnlyUnlessShowAll(t *testing.T) {
	root := t.TempDir()
	insts := []prism.Instance{
		makeInst(t, root, instSpec{name: "G1", gtnh: true}),
		makeInst(t, root, instSpec{name: "N1"}),
		makeInst(t, root, instSpec{name: "G2", gtnh: true}),
	}
	m, _ := loadedModel(root, 80, 24, insts...)

	gtnhOnly := names(m.visible())
	m.showAll = true
	all := names(m.visible())

	if gtnhOnly != "G1,G2" || all != "G1,N1,G2" {
		t.Errorf("visible = %s, with showAll %s; want G1,G2 and G1,N1,G2", gtnhOnly, all)
	}
}

func names(insts []prism.Instance) string {
	var out []string
	for _, in := range insts {
		out = append(out, in.Name)
	}
	return strings.Join(out, ",")
}

// C3
func TestC3ShowAllKeepsTheSelectedFolder(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24,
		makeInst(t, root, instSpec{name: "G1", gtnh: true}),
		makeInst(t, root, instSpec{name: "N1"}),
		makeInst(t, root, instSpec{name: "G2", gtnh: true}))
	m.focus = focusSidebar
	press(m, "down")

	press(m, "a")
	in, _ := m.current()

	if in.Name != "G2" || m.sel != 2 {
		t.Errorf("after a: current %q sel %d, want G2 at 2", in.Name, m.sel)
	}
}

// C3
func TestC3HidingTheSelectedInstanceSelectsTheFirst(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24,
		makeInst(t, root, instSpec{name: "G1", gtnh: true}),
		makeInst(t, root, instSpec{name: "G2", gtnh: true}),
		makeInst(t, root, instSpec{name: "N1"}))
	press(m, "a")
	m.focus = focusSidebar
	press(m, "down", "down")

	press(m, "a")
	in, _ := m.current()

	if in.Name != "G1" || m.sel != 0 {
		t.Errorf("after hiding N1: current %q sel %d, want G1 at 0", in.Name, m.sel)
	}
}
