package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// dropSession closes the prepared update and returns to the version list.
func (m *model) dropSession() (tea.Model, tea.Cmd) {
	m.session.Close()
	m.session = nil
	return m.showTargets()
}

func (m *model) prepare() (tea.Model, tea.Cmd) {
	m.startBusy(scPreparing)
	opts := update.Options{
		Client: m.cfg.Client, Manifest: m.manifest, Instance: m.inst,
		Installed: m.detect.Version, Target: m.target, CustomModsURL: m.serverMods,
	}
	rep := m.reporter()
	return m, func() tea.Msg {
		s, err := update.Prepare(opts, rep)
		return orErr(err, preparedMsg{s})
	}
}

// apply runs the prepared update.
func (m *model) apply() (tea.Model, tea.Cmd) {
	m.startBusy(scApplying)
	s, rep := m.session, m.reporter()
	return m, func() tea.Msg {
		r, err := s.Apply(rep)
		return orErr(err, appliedMsg{r})
	}
}
