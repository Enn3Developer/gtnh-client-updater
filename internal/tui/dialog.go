package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// dialog is a box drawn over the workspace that takes every key until it closes. A
// list dialog (list != nil) or an input dialog (input != nil) shows body as its intro.
type dialog struct {
	title    string
	body     func(width int) string
	buttons  []string
	btn      int                                  // selected button
	onButton func(m *model, label string) tea.Cmd // label "" = esc
	width    int                                  // 0 = 60
	list     *dlist
	input    *textinput.Model
	inputErr string
	onPick   func(m *model, key string) tea.Cmd // enter on a list item; nil = press the button
	note     string
}

// ditem is one choice of a list dialog.
type ditem struct{ title, desc, key string }

// dlist is the choosable list inside a dialog.
type dlist struct {
	items  []ditem
	cursor int
	off    int                        // window offset
	toggle func(m *model, key string) // nil = not toggleable
}

// closeOnly is the button handler of dialogs that just close.
func closeOnly(m *model, _ string) tea.Cmd {
	m.closeDialog()
	return nil
}

// openList opens a dialog that lets the player pick one of items, the cursor on the
// item keyed selected (C1).
func (m *model) openList(title, intro string, items []ditem, selected string, onPick func(*model, string) tea.Cmd) {
	l := &dlist{items: items}
	longest := 0
	for i, it := range items {
		if it.key == selected {
			l.cursor = i
		}
		w := ansi.StringWidth(it.title)
		if it.desc != "" {
			w += 2 + ansi.StringWidth(it.desc)
		}
		longest = max(longest, w)
	}
	m.openDialog(&dialog{
		title: title, body: func(int) string { return intro }, list: l, onPick: onPick,
		onButton: closeOnly, width: min(max(longest+6, 50), m.width-6),
	})
}

// openInput opens a dialog with a one-line text field holding value (C2).
func (m *model) openInput(title, intro, value, placeholder, note string, buttons []string, onButton func(m *model, label, value string) tea.Cmd) {
	w := min(64, m.width-6)
	in := textinput.New()
	in.Placeholder = placeholder
	in.Width = w - 4 - 2
	in.Cursor.SetMode(cursor.CursorStatic)
	in.SetValue(value)
	in.CursorEnd()
	in.Focus()
	m.openDialog(&dialog{
		title: title, body: func(int) string { return intro }, input: &in, note: note,
		buttons: buttons, width: w,
		onButton: func(m *model, label string) tea.Cmd {
			v := in.Value()
			if label != "" {
				v = strings.TrimSpace(v)
			}
			return onButton(m, label, v)
		},
	})
}

// confirmDialog asks the player to confirm with ok or cancel (C7): lines, then the warn
// lines and the bad lines, each block after a blank line.
func (m *model) confirmDialog(title string, lines []string, warn []string, bad []string, ok string, onOK func(*model) tea.Cmd, onCancel func(*model) tea.Cmd) {
	body := strings.Join(lines, "\n")
	for _, block := range []struct {
		lines []string
		sty   lipgloss.Style
	}{{warn, warnSty}, {bad, badSty}} {
		if len(block.lines) == 0 {
			continue
		}
		body += "\n"
		for _, l := range block.lines {
			body += "\n" + block.sty.Render(l)
		}
	}
	m.openDialog(&dialog{
		title: title, body: func(int) string { return body }, buttons: []string{ok, "Cancel"}, width: 72,
		onButton: func(m *model, label string) tea.Cmd {
			if label == ok {
				return onOK(m)
			}
			return onCancel(m)
		},
	})
}

// errorDialog tells the player what went wrong and what it means for their instance.
func (m *model) errorDialog(err error, outcome string) {
	sty := badSty
	if outcome == "Nothing was changed." || strings.HasPrefix(outcome, "Everything was put back") {
		sty = okSty
	}
	body := err.Error() + "\n\n" + sty.Render(outcome)
	m.openDialog(&dialog{
		title: "Something went wrong", body: func(int) string { return body },
		buttons: []string{"OK"}, onButton: closeOnly,
	})
}

func (m *model) openDialog(d *dialog) { m.dialog = d }

func (m *model) closeDialog() { m.dialog = nil }

// dialogView is the open dialog's box: rounded dim border with the title in it, the
// content and, when there are buttons, a blank line and the buttons right-aligned (C8).
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
	for _, l := range m.dialogLines(inner) {
		line(l)
	}
	if len(d.buttons) > 0 {
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
	}
	out = append(out, border.Render("╰"+strings.Repeat("─", max(w-2, 0))+"╯"))
	return strings.Join(out, "\n")
}

// dialogLines is the content of the open dialog at inner columns: the wrapped body (an
// empty intro of a list or input dialog is skipped), the list window or the text field
// with its error, and the note.
func (m *model) dialogLines(inner int) []string {
	d := m.dialog
	var out []string
	if body := d.body(inner); body != "" || (d.list == nil && d.input == nil) {
		for _, l := range strings.Split(wrap(body, inner), "\n") {
			out = append(out, strings.TrimRight(l, " "))
		}
	}
	styled := func(s string, sty lipgloss.Style) {
		for _, l := range wrapLines(s, inner) {
			out = append(out, sty.Render(l))
		}
	}
	switch {
	case d.list != nil:
		out = append(out, m.listWindow()...)
	case d.input != nil:
		// the field's view is prompt + Width + the cursor cell: one column of padding over
		out = append(out, ansi.Truncate(d.input.View(), inner, ""))
		if d.inputErr != "" {
			styled(d.inputErr, badSty)
		}
	}
	if d.note != "" {
		styled(d.note, dimSty)
	}
	return out
}

// listRows is how many lines the items of a list dialog get.
func (m *model) listRows() int {
	return max(min(12, m.height-9), 3)
}

// listWindow is the item lines of the open list dialog, windowed around the cursor.
func (m *model) listWindow() []string {
	l := m.dialog.list
	lines := make([]string, len(l.items))
	for i, it := range l.items {
		prefix, title := "  ", it.title
		if i == l.cursor {
			prefix, title = "▸ ", titleSty.Render(it.title)
		}
		lines[i] = prefix + title
		if it.desc != "" {
			lines[i] += "  " + dimSty.Render(it.desc)
		}
	}
	rows := m.listRows()
	l.off = scrollTo(l.off, [2]int{l.cursor, l.cursor + 1}, len(lines), rows)
	visible, _ := bodyWindow(lines, rows, l.off)
	return visible
}

// keyDialog handles k while a dialog is open; handled is true for every key then.
func (m *model) keyDialog(k tea.KeyMsg) (cmd tea.Cmd, handled bool) {
	d := m.dialog
	if d == nil {
		return nil, false
	}
	if d.input != nil {
		return m.keyInput(k), true
	}
	if l := d.list; l != nil {
		switch k.String() {
		case "up":
			l.cursor = max(l.cursor-1, 0)
			return nil, true
		case "down":
			l.cursor = max(min(l.cursor+1, len(l.items)-1), 0)
			return nil, true
		case " ":
			if l.toggle != nil && l.cursor < len(l.items) {
				l.toggle(m, l.items[l.cursor].key)
			}
			return nil, true
		case "enter":
			if d.onPick != nil && l.cursor < len(l.items) {
				return d.onPick(m, l.items[l.cursor].key), true
			}
		}
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

// keyInput handles k in an input dialog: enter, esc and tab/shift+tab work the buttons,
// every other key goes to the text field (C2).
func (m *model) keyInput(k tea.KeyMsg) tea.Cmd {
	d := m.dialog
	switch k.String() {
	case "shift+tab":
		d.btn = max(d.btn-1, 0)
	case "tab":
		d.btn = max(min(d.btn+1, len(d.buttons)-1), 0)
	case "enter":
		if d.btn < len(d.buttons) {
			return d.onButton(m, d.buttons[d.btn])
		}
	case "esc":
		return d.onButton(m, "")
	default:
		in, cmd := d.input.Update(k)
		*d.input = in
		return cmd
	}
	return nil
}

// dialogPairs are the status bar's key/label pairs for the open dialog (C11).
func (m *model) dialogPairs() []string {
	switch d := m.dialog; {
	case d.list != nil:
		pairs := []string{"↑↓", "move", "enter", "choose", "esc", "cancel"}
		if d.list.toggle != nil {
			pairs = append(pairs, "space", "switch")
		}
		return pairs
	case d.input != nil:
		return []string{"enter", "continue", "esc", "cancel"}
	}
	return []string{"←→", "choose", "enter", "ok", "esc", "cancel"}
}

// notify shows text in a dialog with a single OK button that closes it.
func (m *model) notify(title, text string) {
	m.openDialog(&dialog{
		title:    title,
		body:     func(int) string { return text },
		buttons:  []string{"OK"},
		onButton: closeOnly,
	})
}
