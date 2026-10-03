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
	barBg     = lipgloss.Color("#2A2A37")
	chipBg    = lipgloss.Color("#363646")
	keySty    = lipgloss.NewStyle().Bold(true).Foreground(accent).Background(chipBg) // no padding: a hint is as wide as its text
	btnSelSty = lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("#1F1F28")).Bold(true)
	bannerSty = lipgloss.NewStyle().Foreground(lipgloss.Color("#1F1F28")).Background(lipgloss.Color("#E6C384")).Padding(0, 1)
)

func (m *model) bodyWidth() int  { return m.width - 4 }
func (m *model) inputWidth() int { return max(min(m.bodyWidth()-6, 70), 20) }
func (m *model) listWidth() int  { return m.listWidthFor(m.screen) }
func (m *model) listHeight() int { return m.listHeightFor(m.screen) }

// homeCardShows reports whether the instance card sits next to the list on sc.
func (m *model) homeCardShows(sc screen) bool { return sc == scHome && m.width >= 90 }

// listWidthFor is the list width on sc; the home card takes 42 of the usual columns.
func (m *model) listWidthFor(sc screen) int {
	if m.homeCardShows(sc) {
		return m.width - 46
	}
	return max(m.bodyWidth(), 20)
}

// listHeightFor is the list height on sc: the body rows left by the chrome and the
// footer of the list on screen.
func (m *model) listHeightFor(sc screen) int {
	if sc == scHome {
		return max(fitRows(m.height, m.homeHelp()), 5)
	}
	// The title page() draws; the blank line under it is the list's own empty title bar.
	titleRows := strings.Count(m.list.Title, "\n") + 1
	return max(fitRows(m.height, keyBar(m.width, m.listKeyPairs()...))-titleRows, 5)
}

// fitRows is how many body lines fit under the title bar and banner line and above
// footer and the blank line before it; unfloored.
func fitRows(height int, footer string) int {
	if footer == "" {
		return height - 2
	}
	return height - 2 - (strings.Count(footer, "\n") + 1) - 1
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
	body, _, _ := m.page()
	lines := len(strings.Split(body, "\n"))
	rows := m.bodyRows()
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

// header is the app name and version on the left of the title bar.
func (m *model) header() string {
	return "GTNH Launcher " + m.cfg.AppVersion
}

// banner is the one-line self-update offer under the title bar, or "". It shows only
// where v starts the self-update.
func (m *model) banner() string {
	if m.newer == nil || (m.screen != scHome && m.screen != scInstalled && m.screen != scTarget) {
		return ""
	}
	text := fmt.Sprintf("A new version of this updater is out (%s) — press v to get it", m.newer.Version)
	return bannerSty.Render(ansi.Truncate(text, m.bodyWidth(), "…"))
}

// bodyRows is how many body lines of the current page fit in the chrome.
func (m *model) bodyRows() int {
	_, footer, _ := m.page()
	return max(fitRows(m.height, footer), 1)
}

// titleBar is the full-width bar on top: left text, right text, exactly width columns.
// The right side gives way first, then the left.
func titleBar(left, right string, width int) string {
	if width < 10 {
		return ansi.Truncate(left, width, "…")
	}
	if 1+ansi.StringWidth(left)+1 > width {
		left, right = ansi.Truncate(left, width-2, "…"), ""
	}
	lw := ansi.StringWidth(left)
	if room := width - lw - 3; ansi.StringWidth(right) > room {
		right = ansi.Truncate(right, room, "…")
	}
	gap := width - 2 - lw - ansi.StringWidth(right)
	bar := lipgloss.NewStyle().Background(barBg)
	return bar.Render(" ") + titleSty.Background(barBg).Render(left) + bar.Render(strings.Repeat(" ", gap)) +
		dimSty.Background(barBg).Render(right) + bar.Render(" ")
}

// crumb says where the player is, on the right of the title bar: the instance (if it
// is a named GTNH one) and the area of the screen.
func (m *model) crumb() string {
	var area string
	switch m.screen {
	case scLoading:
		return ""
	case scHome:
		return "Home"
	case scSelfUpdate, scSelfUpdated:
		return "Launcher update"
	case scError:
		return "Problem"
	case scName:
		return "New instance"
	case scInstalled, scTarget, scServerMods, scPreparing, scConfirm, scApplying, scDone:
		if m.creating {
			return "New instance"
		}
		area = "Update"
	case scConflicts, scResolve:
		area = "Update › Config files"
	case scSettings:
		area = "Settings"
	case scSettingEdit:
		area = "Settings › " + settingRowOf(m.setting).title
	case scBackups, scRestoreConfirm, scRestoring, scRestored:
		area = "Undo"
	case scLaunching, scPlaying:
		area = "Playing"
	}
	if !m.inst.GTNH || m.inst.Name == "" {
		return area
	}
	return m.inst.Name + " › " + area
}

// keyBar is the bar of key/label pairs at the bottom of a width-column screen.
func keyBar(width int, pairs ...string) string {
	return hintRows(width-4, pairs...)
}

// panel boxes body in a dim rounded border exactly width columns wide, with title in
// the top border when it fits.
func panel(title, body string, width int) string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dimSty.GetForeground()).
		Padding(0, 1).Width(width - 2) // Width counts the padding but not the border
	lines := strings.Split(box.Render(body), "\n")
	if tw := ansi.StringWidth(title); title != "" && tw < width-4 {
		lines[0] = dimSty.Render("╭─ ") + lipgloss.NewStyle().Bold(true).Render(title) +
			dimSty.Render(" "+strings.Repeat("─", width-5-tw)+"╮")
	}
	return strings.Join(lines, "\n")
}

type badgeKind int

const (
	badgeOK badgeKind = iota
	badgeWarn
	badgeDim
	badgeBad
)

// badge is a short coloured status tag.
func badge(kind badgeKind, text string) string {
	switch kind {
	case badgeOK:
		return okSty.Render("● " + text)
	case badgeWarn:
		return warnSty.Render("▲ " + text)
	case badgeBad:
		return badSty.Render("✕ " + text)
	}
	return dimSty.Render("○ " + text)
}

// buttons renders labels as chips in a row with labels[sel] (clamped) highlighted.
func buttons(labels []string, sel int) string {
	if len(labels) == 0 {
		return ""
	}
	sel = max(min(sel, len(labels)-1), 0)
	chip := lipgloss.NewStyle().Foreground(accent).Background(chipBg)
	parts := make([]string, len(labels))
	for i, l := range labels {
		s := chip
		if i == sel {
			s = btnSelSty
		}
		parts[i] = s.Render("[ " + l + " ]")
	}
	return strings.Join(parts, "  ")
}

// chrome wraps a screen's body and footer in the title bar, banner and frame.
func (m *model) chrome(body, footer string, scroll int) string {
	return frame(m.width, m.height, titleBar(m.header(), m.crumb(), m.width), m.banner(), body, footer, scroll)
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

// frame lays out a screen: title bar and banner on top, footer pinned at the bottom,
// body scrolled in between.
func frame(width, height int, title, banner, body, footer string, scroll int) string {
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(s, "\n")
	}
	visible, _ := bodyWindow(split(body), max(fitRows(height, footer), 1), scroll)
	out := []string{title, banner}
	for _, l := range visible {
		out = append(out, "  "+l)
	}
	if footer != "" {
		out = append(out, "")
		for _, l := range split(footer) {
			out = append(out, "  "+l)
		}
	}
	for i, l := range out {
		out[i] = ansi.Truncate(l, width, "…")
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

// hintRows renders key/label pairs like hint, wrapping whole pairs into rows no
// wider than limit.
func hintRows(limit int, pairs ...string) string {
	if limit < 20 {
		return hint(pairs...)
	}
	var rows []string
	row := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		pair := hint(pairs[i], pairs[i+1])
		if row != "" && ansi.StringWidth(row+"    "+pair) <= limit {
			row += "    " + pair
			continue
		}
		if row != "" {
			rows = append(rows, row)
		}
		row = pair
	}
	if row != "" {
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}
