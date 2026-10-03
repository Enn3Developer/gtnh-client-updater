package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// prismDefault is the value of a setting the instance leaves to Prism.
const prismDefault = "Prism's default"

// cantReadCfg replaces the settings rows when the instance's settings can't be read.
const cantReadCfg = "I couldn't read this instance's settings"

// settingRow is a row showing label and value; enter edits the setting id.
func settingRow(id, label, value string) row {
	text := labelled(label, value)
	return row{id: id,
		lines: func(w int, sel bool) []string { return []string{rowLine(w, sel, text, "")} },
		run:   func(m *model) tea.Cmd { return m.editSetting(id) }}
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
		out = append(out, settingRow(k.id, k.label, settingValue(k.id, info)))
	}
	return out
}

// launcherRows are the launcher-wide rows "after" and "prism".
func (m *model) launcherRows() []row {
	after := "quit"
	if m.app.StaysOpen() {
		after = "stay open and show whether it's running"
	}
	exe := m.app.PrismExe
	if exe == "" {
		exe = "found automatically"
	}
	return []row{
		settingRow("after", "After I start the game", after),
		settingRow("prism", "Prism Launcher", exe),
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
func settingValue(key string, info homeInfo) string {
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
		return orNone(hostOf(info.modsURL))
	}
	return prismDefault
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
