package tui

import tea "github.com/charmbracelet/bubbletea"

// buttonLabels returns the labels of the current screen's buttons; nil means the screen has none.
func (m *model) buttonLabels() []string {
	switch m.screen {
	case scConfirm:
		if m.creating {
			return []string{"Create it", "Back"}
		}
		return []string{"Update now", "Back"}
	case scRestoreConfirm:
		return []string{"Undo now", "Back"}
	case scDone, scRestored:
		return []string{"Back", "Play now", "Quit"}
	case scError:
		if m.canGoBack() {
			return []string{"Back", "Quit"}
		}
		return []string{"Quit"}
	case scSelfUpdated:
		return []string{"Restart now", "Quit"}
	case scPlaying:
		return []string{"Back", "Quit"}
	case scBackups:
		if len(m.backups) == 0 {
			return []string{"Back"}
		}
	}
	return nil
}

// buttonFooter renders the button row above the key bar, or just hint(pairs...) when the screen has no buttons.
func (m *model) buttonFooter(pairs ...string) string {
	labels := m.buttonLabels()
	if labels == nil {
		return hint(pairs...)
	}
	return buttons(labels, m.btn) + "\n" + hint(append([]string{"←→", "choose"}, pairs...)...)
}

// keyButtons moves the selection on left/right/tab/shift+tab and activates the selected button on enter.
func (m *model) keyButtons(k tea.KeyMsg) (handled bool, md tea.Model, cmd tea.Cmd) {
	labels := m.buttonLabels()
	switch k.String() {
	case "right", "tab":
		m.btn = min(m.btn+1, len(labels)-1)
		return true, m, nil
	case "left", "shift+tab":
		m.btn = max(m.btn-1, 0)
		return true, m, nil
	case "enter":
		md, cmd = m.activateButton()
		return true, md, cmd
	}
	return false, m, nil
}

// activateButton runs the selected button's action.
func (m *model) activateButton() (tea.Model, tea.Cmd) {
	labels := m.buttonLabels()
	if m.btn < 0 || m.btn >= len(labels) {
		return m, nil
	}
	switch labels[m.btn] {
	case "Update now":
		return m.apply()
	case "Create it":
		return m.applyCreate()
	case "Undo now":
		return m.startRestore()
	case "Back":
		return m.back()
	case "Play now":
		m.selectCreated()
		return m.play(false)
	case "Quit":
		return m.quit()
	case "Restart now":
		m.restart = true
		return m.quit()
	}
	return m, nil
}

// back is the esc action of the current button screen.
func (m *model) back() (tea.Model, tea.Cmd) {
	switch m.screen {
	case scConfirm:
		if m.creating {
			if err := m.closeCreation(); err != nil {
				return m.showLeftover(err)
			}
			return m.showTargets()
		}
		return m.dropSession()
	case scRestoreConfirm:
		return m.showBackups()
	case scDone:
		m.selectCreated()
		return m.reloadHome()
	case scRestored, scPlaying:
		return m.reloadHome()
	case scError:
		return m.goBackFromError()
	case scBackups:
		return m.showHome()
	}
	return m, nil
}
