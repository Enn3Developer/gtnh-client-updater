package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/appcfg"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

const (
	couldntSaveSetting = "I couldn't save that setting"
	msgBadServer       = "That doesn't look like a server address — try play.example.com or play.example.com:25565."
	msgBadMemory       = "Give me a whole number of MB between 1024 and 65536."
	msgBadFile         = "I can't find a file there."
	msgBadWindowSize   = "Give me width and height like 1920x1080."
)

// settingEdit is the setting being edited in place: its row id, the field and the
// message of the last refused value ("" none).
type settingEdit struct {
	id    string
	input textinput.Model
	err   string
}

// editSetting starts editing the setting of row id in its row, or flips "after" (C1).
func (m *model) editSetting(id string) tea.Cmd {
	in, ok := m.current()
	if m.dialog != nil || !ok || m.jobShown(in.Dir) {
		return nil
	}
	info := m.home[in.Dir]
	switch id {
	case "memory", "jvm", "java", "window":
		if info.settingsErr != nil {
			return nil
		}
	case "after":
		m.toggleAfterPlay()
		return nil
	}
	field := textinput.New()
	field.Prompt = ""
	field.Width = max(m.pageWidth()-2-15-2, 10)
	field.Cursor.SetMode(cursor.CursorStatic)
	field.SetValue(rawSettingValue(id, info, m.app))
	field.CursorEnd()
	field.Focus()
	m.edit = &settingEdit{id: id, input: field}
	m.savedRow = ""
	m.focus = focusPage
	for i, r := range m.rows() {
		if r.id == id {
			m.row = i
			break
		}
	}
	return nil
}

// toggleAfterPlay flips whether the launcher stays open after Play and saves it.
func (m *model) toggleAfterPlay() {
	prev := m.app
	m.app.AfterPlay = appcfg.AfterPlayStay
	if m.app.StaysOpen() == prev.StaysOpen() {
		m.app.AfterPlay = appcfg.AfterPlayQuit
	}
	if err := m.saveApp(m.app); err != nil {
		m.app = prev
		m.notify(couldntSaveSetting, err.Error())
		return
	}
	m.savedRow = "after"
}

// keyEdit handles every key while a setting is being edited; false when none is (C3).
func (m *model) keyEdit(k tea.KeyMsg) (tea.Cmd, bool) {
	if m.edit == nil {
		return nil, false
	}
	switch k.String() {
	case "enter":
		return m.saveEdit(), true
	case "esc":
		m.cancelEdit()
		return nil, true
	case "up", "down", "tab", "shift+tab", "left", "right":
		return nil, true
	}
	var cmd tea.Cmd
	m.edit.input, cmd = m.edit.input.Update(k)
	return cmd, true
}

// saveEdit checks and saves the edited value, or keeps editing with the reason (C4).
func (m *model) saveEdit() tea.Cmd {
	e := m.edit
	v := strings.TrimSpace(e.input.Value())
	if msg := checkSetting(e.id, v); msg != "" {
		e.err = msg
		return nil
	}
	in, _ := m.current()
	m.edit = nil
	if err := m.saveSetting(in.Dir, e.id, v); err != nil {
		m.notify(couldntSaveSetting, err.Error())
		return nil
	}
	m.refresh()
	m.savedRow = e.id
	return nil
}

// cancelEdit stops editing without saving.
func (m *model) cancelEdit() {
	m.edit = nil
}

// settingHelp is the dim sentence under the field of setting id (C2).
func settingHelp(id string) string {
	switch id {
	case "memory":
		return "How much memory the game may use, in MB — GTNH runs well with 6144 to 8192. Empty means Prism's default."
	case "jvm":
		return "Extra arguments for Java. Only change this if someone told you what to put here. Empty means Prism's default."
	case "java":
		return "Full path of the java program Prism should use for this instance. Empty means Prism's default."
	case "window":
		return "Width and height of the game window, like 1920x1080. Empty means Prism's default."
	case "server":
		return "The server you usually play on, like play.example.com or play.example.com:25565. Empty means none."
	case "mods":
		return "The link your server owner gave you for its extra mods. Empty means none."
	case "prism":
		return "Where Prism Launcher is on this computer — the full path of its program. Empty lets me find it myself."
	}
	return ""
}

// checkSetting validates the trimmed value v for the setting id: "" if it's fine,
// otherwise the text to show the player (C6).
func checkSetting(id, v string) string {
	if v == "" {
		return ""
	}
	switch id {
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

// saveSetting stores the trimmed, valid value v for the setting id of the instance in dir (C4, C7).
func (m *model) saveSetting(dir, id, v string) error {
	switch id {
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
	switch id {
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

// rawSettingValue is what the field of setting id starts with: the value as stored,
// "" when at Prism's default or none.
func rawSettingValue(id string, info homeInfo, app appcfg.Config) string {
	s := info.settings
	switch {
	case id == "memory" && s.OverrideMemory:
		return strconv.Itoa(s.MaxMemMB)
	case id == "jvm" && s.OverrideJavaArgs:
		return s.JvmArgs
	case id == "java" && s.OverrideJavaLocation:
		return s.JavaPath
	case id == "window" && s.OverrideWindow:
		return fmt.Sprintf("%dx%d", s.WinWidth, s.WinHeight)
	case id == "server":
		return info.server
	case id == "mods":
		return info.modsURL
	case id == "prism":
		return app.PrismExe
	}
	return ""
}
