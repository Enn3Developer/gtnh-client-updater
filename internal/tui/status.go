package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// statusBar is the bottom line: key/label pairs on barBg across width; pairs that don't
// fit are dropped from the end, a lone first pair is truncated (C7). A last "q quit"
// stays: the pairs before it give way first, so a narrow terminal still says how to quit.
func statusBar(width int, pairs ...string) string {
	if n := len(pairs) / 2; n > 1 && pairs[2*n-2] == "q" && pairsWidth(pairs...) > width-1 {
		quit := pairs[2*n-2 : 2*n]
		for k := n - 1; k >= 1; k-- {
			if pairsWidth(pairs[:2*k]...)+3+pairsWidth(quit...) <= width-1 {
				return barOf(width, append(slices.Clone(pairs[:2*k]), quit...)...)
			}
		}
	}
	return barOf(width, pairs...)
}

// pairsWidth is how wide the pairs are on the status bar.
func pairsWidth(pairs ...string) int {
	w := 0
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			w += 3
		}
		w += ansi.StringWidth(pairs[i+1])
		if pairs[i] != "" {
			w += ansi.StringWidth(pairs[i]) + 1
		}
	}
	return w
}

// barOf draws pairs as statusBar does, dropping from the end what doesn't fit.
func barOf(width int, pairs ...string) string {
	bg := lipgloss.NewStyle().Background(barBg)
	keySty := titleSty.Background(barBg)
	labelSty := dimSty.Background(barBg)
	text, room := "", width-1
	for i := 0; i+1 < len(pairs); i += 2 {
		pair := labelSty.Render(pairs[i+1])
		if pairs[i] != "" {
			pair = keySty.Render(pairs[i]) + bg.Render(" ") + pair
		}
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

// cantQuitNow replaces the "please don't close" pair after q or ctrl+c was refused
// mid-apply, until the next key.
const cantQuitNow = "I can't quit in the middle of this — wait for it to finish"

// statusPairs are the key/label pairs for what's on screen now (C7).
func (m *model) statusPairs() []string {
	switch {
	case m.noPrism && m.dialog == nil:
		return []string{"enter", "show me where", "q", "quit"}
	case m.noPrism:
		return m.dialogPairs()
	case !m.loaded || m.loadErr != nil:
		return []string{"q", "quit"}
	case m.dialog != nil && m.dialog.pairs != nil:
		return m.dialog.pairs(m)
	case m.dialog != nil && m.dialog.buttons == nil && m.dialog.list == nil && m.dialog.input == nil:
		if m.quitRefused {
			return []string{"", cantQuitNow}
		}
		return []string{"", "updating the launcher — please don't close this window"} // the progress dialog (C6)
	case m.dialog != nil:
		return m.dialogPairs()
	case m.edit != nil:
		return []string{"enter", "save", "esc", "cancel"}
	}
	if len(m.visible()) == 0 { // the empty page: nothing to move or run
		pairs := []string{"enter", "new instance"}
		if m.hasOthers() {
			pairs = append(pairs, "a", "show all")
		}
		return append(pairs, "q", "quit")
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
	if m.busyApplying() && m.quitRefused {
		return append(pairs, "", cantQuitNow)
	}
	if m.busyApplying() {
		return append(pairs, "", "updating — please don't close this window")
	}
	pairs = append(pairs, "n", "new instance")
	if m.showAll || m.hasOthers() {
		pairs = append(pairs, "a", "show all")
	}
	if m.quitAfterCancel && m.job != nil {
		return append(pairs, "stopping", "cleaning up…")
	}
	return append(pairs, "q", "quit")
}

// hasOthers reports whether Prism has instances that aren't GTNH.
func (m *model) hasOthers() bool {
	return slices.ContainsFunc(m.insts, func(in prism.Instance) bool { return !in.GTNH })
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
