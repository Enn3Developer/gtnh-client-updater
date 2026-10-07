package tui

import (
	"errors"
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

func (m *model) load() tea.Msg {
	if len(m.cfg.PrismDirs) == 0 {
		return errMsg{errors.New("I couldn't find Prism Launcher on this computer.\n\n" +
			"If it's installed somewhere unusual, start me with\n  -prism-dir <the folder that contains prismlauncher.cfg>")}
	}
	man, err := manifest.Fetch(m.cfg.Client)
	if err != nil {
		return errMsg{fmt.Errorf("I couldn't reach the GTNH download server to see which versions exist. "+
			"Check your internet connection and try again.\n\n(%w)", err)}
	}
	return loadedMsg{man, listInstances(m.cfg.PrismDirs)} // no instances at all: the page offers to create one
}

// listInstances lists the instances of every Prism data dir; unreadable dirs are skipped.
func listInstances(dirs []string) []prism.Instance {
	var insts []prism.Instance
	for _, d := range dirs {
		if found, err := prism.ListInstances(prism.InstancesDir(d)); err == nil {
			insts = append(insts, found...)
		}
	}
	return insts
}

// afterLoad marks the workspace loaded, preselects Config.Instance (by name or folder),
// refreshes and starts the creation Config.Create asks for, else the update Config.Target
// asks for, else the game when Config.Play asks to (its mods synced from
// Config.ServerMods when given).
func (m *model) afterLoad() tea.Cmd {
	m.loaded = true
	m.focus = focusSidebar
	if m.cfg.Instance != "" {
		in, err := m.cfgInstance()
		if err != nil {
			m.refresh()
			return errCmd(err)
		}
		if !in.GTNH {
			m.showAll = true
		}
		m.selectDir(in.Dir)
	}
	m.refresh()
	if m.cfg.Create {
		return m.newInstance()
	}
	if _, ok := m.current(); ok && m.cfg.Target != "" {
		v, err := manifest.Resolve(m.manifest, m.cfg.Target)
		if err != nil {
			m.notify("No such version", fmt.Sprintf("GTNH has no version called %q.", m.cfg.Target))
			return nil
		}
		return m.startUpdate(v)
	}
	if m.cfg.Play {
		m.modsOverride = m.cfg.ServerMods
		return m.playCmd(false)
	}
	return nil
}

// cfgInstance finds the instance named on the command line, by name or folder; a folder
// outside the scanned Prism dirs is loaded and added to m.insts.
func (m *model) cfgInstance() (prism.Instance, error) {
	for _, in := range m.insts {
		if in.Name == m.cfg.Instance || sameDir(in.Dir, m.cfg.Instance) {
			return in, nil
		}
	}
	if in, err := prism.LoadInstance(m.cfg.Instance); err == nil {
		m.insts = append(m.insts, in)
		return in, nil
	}
	return prism.Instance{}, fmt.Errorf("I couldn't find an instance called %q.", m.cfg.Instance)
}

func sameDir(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
}

// reload lists the instances again (the manifest is kept) and sends reloadedMsg.
func (m *model) reload() tea.Cmd {
	dirs := m.cfg.PrismDirs
	return func() tea.Msg { return reloadedMsg{listInstances(dirs)} }
}

// onReloaded takes the new instance list, keeping sel on the same Dir (else 0), and
// refreshes.
func (m *model) onReloaded(msg reloadedMsg) {
	dir := ""
	if in, ok := m.current(); ok {
		dir = in.Dir
	}
	m.insts = msg.insts
	m.selectDir(dir)
	m.refresh()
}
