package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// closeCreation throws away the prepared, unapplied creation, if any. The error is a
// folder it couldn't remove.
func (m *model) closeCreation() error {
	c := m.creation
	m.creation = nil
	if c == nil {
		return nil
	}
	return c.Close()
}

// showLeftover shows the error of a thrown-away creation whose folder is still there.
// The creation never got past its download, hence the preparing phase.
func (m *model) showLeftover(err error) (tea.Model, tea.Cmd) {
	m.err, m.errPhase, m.screen = err, scPreparing, scError
	return m, nil
}

// createDone marks the running PrepareCreate as returned and reports whether the
// player is waiting for it to quit.
func (m *model) createDone() bool {
	if m.cancelCreate == nil {
		return false
	}
	m.cancelCreate()
	m.cancelCreate = nil
	return m.quitAfterCancel
}

// startCreate enters the create flow: pick a version, server mods, a name, then create.
func (m *model) startCreate() (tea.Model, tea.Cmd) {
	m.creating, m.inst, m.target = true, prism.Instance{}, ""
	m.serverMods, m.serverModsAsked = serverModsSetting(m.cfg.ServerMods, nil)
	return m.showTargets()
}

func (m *model) askName() (tea.Model, tea.Cmd) {
	name := update.DefaultInstanceName(m.target)
	if m.cfg.Name != "" && !m.nameUsed {
		name, m.nameUsed = m.cfg.Name, true
	}
	m.screen = scName
	return m, m.focusInput(&m.nameIn, name)
}

func (m *model) prepareCreate() (tea.Model, tea.Cmd) {
	m.startBusy(scPreparing)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelCreate, m.quitAfterCancel = cancel, false
	opts := update.CreateOptions{
		Context: ctx, Client: m.cfg.Client, Manifest: m.manifest, InstancesDir: m.instancesDir(), Name: m.newName,
		Target: m.target, CustomModsURL: m.serverMods, CustomModsAsked: true,
	}
	rep := m.reporter()
	return m, func() tea.Msg {
		c, err := update.PrepareCreate(opts, rep)
		return orErr(err, createReady{c})
	}
}

// applyCreate creates the prepared instance.
func (m *model) applyCreate() (tea.Model, tea.Cmd) {
	m.startBusy(scApplying)
	c, rep := m.creation, m.reporter()
	m.creation = nil // Apply always closes it
	return m, func() tea.Msg {
		r, err := c.Apply(rep)
		return orErr(err, createdMsg{r})
	}
}
