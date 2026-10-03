package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// dialog is a box drawn over the workspace that takes every key until it closes.
type dialog struct {
	title    string
	body     func(width int) string
	buttons  []string
	btn      int                                  // selected button
	onButton func(m *model, label string) tea.Cmd // label "" = esc
	width    int                                  // 0 = 60
}

func (m *model) openDialog(d *dialog) { m.dialog = d }

func (m *model) closeDialog() { m.dialog = nil }

// dialogView is the open dialog's box: rounded dim border with the title in it, the
// wrapped body, a blank line and the buttons right-aligned (C8).
func (m *model) dialogView() string {
	d := m.dialog
	w := d.width
	if w == 0 {
		w = 60
	}
	w = min(w, m.width-6)
	inner := max(w-4, 1)
	bg := lipgloss.NewStyle().Background(barBg)
	border := dimSty
	title := ansi.Truncate(d.title, max(w-6, 0), "…")
	out := []string{border.Render("╭─ ") + titleSty.Render(title) +
		border.Render(" "+strings.Repeat("─", max(w-5-ansi.StringWidth(title), 0))+"╮")}
	line := func(s string) {
		s = ansi.Truncate(s, inner, "…")
		out = append(out, border.Render("│")+bg.Render(" "+padTo(s, inner)+" ")+border.Render("│"))
	}
	for _, l := range strings.Split(wrap(d.body(inner), inner), "\n") {
		line(strings.TrimRight(l, " "))
	}
	line("")
	chips := make([]string, len(d.buttons))
	for i, b := range d.buttons {
		sty := dimSty
		if i == d.btn {
			sty = btnSelSty
		}
		chips[i] = sty.Render("[ " + b + " ]")
	}
	row := strings.Join(chips, "  ")
	line(strings.Repeat(" ", max(inner-ansi.StringWidth(row), 0)) + row)
	out = append(out, border.Render("╰"+strings.Repeat("─", max(w-2, 0))+"╯"))
	return strings.Join(out, "\n")
}

// keyDialog handles k while a dialog is open; handled is true for every key then.
func (m *model) keyDialog(k tea.KeyMsg) (cmd tea.Cmd, handled bool) {
	d := m.dialog
	if d == nil {
		return nil, false
	}
	switch k.String() {
	case "left", "shift+tab":
		d.btn = max(d.btn-1, 0)
	case "right", "tab":
		d.btn = max(min(d.btn+1, len(d.buttons)-1), 0)
	case "enter":
		if d.btn < len(d.buttons) {
			return d.onButton(m, d.buttons[d.btn]), true
		}
	case "esc":
		return d.onButton(m, ""), true
	}
	return nil, true
}

// notify shows text in a dialog with a single OK button that closes it.
func (m *model) notify(title, text string) {
	m.openDialog(&dialog{
		title:   title,
		body:    func(int) string { return text },
		buttons: []string{"OK"},
		onButton: func(m *model, _ string) tea.Cmd {
			m.closeDialog()
			return nil
		},
	})
}
