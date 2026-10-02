package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

var (
	keyNotMine   = key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "not this one"))
	keyServer    = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mods"))
	keyOther     = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	keySelfUpd   = key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "update me"))
	keyShowOther = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all instances"))
	keyNew       = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new instance"))
	keyPick      = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "choose"))
	keySwitch    = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "switch"))
	keyAllNew    = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "all new"))
	keyDone      = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done"))
)

type item struct{ title, desc, key string }

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title + " " + i.desc }

func (m *model) showList(sc screen, title string, items []list.Item, selected string, help ...key.Binding) (tea.Model, tea.Cmd) {
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(accent).BorderForeground(accent)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(lipgloss.Color("#98BB6C")).BorderForeground(accent)
	l := list.New(items, d, 0, 0)
	l.SetSize(m.listWidth(), m.listHeight()) // New doesn't size the help line, SetSize does
	l.Title = ansi.Truncate(title, m.listWidth()-2, "…")
	l.Styles.Title = titleSty.Padding(0, 1)
	l.SetStatusBarItemName("choice", "choices")
	l.SetShowStatusBar(len(items) > 8)
	for i, it := range items {
		if it.(item).key == selected {
			l.Select(i)
		}
	}
	l.AdditionalShortHelpKeys = func() []key.Binding {
		out := append([]key.Binding{}, help...)
		if m.newer != nil && !m.choosingConfigs() {
			out = append(out, keySelfUpd)
		}
		return out
	}
	m.list, m.screen, m.hasList = l, sc, true
	return m, nil
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
	title := "Which version of each file do you want? Space switches, enter when you're done."
	// k (keep all) works but stays out of the help: with it the line passes ~100 columns.
	md, cmd := m.showList(scResolve, title, m.resolveItems(), "", keySwitch, keyAllNew, keyDone, keyOther)
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

func (m *model) showInstances() (tea.Model, tea.Cmd) {
	m.creating = false
	var items []list.Item
	sel, hidden := "", 0
	for _, in := range m.insts {
		if !in.GTNH && !m.showAll {
			hidden++
			continue
		}
		var desc string
		if in.GTNH {
			st, _ := update.LoadState(in.Dir)
			v := update.DetectVersion(in, st, m.manifest).Version
			if v == "" {
				v = "version unknown"
			}
			desc = "GTNH " + v
			if sel == "" {
				sel = in.Dir // the list is sorted most recently played first
			}
		} else {
			desc = "not a GTNH instance"
		}
		if a := ago(in.LastLaunch, time.Now()); a != "" {
			desc += " · played " + a
		} else {
			desc += " · never played"
		}
		items = append(items, item{in.Name, desc, in.Dir})
	}
	if m.inst.Dir != "" {
		sel = m.inst.Dir
	}
	help := []key.Binding{keyNew}
	if hidden > 0 || m.showAll {
		help = append(help, keyShowOther)
	}
	return m.showList(scInstance, "Which modpack do you want to update?", items, sel, help...)
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
	return m.showList(scInstalled, title, items, m.detect.Version, keyOther)
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
