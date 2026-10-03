package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
)

// updateRows are the update, versions and undo rows of a GTNH instance (C4).
func (m *model) updateRows() []row {
	var out []row
	for _, e := range m.updateEntries() {
		if e.row != nil {
			out = append(out, *e.row)
		}
	}
	return out
}

// hintedRow is a one-line row with text and its shortcut as the hint.
func hintedRow(id, key, text string, run func(m *model) tea.Cmd) row {
	return row{id: id, key: key, run: run,
		lines: func(w int, sel bool) []string { return []string{rowLine(w, sel, text, key)} }}
}

// updateEntries are the "Update" heading, its rows and info line, and a blank line.
func (m *model) updateEntries() []entry {
	in, _ := m.current()
	info := m.home[in.Dir]
	now := time.Now()
	es := textEntries(heading("Update"))
	switch {
	case info.version == "":
		es = append(es, entry{row: ptr(hintedRow("update", "u",
			"I'm not sure which version this is — tell me and I'll check for updates",
			func(m *model) tea.Cmd { return m.startUpdate(info.rec) }))})
	case info.rec != info.version:
		parts := []string{"GTNH " + info.rec + " is out"}
		if r, ok := m.release(info.rec); ok {
			parts = append(parts, strings.ToLower(kindOf(r)))
			if a := ago(r.ReleaseDate, now); a != "" {
				parts = append(parts, a)
			}
		}
		es = append(es, entry{row: ptr(hintedRow("update", "u", strings.Join(parts, " · "),
			func(m *model) tea.Cmd { return m.startUpdate(info.rec) }))})
	default:
		es = append(es, entry{text: infoLine("You have the newest stable version.")})
	}
	es = append(es, entry{row: ptr(hintedRow("versions", "o", "Choose another version…",
		func(m *model) tea.Cmd { return m.chooseVersion() }))})
	if b := info.backup; b != nil {
		text := "Last update " + b.Info.From + " → " + b.Info.To + ", " + ago(b.Info.When, now) + " · undo"
		es = append(es, entry{row: ptr(hintedRow("undo", "b", text,
			func(m *model) tea.Cmd { return m.startUndo() }))})
	}
	return append(es, entry{})
}

func ptr(r row) *row { return &r }

// release is the manifest entry of version.
func (m *model) release(version string) (manifest.Release, bool) {
	if m.manifest != nil {
		for _, r := range m.manifest.Releases {
			if r.Version == version {
				return r, true
			}
		}
	}
	return manifest.Release{}, false
}

// updateSection is the "Update" heading, its rows and info line, and a blank line.
func (m *model) updateSection(width int) []string {
	return m.sectionLines(width, m.updateEntries())
}

// kindOf turns a manifest title plus version into words a player knows.
func kindOf(r manifest.Release) string {
	v := strings.ToLower(r.Version)
	switch {
	case r.Stable():
		return "Stable release"
	case strings.Contains(v, "rc"):
		return "Release candidate"
	case strings.Contains(v, "beta"):
		return "Beta"
	case strings.Contains(v, "pre"):
		return "Pre-release"
	}
	return r.Title
}

// defaultTarget preselects the newest stable release, unless the player already runs
// something newer (a beta or RC): then the newest release overall, so a blind Enter
// never downgrades.
func defaultTarget(man *manifest.Manifest, installed string) string {
	for _, r := range man.Releases {
		if r.Stable() && manifest.CompareVersions(r.Version, installed) >= 0 {
			return r.Version
		}
	}
	return man.Releases[0].Version
}
