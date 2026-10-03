package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// row is one selectable line (or lines) of the page.
type row struct {
	id    string // "play", "join", "update", "versions", "undo", a setting key, "after", "prism"
	key   string // shortcut shown as its hint; "" = none
	text  string // stripped text of a hinted row; "" = not hinted
	hint  string // its hint; "" = none
	lines func(width int, selected bool) []string
	run   func(m *model) tea.Cmd // enter on the row; nil = nothing
}

// entry is one piece of a page section: a row, or (row == nil) a plain line.
type entry struct {
	text string
	row  *row
}

func textEntries(lines ...string) []entry {
	out := make([]entry, len(lines))
	for i, l := range lines {
		out[i] = entry{text: l}
	}
	return out
}

func rowEntries(rows []row) []entry {
	out := make([]entry, len(rows))
	for i := range rows {
		out[i] = entry{row: &rows[i]}
	}
	return out
}

// entries are the sections of the current instance's page, top to bottom.
func (m *model) entries() []entry {
	in, ok := m.current()
	if !ok {
		return nil
	}
	if p, ok := m.pending(); ok && p.Dir == in.Dir {
		return m.pendingEntries()
	}
	es := m.playEntries()
	if m.home[in.Dir].gtnh {
		es = append(es, m.updateEntries()...)
	}
	es = append(es, m.settingsEntries()...)
	return append(es, m.launcherEntries()...)
}

// rows are the selectable rows of the current instance's page, top to bottom.
func (m *model) rows() []row {
	var out []row
	for _, e := range m.entries() {
		if e.row != nil {
			out = append(out, *e.row)
		}
	}
	return out
}

// selectedID is the id of the page row drawn as selected; "" when the page isn't focused.
func (m *model) selectedID() string {
	rs := m.rows()
	if m.focus != focusPage || m.row < 0 || m.row >= len(rs) {
		return ""
	}
	return rs[m.row].id
}

// render draws entries at width with the selected row marked; span is the line range
// [from, to) of the selected row (0, 0 when none is).
func (m *model) render(width int, es []entry) (lines []string, span [2]int) {
	sel := ""
	rs := m.rows()
	if m.row >= 0 && m.row < len(rs) {
		sel = rs[m.row].id
	}
	marked := m.selectedID()
	mw := measure(width)
	col := hintCol(mw, rs)
	for _, e := range es {
		if e.row == nil {
			lines = append(lines, ansi.Truncate(e.text, mw, "…"))
			continue
		}
		from := len(lines)
		selected := marked != "" && e.row.id == marked
		var rl []string
		if e.row.hint != "" {
			rl = []string{rowLineAt(mw, col, selected, e.row.text, e.row.hint)}
		} else {
			rl = e.row.lines(mw, selected)
		}
		for _, l := range rl {
			lines = append(lines, ansi.Truncate(l, mw, "…"))
		}
		if e.row.id == sel {
			span = [2]int{from, len(lines)}
		}
	}
	return lines, span
}

// sectionLines draws one section's entries.
func (m *model) sectionLines(width int, es []entry) []string {
	lines, _ := m.render(width, es)
	return lines
}

// pageView is every section of the page windowed by bodyWindow at m.pageScroll, which is
// first moved so the selected row is fully visible and off the "more" markers (C4).
func (m *model) pageView(width, height int) string {
	switch {
	case m.loadErr != nil:
		return m.loadErrView(width)
	case !m.loaded:
		return m.spin.View() + " Looking for your GTNH instances…"
	case len(m.visible()) == 0:
		return "No GTNH instances in Prism yet.\n" + dimSty.Render("Press n to make one.")
	}
	m.row = max(min(m.row, len(m.rows())-1), 0)
	lines, span := m.render(width, m.entries())
	m.pageScroll = scrollTo(m.pageScroll, span, len(lines), max(height, 1))
	visible, _ := bodyWindow(lines, max(height, 1), m.pageScroll)
	return strings.Join(visible, "\n")
}

// scrollTo is the window offset nearest to off that shows the lines span[0]..span[1]-1
// of total in a window of h lines without putting them on a "more" marker line.
func scrollTo(off int, span [2]int, total, h int) int {
	maxOff := total - h
	if maxOff <= 0 {
		return 0
	}
	off = max(min(off, maxOff), 0)
	top := 0
	if off > 0 {
		top = 1
	}
	if span[0] < off+top {
		return max(span[0]-1, 0)
	}
	bottom := 0
	if off < maxOff {
		bottom = 1
	}
	if span[1]-1 > off+h-1-bottom {
		return min(span[1]-h+1, maxOff)
	}
	return off
}

// loadErrView is why loading failed: the first line in red, wrapped to width.
func (m *model) loadErrView(width int) string {
	lines := strings.Split(wrap(m.loadErr.Error(), width), "\n")
	lines[0] = badSty.Render(lines[0])
	return strings.Join(lines, "\n")
}

// pageWidth is the page's columns: what the sidebar and its rule (or the margin) leave.
func (m *model) pageWidth() int {
	if m.sidebarShows() {
		return m.width - m.sidebarWidth() - 3
	}
	return m.width - 2
}

// heading is a section title.
func heading(s string) string {
	return lipgloss.NewStyle().Bold(true).Render(s)
}

// infoLine is a dim line that isn't a row, indented like an unselected row.
func infoLine(s string) string {
	return "  " + dimSty.Render(s)
}

// rowLine is one row: "▸ " + accent bold text when selected, else "  " + text; a dim
// hint right-aligned so the line is exactly width wide, text truncated with "…" to fit.
func rowLine(width int, selected bool, text, hint string) string {
	prefix, sty := "  ", lipgloss.NewStyle()
	if selected {
		prefix, sty = "▸ ", titleSty
	}
	return styledRow(width, prefix, sty, text, hint)
}

// measure is the page's content measure: min(pageWidth, 84) columns.
func measure(pageWidth int) int {
	return min(pageWidth, 84)
}

// hintCol is the 0-based column where every hinted row of the page puts its hint:
// min(max(widest hinted-row text + 4, 32), measure - widest hint - 1) over the rows with hint != "".
func hintCol(measure int, rs []row) int {
	text, hint := 0, 0
	for _, r := range rs {
		if r.hint != "" {
			text = max(text, ansi.StringWidth(r.text))
			hint = max(hint, ansi.StringWidth(r.hint))
		}
	}
	return min(max(text+4, 32), measure-hint-1)
}

// rowLineAt is rowLine with the hint at column col instead of flush right.
func rowLineAt(width, col int, selected bool, text, hint string) string {
	prefix, sty := "  ", lipgloss.NewStyle()
	if selected {
		prefix, sty = "▸ ", titleSty
	}
	if col < 3 || col+ansi.StringWidth(hint) > width {
		return styledRow(width, prefix, sty, text, hint)
	}
	text = ansi.Truncate(text, col-3, "…")
	gap := col - 2 - ansi.StringWidth(text)
	return prefix + sty.Render(text) + strings.Repeat(" ", gap) + dimSty.Render(hint)
}

// styledRow is rowLine with the prefix and the text style given.
func styledRow(width int, prefix string, sty lipgloss.Style, text, hint string) string {
	if hint == "" {
		return ansi.Truncate(prefix+sty.Render(text), width, "…")
	}
	hw := ansi.StringWidth(hint)
	room := width - 2 - hw - 1
	if room < 1 {
		return ansi.Truncate(prefix+sty.Render(text), width, "…")
	}
	text = ansi.Truncate(text, room, "…")
	gap := width - 2 - ansi.StringWidth(text) - hw
	return prefix + sty.Render(text) + strings.Repeat(" ", gap) + dimSty.Render(hint)
}

// labelled is a setting row's text: the label padded to width columns (at least one
// space after it), then the value.
func labelled(label, value string, width int) string {
	return label + strings.Repeat(" ", max(width-ansi.StringWidth(label), 1)) + value
}
