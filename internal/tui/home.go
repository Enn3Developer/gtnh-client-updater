package tui

import (
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// homeInfo is what the home screen shows about one instance.
type homeInfo struct {
	gtnh                                  bool
	version, rec, played, server, modsURL string
	flavor                                manifest.Flavor
	backup                                *update.Backup // newest restorable backup; nil = nothing to undo
}

// cardSty boxes the instance card: 38 columns of text, 42 on screen. lipgloss's Width
// counts the padding but not the border.
var cardSty = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#727169")).Padding(0, 1).Width(40)

func (m *model) homeInfoOf(in prism.Instance) homeInfo {
	st, _ := update.LoadState(in.Dir)
	info := homeInfo{
		gtnh:    in.GTNH,
		version: update.DetectVersion(in, st, m.manifest).Version,
		played:  ago(in.LastLaunch, time.Now()),
		flavor:  update.FlavorOf(in),
	}
	info.rec = defaultTarget(m.manifest, info.version)
	if st != nil {
		info.server, info.modsURL = st.ServerAddress, st.CustomModsURL
	}
	if in.GTNH {
		if bs, _ := update.ListBackups(in.Dir); len(bs) > 0 {
			info.backup = &bs[0]
		}
	}
	return info
}

// homeDesc is the line under an instance's name on the home list.
func homeDesc(in prism.Instance, info homeInfo) string {
	played := " · never played"
	if info.played != "" {
		played = " · played " + info.played
	}
	if !in.GTNH {
		return "not a GTNH instance" + played
	}
	v := info.version
	if v == "" {
		v = "version unknown"
	}
	status := " · up to date"
	if info.rec != info.version {
		status = " · update available"
	}
	return "GTNH " + v + played + status
}

// selectedHome is the selected home item's key and card data.
func (m *model) selectedHome() (string, homeInfo, bool) {
	sel, ok := m.list.SelectedItem().(item)
	if !ok {
		return "", homeInfo{}, false
	}
	return sel.key, m.home[sel.key], true
}

func (m *model) cardView() string {
	_, info, ok := m.selectedHome()
	if !ok {
		return ""
	}
	line := func(label, value string) string { return dimSty.Render(label) + value }
	installed := "version unknown"
	if info.version != "" {
		installed = "GTNH " + info.version
	}
	upd := "—"
	switch {
	case info.gtnh && info.rec != info.version:
		upd = warnSty.Render(info.rec + " is out — press u")
	case info.gtnh:
		upd = okSty.Render("up to date")
	}
	played := "never"
	if info.played != "" {
		played = info.played
	}
	server := "none set"
	if info.server != "" {
		server = info.server + " — j joins it"
	}
	mods := "none"
	if info.modsURL != "" {
		mods = hostOf(info.modsURL)
	}
	lines := []string{
		line("Installed  ", installed),
		line("Update     ", upd),
	}
	if info.gtnh {
		undo := dimSty.Render("nothing to undo")
		if info.backup != nil {
			undo = warnSty.Render("back to " + info.backup.Info.From + " — press b")
		}
		lines = append(lines, line("Undo       ", undo))
	}
	lines = append(lines,
		line("Played     ", played),
		line("Server     ", server),
		line("Server mods  ", mods),
	)
	if info.gtnh {
		pack := "Java 8"
		if info.flavor == manifest.Java17 {
			pack = "Java 17+"
		}
		lines = append(lines, line("Pack       ", pack))
	}
	return cardSty.Render(strings.Join(lines, "\n"))
}

func (m *model) homeHelp() string {
	pairs := []string{"enter", "play"}
	if _, info, ok := m.selectedHome(); ok && info.server != "" {
		pairs = append(pairs, "j", "join")
	}
	pairs = append(pairs, "u", "update", "s", "settings", "b", "undo", "n", "new")
	if m.showAll || slices.ContainsFunc(m.insts, func(in prism.Instance) bool { return !in.GTNH }) {
		pairs = append(pairs, "a", "all")
	}
	pairs = append(pairs, "q", "quit")
	if m.newer != nil {
		pairs = append(pairs, "v", "new version")
	}
	return hintRows(m.width-6, pairs...)
}

// instanceOf is the instance in m.insts with folder dir.
func (m *model) instanceOf(dir string) (prism.Instance, bool) {
	i := slices.IndexFunc(m.insts, func(in prism.Instance) bool { return in.Dir == dir })
	if i < 0 {
		return prism.Instance{}, false
	}
	return m.insts[i], true
}

func (m *model) keyHome(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.list.FilterState() == list.Filtering {
		return m.updateList(k) // typing into the filter: let the list have every key
	}
	switch k.String() {
	case "n":
		return m.startCreate()
	case "a":
		m.showAll = !m.showAll
		return m.showHome()
	case "q":
		return m.quit()
	case "v":
		if m.newer != nil {
			return m.startSelfUpdate()
		}
		return m, nil
	case "enter", "p", "j", "u", "s", "b":
		return m.keyHomeInstance(k.String())
	}
	return m.updateList(k)
}

// keyHomeInstance runs a home key that acts on the selected instance.
func (m *model) keyHomeInstance(k string) (tea.Model, tea.Cmd) {
	dir, info, ok := m.selectedHome()
	if !ok {
		return m, nil
	}
	in, ok := m.instanceOf(dir)
	if !ok {
		return m, nil
	}
	switch k {
	case "j":
		addr := info.server
		if addr == "" {
			return m, nil
		}
		m.inst = in
		return m.play(true)
	case "u":
		return m.pickInstance(in)
	case "s":
		m.inst = in
		return m.showSettings()
	case "b":
		m.inst = in
		return m.showBackups()
	}
	m.inst = in
	return m.play(false)
}

func (m *model) showHome() (tea.Model, tea.Cmd) {
	m.creating = false
	m.home = make(map[string]homeInfo, len(m.insts))
	var items []list.Item
	sel := ""
	for _, in := range m.insts {
		info := m.homeInfoOf(in)
		m.home[in.Dir] = info
		if !in.GTNH && !m.showAll {
			continue
		}
		if in.GTNH && sel == "" {
			sel = in.Dir
		}
		items = append(items, item{in.Name, homeDesc(in, info), in.Dir})
	}
	if m.inst.Dir != "" {
		sel = m.inst.Dir
	}
	md, cmd := m.showList(scHome, "Which instance do you want to play?", items, sel)
	// The help rows depend on the selected instance (its j key), known only now.
	m.list.SetSize(m.listWidth(), m.listHeight())
	m.list.SetShowHelp(false)
	m.list.KeyMap.CursorDown = key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "down"))
	m.list.KeyMap.CursorUp = key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "up"))
	return md, cmd
}

// hintRows renders key/label pairs like hint, wrapping whole pairs into rows no
// wider than limit.
func hintRows(limit int, pairs ...string) string {
	if limit < 20 {
		return hint(pairs...)
	}
	var rows []string
	row := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		pair := hint(pairs[i], pairs[i+1])
		if row != "" && ansi.StringWidth(row+"    "+pair) <= limit {
			row += "    " + pair
			continue
		}
		if row != "" {
			rows = append(rows, row)
		}
		row = pair
	}
	if row != "" {
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}
