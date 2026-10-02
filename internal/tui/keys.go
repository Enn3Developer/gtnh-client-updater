package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

func (m *model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m.quit()
	}
	if m.scrollable() && m.scrollKey(k.String()) {
		return m, nil
	}
	switch m.screen {
	case scInstance, scInstalled, scTarget:
		return m.keyList(k)
	case scServerMods:
		return m.keyServerMods(k)
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
	case scLoading, scPreparing, scApplying, scSelfUpdate:
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
	if m.list.FilterState() == list.Filtering {
		return m.updateList(k) // typing into the filter: let the list have every key
	}
	switch k.String() {
	case "enter":
		if sel, ok := m.list.SelectedItem().(item); ok {
			return m.choose(sel.key)
		}
		return m, nil
	case "u":
		if m.newer != nil {
			return m.startSelfUpdate()
		}
	case "esc":
		if m.list.FilterState() == list.Unfiltered && m.screen != scInstance && len(m.insts) > 0 {
			return m.showInstances()
		}
	case "i":
		if m.screen == scTarget && !m.creating {
			return m.showInstalled()
		}
	case "n":
		if m.screen == scInstance {
			return m.startCreate()
		}
	case "m":
		if m.screen == scTarget {
			return m.askServerMods(false)
		}
	case "a":
		if m.screen == scInstance {
			m.showAll = !m.showAll
			return m.showInstances()
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
				m.inputEr = "That doesn't look like a download link — it should start with https://"
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
	case "enter", "y":
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
	case "q", "enter":
		return m.quit()
	}
	return m, nil
}

func (m *model) keyFinished(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "enter", "esc":
		return m.quit()
	}
	return m, nil
}

func (m *model) keySelfUpdated(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		m.restart = true
		return m.quit()
	case "q", "esc":
		return m.quit()
	}
	return m, nil
}

func (m *model) keyConfirmCreate(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter", "y":
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
	if m.list.FilterState() == list.Filtering {
		return m.updateList(k) // typing into the filter: let the list have every key
	}
	pl := m.session.Plan
	switch k.String() {
	case "enter":
		sel, _ := m.list.SelectedItem().(item)
		switch sel.key {
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
	case "esc":
		if m.list.FilterState() == list.Unfiltered {
			return m.dropSession()
		}
	case "q":
		return m.quit()
	}
	return m.updateList(k)
}

func (m *model) keyResolve(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.list.FilterState() == list.Filtering {
		return m.updateList(k) // typing into the filter: let the list have every key
	}
	pl := m.session.Plan
	switch k.String() {
	case " ":
		if sel, ok := m.list.SelectedItem().(item); ok {
			c := update.TakeNew
			if pl.ChoiceOf(sel.key) == update.TakeNew {
				c = update.KeepMine
			}
			pl.Choose(sel.key, c)
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
	case "esc":
		if m.list.FilterState() == list.Unfiltered {
			return m.showConflicts()
		}
	case "q":
		return m.quit()
	}
	return m.updateList(k)
}
