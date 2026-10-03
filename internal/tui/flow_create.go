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

// showLeftover tells the player about a thrown-away creation whose folder is still
// there. Unused until slice "flows2".
func (m *model) showLeftover(err error) tea.Cmd {
	m.notify("Something went wrong", err.Error())
	return nil
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

// startCreate resets the job for a new instance. Unused until slice "flows2".
func (m *model) startCreate() tea.Cmd {
	m.target = ""
	m.serverMods, m.serverModsAsked = serverModsSetting(m.cfg.ServerMods, nil)
	return nil
}

// askName offers the name of the new instance: cfg.Name once, else the default for
// m.target. Unused until slice "flows2".
func (m *model) askName() tea.Cmd {
	name := update.DefaultInstanceName(m.target)
	if m.cfg.Name != "" && !m.nameUsed {
		name, m.nameUsed = m.cfg.Name, true
	}
	m.newName = name
	return nil
}

func (m *model) instancesDir() string { return prism.InstancesDir(m.cfg.PrismDirs[0]) }

// prepareCreate downloads the new instance. Unused until slice "flows2".
func (m *model) prepareCreate() tea.Cmd {
	m.startBusy()
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelCreate, m.quitAfterCancel = cancel, false
	opts := update.CreateOptions{
		Context: ctx, Client: m.cfg.Client, Manifest: m.manifest, InstancesDir: m.instancesDir(), Name: m.newName,
		Target: m.target, CustomModsURL: m.serverMods, CustomModsAsked: true,
	}
	rep := m.reporter()
	return func() tea.Msg {
		c, err := update.PrepareCreate(opts, rep)
		return orErr(err, createReady{c})
	}
}

// applyCreate creates the prepared instance. Unused until slice "flows2".
func (m *model) applyCreate() tea.Cmd {
	m.startBusy()
	c, rep := m.creation, m.reporter()
	m.creation = nil // Apply always closes it
	return func() tea.Msg {
		r, err := c.Apply(rep)
		return orErr(err, createdMsg{r})
	}
}
