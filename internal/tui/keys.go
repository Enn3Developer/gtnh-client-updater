package tui

import tea "github.com/charmbracelet/bubbletea"

// key handles a key with no dialog open (C5).
func (m *model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.savedRow = ""
	s := k.String()
	if s == "q" || s == "ctrl+c" {
		return m, m.quitKey()
	}
	if !m.loaded {
		return m, nil
	}
	switch s {
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	case "tab", "shift+tab", "left", "right":
		if m.sidebarShows() && m.focus == focusPage {
			m.focus = focusSidebar
		} else if m.sidebarShows() {
			m.focus = focusPage
		}
	case "enter":
		if m.focus == focusSidebar {
			return m, m.playCmd(false)
		}
		if rs := m.rows(); m.row < len(rs) && rs[m.row].run != nil {
			return m, rs[m.row].run(m)
		}
	case "esc":
		if m.focus == focusPage && m.row == 0 {
			m.dismissPlay()
		}
	case "p":
		return m, m.playCmd(false)
	case "j":
		return m, m.runRow("join") // only GTNH instances with a server have one
	case "u":
		return m, m.runRow("update")
	case "o":
		return m, m.runRow("versions")
	case "b":
		return m, m.runRow("undo")
	case "s":
		m.jumpToSettings()
	case "n":
		return m, m.newInstance()
	case "a":
		m.toggleShowAll()
	case "v":
		if m.newer != nil {
			return m, m.selfUpdate()
		}
	}
	return m, nil
}

// move moves the page row or, in the sidebar, the selected instance by d, clamped.
func (m *model) move(d int) {
	if m.focus == focusPage {
		m.row = max(min(m.row+d, len(m.rows())-1), 0)
		return
	}
	m.sel = max(min(m.sel+d, len(m.visible())-1), 0)
	m.row, m.pageScroll = 0, 0
}

// runRow runs the page row with id, if the page has one.
func (m *model) runRow(id string) tea.Cmd {
	for _, r := range m.rows() {
		if r.id == id && r.run != nil {
			return r.run(m)
		}
	}
	if in, _ := m.current(); (id == "update" || id == "versions" || id == "undo") && m.jobShown(in.Dir) {
		m.notifyBusy()
	}
	return nil
}

// jumpToSettings focuses the first settings row, or the first launcher row without one.
func (m *model) jumpToSettings() {
	m.focus = focusPage
	ids := map[string]bool{}
	for _, r := range m.settingsRows() {
		ids[r.id] = true
	}
	if len(ids) == 0 {
		for _, r := range m.launcherRows() {
			ids[r.id] = true
		}
	}
	for i, r := range m.rows() {
		if ids[r.id] {
			m.row = i
			return
		}
	}
}

func (m *model) toggleShowAll() {
	dir := ""
	if in, ok := m.current(); ok {
		dir = in.Dir
	}
	m.showAll = !m.showAll
	m.selectDir(dir)
	m.row, m.pageScroll = 0, 0
	if !m.sidebarShows() {
		m.focus = focusPage
	}
}
