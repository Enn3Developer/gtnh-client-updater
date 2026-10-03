package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// sidebarShows reports whether the instance sidebar is drawn: more than one visible
// instance.
func (m *model) sidebarShows() bool {
	return m.loaded && len(m.visible()) > 1
}

// sidebarWidth is clamp(longest visible name + 4, 16, 28); 0 when the sidebar is hidden.
func (m *model) sidebarWidth() int {
	if !m.sidebarShows() {
		return 0
	}
	longest := 0
	for _, in := range m.visible() {
		longest = max(longest, ansi.StringWidth(in.Name))
	}
	return max(min(longest+4, 28), 16)
}

// sidebarView is the dim "Instances" heading and one glyph + name line per visible
// instance, scrolled to keep sel visible (C3).
func (m *model) sidebarView(height int) string {
	width := m.sidebarWidth()
	vis := m.visible()
	room := max(height-1, 1)
	first := max(m.sel-room+1, 0)
	out := []string{dimSty.Render("Instances")}
	for i := first; i < len(vis) && i < first+room; i++ {
		in := vis[i]
		g, gs := glyph(m.home[in.Dir]), glyphSty(m.home[in.Dir])
		if (m.play.dir == in.Dir && m.play.watching()) || m.jobShown(in.Dir) {
			g, gs = "◐", lipgloss.NewStyle().Foreground(accent)
		}
		name := ansi.Truncate(in.Name, width-2, "…")
		if i != m.sel {
			out = append(out, gs.Render(g)+" "+name)
			continue
		}
		sty := lipgloss.NewStyle().Foreground(accent)
		if m.focus == focusSidebar {
			sty = sty.Background(barBg)
		}
		out = append(out, sty.Render(padTo(g+" "+name, width)))
	}
	return strings.Join(out, "\n")
}

// glyph is an instance's status mark: ● up to date, ▲ newer version out, ○ not GTNH or
// version unknown (C3; the playing instance's ◐ is the sidebar's to draw).
func glyph(info homeInfo) string {
	switch {
	case !info.gtnh || info.version == "":
		return "○"
	case info.rec != info.version:
		return "▲"
	}
	return "●"
}

// glyphSty is the colour of glyph(info).
func glyphSty(info homeInfo) lipgloss.Style {
	switch glyph(info) {
	case "●":
		return okSty
	case "▲":
		return warnSty
	}
	return dimSty
}
