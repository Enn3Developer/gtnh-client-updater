package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// statusBar is the bottom line: key/label pairs on barBg across width; pairs that don't
// fit are dropped from the end, a lone first pair is truncated (C7).
func statusBar(width int, pairs ...string) string {
	bg := lipgloss.NewStyle().Background(barBg)
	keySty := titleSty.Background(barBg)
	labelSty := dimSty.Background(barBg)
	text, room := "", width-1
	for i := 0; i+1 < len(pairs); i += 2 {
		pair := keySty.Render(pairs[i]) + bg.Render(" ") + labelSty.Render(pairs[i+1])
		if text == "" {
			text = ansi.Truncate(pair, room, "…")
			continue
		}
		next := text + bg.Render("   ") + pair
		if ansi.StringWidth(next) > room {
			break
		}
		text = next
	}
	line := bg.Render(" ") + text
	return line + bg.Render(strings.Repeat(" ", max(width-ansi.StringWidth(line), 0)))
}

// statusPairs are the key/label pairs for what's on screen now (C7).
func (m *model) statusPairs() []string {
	switch {
	case !m.loaded || m.loadErr != nil:
		return []string{"q", "quit"}
	case m.dialog != nil && m.dialog.buttons == nil && m.dialog.list == nil && m.dialog.input == nil:
		return []string{"updating", "please don't close this window"} // the progress dialog (C14)
	case m.dialog != nil:
		return m.dialogPairs()
	}
	var pairs []string
	if m.focus == focusSidebar {
		pairs = []string{"↑↓", "instance", "enter", "play", "tab", "page"}
	} else {
		enter := "run"
		if m.editsRow() {
			enter = "edit"
		}
		pairs = []string{"↑↓", "move", "enter", enter}
		if m.sidebarShows() {
			pairs = append(pairs, "tab", "instances")
		}
	}
	pairs = append(pairs, "n", "new instance")
	if m.showAll || slices.ContainsFunc(m.insts, func(in prism.Instance) bool { return !in.GTNH }) {
		pairs = append(pairs, "a", "show all")
	}
	if m.busyApplying() {
		return append(pairs, "updating", "please don't close this window")
	}
	if m.quitAfterCancel && m.job != nil {
		return append(pairs, "stopping", "cleaning up…")
	}
	return append(pairs, "q", "quit")
}

// editsRow reports whether the selected page row is a settings or launcher row.
func (m *model) editsRow() bool {
	rs := m.rows()
	if m.row < 0 || m.row >= len(rs) {
		return false
	}
	id := rs[m.row].id
	for _, r := range append(m.settingsRows(), m.launcherRows()...) {
		if r.id == id {
			return true
		}
	}
	return false
}
