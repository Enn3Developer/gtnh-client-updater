package tui

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// resolveCfgTarget turns the command line's version (e.g. "latest") into a manifest key.
func (m *model) resolveCfgTarget() error {
	if m.cfg.Target == "" {
		return nil
	}
	v, err := manifest.Resolve(m.manifest, m.cfg.Target)
	if err != nil {
		if m.cfg.Target != "latest-stable" {
			err = fmt.Errorf("GTNH has no version called %q.", m.cfg.Target)
		}
		return err
	}
	m.cfg.Target = v
	return nil
}

func (m *model) afterLoad() (tea.Model, tea.Cmd) {
	if err := m.resolveCfgTarget(); err != nil {
		return m, errCmd(err)
	}
	if m.cfg.Create {
		return m.startCreate()
	}
	if m.cfg.Instance != "" {
		for _, in := range m.insts {
			if in.Name == m.cfg.Instance || sameDir(in.Dir, m.cfg.Instance) {
				return m.pickInstance(in)
			}
		}
		if in, err := prism.LoadInstance(m.cfg.Instance); err == nil {
			m.insts = append(m.insts, in)
			return m.pickInstance(in)
		}
		return m, errCmd(fmt.Errorf("I couldn't find an instance called %q.", m.cfg.Instance))
	}
	var gtnh []prism.Instance
	for _, in := range m.insts {
		if in.GTNH {
			gtnh = append(gtnh, in)
		}
	}
	if len(m.insts) == 0 {
		return m.startCreate()
	}
	if len(gtnh) == 1 {
		return m.pickInstance(gtnh[0])
	}
	m.showAll = len(gtnh) == 0
	return m.showInstances()
}

func sameDir(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
}

// choose handles Enter on a list screen.
func (m *model) choose(key string) (tea.Model, tea.Cmd) {
	switch m.screen {
	case scInstance:
		for _, in := range m.insts {
			if in.Dir == key {
				return m.pickInstance(in)
			}
		}
	case scInstalled:
		m.detect = update.Detection{Version: key, Source: "you told me"}
		return m.showTargets()
	case scTarget:
		m.target = key
		return m.proceed()
	}
	return m, nil
}

// proceed continues once the target is chosen: server mods if not asked yet, then the
// name of a new instance or the update.
func (m *model) proceed() (tea.Model, tea.Cmd) {
	if !m.serverModsAsked {
		return m.askServerMods(true)
	}
	if m.creating {
		return m.askName()
	}
	return m.prepare()
}

func (m *model) instancesDir() string { return prism.InstancesDir(m.cfg.PrismDirs[0]) }

func (m *model) pickInstance(in prism.Instance) (tea.Model, tea.Cmd) {
	m.inst, m.target, m.creating = in, "", false
	if prism.Running(in) {
		return m, errCmd(fmt.Errorf("%s is running right now. Close Minecraft, then start me again.", in.Name))
	}
	st, err := update.LoadState(in.Dir)
	if err != nil {
		return m, errCmd(fmt.Errorf("I couldn't read my notes about this instance: %w", err))
	}
	m.detect = update.DetectVersion(in, st, m.manifest)
	if m.cfg.Installed != "" {
		m.detect = update.Detection{Version: m.cfg.Installed, Source: "command line"}
	}
	m.serverMods, m.serverModsAsked = serverModsSetting(m.cfg.ServerMods, st)
	if m.detect.Version == "" {
		return m.showInstalled()
	}
	return m.showTargets()
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

func (m *model) askServerMods(thenPrepare bool) (tea.Model, tea.Cmd) {
	m.modsThenPrepare = thenPrepare
	m.screen = scServerMods
	return m, m.focusInput(&m.input, m.serverMods)
}

// focusInput fills ti with value, cursor at the end, and focuses it.
func (m *model) focusInput(ti *textinput.Model, value string) tea.Cmd {
	ti.SetValue(value)
	ti.CursorEnd()
	ti.Width = m.inputWidth()
	return ti.Focus()
}

// canGoBack reports whether the error screen may return to a list: not after a failed
// apply (the player must read what happened) and not before anything was loaded.
func (m *model) canGoBack() bool {
	return m.manifest != nil && m.errPhase != scApplying && m.errPhase != scLoading
}

// goBackFromError returns to the version list after a failed download or self-update,
// otherwise to the instance list (e.g. the picked instance was running).
func (m *model) goBackFromError() (tea.Model, tea.Cmd) {
	if (m.inst.Dir != "" || m.creating) && (m.errPhase == scPreparing || m.errPhase == scSelfUpdate) {
		return m.showTargets()
	}
	return m.showInstances()
}
