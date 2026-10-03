package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// recorder is a dialog whose buttons record the label they were pressed with.
func recorder(labels *[]string, buttons ...string) *dialog {
	return &dialog{
		title:   "Hello",
		body:    func(int) string { return "Body text" },
		buttons: buttons,
		onButton: func(_ *model, label string) tea.Cmd {
			*labels = append(*labels, label)
			return nil
		},
	}
}

// C8
func TestC8DialogViewIsABoxOfEqualWidthLines(t *testing.T) {
	m, _ := oneFull(t)
	var labels []string
	m.openDialog(recorder(&labels, "Cancel", "OK"))

	box := m.dialogView()
	lines := strings.Split(box, "\n")
	plain := trimLines(box)

	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 60 {
			t.Errorf("dialog line %d is %d columns, want 60: %q", i+1, w, ansi.Strip(l))
		}
	}
	top, bottom := plain[0], plain[len(plain)-1]
	if !strings.HasPrefix(top, "╭─ Hello ") || !strings.HasSuffix(top, "─╮") {
		t.Errorf("top border = %q", top)
	}
	if !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") {
		t.Errorf("bottom border = %q", bottom)
	}
	if !containsLine(plain, "Body text") {
		t.Errorf("body missing: %q", plain)
	}
	buttons := strings.TrimRight(strings.TrimSuffix(plain[len(plain)-2], "│"), " ")
	if !strings.Contains(buttons, "[ Cancel ]") || !strings.HasSuffix(buttons, "[ OK ]") {
		t.Errorf("buttons line = %q, want chips right-aligned", plain[len(plain)-2])
	}
	if blank := strings.Trim(plain[len(plain)-3], "│ "); blank != "" {
		t.Errorf("line above the buttons = %q, want blank", plain[len(plain)-3])
	}
}

// C8
func TestC8DialogWidthShrinksWithTheTerminal(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 50, 24, makeInst(t, root, fullSpec("Home")))
	var labels []string
	m.openDialog(recorder(&labels, "OK"))

	top := strings.Split(m.dialogView(), "\n")[0]

	if w := ansi.StringWidth(top); w != 44 {
		t.Errorf("dialog at width 50 is %d columns, want 50-6", w)
	}
}

// C8
func TestC8DialogWidthCanBeSet(t *testing.T) {
	m, _ := oneFull(t)
	var labels []string
	d := recorder(&labels, "OK")
	d.width = 30
	m.openDialog(d)

	top := strings.Split(m.dialogView(), "\n")[0]

	if w := ansi.StringWidth(top); w != 30 {
		t.Errorf("dialog is %d columns, want 30", w)
	}
}

// C8: a title too long for the box is truncated with … inside the top border.
// Kills dialog.go:37.
func TestC8LongTitleIsTruncatedInTheBorder(t *testing.T) {
	m, _ := oneFull(t)
	var labels []string
	d := recorder(&labels, "OK")
	d.title = "A very long dialog title that cannot fit"
	d.width = 30
	m.openDialog(d)

	lines := strings.Split(m.dialogView(), "\n")
	top := ansi.Strip(lines[0])

	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 30 {
			t.Errorf("dialog line %d is %d columns, want 30", i+1, w)
		}
	}
	if !strings.HasPrefix(top, "╭─ A very long") || !strings.Contains(top, "…") || !strings.HasSuffix(top, "╮") {
		t.Errorf("top border = %q, want the title truncated with …", top)
	}
}

// C8
func TestC8KeyDialogWithoutADialogIsUnhandled(t *testing.T) {
	m, _ := oneFull(t)

	_, handled := m.keyDialog(keyMsg("enter"))

	if handled {
		t.Errorf("keyDialog handled a key with no dialog open")
	}
}

// C8
func TestC8ArrowsAndTabMoveBetweenButtonsClamped(t *testing.T) {
	m, _ := oneFull(t)
	var labels []string
	m.openDialog(recorder(&labels, "Cancel", "OK"))

	steps := []struct {
		key  string
		want int
	}{{"right", 1}, {"right", 1}, {"left", 0}, {"left", 0}, {"tab", 1}, {"tab", 1}, {"shift+tab", 0}, {"shift+tab", 0}}
	for i, s := range steps {
		_, handled := m.keyDialog(keyMsg(s.key))
		if !handled || m.dialog.btn != s.want {
			t.Errorf("step %d %s: handled %v btn %d, want true %d", i+1, s.key, handled, m.dialog.btn, s.want)
		}
	}
}

// C8
func TestC8EnterPressesTheSelectedButtonAndEscPassesEmpty(t *testing.T) {
	m, _ := oneFull(t)
	var labels []string
	m.openDialog(recorder(&labels, "Cancel", "OK"))

	press(m, "right", "enter", "esc")

	if strings.Join(labels, "|") != "OK|" {
		t.Errorf("onButton labels %q, want OK then empty", labels)
	}
}

// C8
func TestC8KeysDoNotReachThePageOrQuit(t *testing.T) {
	m, f := twoGTNH(t)
	m.focus = focusPage
	var labels []string
	m.openDialog(recorder(&labels, "OK"))

	cmd := press(m, "down", "up", "q", "p", "s", "a")

	if m.quitting || hasQuit(runCmd(cmd)) || m.row != 0 || m.sel != 0 || m.showAll || len(f.launches) != 0 {
		t.Errorf("keys leaked past the dialog: quitting %v row %d sel %d showAll %v launches %d",
			m.quitting, m.row, m.sel, m.showAll, len(f.launches))
	}
	if m.dialog == nil {
		t.Errorf("dialog closed by keys that aren't enter/esc")
	}
}

// C8
func TestC8NotifyOpensAnOKDialogThatEnterCloses(t *testing.T) {
	m, _ := oneFull(t)
	m.notify("Heads up", "Something to know.")
	shown := screen(m)
	buttons := strings.Join(m.dialog.buttons, ",")

	press(m, "enter")

	if !containsLine(shown, "Heads up") || !containsLine(shown, "Something to know.") || buttons != "OK" {
		t.Errorf("notify dialog (buttons %s) not shown:\n%s", buttons, strings.Join(shown, "\n"))
	}
	if m.dialog != nil || containsLine(screen(m), "Heads up") {
		t.Errorf("enter didn't close the notify dialog")
	}
}

// C8
func TestC8ErrorsAfterLoadOpenADialog(t *testing.T) {
	m, _ := oneFull(t)

	m.Update(errMsg{errors.New("the disk is full")})

	if m.dialog == nil || m.dialog.title != "Something went wrong" {
		t.Fatalf("dialog %+v, want Something went wrong", m.dialog)
	}
	if !strings.Contains(flat(m.dialog.body(200)), "the disk is full") {
		t.Errorf("body %q doesn't hold the error", flat(m.dialog.body(200)))
	}
}

// C8/C7/C11
func TestC8StatusBarOffersTheDialogKeys(t *testing.T) {
	m, _ := oneFull(t)
	m.notify("Heads up", "Something to know.")

	got := strings.Join(m.statusPairs(), ",")

	if got != "←→,choose,enter,ok,esc,cancel" {
		t.Errorf("statusPairs = %s", got)
	}
}
