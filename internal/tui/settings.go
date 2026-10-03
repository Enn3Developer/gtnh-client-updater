package tui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/appcfg"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// settingRow is one row of the settings screen and the texts of its edit screen.
type settingRow struct{ key, title, intro, foot string }

const instanceFoot = "Leave it empty to go back to Prism's default. If Prism is open right now, restart it so it notices."

// settingRows in screen order; server and mods only show for GTNH instances.
var settingRows = []settingRow{
	{"server", "Server to join", "Which server do you usually play on? Type its address, like play.example.com or play.example.com:25565. With one set, j on the home screen joins it.", "Leave it empty for none."},
	{"mods", "Server extra mods link", serverModsIntro, "Leave it empty for none."},
	{"memory", "Memory for the game", "How much memory may the game use, in MB? GTNH runs well with 6144 to 8192.", instanceFoot},
	{"jvm", "Java arguments", "Extra arguments for Java. Only change this if someone told you what to put here.", instanceFoot},
	{"java", "Java to run it with", "Full path of the java executable Prism should use for this instance.", instanceFoot},
	{"window", "Window size", "Width and height of the game window, like 1920x1080.", instanceFoot},
	{"after", "After I start the game", "", ""},
	{"prism", "Prism Launcher location", "Where is Prism Launcher on this computer? The full path of its program file.", "Leave it empty to let me find Prism myself."},
}

const (
	prismDefault     = "Prism's default"
	cantReadCfg      = "I couldn't read this instance's settings"
	cantReadState    = "couldn't read the saved settings"
	msgBadServer     = "That doesn't look like a server address — try play.example.com or play.example.com:25565."
	msgBadMemory     = "Give me a whole number of MB between 1024 and 65536."
	msgBadFile       = "I can't find a file there."
	msgBadWindowSize = "Give me width and height like 1920x1080."
)

var (
	keyChange = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "change"))
	keyQuit   = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
)

func settingRowOf(k string) settingRow {
	for _, r := range settingRows {
		if r.key == k {
			return r
		}
	}
	return settingRow{key: k}
}

// showSettings lists the settings of m.inst plus the launcher-wide ones, selecting m.setting
// or, when that row isn't shown, the first setting row.
func (m *model) showSettings() (tea.Model, tea.Cmd) {
	md, cmd := m.showListWith(scSettings, "Settings for "+m.inst.Name, m.settingsDelegate(), m.settingsItems(), m.setting, keyChange, keyOther, keyQuit)
	if sel := m.list.SelectedItem(); sel == nil || isSection(sel) {
		for i, it := range m.list.Items() {
			if !isSection(it) {
				m.list.Select(i)
				break
			}
		}
	}
	return md, cmd
}

// settingShown reports whether the row r belongs on the settings screen of m.inst.
func (m *model) settingShown(r settingRow) bool {
	return m.inst.GTNH || (r.key != "server" && r.key != "mods")
}

// settingsItems builds the settings rows with their current values, each group
// under its section heading.
func (m *model) settingsItems() []list.Item {
	st, stErr := update.LoadState(m.inst.Dir)
	if st == nil {
		st = &update.State{}
	}
	s, sErr := prism.ReadSettings(m.inst.Dir)
	items := []list.Item{item{title: "This instance"}}
	for _, r := range settingRows {
		if !m.settingShown(r) {
			continue
		}
		if r.key == "after" { // the first launcher-wide row
			items = append(items, item{title: "The launcher"})
		}
		desc := ""
		switch r.key {
		case "server", "mods":
			desc = stateDesc(r.key, st)
			if stErr != nil {
				desc = cantReadState
			}
		case "memory", "jvm", "java", "window":
			desc = m.overrideDesc(r.key, s)
			if sErr != nil {
				desc = cantReadCfg
			}
		case "after":
			desc = "quit the launcher"
			if m.app.StaysOpen() {
				desc = "stay open and show whether the game is running"
			}
		case "prism":
			desc = m.app.PrismExe
			if desc == "" {
				desc = "found automatically"
			}
		}
		items = append(items, item{title: r.title, desc: desc, key: r.key})
	}
	return items
}

func stateDesc(k string, st *update.State) string {
	if k == "server" {
		if st.ServerAddress == "" {
			return "none"
		}
		return st.ServerAddress
	}
	if st.CustomModsURL == "" {
		return "none"
	}
	return hostOf(st.CustomModsURL)
}

func (m *model) overrideDesc(k string, s prism.Settings) string {
	switch {
	case k == "memory" && s.OverrideMemory:
		return fmt.Sprintf("%d MB (at least %d MB)", s.MaxMemMB, s.MinMemMB)
	case k == "jvm" && s.OverrideJavaArgs:
		return ansi.Truncate(s.JvmArgs, m.listWidthFor(scSettings)-2, "…")
	case k == "java" && s.OverrideJavaLocation:
		return s.JavaPath
	case k == "window" && s.OverrideWindow:
		return fmt.Sprintf("%d×%d", s.WinWidth, s.WinHeight)
	}
	return prismDefault
}

func (m *model) keySettings(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.savedRow = ""
	m.list.SetDelegate(m.settingsDelegate())
	prev := m.list.Index()
	if m.list.FilterState() == list.Filtering {
		md, cmd := m.updateList(k)
		m.skipSections(prev)
		return md, cmd
	}
	switch k.String() {
	case "enter":
		sel, ok := m.list.SelectedItem().(item)
		if !ok {
			return m, nil
		}
		if sel.key == "" { // a section heading
			return m, nil
		}
		m.setting = sel.key
		if sel.key == "after" {
			return m.toggleAfterPlay()
		}
		return m.editSetting(sel.key)
	case "esc":
		if m.list.FilterState() == list.Unfiltered {
			return m.reloadHome()
		}
	case "q":
		return m.quit()
	}
	md, cmd := m.updateList(k)
	m.skipSections(prev)
	return md, cmd
}

// toggleAfterPlay flips whether the launcher stays open after Play and saves it.
func (m *model) toggleAfterPlay() (tea.Model, tea.Cmd) {
	prev := m.app
	m.app.AfterPlay = appcfg.AfterPlayStay
	if m.app.StaysOpen() == prev.StaysOpen() {
		m.app.AfterPlay = appcfg.AfterPlayQuit
	}
	if err := m.saveApp(m.app); err != nil {
		m.app = prev
		m.err, m.errPhase, m.screen = fmt.Errorf("I couldn't save that setting: %w", err), scSettings, scError
		return m, nil
	}
	m.savedRow = "after"
	m.list.SetItems(m.settingsItems())
	m.list.SetDelegate(m.settingsDelegate())
	return m, nil
}

func (m *model) keySettingEdit(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		v := strings.TrimSpace(m.setIn.Value())
		if msg := checkSetting(m.setting, v); msg != "" {
			m.setEr = msg
			return m, nil
		}
		if err := m.saveSetting(m.setting, v); err != nil {
			m.err, m.errPhase, m.screen = fmt.Errorf("I couldn't save that setting: %w", err), scSettingEdit, scError
			return m, nil
		}
		m.setEr = ""
		m.savedRow = m.setting
		return m.showSettings()
	case "esc":
		m.setEr = ""
		return m.showSettings()
	}
	var cmd tea.Cmd
	m.setIn, cmd = m.setIn.Update(k)
	return m, cmd
}

// editSetting opens the text field for the setting key, prefilled with its current value.
// It stays on the list when the value can't be read.
func (m *model) editSetting(k string) (tea.Model, tea.Cmd) {
	raw, ok := m.settingValue(k)
	if !ok {
		return m, nil
	}
	m.setting, m.setEr, m.screen = k, "", scSettingEdit
	return m, m.focusInput(&m.setIn, raw)
}

// settingValue is the current value of the setting k as typed on its edit screen.
func (m *model) settingValue(k string) (raw string, ok bool) {
	switch k {
	case "server", "mods":
		st, err := update.LoadState(m.inst.Dir)
		if err != nil {
			return "", false
		}
		if st == nil {
			return "", true
		}
		if k == "server" {
			return st.ServerAddress, true
		}
		return st.CustomModsURL, true
	case "prism":
		return m.app.PrismExe, true
	}
	s, err := prism.ReadSettings(m.inst.Dir)
	if err != nil {
		return "", false
	}
	switch {
	case k == "memory" && s.OverrideMemory:
		return strconv.Itoa(s.MaxMemMB), true
	case k == "jvm" && s.OverrideJavaArgs:
		return s.JvmArgs, true
	case k == "java" && s.OverrideJavaLocation:
		return s.JavaPath, true
	case k == "window" && s.OverrideWindow:
		return fmt.Sprintf("%dx%d", s.WinWidth, s.WinHeight), true
	}
	return "", true
}

func (m *model) settingEditView() (body, footer string) {
	r := settingRowOf(m.setting)
	body, _ = m.inputView(r.title, r.intro, m.setIn, m.setEr, r.foot)
	return body, hint("enter", "save", "esc", "back")
}

// checkSetting validates the trimmed value v for the setting key: "" if it's fine,
// otherwise the text to show the player.
func checkSetting(k, v string) string {
	if v == "" {
		return ""
	}
	switch k {
	case "server":
		if !checkServerAddress(v) {
			return msgBadServer
		}
	case "mods":
		if update.CheckCustomModsURL(v) != nil {
			return msgBadModsLink
		}
	case "memory":
		n, err := strconv.Atoi(v)
		if err != nil || n < 1024 || n > 65536 {
			return msgBadMemory
		}
	case "java", "prism":
		fi, err := os.Stat(v)
		if err != nil || !fi.Mode().IsRegular() {
			return msgBadFile
		}
	case "window":
		if _, _, ok := parseWindowSize(v); !ok {
			return msgBadWindowSize
		}
	}
	return ""
}

// saveSetting stores the trimmed, valid value v for the setting key.
func (m *model) saveSetting(k, v string) error {
	dir := m.inst.Dir
	switch k {
	case "server":
		return update.UpdateState(dir, func(st *update.State) { st.ServerAddress = v })
	case "mods":
		return update.UpdateState(dir, func(st *update.State) {
			st.CustomModsURL = v
			st.CustomModsAsked = true
		})
	case "prism":
		prev := m.app
		m.app.PrismExe = v
		if err := m.saveApp(m.app); err != nil {
			m.app = prev
			return err
		}
		return nil
	}
	s, err := prism.ReadSettings(dir)
	if err != nil {
		return err
	}
	switch k {
	case "memory":
		s.OverrideMemory = v != ""
		if v != "" {
			n, _ := strconv.Atoi(v) // checked by checkSetting
			s.MaxMemMB = n
			minMB := s.MinMemMB
			if minMB == 0 {
				minMB = 1024
			}
			s.MinMemMB = min(minMB, n)
		}
	case "jvm":
		s.OverrideJavaArgs = v != ""
		if v != "" {
			s.JvmArgs = v
		}
	case "java":
		s.OverrideJavaLocation = v != ""
		if v != "" {
			s.JavaPath = v
		}
	case "window":
		s.OverrideWindow = v != ""
		if v != "" {
			s.WinWidth, s.WinHeight, _ = parseWindowSize(v) // checked by checkSetting
		}
	}
	return prism.WriteSettings(dir, s)
}

// parseWindowSize reads "WIDTHxHEIGHT" (x or ×) within the sizes Minecraft accepts.
func parseWindowSize(v string) (w, h int, ok bool) {
	if strings.Count(v, "x")+strings.Count(v, "×") != 1 {
		return 0, 0, false
	}
	ws, hs, found := strings.Cut(v, "x")
	if !found {
		ws, hs, _ = strings.Cut(v, "×")
	}
	w, errW := strconv.Atoi(ws)
	h, errH := strconv.Atoi(hs)
	if errW != nil || errH != nil || w < 320 || w > 16384 || h < 320 || h > 16384 {
		return 0, 0, false
	}
	return w, h, true
}

// checkServerAddress reports whether v looks like host or host:port.
func checkServerAddress(v string) bool {
	if strings.Contains(v, " ") || strings.Contains(v, "://") {
		return false
	}
	parts := strings.Split(v, ":")
	if len(parts) > 2 || parts[0] == "" {
		return false
	}
	if len(parts) == 2 {
		port, err := strconv.Atoi(parts[1])
		if err != nil || port < 1 || port > 65535 {
			return false
		}
	}
	return true
}

// formDelegate draws the settings list as a one-line-per-row form.
type formDelegate struct {
	labelWidth int
	savedRow   string
}

var _ list.ItemDelegate = formDelegate{}

const savedMarker = "✓ saved"

var (
	formCursorSty = lipgloss.NewStyle().Foreground(accent)
	formLabelSty  = lipgloss.NewStyle().Foreground(accent).Bold(true)
)

func (d formDelegate) Height() int { return 1 }

func (d formDelegate) Spacing() int { return 0 }

func (d formDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d formDelegate) Render(w io.Writer, lm list.Model, index int, it list.Item) {
	i, ok := it.(item)
	if !ok {
		return
	}
	width := lm.Width()
	if isSection(i) {
		rule := "── " + i.title + " " + strings.Repeat("─", max(0, width-4-ansi.StringWidth(i.title)))
		fmt.Fprint(w, ansi.Truncate(dimSty.Render(rule), width, ""))
		return
	}
	label := i.title + strings.Repeat(" ", max(0, d.labelWidth-ansi.StringWidth(i.title)))
	room := width - 2 - d.labelWidth - 2
	marker := ""
	if i.key == d.savedRow {
		room -= 1 + ansi.StringWidth(savedMarker)
		marker = " " + okSty.Render(savedMarker)
	}
	value := ansi.Truncate(i.desc, max(0, room), "…")
	line := "  " + dimSty.Render(label) + "  " + value + marker
	if index == lm.Index() {
		line = formCursorSty.Render("▸ ") + formLabelSty.Render(label) + "  " + formCursorSty.Render(value) + marker
	}
	fmt.Fprint(w, ansi.Truncate(line, width, ""))
}

// settingsDelegate builds the form delegate for the current settings screen, its labels
// as wide as the widest one shown.
func (m *model) settingsDelegate() formDelegate {
	d := formDelegate{savedRow: m.savedRow}
	for _, r := range settingRows {
		if m.settingShown(r) {
			d.labelWidth = max(d.labelWidth, ansi.StringWidth(r.title))
		}
	}
	return d
}

// isSection reports whether it is a section heading rather than a setting row.
func isSection(it list.Item) bool {
	i, ok := it.(item)
	return ok && i.key == ""
}

// skipSections moves the cursor off a section heading after a move from prevIndex:
// onward in the direction it moved, or back when no setting row lies that way.
func (m *model) skipSections(prevIndex int) {
	items := m.list.VisibleItems()
	idx := m.list.Index()
	if idx < 0 || idx >= len(items) || !isSection(items[idx]) {
		return
	}
	dir := -1
	if idx >= prevIndex { // moved down onto a section (or the filter reset to the top)
		dir = +1
	}
	if j, ok := settingFrom(items, idx, dir); ok {
		m.list.Select(j)
	} else if j, ok := settingFrom(items, idx, -dir); ok {
		m.list.Select(j)
	}
}

// settingFrom is the index of the first setting row from idx stepping by dir.
func settingFrom(items []list.Item, idx, dir int) (int, bool) {
	for j := idx; j >= 0 && j < len(items); j += dir {
		if !isSection(items[j]) {
			return j, true
		}
	}
	return 0, false
}
