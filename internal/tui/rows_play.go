package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
)

// playRows are the play row and, when the instance has a server, the join row. While
// the game of this instance is watched, the play row says how it's going (C6); while a
// job runs on it, the job row replaces them both.
func (m *model) playRows() []row {
	in, ok := m.current()
	if !ok {
		return nil
	}
	if m.jobShown(in.Dir) {
		return []row{m.jobRow()}
	}
	info := m.home[in.Dir]
	rows := []row{{id: "play", key: "enter", lines: m.playLines, run: func(m *model) tea.Cmd {
		if in, ok := m.current(); ok && m.gameShown(in.Dir) {
			m.dismissPlay()
			return nil
		}
		return m.playCmd(false)
	}}}
	if !m.gameShown(in.Dir) {
		rows[0].text, rows[0].hint = "Play", "enter"
	}
	if info.gtnh && info.server != "" {
		rows = append(rows, hintedRow("join", "j", "Play and join "+info.server,
			func(m *model) tea.Cmd { return m.playCmd(true) }))
	}
	return rows
}

// playLines draws the play row: "Play", or how the watched game of this instance is
// doing.
func (m *model) playLines(w int, sel bool) []string {
	in, _ := m.current()
	if !m.gameShown(in.Dir) {
		return []string{rowLine(w, sel, "Play", "enter")}
	}
	switch m.play.state {
	case "starting":
		return wrappedRow(w, sel, "◐ Prism Launcher is starting the game…")
	case "running":
		lines := []string{rowLine(w, sel, "● The game is running since "+m.play.since.Format("15:04"), "running")}
		for _, l := range wrapLines("Leave me open or quit — the game keeps running either way.", w-2) {
			lines = append(lines, infoLine(l))
		}
		return lines
	case "closed":
		return wrappedRow(w, sel, "The game closed.")
	case "slow":
		return wrappedRow(w, sel, "I haven't seen the game start yet — have a look at Prism's window.")
	}
	return wrappedRow(w, sel, "The game should be starting from Prism now; I can't tell on this computer whether it's running.")
}

// wrappedRow is a hintless row whose text is word-wrapped to the row width, the
// continuation lines indented like the row body.
func wrappedRow(w int, sel bool, text string) []string {
	parts := wrapLines(text, w-2)
	lines := []string{rowLine(w, sel, parts[0], "")}
	for _, p := range parts[1:] {
		lines = append(lines, rowLine(w, false, p, ""))
	}
	return lines
}

// wrapLines word-wraps s to width columns (hard-breaking words longer than that).
func wrapLines(s string, width int) []string {
	return strings.Split(ansi.Wrap(s, max(width, 1), ""), "\n")
}

// wrapAtMost is wrapLines cut to n lines, the last ending in "…" when text was dropped.
func wrapAtMost(s string, width, n int) []string {
	lines := wrapLines(s, width)
	if len(lines) > n {
		lines = lines[:n]
		lines[n-1] = ansi.Truncate(lines[n-1]+" …", max(width, 1), "…")
	}
	return lines
}

// playEntries are the hero (name, meta line, blank), the play rows and a blank line.
func (m *model) playEntries() []entry {
	in, _ := m.current()
	info := m.home[in.Dir]
	played := "never played"
	if info.played != "" {
		played = "played " + info.played
	}
	meta := "Not a GTNH instance · " + played
	if info.gtnh {
		java := "Java 8"
		if info.flavor == manifest.Java17 {
			java = "Java 17+"
		}
		ver := "GTNH " + info.version
		if info.version == "" {
			ver = "GTNH · version unknown"
		}
		meta = ver + " · " + java + " · " + played
	}
	es := textEntries(titleSty.Render(in.Name), dimSty.Render(meta), "")
	es = append(es, rowEntries(m.playRows())...)
	return append(es, entry{})
}

// playSection is the hero (name, meta line, blank), the play rows and a blank line.
func (m *model) playSection(width int) []string {
	return m.sectionLines(width, m.playEntries())
}
