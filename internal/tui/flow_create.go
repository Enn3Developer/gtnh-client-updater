package tui

import (
	"context"
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
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

// newInstance starts making a new instance (C5): the version from the command line once,
// else the picker.
func (m *model) newInstance() tea.Cmd {
	if m.job != nil {
		m.notifyBusy()
		return nil
	}
	if m.manifest == nil {
		return nil
	}
	if m.cfg.Target != "" && !m.targetUsed {
		m.targetUsed = true
		v, err := manifest.Resolve(m.manifest, m.cfg.Target)
		if err != nil {
			m.notify("No such version", fmt.Sprintf("GTNH has no version called %q.", m.cfg.Target))
			return nil
		}
		m.askCreateName(v)
		return nil
	}
	m.pickCreateVersion()
	return nil
}

// pickCreateVersion asks which GTNH version the new instance gets (C5).
func (m *model) pickCreateVersion() {
	now := m.now()
	rec := defaultTarget(m.manifest, "")
	items := make([]ditem, len(m.manifest.Releases))
	for i, r := range m.manifest.Releases {
		var tags []string
		if r.Version == rec {
			tags = append(tags, "recommended")
		}
		if update.NewInstanceFlavor(r) == manifest.Java8 {
			tags = append(tags, "Java 8 only")
		}
		items[i] = ditem{title: r.Version, desc: releaseDesc(r, now, tags...), key: r.Version}
	}
	m.openList("Which GTNH version do you want to install?", "", items, rec, func(m *model, v string) tea.Cmd {
		m.closeDialog()
		m.askCreateName(v)
		return nil
	})
}

// askCreateName asks the name of the new instance on target (C7): cfg.Name the first
// time, else the default name.
func (m *model) askCreateName(target string) {
	value := update.DefaultInstanceName(target)
	if m.cfg.Name != "" && !m.nameUsed {
		value, m.nameUsed = m.cfg.Name, true
	}
	m.openInput("What should the new instance be called?", "That's the name you'll see in Prism; it's also the folder name.",
		value, "", "It'll be created in "+m.instancesDir(), []string{"Create"},
		func(m *model, label, value string) tea.Cmd {
			if label == "" {
				m.closeDialog()
				return nil
			}
			if err := update.CheckInstanceName(m.instancesDir(), value); err != nil {
				m.dialog.inputErr = err.Error()
				return nil
			}
			m.closeDialog()
			return m.beginCreate(target, value)
		})
}

// beginCreate starts the job that downloads the new instance name on target (C8), with
// the server mods of the command line, and selects its pending row.
func (m *model) beginCreate(target, name string) tea.Cmd {
	m.target, m.newName = target, name
	m.serverMods, m.serverModsGiven = serverModsLink(m.cfg.ServerMods, nil)
	m.job = &job{kind: jobCreate, dir: filepath.Join(m.instancesDir(), name), title: "Getting GTNH " + target + " ready", phase: "prepare"}
	if m.home == nil {
		m.home = map[string]homeInfo{}
	}
	m.home[m.job.dir] = m.pendingInfo()
	m.selectDir(m.job.dir)
	m.focus = focusPage
	m.row, m.pageScroll = 0, 0
	return m.prepareCreate()
}

// onCreateReady keeps the prepared creation and asks for confirmation (C9); when the
// player quit during the download it is thrown away and the program quits (C10).
func (m *model) onCreateReady(c *update.Creation) tea.Cmd {
	if m.createDone() {
		c.Close()
		m.job = nil
		_, cmd := m.quit()
		return cmd
	}
	m.creation = c
	lines := []string{num(int64(c.Files)) + " files will be installed", "In " + c.Dir}
	modsLines, modsWarn := modsPreview(c.ModsPlan, m.serverMods)
	lines = append(lines, modsLines...)
	if c.Flavor == manifest.Java8 {
		lines = append(lines, "This version only comes as a Java 8 pack")
	} else {
		lines = append(lines, "Java 17+ pack")
	}
	lines = append(lines, "Your other instances aren't touched")
	var warn []string
	for _, w := range m.warns {
		warn = append(warn, "Heads up: "+w)
	}
	warn = append(warn, modsWarn...)
	m.confirmDialog("Create "+m.newName+" with GTNH "+m.target+"?", lines, warn, nil, "Create", func(m *model) tea.Cmd {
		m.job.phase, m.job.title = "apply", "Creating "+m.newName
		m.closeDialog()
		return m.applyCreate()
	}, func(m *model) tea.Cmd {
		m.closeDialog()
		m.job = nil
		if err := m.closeCreation(); err != nil {
			m.notify("Something went wrong", err.Error())
		}
		m.refresh()
		return nil
	})
	return nil
}

// onCreated ends the job, keeps its notice for the new instance, selects it and
// reloads (C9).
func (m *model) onCreated(r *update.CreateResult) tea.Cmd {
	m.job = nil
	n := notice{text: "Created just now with GTNH " + m.target + " · " + num(int64(r.Files)) + " files installed"}
	n.addMods(r.Mods, r.ModsErr, false)
	m.notices[r.Instance.Dir] = n
	m.insts = append(m.insts, r.Instance) // keeps the selection until the reload lists it
	m.selectDir(r.Instance.Dir)
	m.refresh()
	return m.reload()
}

func (m *model) instancesDir() string { return prism.InstancesDir(m.cfg.PrismDirs[0]) }

// prepareCreate downloads the new instance.
func (m *model) prepareCreate() tea.Cmd {
	m.startBusy()
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelCreate, m.quitAfterCancel = cancel, false
	opts := update.CreateOptions{
		Context: ctx, Client: m.cfg.Client, Manifest: m.manifest, InstancesDir: m.instancesDir(), Name: m.newName,
		Target: m.target, CustomModsURL: m.serverMods, CustomModsAsked: m.serverModsGiven,
	}
	rep := m.reporter()
	return func() tea.Msg {
		c, err := update.PrepareCreate(opts, rep)
		return orErr(err, createReady{c})
	}
}

// applyCreate creates the prepared instance.
func (m *model) applyCreate() tea.Cmd {
	m.startBusy()
	c, rep := m.creation, m.reporter()
	m.creation = nil // Apply always closes it
	return func() tea.Msg {
		r, err := c.Apply(rep)
		return orErr(err, createdMsg{r})
	}
}
