package tui

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Tests for the decision-screen buttons (screens spec C1-C5). Clause numbers in the
// comments refer to the screens spec.

var (
	keyRight    = tea.KeyMsg{Type: tea.KeyRight}
	keyLeft     = tea.KeyMsg{Type: tea.KeyLeft}
	keyTab      = tea.KeyMsg{Type: tea.KeyTab}
	keyShiftTab = tea.KeyMsg{Type: tea.KeyShiftTab}
)

// ---- fixtures: one model per button screen of C1 ----

func confirmUpdateModel(t *testing.T) *model {
	t.Helper()
	m := routeModel(t)
	m.target = "2.9.0-RC-1"
	m.session = &update.Session{Plan: &update.Plan{}}
	m.screen = scConfirm
	return m
}

// confirmCreateModel is on the create confirm with a half-made folder (returned).
func confirmCreateModel(t *testing.T) (*model, *update.Creation) {
	t.Helper()
	m := routeModel(t)
	m.startCreate()
	m.target, m.newName = "2.8.4", "My Pack"
	c := halfMade(t)
	m.Update(createReady{c})
	if m.screen != scConfirm || !m.creating {
		t.Fatalf("setup: screen %d, creating %v after createReady; want scConfirm (%d), true", m.screen, m.creating, scConfirm)
	}
	return m, c
}

func restoreConfirmModel(t *testing.T) *model {
	t.Helper()
	m, _ := undoModel(t, termW, downgradeInfo())
	toConfirm(t, m)
	return m
}

func selfUpdatedModel(t *testing.T) *model {
	t.Helper()
	m := routeModel(t)
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	m.screen = scSelfUpdated
	return m
}

// nothingToUndoModel is on the "Nothing to undo" page of an instance without backups.
func nothingToUndoModel(t *testing.T) *model {
	t.Helper()
	m, _ := undoModel(t, termW)
	press(m, runes("b"))
	if m.screen != scBackups || len(m.backups) != 0 {
		t.Fatalf("setup: screen %d with %d backups after b; want scBackups (%d) with none", m.screen, len(m.backups), scBackups)
	}
	return m
}

// restoredPlayableModel is on the restored page with a fake Prism installed.
func restoredPlayableModel(t *testing.T) (*model, *fakePrism) {
	t.Helper()
	m := restoredModel(t, &update.RestoreResult{From: "2.8.4", To: "2.8.1", MovedBack: 1})
	fp := &fakePrism{}
	fp.install(m)
	return m, fp
}

type buttonScreen struct {
	name   string
	build  func(t *testing.T) *model
	labels []string
}

// buttonScreens are the screens of C1 with the labels they must show, in order.
func buttonScreens() []buttonScreen {
	return []buttonScreen{
		{"confirm update", confirmUpdateModel, []string{"Update now", "Back"}},
		{"confirm create", func(t *testing.T) *model { m, _ := confirmCreateModel(t); return m }, []string{"Create it", "Back"}},
		{"restore confirm", restoreConfirmModel, []string{"Undo now", "Back"}},
		{"done update", func(t *testing.T) *model { return doneUpdate(t).m }, []string{"Back", "Play now", "Quit"}},
		{"done create", func(t *testing.T) *model { h, _ := doneCreate(t); return h.m }, []string{"Back", "Play now", "Quit"}},
		{"restored", func(t *testing.T) *model { m, _ := restoredPlayableModel(t); return m }, []string{"Back", "Play now", "Quit"}},
		{"error with back", func(t *testing.T) *model { return errorModel(t, scPreparing) }, []string{"Back", "Quit"}},
		{"error without back", func(t *testing.T) *model { return errorModel(t, scApplying) }, []string{"Quit"}},
		{"self updated", selfUpdatedModel, []string{"Restart now", "Quit"}},
		{"playing", func(t *testing.T) *model { return playing(t).m }, []string{"Back", "Quit"}},
		{"nothing to undo", nothingToUndoModel, []string{"Back"}},
	}
}

var bracketed = regexp.MustCompile(`\[ [^\]]* \]`)

// ---- C1, C4 what the button screens show ----

func TestButtonScreensHaveTheirLabelsInOrder(t *testing.T) { // C1
	for _, c := range buttonScreens() {
		t.Run(c.name, func(t *testing.T) {
			m := c.build(t)
			if got := m.buttonLabels(); !slices.Equal(got, c.labels) {
				t.Errorf("buttonLabels() on %s = %q, want %q", c.name, got, c.labels)
			}
		})
	}
}

func TestButtonScreensShowEveryButtonAndNothingElseInBrackets(t *testing.T) { // C1, C4
	for _, c := range buttonScreens() {
		t.Run(c.name, func(t *testing.T) {
			m := c.build(t)
			var want []string
			for _, l := range m.buttonLabels() {
				want = append(want, "[ "+l+" ]")
			}
			got := bracketed.FindAllString(ansi.Strip(m.View()), -1)
			if len(want) != len(c.labels) || !slices.Equal(got, want) {
				t.Errorf("bracketed text in the %s view = %q, want %q (labels %q)", c.name, got, want, c.labels)
			}
		})
	}
}

func TestButtonScreenFootersAreButtonsOverKeyBarStartingWithChoose(t *testing.T) { // C4
	for _, c := range buttonScreens() {
		t.Run(c.name, func(t *testing.T) {
			m := c.build(t)
			labels := m.buttonLabels()
			_, footer, _ := m.page()
			lines := strings.Split(ansi.Strip(footer), "\n")
			if len(lines) != 2 || words(lines[0]) != words(buttons(labels, 0)) || !strings.HasPrefix(lines[1], "←→ choose    ") {
				t.Errorf("%s footer lines = %q; want 2 lines: the buttons %q, then the key bar starting \"←→ choose\"", c.name, lines, labels)
			}
		})
	}
}

func TestFooterOfEachButtonScreenReadsButtonsThenItsKeys(t *testing.T) { // C4
	cases := []struct {
		name  string
		build func(t *testing.T) *model
		want  string
	}{
		{"confirm update", confirmUpdateModel, "[ Update now ] [ Back ] ←→ choose enter update now esc back"},
		{"confirm create", func(t *testing.T) *model { m, _ := confirmCreateModel(t); return m },
			"[ Create it ] [ Back ] ←→ choose enter create it esc back"},
		{"restore confirm", restoreConfirmModel, "[ Undo now ] [ Back ] ←→ choose enter undo now esc back"},
		{"done update", func(t *testing.T) *model { return doneUpdate(t).m },
			"[ Back ] [ Play now ] [ Quit ] ←→ choose enter back p play now q quit"},
		{"done create", func(t *testing.T) *model { h, _ := doneCreate(t); return h.m },
			"[ Back ] [ Play now ] [ Quit ] ←→ choose enter back p play now q quit"},
		{"restored", func(t *testing.T) *model { m, _ := restoredPlayableModel(t); return m },
			"[ Back ] [ Play now ] [ Quit ] ←→ choose enter back p play now q quit"},
		{"error with back", func(t *testing.T) *model { return errorModel(t, scPreparing) },
			"[ Back ] [ Quit ] ←→ choose enter back q quit"},
		{"error without back", func(t *testing.T) *model { return errorModel(t, scApplying) },
			"[ Quit ] ←→ choose enter quit"},
		{"self updated", selfUpdatedModel, "[ Restart now ] [ Quit ] ←→ choose enter restart now q quit"},
		{"playing", func(t *testing.T) *model { return playing(t).m }, "[ Back ] [ Quit ] ←→ choose enter back q quit"},
		{"nothing to undo", nothingToUndoModel, "[ Back ] ←→ choose esc back"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.build(t)
			if m.buttonLabels() == nil {
				t.Fatalf("%s has no buttons, want some", c.name)
			}
			if got := pageFooter(m); got != c.want {
				t.Errorf("%s footer reads %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func TestButtonFooterIsButtonsThenChooseAndThePairs(t *testing.T) { // C4
	m := confirmUpdateModel(t)
	m.btn = 1
	want := buttons([]string{"Update now", "Back"}, 1) + "\n" + hint("←→", "choose", "enter", "update now", "esc", "back")
	if got := m.buttonFooter("enter", "update now", "esc", "back"); got != want {
		t.Errorf("buttonFooter on confirm with btn 1 = %q, want %q", ansi.Strip(got), ansi.Strip(want))
	}
}

func TestButtonFooterWithoutButtonsIsJustTheHint(t *testing.T) { // C4
	m := routeModel(t)
	m.askServerMods(true)
	want := hint("enter", "continue", "esc", "back")
	if got := m.buttonFooter("enter", "continue", "esc", "back"); got != want {
		t.Errorf("buttonFooter on server mods = %q, want hint(...) %q", ansi.Strip(got), ansi.Strip(want))
	}
}

func TestSelectedButtonIsTheHighlightedOne(t *testing.T) { // C2, C4
	withTrueColor(t)
	m := confirmUpdateModel(t)
	if m.buttonLabels() == nil {
		t.Fatal("confirm has no buttons, want Update now and Back")
	}
	press(m, keyRight)
	_, footer, _ := m.page()
	row := strings.Split(footer, "\n")[0]
	sel := strings.Index(row, bgAccent)
	if strings.Count(row, bgAccent) != 1 || sel < strings.Index(row, "Update now") || sel > strings.Index(row, "Back") {
		t.Errorf("button row after right = %q; want only Back highlighted", row)
	}
}

// ---- C1 screens without buttons ----

func TestScreensWithoutButtonsHaveNoButtonLabels(t *testing.T) { // C1
	names := []string{"home", "installed", "target 30 releases", "settings", "backups list", "conflicts", "resolve",
		"preparing", "preparing cancelling", "applying", "server mods", "name", "launching", "self update", "loading"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			m := chromeScreen(t, name, termW, termH)
			if got := m.buttonLabels(); got != nil {
				t.Errorf("buttonLabels() on %s = %q, want nil", name, got)
			}
			if _, footer, _ := m.page(); strings.Contains(ansi.Strip(footer), "←→") {
				t.Errorf("%s footer %q offers ←→ choose without buttons", name, ansi.Strip(footer))
			}
		})
	}
}

func TestBackupsListWithOneBackupHasNoButtons(t *testing.T) { // C1
	m, _ := undoModel(t, termW, downgradeInfo())
	press(m, runes("b"))
	if got := m.buttonLabels(); m.screen != scBackups || !m.isListScreen() || got != nil {
		t.Errorf("backup list: screen %d, list %v, buttonLabels() %q; want scBackups list, nil", m.screen, m.isListScreen(), got)
	}
}

func TestArrowsStillMoveTheCursorOfATextField(t *testing.T) { // C2: only screens with buttons take ←→
	m := routeModel(t)
	m.askServerMods(true)
	if got := m.buttonLabels(); got != nil {
		t.Fatalf("buttonLabels() on server mods = %q, want nil", got)
	}
	press(m, runes("a"), runes("b"), keyLeft, runes("c"), keyRight, runes("d"))
	if m.input.Value() != "acbd" || m.btn != 0 || m.screen != scServerMods {
		t.Errorf("a b ← c → d on server mods: value %q, btn %d, screen %d; want \"acbd\", 0, scServerMods", m.input.Value(), m.btn, m.screen)
	}
}

// ---- C2 moving the selection ----

func TestRightAndTabMoveToTheNextButtonAndStopAtTheLast(t *testing.T) { // C2, K1
	for _, c := range buttonScreens() {
		for _, k := range []tea.KeyMsg{keyRight, keyTab} {
			t.Run(c.name+" "+k.String(), func(t *testing.T) {
				m := c.build(t)
				n := len(m.buttonLabels())
				screen := m.screen
				for i := range n + 1 {
					cmd := press(m, k)
					if want := min(i+1, n-1); m.btn != want || m.screen != screen || cmd != nil {
						t.Fatalf("%s press %d on %s: btn %d, screen %d, cmd nil %v; want btn %d, screen %d, nil cmd",
							k, i+1, c.name, m.btn, m.screen, cmd == nil, want, screen)
					}
				}
			})
		}
	}
}

func TestLeftAndShiftTabMoveToThePreviousButtonAndStopAtTheFirst(t *testing.T) { // C2
	for _, c := range buttonScreens() {
		for _, k := range []tea.KeyMsg{keyLeft, keyShiftTab} {
			t.Run(c.name+" "+k.String(), func(t *testing.T) {
				m := c.build(t)
				n := len(m.buttonLabels())
				screen := m.screen
				for range n - 1 {
					press(m, keyRight)
				}
				for i := range n {
					cmd := press(m, k)
					if want := max(n-2-i, 0); m.btn != want || m.screen != screen || cmd != nil {
						t.Fatalf("%s press %d on %s: btn %d, screen %d, cmd nil %v; want btn %d, screen %d, nil cmd",
							k, i+1, c.name, m.btn, m.screen, cmd == nil, want, screen)
					}
				}
			})
		}
	}
}

func TestKeyButtonsHandlesOnlyMovesAndEnter(t *testing.T) { // C2
	cases := []struct {
		key     tea.KeyMsg
		handled bool
		btn     int
	}{
		{keyRight, true, 1},
		{keyTab, true, 1},
		{keyLeft, true, 0},
		{keyShiftTab, true, 0},
		{runes("y"), false, 0},
		{runes("n"), false, 0},
		{runes("q"), false, 0},
		{keyEsc, false, 0},
		{keyDown, false, 0},
		{keyEnd, false, 0},
		{keyPgDn, false, 0},
	}
	for _, c := range cases {
		t.Run(c.key.String(), func(t *testing.T) {
			m := confirmUpdateModel(t)
			handled, _, cmd := m.keyButtons(c.key)
			if handled != c.handled || cmd != nil || m.btn != c.btn || m.screen != scConfirm || m.session == nil {
				t.Errorf("keyButtons(%s) on confirm: handled %v, cmd nil %v, btn %d, screen %d, session nil %v; want %v, nil, %d, scConfirm, kept",
					c.key, handled, cmd == nil, m.btn, m.screen, m.session == nil, c.handled, c.btn)
			}
		})
	}
}

func TestKeyButtonsEnterActivatesTheSelectedButton(t *testing.T) { // C2, C3
	m := confirmUpdateModel(t)
	handled, _, cmd := m.keyButtons(keyEnter)
	if !handled || cmd == nil || m.screen != scApplying {
		t.Errorf("keyButtons(enter) on confirm: handled %v, cmd nil %v, screen %d; want true, a cmd, scApplying (%d)", handled, cmd == nil, m.screen, scApplying)
	}
}

func TestScrollKeysDoNotMoveTheSelection(t *testing.T) { // C2
	m := confirmModel(t)
	if m.buttonLabels() == nil {
		t.Fatal("confirm has no buttons, want Update now and Back")
	}
	press(m, keyRight, keyDown, keyDown, keyPgDn, keyUp)
	if m.btn != 1 || m.scroll == 0 || m.screen != scConfirm {
		t.Errorf("right then scroll keys on confirm: btn %d, scroll %d, screen %d; want 1, scrolled, scConfirm", m.btn, m.scroll, m.screen)
	}
}

// ---- C3 activating a button ----

// choose selects button i with right presses, then presses enter.
func choose(m *model, i int) tea.Cmd {
	for range i {
		press(m, keyRight)
	}
	return press(m, keyEnter)
}

func TestEnterActivatesTheSelectedButton(t *testing.T) { // C3
	type check func(t *testing.T, m *model, cmd tea.Cmd)
	cases := []struct {
		name  string
		label string
		index int
		setup func(t *testing.T) (*model, check)
	}{
		{"confirm update", "Update now", 0, func(t *testing.T) (*model, check) {
			return confirmUpdateModel(t), func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scApplying || cmd == nil {
					t.Errorf("Update now: screen %d, cmd nil %v; want scApplying (%d), a cmd", m.screen, cmd == nil, scApplying)
				}
			}
		}},
		{"confirm update", "Back", 1, func(t *testing.T) (*model, check) {
			return confirmUpdateModel(t), func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scTarget || m.session != nil || m.quitting || isQuit(cmd) {
					t.Errorf("Back on confirm: screen %d, session nil %v, quitting %v; want scTarget (%d), nil, false", m.screen, m.session == nil, m.quitting, scTarget)
				}
			}
		}},
		{"confirm create", "Create it", 0, func(t *testing.T) (*model, check) {
			m, _ := confirmCreateModel(t)
			return m, func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scApplying || cmd == nil {
					t.Errorf("Create it: screen %d, cmd nil %v; want scApplying (%d), a cmd", m.screen, cmd == nil, scApplying)
				}
			}
		}},
		{"confirm create", "Back", 1, func(t *testing.T) (*model, check) {
			m, c := confirmCreateModel(t)
			return m, func(t *testing.T, m *model, cmd tea.Cmd) {
				_, err := os.Stat(c.Dir)
				if m.screen != scTarget || m.creation != nil || !os.IsNotExist(err) || m.quitting {
					t.Errorf("Back on create confirm: screen %d, creation nil %v, folder stat err %v, quitting %v; want scTarget (%d), nil, removed, false",
						m.screen, m.creation == nil, err, m.quitting, scTarget)
				}
			}
		}},
		{"restore confirm", "Undo now", 0, func(t *testing.T) (*model, check) {
			return restoreConfirmModel(t), func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scRestoring || cmd == nil {
					t.Errorf("Undo now: screen %d, cmd nil %v; want scRestoring (%d), a cmd", m.screen, cmd == nil, scRestoring)
				}
			}
		}},
		{"restore confirm", "Back", 1, func(t *testing.T) (*model, check) {
			return restoreConfirmModel(t), func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scBackups || !m.isListScreen() || len(m.list.Items()) != 1 || m.quitting {
					t.Errorf("Back on restore confirm: screen %d, list %v, %d items, quitting %v; want the backup list", m.screen, m.isListScreen(), len(m.list.Items()), m.quitting)
				}
			}
		}},
		{"done update", "Back", 0, func(t *testing.T) (*model, check) {
			h := doneUpdate(t)
			return h.m, func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scLoading || m.session != nil || m.result != nil || m.quitting {
					t.Errorf("Back on done: screen %d, session nil %v, result nil %v, quitting %v; want scLoading (%d), nil, nil, false",
						m.screen, m.session == nil, m.result == nil, m.quitting, scLoading)
				}
				msgOf[reloadedMsg](t, cmd)
			}
		}},
		{"done update", "Play now", 1, func(t *testing.T) (*model, check) {
			h := doneUpdate(t)
			return h.m, func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scLaunching || cmd == nil {
					t.Fatalf("Play now on done: screen %d, cmd nil %v; want scLaunching (%d), a cmd", m.screen, cmd == nil, scLaunching)
				}
				msgOf[launchedMsg](t, cmd)
				if len(h.prism.launches) != 1 || h.prism.launches[0].inst.Dir != h.older.Dir || h.prism.launches[0].server != "" {
					t.Errorf("Play now on done launched %+v, want Older once without a server", h.prism.launches)
				}
			}
		}},
		{"done update", "Quit", 2, func(t *testing.T) (*model, check) {
			return doneUpdate(t).m, func(t *testing.T, m *model, cmd tea.Cmd) {
				if !m.quitting || !isQuit(cmd) {
					t.Errorf("Quit on done: quitting %v, quit cmd %v; want true, true", m.quitting, isQuit(cmd))
				}
			}
		}},
		{"done create", "Back", 0, func(t *testing.T) (*model, check) {
			h, made := doneCreate(t)
			return h.m, func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scLoading || m.inst.Dir != made.Dir || m.creating || m.quitting {
					t.Errorf("Back on created: screen %d, inst %q, creating %v, quitting %v; want scLoading (%d), %q, false, false",
						m.screen, m.inst.Dir, m.creating, m.quitting, scLoading, made.Dir)
				}
				msgOf[reloadedMsg](t, cmd)
			}
		}},
		{"done create", "Play now", 1, func(t *testing.T) (*model, check) {
			h, made := doneCreate(t)
			return h.m, func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scLaunching || cmd == nil || m.inst.Dir != made.Dir {
					t.Fatalf("Play now on created: screen %d, cmd nil %v, inst %q; want scLaunching (%d), a cmd, %q", m.screen, cmd == nil, m.inst.Dir, scLaunching, made.Dir)
				}
				msgOf[launchedMsg](t, cmd)
				if len(h.prism.launches) != 1 || h.prism.launches[0].inst.Dir != made.Dir {
					t.Errorf("Play now on created launched %+v, want Made once", h.prism.launches)
				}
			}
		}},
		{"done create", "Quit", 2, func(t *testing.T) (*model, check) {
			h, _ := doneCreate(t)
			return h.m, func(t *testing.T, m *model, cmd tea.Cmd) {
				if !m.quitting || !isQuit(cmd) {
					t.Errorf("Quit on created: quitting %v, quit cmd %v; want true, true", m.quitting, isQuit(cmd))
				}
			}
		}},
		{"restored", "Back", 0, func(t *testing.T) (*model, check) {
			m, _ := restoredPlayableModel(t)
			return m, func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scLoading || m.restored != nil || m.quitting {
					t.Errorf("Back on restored: screen %d, restored nil %v, quitting %v; want scLoading (%d), nil, false", m.screen, m.restored == nil, m.quitting, scLoading)
				}
				msgOf[reloadedMsg](t, cmd)
			}
		}},
		{"restored", "Play now", 1, func(t *testing.T) (*model, check) {
			m, fp := restoredPlayableModel(t)
			return m, func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scLaunching || cmd == nil {
					t.Fatalf("Play now on restored: screen %d, cmd nil %v; want scLaunching (%d), a cmd", m.screen, cmd == nil, scLaunching)
				}
				msgOf[launchedMsg](t, cmd)
				if len(fp.launches) != 1 || fp.launches[0].inst.Dir != m.inst.Dir {
					t.Errorf("Play now on restored launched %+v, want Pack once", fp.launches)
				}
			}
		}},
		{"restored", "Quit", 2, func(t *testing.T) (*model, check) {
			m, _ := restoredPlayableModel(t)
			return m, func(t *testing.T, m *model, cmd tea.Cmd) {
				if !m.quitting || !isQuit(cmd) {
					t.Errorf("Quit on restored: quitting %v, quit cmd %v; want true, true", m.quitting, isQuit(cmd))
				}
			}
		}},
		{"error with back", "Back", 0, func(t *testing.T) (*model, check) {
			return errorModel(t, scPreparing), func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scTarget || m.quitting || isQuit(cmd) {
					t.Errorf("Back on error: screen %d, quitting %v, quit cmd %v; want scTarget (%d), false, false", m.screen, m.quitting, isQuit(cmd), scTarget)
				}
			}
		}},
		{"error with back", "Quit", 1, func(t *testing.T) (*model, check) {
			return errorModel(t, scPreparing), func(t *testing.T, m *model, cmd tea.Cmd) {
				if !m.quitting || !isQuit(cmd) {
					t.Errorf("Quit on error: quitting %v, quit cmd %v; want true, true", m.quitting, isQuit(cmd))
				}
			}
		}},
		{"error without back", "Quit", 0, func(t *testing.T) (*model, check) {
			return errorModel(t, scApplying), func(t *testing.T, m *model, cmd tea.Cmd) {
				if !m.quitting || !isQuit(cmd) {
					t.Errorf("Quit on error without back: quitting %v, quit cmd %v; want true, true", m.quitting, isQuit(cmd))
				}
			}
		}},
		{"self updated", "Restart now", 0, func(t *testing.T) (*model, check) {
			return selfUpdatedModel(t), func(t *testing.T, m *model, cmd tea.Cmd) {
				if !m.restart || !m.quitting || !isQuit(cmd) {
					t.Errorf("Restart now: restart %v, quitting %v, quit cmd %v; want all true", m.restart, m.quitting, isQuit(cmd))
				}
			}
		}},
		{"self updated", "Quit", 1, func(t *testing.T) (*model, check) {
			return selfUpdatedModel(t), func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.restart || !m.quitting || !isQuit(cmd) {
					t.Errorf("Quit on self-updated: restart %v, quitting %v, quit cmd %v; want false, true, true", m.restart, m.quitting, isQuit(cmd))
				}
			}
		}},
		{"playing", "Back", 0, func(t *testing.T) (*model, check) {
			return playing(t).m, func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scLoading || m.quitting {
					t.Errorf("Back on playing: screen %d, quitting %v; want scLoading (%d), false", m.screen, m.quitting, scLoading)
				}
				msgOf[reloadedMsg](t, cmd)
			}
		}},
		{"playing", "Quit", 1, func(t *testing.T) (*model, check) {
			return playing(t).m, func(t *testing.T, m *model, cmd tea.Cmd) {
				if !m.quitting || !isQuit(cmd) {
					t.Errorf("Quit on playing: quitting %v, quit cmd %v; want true, true", m.quitting, isQuit(cmd))
				}
			}
		}},
		{"nothing to undo", "Back", 0, func(t *testing.T) (*model, check) {
			return nothingToUndoModel(t), func(t *testing.T, m *model, cmd tea.Cmd) {
				if m.screen != scHome || m.quitting || isQuit(cmd) {
					t.Errorf("Back on nothing to undo: screen %d, quitting %v, quit cmd %v; want scHome (%d), false, false", m.screen, m.quitting, isQuit(cmd), scHome)
				}
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name+" "+c.label, func(t *testing.T) {
			m, check := c.setup(t)
			if labels := m.buttonLabels(); c.index >= len(labels) || labels[c.index] != c.label {
				t.Fatalf("buttonLabels() on %s = %q, want %q at %d", c.name, labels, c.label, c.index)
			}
			check(t, m, choose(m, c.index))
		})
	}
}

func TestEnterOnAButtonScreenDoesWhatTheDefaultButtonDoes(t *testing.T) { // C3: enter with the default selection
	m := doneUpdate(t).m
	if m.buttonLabels() == nil || m.btn != 0 {
		t.Fatalf("done: buttonLabels() %q, btn %d; want buttons with the first selected", m.buttonLabels(), m.btn)
	}
	cmd := press(m, keyEnter)
	if m.screen != scLoading || cmd == nil || m.quitting {
		t.Errorf("enter on done: screen %d, cmd nil %v, quitting %v; want scLoading (%d), a cmd, false", m.screen, cmd == nil, m.quitting, scLoading)
	}
}

func TestEscKeepsItsMeaningWhateverIsSelected(t *testing.T) { // C3: esc keeps its HEAD behaviour
	cases := []struct {
		name  string
		build func(t *testing.T) *model
		want  screen
		quit  bool
	}{
		{"confirm update goes to versions", confirmUpdateModel, scTarget, false},
		{"done goes to loading", func(t *testing.T) *model { return doneUpdate(t).m }, scLoading, false},
		{"self updated quits", selfUpdatedModel, scSelfUpdated, true},
		{"nothing to undo goes home", nothingToUndoModel, scHome, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.build(t)
			n := len(m.buttonLabels())
			for range n - 1 {
				press(m, keyRight)
			}
			cmd := press(m, keyEsc)
			if m.screen != c.want || m.quitting != c.quit || isQuit(cmd) != c.quit || m.restart {
				t.Errorf("esc on %s with the last of %d buttons selected: screen %d, quitting %v, quit cmd %v, restart %v; want %d, %v, %v, false",
					c.name, n, m.screen, m.quitting, isQuit(cmd), m.restart, c.want, c.quit, c.quit)
			}
		})
	}
}

// ---- C5 buttons never scroll away ----

func TestButtonsStayOnScreenWhenTheBodyScrollsToTheEnd(t *testing.T) { // C5
	m := confirmModel(t)
	if m.buttonLabels() == nil {
		t.Fatal("confirm has no buttons, want Update now and Back")
	}
	press(m, keyEnd)
	v := ansi.Strip(m.View())
	if m.scroll == 0 || !strings.Contains(v, "[ Update now ]") || !strings.Contains(v, "[ Back ]") {
		t.Errorf("confirm scrolled to %d: view lacks the buttons:\n%s", m.scroll, v)
	}
}
