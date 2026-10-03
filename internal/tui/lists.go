package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

var (
	keyNotMine = key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "not this one"))
	keyServer  = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mods"))
	keyOther   = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	keySelfUpd = key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "new version"))
	keyPick    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "choose"))
	keySwitch  = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "switch"))
	keyAllNew  = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "all new"))
	keyDone    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done"))
	keyKeepAll = key.NewBinding(key.WithKeys("k"), key.WithHelp("k", "keep all"))
)

type item struct{ title, desc, key string }

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title + " " + i.desc }

// selectedItem is the item under the list cursor; false when there is none.
func (m *model) selectedItem() (item, bool) {
	sel, ok := m.list.SelectedItem().(item)
	return sel, ok
}

// selectedKey is the key of the item under the list cursor; false when there is none.
func (m *model) selectedKey() (string, bool) {
	sel, ok := m.selectedItem()
	return sel.key, ok
}

// listKey handles the keys every list screen treats alike: while the filter is being typed
// the list gets every key, esc on an unfiltered list goes back (when back isn't nil) and q
// quits. handled is false for any other key, which is the caller's to handle.
func (m *model) listKey(k tea.KeyMsg, back func() (tea.Model, tea.Cmd)) (md tea.Model, cmd tea.Cmd, handled bool) {
	switch {
	case m.list.FilterState() == list.Filtering:
		md, cmd = m.updateList(k) // typing into the filter: let the list have every key
		return md, cmd, true
	case k.String() == "esc" && m.list.FilterState() == list.Unfiltered && back != nil:
		md, cmd = back()
		return md, cmd, true
	case k.String() == "q":
		md, cmd = m.quit()
		return md, cmd, true
	}
	return nil, nil, false
}

func (m *model) showList(sc screen, title string, items []list.Item, selected string, help ...key.Binding) (tea.Model, tea.Cmd) {
	return m.showListWith(sc, title, m.listDelegate(), items, selected, help...)
}

// showListWith shows a list drawn by d and remembers help for the key bar.
func (m *model) showListWith(sc screen, title string, d list.ItemDelegate, items []list.Item, selected string, help ...key.Binding) (tea.Model, tea.Cmd) {
	l := list.New(items, d, 0, 0)
	l.DisableQuitKeybindings() // q and esc go through m.quit and the screen's own back
	l.Title = indentWrap(title, m.listWidthFor(sc)-2, 0)
	if sc != scHome {
		l.SetShowTitle(false) // the list cuts its title to one line, so page() draws it
	}
	m.listTitle = title
	l.Styles.Title = titleSty
	l.Styles.TitleBar = l.Styles.TitleBar.PaddingLeft(0) // the frame indents the body already
	l.SetShowHelp(false)                                 // the key bar replaces it
	l.SetStatusBarItemName("choice", "choices")
	l.SetShowStatusBar(len(items) > 8)
	for i, it := range items {
		if it.(item).key == selected {
			l.Select(i)
		}
	}
	m.list, m.screen, m.hasList, m.listKeys = l, sc, true, help
	// Sized only now: the height depends on the key bar of the list on screen.
	m.list.SetSize(m.listWidthFor(sc), m.listHeightFor(sc))
	return m, nil
}

// resizeList fits the list on screen (if any) to the current terminal size, re-wrapping
// the title page() draws above it.
func (m *model) resizeList() {
	if !m.hasList {
		return
	}
	if m.screen != scHome {
		m.list.Title = indentWrap(m.listTitle, m.listWidth()-2, 0)
	}
	m.list.SetSize(m.listWidth(), m.listHeight())
}

// listDelegate draws the items of the usual lists.
func (m *model) listDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(accent).BorderForeground(accent)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(lipgloss.Color("#98BB6C")).BorderForeground(accent)
	return d
}

// listKeyPairs are the key/label pairs of the list on screen, for keyBar.
func (m *model) listKeyPairs() []string {
	pairs := []string{"↑↓", "move"}
	for _, b := range m.listKeys {
		pairs = append(pairs, b.Help().Key, b.Help().Desc)
	}
	if len(m.list.Items()) > 1 {
		pairs = append(pairs, "/", "filter")
	}
	if m.newer != nil && (m.screen == scInstalled || m.screen == scTarget) {
		pairs = append(pairs, keySelfUpd.Help().Key, keySelfUpd.Help().Desc)
	}
	return pairs
}

func (m *model) showConflicts() (tea.Model, tea.Cmd) {
	n := m.session.Plan.Count(update.Conflict)
	title := fmt.Sprintf("%d config %s changed both on your side and in the new version. What should I do?",
		n, plural(n, "file was", "files were"))
	items := []list.Item{
		item{"Use the new versions", "Recommended. Your old copies go to the backup folder.", "new"},
		item{"Keep mine", "The new ones are saved next to them with .mcnew at the end, so you can compare.", "mine"},
		item{"Let me choose file by file", "You decide for each file.", "pick"},
	}
	return m.showList(scConflicts, title, items, choiceKey(m.configsChoice()), keyPick, keyOther)
}

// configsChoice is the conflict answer to preselect, from Config.Configs.
func (m *model) configsChoice() update.Choice {
	if c, err := update.ParseChoice(m.cfg.Configs); err == nil {
		return c
	}
	return update.Recommended
}

// choiceKey is the scConflicts item that stands for c.
func choiceKey(c update.Choice) string {
	if c == update.KeepMine {
		return "mine"
	}
	return "new"
}

func (m *model) showResolve() (tea.Model, tea.Cmd) {
	title := "Which version of each file do you want?"
	md, cmd := m.showList(scResolve, title, m.resolveItems(), "", keySwitch, keyAllNew, keyKeepAll, keyDone, keyOther)
	// k means "keep all" here, so it no longer moves the cursor up.
	m.list.KeyMap.CursorUp = key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "up"))
	return md, cmd
}

// refreshResolve redraws the file-by-file list after a choice changed.
func (m *model) refreshResolve() tea.Cmd { return m.list.SetItems(m.resolveItems()) }

func (m *model) resolveItems() []list.Item {
	pl := m.session.Plan
	var items []list.Item
	for _, p := range pl.Conflicts() {
		desc := "keep mine"
		if pl.ChoiceOf(p) == update.TakeNew {
			desc = "new version"
		}
		items = append(items, item{strings.TrimPrefix(p, ".minecraft/"), desc, p})
	}
	return items
}

// kindOf turns a manifest title plus version into words a player knows.
func kindOf(r manifest.Release) string {
	v := strings.ToLower(r.Version)
	switch {
	case r.Stable():
		return "Stable release"
	case strings.Contains(v, "rc"):
		return "Release candidate"
	case strings.Contains(v, "beta"):
		return "Beta"
	case strings.Contains(v, "pre"):
		return "Pre-release"
	}
	return r.Title
}

func (m *model) releaseItems(mark func(manifest.Release) []string) []list.Item {
	var items []list.Item
	now := time.Now()
	for _, r := range m.manifest.Releases {
		parts := []string{kindOf(r)}
		if a := ago(r.ReleaseDate, now); a != "" {
			parts = append(parts, a)
		}
		parts = append(parts, mark(r)...)
		items = append(items, item{r.Version, strings.Join(parts, " · "), r.Version})
	}
	return items
}

func (m *model) showInstalled() (tea.Model, tea.Cmd) {
	items := m.releaseItems(func(manifest.Release) []string { return nil })
	title := "Which GTNH version is " + m.inst.Name + " on right now?"
	return m.showList(scInstalled, title, items, m.detect.Version, keyOther, keyQuit)
}

func (m *model) showTargets() (tea.Model, tea.Cmd) {
	if m.cfg.Target != "" && m.target == "" && m.screen != scConfirm && m.screen != scError {
		m.target = m.cfg.Target
		return m.proceed()
	}
	if m.creating {
		return m.showCreateTargets()
	}
	flavor := update.FlavorOf(m.inst)
	rec := defaultTarget(m.manifest, m.detect.Version)
	items := m.releaseItems(func(r manifest.Release) []string {
		var tags []string
		if r.Version == rec && r.Version != m.detect.Version {
			tags = append(tags, "recommended")
		}
		if r.Version == m.detect.Version {
			tags = append(tags, "you have this one")
		}
		if _, err := r.URL(flavor); err != nil {
			tags = append(tags, "not available for your Java")
		}
		return tags
	})
	title := fmt.Sprintf("%s is on GTNH %s. Which version do you want?", m.inst.Name, m.detect.Version)
	return m.showTargetList(title, items, rec, keyNotMine, keyServer)
}

// showCreateTargets is the version list of the create flow.
func (m *model) showCreateTargets() (tea.Model, tea.Cmd) {
	rec := defaultTarget(m.manifest, "")
	items := m.releaseItems(func(r manifest.Release) []string {
		var tags []string
		if r.Version == rec {
			tags = append(tags, "recommended")
		}
		if update.NewInstanceFlavor(r) == manifest.Java8 {
			tags = append(tags, "Java 8 only")
		}
		return tags
	})
	title := "Which GTNH version do you want to install?"
	if len(m.insts) == 0 {
		title = "Prism has no instances yet. " + title
	}
	return m.showTargetList(title, items, rec, keyServer)
}

// showTargetList shows a version list with the current target selected (rec if none);
// esc back to the instances is offered only when there are some.
func (m *model) showTargetList(title string, items []list.Item, rec string, help ...key.Binding) (tea.Model, tea.Cmd) {
	sel := m.target
	if sel == "" {
		sel = rec
	}
	if len(m.insts) > 0 {
		help = append(help, keyOther)
	}
	help = append(help, keyQuit)
	return m.showList(scTarget, title, items, sel, help...)
}

// defaultTarget preselects the newest stable release, unless the player already runs
// something newer (a beta or RC): then the newest release overall, so a blind Enter
// never downgrades.
func defaultTarget(man *manifest.Manifest, installed string) string {
	for _, r := range man.Releases {
		if r.Stable() && manifest.CompareVersions(r.Version, installed) >= 0 {
			return r.Version
		}
	}
	return man.Releases[0].Version
}
