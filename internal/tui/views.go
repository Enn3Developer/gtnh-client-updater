package tui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	body, footer, scroll := m.page()
	return m.chrome(body, footer, scroll)
}

// page builds the current screen for chrome: body, the pinned footer and the scroll
// offset to show.
func (m *model) page() (body, footer string, scroll int) {
	scroll = m.scroll
	switch m.screen {
	case scHome:
		body = m.list.View()
		if m.homeCardShows(scHome) {
			body = lipgloss.JoinHorizontal(lipgloss.Top, body, m.cardView())
		}
		footer = m.homeHelp()
	case scInstalled, scTarget, scSettings, scConflicts, scResolve:
		body, footer = m.titledList(), keyBar(m.width, m.listKeyPairs()...)
	case scLoading:
		body, scroll = m.spin.View()+" Looking for your GTNH instances…", math.MaxInt32
	case scServerMods:
		body, footer = m.serverModsView()
	case scName:
		body, footer = m.nameView()
	case scSettingEdit:
		body, footer = m.settingEditView()
	case scBackups:
		if len(m.backups) > 0 {
			body, footer = m.titledList(), keyBar(m.width, m.listKeyPairs()...)
		} else {
			body, footer = m.backupsView()
		}
	case scRestoreConfirm:
		body, footer = m.restoreConfirmView()
	case scRestored:
		body, footer = m.restoredView()
	case scPreparing, scApplying, scRestoring, scSelfUpdate:
		body, footer = m.busyView()
		scroll = math.MaxInt32 // keep the newest step in view
	case scConfirm:
		if m.creating {
			body, footer = m.confirmCreateView()
		} else {
			body, footer = m.confirmView()
		}
	case scDone:
		if m.creating {
			body, footer = m.createdView()
		} else {
			body, footer = m.doneView()
		}
	case scLaunching:
		body, footer = m.launchingView()
	case scPlaying:
		body, footer = m.playingView()
	case scError:
		body, footer = m.errorView()
	case scSelfUpdated:
		b := m.blocks()
		b.success("The launcher is now version " + m.newer.Version + ".")
		body = b.String()
		footer = m.buttonFooter("enter", "restart now", "q", "quit")
	}
	return strings.TrimRight(body, "\n"), footer, scroll
}

// titledList is a non-home list under its wrapped title. The list's own title bar,
// empty but for the filter prompt, is the blank line between them.
func (m *model) titledList() string {
	return titleSty.Render(m.list.Title) + "\n" + m.list.View()
}
