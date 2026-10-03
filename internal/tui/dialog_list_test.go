package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// spaceKey is the space bar as bubbletea delivers it.
var spaceKey = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}

// boxText is the open dialog's inner lines: borders and the one-column padding removed,
// trailing spaces trimmed (the title border and the bottom border are dropped).
func boxText(m *model) []string {
	lines := trimLines(m.dialogView())
	var out []string
	for _, l := range lines[1 : len(lines)-1] {
		l = strings.TrimPrefix(strings.TrimSuffix(l, "│"), "│ ")
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

func boxWidth(m *model) int {
	return ansi.StringWidth(strings.Split(m.dialogView(), "\n")[0])
}

func threeItems() []ditem {
	return []ditem{
		{title: "Alpha", desc: "first one", key: "a"},
		{title: "Bravo", desc: "second", key: "b"},
		{title: "Charlie", key: "c"},
	}
}

// numbered is n items "Item 00".. without descriptions, keyed "k00"...
func numbered(n int) []ditem {
	out := make([]ditem, n)
	for i := range out {
		out[i] = ditem{title: fmt.Sprintf("Item %02d", i), key: fmt.Sprintf("k%02d", i)}
	}
	return out
}

// ---- C1 list dialogs ----

// C1
func TestC1OpenListPutsTheCursorOnTheSelectedKey(t *testing.T) {
	cases := []struct {
		selected string
		want     int
	}{{"a", 0}, {"b", 1}, {"c", 2}, {"", 0}, {"missing", 0}}
	for _, c := range cases {
		t.Run(c.selected, func(t *testing.T) {
			m, _ := oneFull(t)

			m.openList("Pick a pack", "", threeItems(), c.selected, nil)

			l := listOf(t, m)
			if l.cursor != c.want || len(m.dialog.buttons) != 0 || m.dialog.title != "Pick a pack" {
				t.Errorf("cursor %d buttons %q title %q, want %d, none, Pick a pack", l.cursor, m.dialog.buttons, m.dialog.title, c.want)
			}
		})
	}
}

// C1: intro, then the items; no blank line and no button row without buttons.
func TestC1ListBoxShowsTheIntroAndTheItems(t *testing.T) {
	m, _ := oneFull(t)

	m.openList("Pick a pack", "Pick one.", threeItems(), "b", nil)

	want := []string{"Pick one.", "  Alpha  first one", "▸ Bravo  second", "  Charlie"}
	if got := boxText(m); !eq(got, want) {
		t.Errorf("box\n%q\nwant\n%q", got, want)
	}
}

// C1
func TestC1AnEmptyIntroIsSkipped(t *testing.T) {
	m, _ := oneFull(t)

	m.openList("Pick a pack", "", threeItems(), "a", nil)

	want := []string{"▸ Alpha  first one", "  Bravo  second", "  Charlie"}
	if got := boxText(m); !eq(got, want) {
		t.Errorf("box\n%q\nwant\n%q", got, want)
	}
}

// C1: the note under the items, then (with buttons) a blank line and the buttons.
func TestC1NoteAndButtonsFollowTheItems(t *testing.T) {
	m, _ := oneFull(t)
	m.openList("Pick a pack", "", threeItems(), "a", nil)
	m.dialog.note = "A note."
	m.dialog.buttons = []string{"Done"}

	got := boxText(m)

	if len(got) != 6 || !eq(got[:5], []string{"▸ Alpha  first one", "  Bravo  second", "  Charlie", "A note.", ""}) {
		t.Fatalf("box %q", got)
	}
	if last := got[5]; strings.TrimSpace(last) != "[ Done ]" || !strings.HasPrefix(last, "   ") {
		t.Errorf("button row %q, want [ Done ] right-aligned", last)
	}
}

// C1
func TestC1ListWidthFitsTheLongestItem(t *testing.T) {
	cases := []struct {
		name  string
		items []ditem
		want  int
	}{
		{"short items get 50", threeItems(), 50},
		{"title and desc + 6", []ditem{{title: strings.Repeat("x", 40), desc: strings.Repeat("y", 20), key: "x"}}, 68},
		{"title only + 6", []ditem{{title: strings.Repeat("x", 55), key: "x"}}, 61},
		{"capped by the terminal", []ditem{{title: strings.Repeat("x", 100), key: "x"}}, 74},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := oneFull(t)

			m.openList("Pick", "", c.items, "", nil)

			if w := boxWidth(m); w != c.want {
				t.Errorf("width %d, want %d", w, c.want)
			}
		})
	}
}

// C1: the items take max(min(12, height-9), 3) lines.
func TestC1ListWindowFollowsTheTerminalHeight(t *testing.T) {
	cases := []struct{ height, rows int }{{24, 12}, {21, 12}, {20, 11}, {13, 4}, {12, 3}, {11, 3}, {10, 3}}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.height), func(t *testing.T) {
			m, _ := newTestModel(Config{}, 80, c.height)

			m.openList("Pick", "", numbered(20), "", nil)

			if got := boxText(m); len(got) != c.rows {
				t.Errorf("%d item lines at height %d, want %d: %q", len(got), c.height, c.rows, got)
			}
		})
	}
}

// C1
func TestC1ListWindowMarksWhatIsBelow(t *testing.T) {
	m, _ := oneFull(t)

	m.openList("Pick", "", numbered(20), "", nil)

	got := boxText(m)
	want := []string{"▸ Item 00", "  Item 01", "  Item 02", "  Item 03", "  Item 04", "  Item 05",
		"  Item 06", "  Item 07", "  Item 08", "  Item 09", "  Item 10"}
	if len(got) != 12 || !eq(got[:11], want) || strings.TrimSpace(got[11]) != "↓ 8 more" {
		t.Errorf("box %q", got)
	}
}

// C1: moving past the window scrolls just enough, off the markers.
func TestC1ListWindowScrollsWithTheCursor(t *testing.T) {
	m, _ := oneFull(t)
	m.openList("Pick", "", numbered(20), "", nil)

	for range 11 {
		press(m, "down")
	}

	got := boxText(m)
	want := []string{"  Item 02", "  Item 03", "  Item 04", "  Item 05", "  Item 06",
		"  Item 07", "  Item 08", "  Item 09", "  Item 10", "▸ Item 11"}
	if len(got) != 12 || strings.TrimSpace(got[0]) != "↑ 1 more" || !eq(got[1:11], want) || strings.TrimSpace(got[11]) != "↓ 7 more" {
		t.Errorf("box %q", got)
	}
}

// C1: at the end the offset stays put while the cursor moves inside the window.
func TestC1ListWindowKeepsItsOffset(t *testing.T) {
	m, _ := oneFull(t)
	m.openList("Pick", "", numbered(20), "", nil)
	for range 19 {
		press(m, "down")
	}

	press(m, "up")

	got := boxText(m)
	if len(got) != 12 || strings.TrimSpace(got[0]) != "↑ 8 more" || got[1] != "  Item 09" ||
		got[10] != "▸ Item 18" || got[11] != "  Item 19" {
		t.Errorf("box %q", got)
	}
}

// C1
func TestC1UpAndDownMoveTheCursorClamped(t *testing.T) {
	m, _ := oneFull(t)
	m.openList("Pick", "", threeItems(), "", nil)

	press(m, "down", "down", "down", "down")
	bottom := listOf(t, m).cursor
	press(m, "up")
	mid := listOf(t, m).cursor
	press(m, "up", "up", "up")

	if bottom != 2 || mid != 1 || listOf(t, m).cursor != 0 {
		t.Errorf("cursor %d, %d, %d; want 2, 1, 0", bottom, mid, listOf(t, m).cursor)
	}
}

// C1
func TestC1SpaceTogglesTheItemUnderTheCursor(t *testing.T) {
	m, _ := oneFull(t)
	m.openList("Pick", "", threeItems(), "", nil)
	var toggled []string
	listOf(t, m).toggle = func(_ *model, key string) { toggled = append(toggled, key) }

	press(m, "down")
	m.Update(spaceKey)

	if !eq(toggled, []string{"b"}) || m.dialog == nil {
		t.Errorf("toggled %q, want [b] with the dialog open", toggled)
	}
}

// C1
func TestC1SpaceWithoutAToggleDoesNothing(t *testing.T) {
	m, _ := oneFull(t)
	var picked []string
	m.openList("Pick", "", threeItems(), "b", func(_ *model, key string) tea.Cmd {
		picked = append(picked, key)
		return nil
	})

	m.Update(spaceKey)

	if m.dialog == nil || listOf(t, m).cursor != 1 || len(picked) != 0 {
		t.Errorf("dialog %v cursor %d picked %q, want unchanged", m.dialog != nil, listOf(t, m).cursor, picked)
	}
}

// C1
func TestC1EnterPicksTheItemUnderTheCursor(t *testing.T) {
	m, _ := oneFull(t)
	var picked []string
	m.openList("Pick", "", threeItems(), "", func(_ *model, key string) tea.Cmd {
		picked = append(picked, key)
		return nil
	})

	press(m, "down", "down", "enter")

	if !eq(picked, []string{"c"}) {
		t.Errorf("picked %q, want [c]", picked)
	}
}

// C1: without onPick, enter presses the selected button.
func TestC1EnterWithoutOnPickPressesTheButton(t *testing.T) {
	m, _ := oneFull(t)
	m.openList("Pick", "", threeItems(), "", nil)
	var labels []string
	m.dialog.buttons = []string{"Back", "Done"}
	m.dialog.onButton = func(_ *model, label string) tea.Cmd {
		labels = append(labels, label)
		return nil
	}

	press(m, "right", "enter")

	if !eq(labels, []string{"Done"}) {
		t.Errorf("labels %q, want [Done]", labels)
	}
}

// C1
func TestC1EnterWithoutOnPickOrButtonsDoesNothing(t *testing.T) {
	m, _ := oneFull(t)
	m.openList("Pick", "", threeItems(), "b", nil)

	cmd := press(m, "enter")

	if cmd != nil || m.dialog == nil || listOf(t, m).cursor != 1 {
		t.Errorf("cmd %v dialog %v, want nothing to happen", cmd != nil, m.dialog != nil)
	}
}

// C1
func TestC1EscClosesAListDialog(t *testing.T) {
	m, _ := oneFull(t)
	m.openList("Pick", "", threeItems(), "b", nil)

	cmd := press(m, "esc")

	if cmd != nil || m.dialog != nil {
		t.Errorf("cmd %v dialog %v, want closed with no command", cmd != nil, m.dialog != nil)
	}
}

// C1
func TestC1ButtonKeysMoveTheButtonSelection(t *testing.T) {
	m, _ := oneFull(t)
	m.openList("Pick", "", threeItems(), "", nil)
	m.dialog.buttons = []string{"Back", "Done"}

	press(m, "right")
	right := m.dialog.btn
	press(m, "shift+tab")
	back := m.dialog.btn
	press(m, "tab")

	if right != 1 || back != 0 || m.dialog.btn != 1 || listOf(t, m).cursor != 0 {
		t.Errorf("btn %d, %d, %d cursor %d; want 1, 0, 1 and the cursor kept", right, back, m.dialog.btn, listOf(t, m).cursor)
	}
}

// C1
func TestC1ButtonKeysWithoutButtonsDoNothing(t *testing.T) {
	m, _ := oneFull(t)
	m.openList("Pick", "", threeItems(), "b", nil)

	press(m, "right", "tab", "left", "shift+tab", "right")

	if m.dialog == nil || m.dialog.btn != 0 || listOf(t, m).cursor != 1 {
		t.Errorf("dialog %v btn %d cursor %d, want 0 and 1", m.dialog != nil, m.dialog.btn, listOf(t, m).cursor)
	}
}

// C1: other keys are swallowed: no quit, nothing reaches the page.
func TestC1OtherKeysAreSwallowedByAListDialog(t *testing.T) {
	m, f := twoGTNH(t)
	m.focus = focusPage
	m.openList("Pick", "", threeItems(), "b", nil)

	cmd := press(m, "q", "p", "a", "s", "j", "n", "x")

	if m.quitting || hasQuit(runCmd(cmd)) || m.showAll || m.row != 0 || m.sel != 0 || len(f.launches) != 0 {
		t.Errorf("keys leaked: quitting %v showAll %v row %d sel %d launches %d", m.quitting, m.showAll, m.row, m.sel, len(f.launches))
	}
	if m.dialog == nil || listOf(t, m).cursor != 1 {
		t.Errorf("dialog %v, want it open with the cursor kept", m.dialog != nil)
	}
}

// ---- C2 input dialogs ----

type inputCall struct{ label, value string }

func openTestInput(m *model, value string, calls *[]inputCall) {
	m.openInput("Server link", "Paste it.", value, "https://…", "A note", []string{"Continue", "Skip"},
		func(_ *model, label, value string) tea.Cmd {
			*calls = append(*calls, inputCall{label, value})
			return nil
		})
}

// C2
func TestC2OpenInputFocusesAFieldWithTheValue(t *testing.T) {
	m, _ := oneFull(t)
	var calls []inputCall

	openTestInput(m, "abc", &calls)

	in := m.dialog.input
	if in == nil {
		t.Fatalf("no input in the dialog")
	}
	if in.Value() != "abc" || in.Position() != 3 || !in.Focused() || in.Placeholder != "https://…" {
		t.Errorf("value %q pos %d focused %v placeholder %q", in.Value(), in.Position(), in.Focused(), in.Placeholder)
	}
	if m.dialog.note != "A note" || !eq(m.dialog.buttons, []string{"Continue", "Skip"}) || m.dialog.btn != 0 {
		t.Errorf("note %q buttons %q btn %d", m.dialog.note, m.dialog.buttons, m.dialog.btn)
	}
}

// C2
func TestC2InputDialogWidth(t *testing.T) {
	cases := []struct{ termWidth, box, field int }{{80, 64, 58}, {70, 64, 58}, {60, 54, 48}}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.termWidth), func(t *testing.T) {
			m, _ := newTestModel(Config{}, c.termWidth, 24)
			var calls []inputCall

			openTestInput(m, "abc", &calls)

			if w := boxWidth(m); w != c.box || m.dialog.input.Width != c.field {
				t.Errorf("box %d field %d, want %d %d", w, m.dialog.input.Width, c.box, c.field)
			}
		})
	}
}

// C2
func TestC2InputBoxShowsIntroFieldNoteAndButtons(t *testing.T) {
	m, _ := oneFull(t)
	var calls []inputCall
	openTestInput(m, "abc", &calls)

	got := boxText(m)

	if len(got) != 5 || !eq(got[:4], []string{"Paste it.", "> abc", "A note", ""}) ||
		!strings.HasSuffix(got[4], "[ Continue ]  [ Skip ]") {
		t.Errorf("box %q", got)
	}
}

// C2
func TestC2InputErrorShowsUnderTheField(t *testing.T) {
	withColors(t)
	m, _ := oneFull(t)
	var calls []inputCall
	openTestInput(m, "abc", &calls)

	m.dialog.inputErr = "That's not a link"

	got := boxText(m)
	if len(got) != 6 || !eq(got[:5], []string{"Paste it.", "> abc", "That's not a link", "A note", ""}) {
		t.Errorf("box %q", got)
	}
	if !strings.Contains(m.dialogView(), badSty.Render("That's not a link")) {
		t.Errorf("input error not in badSty")
	}
}

// C2: letters, q included, are typed; enter passes the trimmed value.
func TestC2TypedKeysGoToTheFieldAndEnterSubmits(t *testing.T) {
	m, f := twoGTNH(t)
	m.focus = focusPage
	var calls []inputCall
	openTestInput(m, "  abc", &calls)

	press(m, "q", "s", "a", "j", " ")
	cmd := press(m, "enter")

	if len(calls) != 1 || calls[0] != (inputCall{"Continue", "abcqsaj"}) {
		t.Errorf("calls %+v, want Continue abcqsaj", calls)
	}
	if m.quitting || hasQuit(runCmd(cmd)) || m.showAll || len(f.launches) != 0 || m.focus != focusPage {
		t.Errorf("keys leaked: quitting %v showAll %v launches %d focus %v", m.quitting, m.showAll, len(f.launches), m.focus)
	}
}

// C2
func TestC2EscPassesAnEmptyLabel(t *testing.T) {
	m, _ := oneFull(t)
	var calls []inputCall
	openTestInput(m, "abc", &calls)

	press(m, "esc")

	if len(calls) != 1 || calls[0] != (inputCall{"", "abc"}) {
		t.Errorf("calls %+v, want one with label \"\" and abc", calls)
	}
}

// C2
func TestC2TabMovesTheButtonSelection(t *testing.T) {
	m, _ := oneFull(t)
	var calls []inputCall
	openTestInput(m, "abc", &calls)

	press(m, "tab")
	afterTab := m.dialog.btn
	press(m, "enter")

	if afterTab != 1 || len(calls) != 1 || calls[0] != (inputCall{"Skip", "abc"}) {
		t.Errorf("btn %d calls %+v, want 1 and Skip abc", afterTab, calls)
	}
}

// C2: tab stops at the last button.
func TestC2TabIsClampedAtTheLastButton(t *testing.T) {
	m, _ := oneFull(t)
	var calls []inputCall
	openTestInput(m, "abc", &calls)

	press(m, "tab", "tab", "tab", "enter")

	if len(calls) != 1 || calls[0] != (inputCall{"Skip", "abc"}) {
		t.Errorf("calls %+v, want Skip abc", calls)
	}
}

// C8: the selected button is highlighted, the others dim.
func TestC8SelectedButtonIsHighlighted(t *testing.T) {
	withColors(t)
	m, _ := oneFull(t)
	var calls confirmCalls
	openTestConfirm(m, nil, nil, &calls)

	press(m, "right")

	box := m.dialogView()
	if !strings.Contains(box, btnSelSty.Render("[ Cancel ]")) || !strings.Contains(box, dimSty.Render("[ Go ]")) {
		t.Errorf("box %q: want [ Cancel ] highlighted and [ Go ] dim", box)
	}
}

// C8/C1: only list and input dialogs skip an empty intro; a plain dialog keeps its
// (empty) body line above the blank line and the buttons.
func TestC8PlainDialogKeepsAnEmptyBodyLine(t *testing.T) {
	m, _ := oneFull(t)

	m.notify("Heads up", "")

	got := boxText(m)
	if len(got) != 3 || got[0] != "" || got[1] != "" || strings.TrimSpace(got[2]) != "[ OK ]" {
		t.Errorf("box %q, want empty body, blank line, [ OK ]", got)
	}
}

// C2
func TestC2ShiftTabMovesTheButtonSelectionBack(t *testing.T) {
	m, _ := oneFull(t)
	var calls []inputCall
	openTestInput(m, "abc", &calls)

	press(m, "tab", "shift+tab", "enter")

	if len(calls) != 1 || calls[0] != (inputCall{"Continue", "abc"}) {
		t.Errorf("calls %+v, want Continue abc", calls)
	}
}

// C2: left/right move the text cursor, not the buttons.
func TestC2ArrowsMoveTheTextCursor(t *testing.T) {
	m, _ := oneFull(t)
	var calls []inputCall
	openTestInput(m, "abc", &calls)

	press(m, "left", "Z")
	afterLeft, btn := m.dialog.input.Value(), m.dialog.btn
	press(m, "right", "Y")

	if afterLeft != "abZc" || btn != 0 || m.dialog.input.Value() != "abZcY" {
		t.Errorf("value %q then %q, btn %d; want abZc, abZcY, 0", afterLeft, m.dialog.input.Value(), btn)
	}
}

// C2
func TestC2BackspaceDeletesInTheField(t *testing.T) {
	m, _ := oneFull(t)
	var calls []inputCall
	openTestInput(m, "abc", &calls)

	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	if m.dialog == nil || m.dialog.input.Value() != "ab" {
		t.Errorf("value %q, want ab", m.dialog.input.Value())
	}
}

// ---- C7 confirmDialog, C4 errorDialog ----

type confirmCalls struct{ ok, cancel int }

func openTestConfirm(m *model, warn, bad []string, calls *confirmCalls) {
	m.confirmDialog("Do it?", []string{"First line", "Second line"}, warn, bad, "Go",
		func(*model) tea.Cmd { calls.ok++; return nil },
		func(*model) tea.Cmd { calls.cancel++; return nil })
}

// C7
func TestC7ConfirmDialogBody(t *testing.T) {
	cases := []struct {
		name      string
		warn, bad []string
		want      []string
	}{
		{"plain", nil, nil, []string{"First line", "Second line"}},
		{"warn", []string{"W1", "W2"}, nil, []string{"First line", "Second line", "", "W1", "W2"}},
		{"bad", nil, []string{"B1"}, []string{"First line", "Second line", "", "B1"}},
		{"both", []string{"W1"}, []string{"B1"}, []string{"First line", "Second line", "", "W1", "", "B1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := oneFull(t)
			var calls confirmCalls

			openTestConfirm(m, c.warn, c.bad, &calls)

			if got := bodyLines(t, m); !eq(got, c.want) {
				t.Errorf("body %q, want %q", got, c.want)
			}
			if m.dialog.title != "Do it?" || !eq(m.dialog.buttons, []string{"Go", "Cancel"}) || m.dialog.btn != 0 {
				t.Errorf("title %q buttons %q btn %d", m.dialog.title, m.dialog.buttons, m.dialog.btn)
			}
		})
	}
}

// C7
func TestC7ConfirmDialogStylesWarnAndBadLines(t *testing.T) {
	withColors(t)
	m, _ := oneFull(t)
	var calls confirmCalls

	openTestConfirm(m, []string{"Careful now"}, []string{"This is bad"}, &calls)

	body := m.dialog.body(200)
	if !strings.Contains(body, warnSty.Render("Careful now")) || !strings.Contains(body, badSty.Render("This is bad")) {
		t.Errorf("body %q lacks warnSty/badSty lines", body)
	}
}

// C7
func TestC7ConfirmDialogWidth(t *testing.T) {
	cases := []struct{ termWidth, want int }{{80, 72}, {78, 72}, {60, 54}}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.termWidth), func(t *testing.T) {
			m, _ := newTestModel(Config{}, c.termWidth, 24)
			var calls confirmCalls

			openTestConfirm(m, nil, nil, &calls)

			if w := boxWidth(m); w != c.want {
				t.Errorf("width %d, want %d", w, c.want)
			}
		})
	}
}

// C7
func TestC7ConfirmDialogButtons(t *testing.T) {
	cases := []struct {
		keys []string
		want confirmCalls
	}{
		{[]string{"enter"}, confirmCalls{ok: 1}},
		{[]string{"right", "enter"}, confirmCalls{cancel: 1}},
		{[]string{"esc"}, confirmCalls{cancel: 1}},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.keys, "+"), func(t *testing.T) {
			m, _ := oneFull(t)
			var calls confirmCalls
			openTestConfirm(m, nil, nil, &calls)

			press(m, c.keys...)

			if calls != c.want {
				t.Errorf("calls %+v, want %+v", calls, c.want)
			}
		})
	}
}

// C4
func TestC4ErrorDialogShowsTheErrorAndTheOutcome(t *testing.T) {
	m, _ := oneFull(t)

	m.errorDialog(errors.New("the disk is full"), "Nothing was changed.")

	if m.dialog == nil || m.dialog.title != "Something went wrong" || !eq(m.dialog.buttons, []string{"OK"}) {
		t.Fatalf("dialog %+v", m.dialog)
	}
	if got := bodyLines(t, m); !eq(got, []string{"the disk is full", "", "Nothing was changed."}) {
		t.Errorf("body %q", got)
	}
}

// C4
func TestC4ErrorDialogOutcomeStyle(t *testing.T) {
	cases := []struct {
		outcome string
		good    bool
	}{
		{"Nothing was changed.", true},
		{"Everything was put back the way it was, so your instance is exactly as before.", true},
		{"Some files may have changed. The originals are in the .gtnh-updater folder inside the instance.", false},
		{"Nothing was changed, probably.", false},
	}
	for _, c := range cases {
		t.Run(c.outcome, func(t *testing.T) {
			withColors(t)
			m, _ := oneFull(t)

			m.errorDialog(errors.New("boom"), c.outcome)

			want := badSty.Render(c.outcome)
			if c.good {
				want = okSty.Render(c.outcome)
			}
			if body := m.dialog.body(200); !strings.Contains(body, want) {
				t.Errorf("body %q lacks %q", body, want)
			}
		})
	}
}

// C4
func TestC4ErrorDialogClosesWithEnterOrEsc(t *testing.T) {
	for _, k := range []string{"enter", "esc"} {
		t.Run(k, func(t *testing.T) {
			m, _ := oneFull(t)
			m.errorDialog(errors.New("boom"), "Nothing was changed.")

			cmd := press(m, k)

			if m.dialog != nil || cmd != nil {
				t.Errorf("%s: dialog %v cmd %v, want closed", k, m.dialog != nil, cmd != nil)
			}
		})
	}
}

// ---- C11 status pairs with a dialog ----

// C11
func TestC11StatusPairsFollowTheDialogKind(t *testing.T) {
	cases := []struct {
		name string
		open func(m *model)
		want string
	}{
		{"list", func(m *model) { m.openList("Pick", "", threeItems(), "", nil) },
			"↑↓,move,enter,choose,esc,cancel"},
		{"toggle list", func(m *model) {
			m.openList("Pick", "", threeItems(), "", nil)
			m.dialog.list.toggle = func(*model, string) {}
		}, "↑↓,move,enter,choose,esc,cancel,space,switch"},
		{"input", func(m *model) {
			var calls []inputCall
			openTestInput(m, "", &calls)
		}, "enter,continue,esc,cancel"},
		{"confirm", func(m *model) {
			var calls confirmCalls
			openTestConfirm(m, nil, nil, &calls)
		}, "←→,choose,enter,ok,esc,cancel"},
		{"error", func(m *model) { m.errorDialog(errors.New("boom"), "Nothing was changed.") },
			"←→,choose,enter,ok,esc,cancel"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := oneFull(t)
			c.open(m)

			if got := strings.Join(m.statusPairs(), ","); got != c.want {
				t.Errorf("statusPairs = %s, want %s", got, c.want)
			}
		})
	}
}
