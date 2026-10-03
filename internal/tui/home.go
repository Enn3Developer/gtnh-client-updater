package tui

import (
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
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

// cardLabelWidth is the column every instance-card label is padded to.
const cardLabelWidth = 13

const (
	cardWidth      = 42 // the instance card's on-screen columns
	cardValueWidth = 25 // cardWidth - 4 - cardLabelWidth: inside border and padding, after the label
	cardTitleWidth = 34 // longest name that still fits the top border
)

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

// homeDesc is the line under an instance's name on the home list: its parts joined by
// " · " with the status badge last, "GTNH <ver> · <played> · <status>" ("GTNH · ..." when
// the version is unknown, "<played> · <status>" when not GTNH); <played> is "never played"
// or "played <ago>".
func homeDesc(in prism.Instance, info homeInfo) string {
	played := "never played"
	if info.played != "" {
		played = "played " + info.played
	}
	parts := []string{played, homeStatus(info)}
	if in.GTNH {
		gtnh := "GTNH"
		if info.version != "" {
			gtnh += " " + info.version
		}
		parts = append([]string{gtnh}, parts...)
	}
	return strings.Join(parts, " · ")
}

// homeStatus is an instance's status badge: the recommended version when it differs,
// up to date, version unknown, or not a GTNH instance.
func homeStatus(info homeInfo) string {
	if !info.gtnh {
		return badge(badgeDim, "not a GTNH instance")
	}
	if info.version == "" {
		return badge(badgeDim, "version unknown")
	}
	if info.rec != info.version {
		return badge(badgeWarn, info.rec+" available")
	}
	return badge(badgeOK, "up to date")
}

// selectedHome is the selected home item's key and card data.
func (m *model) selectedHome() (string, homeInfo, bool) {
	sel, ok := m.list.SelectedItem().(item)
	if !ok {
		return "", homeInfo{}, false
	}
	return sel.key, m.home[sel.key], true
}

// cardView is the selected instance's card: a panel titled with its name (truncated to 34
// columns), 42 columns wide, labels padded to cardLabelWidth; "" with no selection.
func (m *model) cardView() string {
	dir, info, ok := m.selectedHome()
	if !ok {
		return ""
	}
	name := m.list.SelectedItem().(item).title
	if in, ok := m.instanceOf(dir); ok {
		name = in.Name
	}
	var rows []string
	row := func(label, value, hint string) {
		if hint != "" && ansi.StringWidth(value+hint) <= cardValueWidth {
			value += dimSty.Render(hint)
		}
		value = ansi.Truncate(value, cardValueWidth, "…")
		rows = append(rows, dimSty.Render(label+strings.Repeat(" ", cardLabelWidth-len(label)))+value)
	}
	installed := "version unknown"
	if info.version != "" {
		installed = "GTNH " + info.version
	}
	row("Installed", installed, "")
	if !info.gtnh {
		row("Update", "—", "")
	} else if info.version != "" && info.rec != info.version {
		row("Update", homeStatus(info), " · press u")
	} else {
		row("Update", homeStatus(info), "")
	}
	if info.gtnh {
		if info.backup != nil {
			row("Undo", warnSty.Render("back to "+info.backup.Info.From), " · press b")
		} else {
			row("Undo", dimSty.Render("nothing to undo"), "")
		}
	}
	played := "never"
	if info.played != "" {
		played = info.played
	}
	row("Played", played, "")
	if info.server != "" {
		row("Server", info.server, " · press j to join")
	} else {
		row("Server", "none set", "")
	}
	mods := "none"
	if info.modsURL != "" {
		mods = hostOf(info.modsURL)
	}
	row("Server mods", mods, "")
	if info.gtnh {
		pack := "Java 8"
		if info.flavor == manifest.Java17 {
			pack = "Java 17+"
		}
		row("Pack", pack, "")
	}
	return panel(ansi.Truncate(name, cardTitleWidth, "…"), strings.Join(rows, "\n"), cardWidth)
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
	m.list.KeyMap.CursorDown = key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "down"))
	m.list.KeyMap.CursorUp = key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "up"))
	return md, cmd
}
