package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	return row{id: id, key: key, text: text, hint: key, run: run,
		lines: func(w int, sel bool) []string { return []string{rowLine(w, sel, text, key)} }}
}

// updateEntries are the "Update" heading, its rows and info line, and a blank line.
func (m *model) updateEntries() []entry {
	in, _ := m.current()
	info := m.home[in.Dir]
	now := m.now()
	es := textEntries(heading("Update"))
	if m.jobShown(in.Dir) {
		return append(es, entry{text: infoLine("Busy — wait for it to finish.")}, entry{})
	}
	if n, ok := m.notices[in.Dir]; ok {
		es = append(es, textEntries(m.noticeLines(measure(m.pageWidth()), n)...)...)
	}
	switch {
	case info.version == "":
		es = append(es, entry{row: ptr(hintedRow("update", "u",
			"Which version is this? Tell me and I'll check for updates",
			func(m *model) tea.Cmd { return m.startUpdate(info.rec) }))})
	case info.rec != info.version:
		text := "GTNH " + info.rec + " is out"
		if r, ok := m.release(info.rec); ok {
			text += " · " + releaseDesc(r, now)
		}
		es = append(es, entry{row: ptr(hintedRow("update", "u", text,
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

// notice is the outcome of the last job, shown in the Update section.
type notice struct {
	text string   // the ok line; "" = none
	warn []string // what went wrong or needs the player's eye
	info []string // dim extra lines
}

// chooseVersion lets the player pick the version to install for the current instance
// (C9); a version without a pack for the instance's Java is explained, not installed.
func (m *model) chooseVersion() tea.Cmd {
	if m.job != nil {
		m.notifyBusy()
		return nil
	}
	in, _ := m.current()
	info := m.home[in.Dir]
	now := m.now()
	items := make([]ditem, len(m.manifest.Releases))
	for i, r := range m.manifest.Releases {
		var tags []string
		if r.Version == info.rec {
			tags = append(tags, "recommended")
		}
		if r.Version == info.version {
			tags = append(tags, "you have this one")
		}
		if !availableFor(r, info.flavor) {
			tags = append(tags, "not available for your Java")
		}
		items[i] = ditem{title: r.Version, desc: releaseDesc(r, now, tags...), key: r.Version}
	}
	m.openList("Which GTNH version do you want?", "", items, info.rec, func(m *model, v string) tea.Cmd {
		if r, _ := m.release(v); !availableFor(r, info.flavor) {
			m.notify("Not available", "GTNH "+v+" has no "+info.flavor.String()+" pack, which is what this instance uses.")
			return nil
		}
		m.closeDialog()
		return m.startUpdate(v)
	})
	return nil
}

// availableFor reports whether r has a pack for flavor.
func availableFor(r manifest.Release, flavor manifest.Flavor) bool {
	_, err := r.URL(flavor)
	return err == nil
}

// releaseDesc is how a version list describes r: its kind and, when dated, how long ago
// it came out, then tags.
func releaseDesc(r manifest.Release, now time.Time, tags ...string) string {
	desc := strings.ToLower(kindOf(r))
	if a := ago(r.ReleaseDate, now); a != "" {
		desc += " · " + a
	}
	for _, t := range tags {
		desc += " · " + t
	}
	return desc
}

// noticeLines are the rendered lines of n in the Update section (C8): the ok line, the
// warnings and the dim info, each wrapped to width.
func (m *model) noticeLines(width int, n notice) []string {
	var out []string
	add := func(text string, sty lipgloss.Style) {
		if text == "" {
			return
		}
		for _, l := range wrapLines(text, width-2) {
			out = append(out, "  "+sty.Render(l))
		}
	}
	add(n.text, okSty)
	for _, w := range n.warn {
		add(w, warnSty)
	}
	for _, i := range n.info {
		add(i, dimSty)
	}
	return out
}
