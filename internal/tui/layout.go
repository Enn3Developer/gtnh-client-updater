package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	accent    = lipgloss.Color("#7FB4CA")
	titleSty  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	okSty     = lipgloss.NewStyle().Foreground(lipgloss.Color("#98BB6C"))
	warnSty   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6C384"))
	badSty    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E46876")).Bold(true)
	dimSty    = lipgloss.NewStyle().Foreground(lipgloss.Color("#727169"))
	keySty    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	bannerSty = lipgloss.NewStyle().Foreground(lipgloss.Color("#1F1F28")).Background(lipgloss.Color("#E6C384")).Padding(0, 1)
)

func (m *model) inputWidth() int { return max(min(m.width-10, 70), 20) }
func (m *model) listWidth() int  { return m.listWidthFor(m.screen) }
func (m *model) listHeight() int { return m.listHeightFor(m.screen) }

// homeCardShows reports whether the instance card sits next to the list on sc.
func (m *model) homeCardShows(sc screen) bool { return sc == scHome && m.width >= 90 }

// listWidthFor is the list width on sc; the home card takes 42 of the usual columns.
func (m *model) listWidthFor(sc screen) int {
	if m.homeCardShows(sc) {
		return m.width - 46
	}
	return max(m.width-4, 20)
}

// listHeightFor is the list height on sc; home keeps two rows for its own help line.
func (m *model) listHeightFor(sc screen) int {
	h := m.height - 5 // padding + header
	if m.newer != nil {
		h -= 2
	}
	if sc == scHome {
		h -= 2
	}
	return max(h, 5)
}

// scrollable reports whether the current screen scrolls with the arrow keys.
func (m *model) scrollable() bool {
	switch m.screen {
	case scConfirm, scDone, scError, scServerMods, scName, scSettingEdit, scRestoreConfirm, scRestored:
		return true
	case scBackups:
		return len(m.backups) == 0
	}
	return false
}

// scrollKey moves the body of a scrollable screen; ok is false for other keys.
// Home and end stay with the text field on the input screens.
func (m *model) scrollKey(k string) (ok bool) {
	header, body, footer, _ := m.page()
	lines := len(strings.Split(body, "\n"))
	rows := fitBodyRows(m.height, len(strings.Split(header, "\n")), len(strings.Split(footer, "\n")))
	page := max(rows/2, 1)
	input := m.screen == scServerMods || m.screen == scName || m.screen == scSettingEdit
	switch {
	case k == "up":
		m.scroll--
	case k == "down":
		m.scroll++
	case k == "pgup":
		m.scroll -= page
	case k == "pgdown":
		m.scroll += page
	case k == "home" && !input:
		m.scroll = 0
	case k == "end" && !input:
		m.scroll = lines
	default:
		return false
	}
	m.scroll = max(min(m.scroll, max(lines-rows, 0)), 0)
	return true
}

func (m *model) header() string {
	return dimSty.Render("GTNH Launcher "+m.cfg.AppVersion) + "\n\n"
}

func (m *model) banner() string {
	if m.newer == nil {
		return ""
	}
	return bannerSty.Render(fmt.Sprintf("A new version of this updater is out (%s) — press v to get it", m.newer.Version)) + "\n\n"
}

// fitBodyRows is how many body lines fit under the blank top line, header and footer.
func fitBodyRows(height, headerLines, footerLines int) int {
	return max(height-1-headerLines-footerLines, 1)
}

// bodyWindow picks the rows of body that fit on screen, scrolled to scroll (clamped).
// When body overflows, the first/last visible row is replaced by a "more" marker.
func bodyWindow(body []string, rows, scroll int) (visible []string, offset int) {
	rows = max(rows, 1)
	if len(body) <= rows {
		return body, 0
	}
	maxOff := len(body) - rows
	offset = max(min(scroll, maxOff), 0)
	visible = append([]string{}, body[offset:offset+rows]...)
	if offset < maxOff {
		visible[rows-1] = dimSty.Render(fmt.Sprintf("↓ %d more (↑/↓ to scroll)", maxOff-offset))
	}
	if offset > 0 {
		visible[0] = dimSty.Render(fmt.Sprintf("↑ %d more", offset))
	}
	return visible, offset
}

// frame lays out a non-list screen: header on top, footer pinned at the bottom, body
// scrolled in between, every line indented and clipped to width.
func frame(header, body, footer string, width, height, scroll int) string {
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(s, "\n")
	}
	head, foot := split(header), split(footer)
	visible, _ := bodyWindow(split(body), fitBodyRows(height, len(head), len(foot)), scroll)
	out := []string{""}
	for _, part := range [][]string{head, visible, foot} {
		for _, l := range part {
			out = append(out, ansi.Truncate("  "+l, width, "…"))
		}
	}
	return strings.Join(out, "\n")
}

func hint(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keySty.Render(pairs[i])+" "+dimSty.Render(pairs[i+1]))
	}
	return strings.Join(parts, "    ")
}

// bullet renders "  • text" with wrapped lines indented under the text.
func (m *model) bullet(s string) string {
	lines := strings.Split(wrap(s, m.width-8), "\n")
	for i, l := range lines {
		prefix := "    "
		if i == 0 {
			prefix = "  • "
		}
		lines[i] = prefix + strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

// indentWrap wraps s to width and indents the continuation lines by indent spaces.
func indentWrap(s string, width, indent int) string {
	return strings.ReplaceAll(strings.TrimRight(wrap(s, width), " "), "\n", "\n"+strings.Repeat(" ", indent))
}

func wrap(s string, width int) string {
	if width < 20 {
		return s
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}
