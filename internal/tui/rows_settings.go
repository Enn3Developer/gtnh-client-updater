package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// prismDefault is the value of a setting the instance leaves to Prism.
const prismDefault = "Prism's default"

// cantReadCfg replaces the settings rows when the instance's settings can't be read.
const cantReadCfg = "I couldn't read this instance's settings"

// settingRow is a row showing label and value; enter edits the setting id. While id is
// being edited the row is the field, its help and its error; after a save it's marked (C2).
func (m *model) settingRow(id, label, value string, width int) row {
	text := labelled(label, value, width)
	return row{id: id,
		lines: func(w int, sel bool) []string {
			if e := m.edit; e != nil && e.id == id {
				out := []string{ansi.Truncate("▸ "+titleSty.Render(labelled(label, "", width))+e.input.View(), w, "…")}
				for _, l := range wrapAtMost(settingHelp(id), w-2, 3) {
					out = append(out, infoLine(l))
				}
				if e.err != "" {
					out = append(out, "  "+badSty.Render(e.err))
				}
				return out
			}
			if m.edit == nil && m.savedRow == id {
				return []string{rowLine(w, sel, text+" "+okSty.Render("✓ saved"), "")}
			}
			return []string{rowLine(w, sel, text, "")}
		},
		run: func(m *model) tea.Cmd { return m.editSetting(id) }}
}

// labelWidth is the label column width of setting id: the launcher rows have longer
// labels (C3).
func labelWidth(id string) int {
	if id == "after" || id == "prism" {
		return 24
	}
	return 15
}

// settingsRows are the instance's setting rows: memory, jvm, java, window, and for a
// GTNH instance server and mods; none when its settings can't be read (C4).
func (m *model) settingsRows() []row {
	in, ok := m.current()
	info := m.home[in.Dir]
	if !ok || info.settingsErr != nil {
		return nil
	}
	keys := []struct{ id, label string }{
		{"memory", "Memory"}, {"jvm", "Java arguments"}, {"java", "Java"}, {"window", "Window"},
	}
	if info.gtnh {
		keys = append(keys, struct{ id, label string }{"server", "Server"}, struct{ id, label string }{"mods", "Server mods"})
	}
	var out []row
	for _, k := range keys {
		out = append(out, m.settingRow(k.id, k.label, settingValue(k.id, info, m.now()), labelWidth(k.id)))
	}
	return out
}

// launcherRows are the launcher-wide rows "after" and "prism".
func (m *model) launcherRows() []row {
	after := "quit"
	if m.app.StaysOpen() {
		after = "stay open"
	}
	exe := m.app.PrismExe
	if exe == "" {
		exe = "found automatically"
	}
	return []row{
		m.settingRow("after", "After I start the game", after, labelWidth("after")),
		m.settingRow("prism", "Prism Launcher", exe, labelWidth("prism")),
	}
}

// settingsEntries are the "Settings" heading, its rows (or cantReadCfg) and a blank line.
func (m *model) settingsEntries() []entry {
	in, _ := m.current()
	es := textEntries(heading("Settings"))
	if m.home[in.Dir].settingsErr != nil {
		es = append(es, entry{text: infoLine(cantReadCfg)})
	}
	es = append(es, rowEntries(m.settingsRows())...)
	return append(es, entry{})
}

// launcherEntries are the "Launcher" heading and its rows.
func (m *model) launcherEntries() []entry {
	return append(textEntries(heading("Launcher")), rowEntries(m.launcherRows())...)
}

// settingsSection is the "Settings" heading, its rows (or cantReadCfg) and a blank line.
func (m *model) settingsSection(width int) []string {
	return m.sectionLines(width, m.settingsEntries())
}

// launcherSection is the "Launcher" heading and its rows.
func (m *model) launcherSection(width int) []string {
	return m.sectionLines(width, m.launcherEntries())
}

// settingValue is what the row of setting key shows for an instance (C4).
func settingValue(key string, info homeInfo, now time.Time) string {
	s := info.settings
	switch key {
	case "memory":
		if s.OverrideMemory {
			return fmt.Sprintf("%d MB (at least %d MB)", s.MaxMemMB, s.MinMemMB)
		}
	case "jvm":
		if s.OverrideJavaArgs {
			return s.JvmArgs
		}
	case "java":
		if s.OverrideJavaLocation {
			return s.JavaPath
		}
	case "window":
		if s.OverrideWindow {
			return fmt.Sprintf("%d×%d", s.WinWidth, s.WinHeight)
		}
	case "server":
		return orNone(info.server)
	case "mods":
		return modsValue(info, now)
	}
	return prismDefault
}

// modsValue is what the Server mods row shows: the link's host and, once synced, how
// many mods it has and when they were last synced.
func modsValue(info homeInfo, now time.Time) string {
	switch {
	case info.modsURL == "":
		return "none"
	case info.synced.IsZero():
		return hostOf(info.modsURL) + " · not synced yet"
	}
	return fmt.Sprintf("%s · %d %s · synced %s", hostOf(info.modsURL), info.mods, plural(info.mods, "mod", "mods"), ago(info.synced, now))
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
