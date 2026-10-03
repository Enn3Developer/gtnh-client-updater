package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// dropSession closes the prepared update. Unused until slice "updateflow".
func (m *model) dropSession() tea.Cmd {
	m.session.Close()
	m.session = nil
	return nil
}

// prepare downloads and plans the update of inst to m.target. Unused until slice
// "updateflow".
func (m *model) prepare(inst prism.Instance) tea.Cmd {
	m.startBusy()
	opts := update.Options{
		Client: m.cfg.Client, Manifest: m.manifest, Instance: inst,
		Installed: m.detect.Version, Target: m.target, CustomModsURL: m.serverMods,
	}
	rep := m.reporter()
	return func() tea.Msg {
		s, err := update.Prepare(opts, rep)
		return orErr(err, preparedMsg{s})
	}
}

// apply runs the prepared update. Unused until slice "updateflow".
func (m *model) apply() tea.Cmd {
	m.startBusy()
	s, rep := m.session, m.reporter()
	return func() tea.Msg {
		r, err := s.Apply(rep)
		return orErr(err, appliedMsg{r})
	}
}

// serverModsSetting is the server-mods URL to use and whether it's settled: the command
// line ("none" = off) wins over what the instance remembers (st may be nil).
func serverModsSetting(cfg string, st *update.State) (url string, asked bool) {
	switch {
	case cfg == "none":
		return "", true
	case cfg != "":
		return cfg, true
	case st != nil && st.CustomModsAsked:
		return st.CustomModsURL, true
	default:
		return "", false
	}
}
