package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

func (m *model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m.quit()
	}
	if m.buttonLabels() != nil {
		if handled, md, cmd := m.keyButtons(k); handled {
			return md, cmd
		}
	}
	if m.scrollable() && m.scrollKey(k.String()) {
		return m, nil
	}
	switch m.screen {
	case scHome:
		return m.keyHome(k)
	case scInstalled, scTarget:
		return m.keyList(k)
	case scServerMods:
		return m.keyServerMods(k)
	case scSettings:
		return m.keySettings(k)
	case scSettingEdit:
		return m.keySettingEdit(k)
	case scName:
		return m.keyName(k)
	case scConflicts:
		return m.keyConflicts(k)
	case scResolve:
		return m.keyResolve(k)
	case scConfirm:
		if m.creating {
			return m.keyConfirmCreate(k)
		}
		return m.keyConfirm(k)
	case scError:
		return m.keyError(k)
	case scDone:
		return m.keyFinished(k)
	case scSelfUpdated:
		return m.keySelfUpdated(k)
	case scPlaying:
		return m.keyPlaying(k)
	case scBackups:
		return m.keyBackups(k)
	case scRestoreConfirm:
		return m.keyRestoreConfirm(k)
	case scRestored:
		return m.keyRestored(k)
	case scLaunching:
		// Busy: Prism is being started.
	case scLoading, scPreparing, scApplying, scRestoring, scSelfUpdate:
		// Busy: only ctrl+c (handled above) interrupts.
	}
	return m, nil
}

// updateList hands a message to the list on screen.
func (m *model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *model) keyList(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	var back func() (tea.Model, tea.Cmd)
	if len(m.insts) > 0 {
		back = m.showHome
	}
	if md, cmd, ok := m.listKey(k, back); ok {
		return md, cmd
	}
	switch k.String() {
	case "enter":
		if key, ok := m.selectedKey(); ok {
			return m.choose(key)
		}
		return m, nil
	case "v":
		if m.newer != nil {
			return m.startSelfUpdate()
		}
	case "i":
		if m.screen == scTarget && !m.creating {
			return m.showInstalled()
		}
	case "m":
		if m.screen == scTarget {
			return m.askServerMods(false)
		}
	}
	return m.updateList(k)
}

func (m *model) keyServerMods(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		v := strings.TrimSpace(m.input.Value())
		if v != "" {
			if err := update.CheckCustomModsURL(v); err != nil {
				m.inputEr = msgBadModsLink
				return m, nil
			}
		}
		m.serverMods, m.serverModsAsked, m.inputEr = v, true, ""
		if m.modsThenPrepare {
			return m.proceed()
		}
		return m.showTargets()
	case "esc":
		m.inputEr = ""
		return m.showTargets()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m *model) keyName(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		v := strings.TrimSpace(m.nameIn.Value())
		if err := update.CheckInstanceName(m.instancesDir(), v); err != nil {
			m.nameEr = err.Error()
			return m, nil
		}
		m.newName, m.nameEr = v, ""
		return m.prepareCreate()
	case "esc":
		m.nameEr = ""
		return m.showTargets()
	}
	var cmd tea.Cmd
	m.nameIn, cmd = m.nameIn.Update(k)
	return m, cmd
}

func (m *model) keyConfirm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y":
		return m.apply()
	case "esc", "n", "q":
		return m.dropSession()
	}
	return m, nil
}

func (m *model) keyError(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		if m.canGoBack() {
			return m.goBackFromError()
		}
		return m.quit()
	case "q":
		return m.quit()
	}
	return m, nil
}

func (m *model) keyFinished(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.selectCreated()
		return m.reloadHome()
	case "p":
		m.selectCreated()
		return m.play(false)
	case "q":
		return m.quit()
	}
	return m, nil
}

// selectCreated makes a just-created instance the current one.
func (m *model) selectCreated() {
	if m.creating && m.created != nil {
		m.inst = m.created.Instance
	}
}

func (m *model) keySelfUpdated(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "esc":
		return m.quit()
	}
	return m, nil
}

func (m *model) keyConfirmCreate(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y":
		return m.applyCreate()
	case "esc", "n", "q":
		if err := m.closeCreation(); err != nil {
			return m.showLeftover(err)
		}
		return m.showTargets()
	}
	return m, nil
}

func (m *model) keyConflicts(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if md, cmd, ok := m.listKey(k, m.dropSession); ok {
		return md, cmd
	}
	pl := m.session.Plan
	switch k.String() {
	case "enter":
		key, _ := m.selectedKey()
		switch key {
		case "new":
			pl.ChooseAll(update.TakeNew)
		case "mine":
			pl.ChooseAll(update.KeepMine)
		case "pick":
			return m.showResolve()
		default:
			return m, nil
		}
		m.screen = scConfirm
		return m, nil
	}
	return m.updateList(k)
}

func (m *model) keyResolve(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if md, cmd, ok := m.listKey(k, m.showConflicts); ok {
		return md, cmd
	}
	pl := m.session.Plan
	switch k.String() {
	case " ":
		if key, ok := m.selectedKey(); ok {
			c := update.TakeNew
			if pl.ChoiceOf(key) == update.TakeNew {
				c = update.KeepMine
			}
			pl.Choose(key, c)
			return m, m.refreshResolve()
		}
		return m, nil
	case "n":
		pl.ChooseAll(update.TakeNew)
		return m, m.refreshResolve()
	case "k":
		pl.ChooseAll(update.KeepMine)
		return m, m.refreshResolve()
	case "enter":
		m.screen = scConfirm
		return m, nil
	}
	return m.updateList(k)
}
