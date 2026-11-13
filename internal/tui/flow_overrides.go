package tui

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Mod overrides: the player's own builds of GTNH mods, used instead of GTNH's (see
// update/overrides.go). The Mod overrides row opens their dialog; an update that changes
// one of their mods asks which version the player wants before its confirmation.

const overridesTitle = "Your mod overrides"

// overridesValue is what the Mod overrides row shows.
func overridesValue(info homeInfo) string {
	switch n := len(info.overrides); n {
	case 0:
		return "none"
	case 1:
		return info.overrides[0].Jar + " instead of GTNH's"
	default:
		return fmt.Sprintf("%d jars instead of GTNH's", n)
	}
}

// manageOverrides is the Mod overrides row: without overrides the field for a jar; with
// some, a list of them (enter on one offers to stop using it) and "Add another…".
func (m *model) manageOverrides() tea.Cmd {
	in, ok := m.current()
	if m.dialog != nil || !ok {
		return nil
	}
	if m.job != nil {
		m.notifyBusy()
		return nil
	}
	ovs := m.home[in.Dir].overrides
	if len(ovs) == 0 {
		m.askOverrideJar(in, "")
		return nil
	}
	items := make([]ditem, 0, len(ovs)+1)
	for _, o := range ovs {
		items = append(items, ditem{o.Jar, "instead of GTNH's " + jarList(o.Replaces), "o:" + o.Jar})
	}
	items = append(items, ditem{"Add another…", "your own build of another GTNH mod", "add"})
	m.openList(overridesTitle, "Your own builds of GTNH mods, used instead of GTNH's. "+
		"GTNH's copies stay in the mods folder, switched off. Pick one to stop using it.\n", items, items[0].key,
		func(m *model, key string) tea.Cmd {
			if key == "add" {
				m.askOverrideJar(in, "")
				return nil
			}
			for _, o := range ovs {
				if "o:"+o.Jar == key {
					m.confirmRemoveOverride(in, o)
				}
			}
			return nil
		})
	return nil
}

// askOverrideJar opens the field for the jar the player wants to use, holding value.
// Continue checks it: a jar of the same mod as one of GTNH's goes to the confirmation,
// one that can't be told apart asks which GTNH mod it replaces.
func (m *model) askOverrideJar(in prism.Instance, value string) {
	m.openInput("Which jar do you want to use?",
		"Your own build of a GTNH mod: I'll switch off GTNH's copy and put yours in its place.",
		value, "the path to the .jar file", "You can drag the file into this window.", []string{"Continue"},
		func(m *model, label, value string) tea.Cmd {
			if label == "" {
				m.closeDialog()
				return nil
			}
			c, err := update.CheckOverride(in, cleanJarPath(value))
			switch {
			case err != nil:
				m.dialog.inputErr = err.Error()
			case len(c.Same) > 0:
				m.confirmAddOverride(in, c, c.Same, value)
			default:
				m.pickReplaced(in, c, value)
			}
			return nil
		})
}

// cleanJarPath is a typed or dropped path: without quotes or a leading ~, and where a
// terminal escaped spaces and such with backslashes (not on Windows, where they separate
// folders), without those.
func cleanJarPath(v string) string {
	p := cleanPath(v)
	if _, err := os.Stat(p); err == nil || runtime.GOOS == "windows" || !strings.Contains(p, `\`) {
		return p
	}
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] == '\\' && i+1 < len(p) {
			i++
		}
		b.WriteByte(p[i])
	}
	return b.String()
}

// pickReplaced asks which of GTNH's jars the jar of c replaces, when its own mod id and
// name don't say.
func (m *model) pickReplaced(in prism.Instance, c *update.OverrideCheck, typed string) {
	items := make([]ditem, len(c.All))
	for i, g := range c.All {
		items[i] = ditem{title: g, key: g}
	}
	m.openList("Which of GTNH's mods does "+c.Jar+" replace?", "I couldn't tell from the jar itself.", items, "",
		func(m *model, key string) tea.Cmd {
			m.confirmAddOverride(in, c, []string{key}, typed)
			return nil
		})
	m.dialog.onButton = func(m *model, _ string) tea.Cmd {
		m.askOverrideJar(in, typed) // esc: back to the field
		return nil
	}
}

// confirmAddOverride asks before putting the jar of c in place of GTNH's replaces.
func (m *model) confirmAddOverride(in prism.Instance, c *update.OverrideCheck, replaces []string, typed string) {
	theirs := "GTNH's " + jarList(replaces)
	put := "put your " + c.Jar + " in the mods folder"
	if c.InMods {
		put = "use your " + c.Jar + " where it is"
	}
	m.confirmDialog("Use "+c.Jar+" instead of "+theirs+"?", []string{
		"I'll switch off " + theirs + " (it stays in the mods folder) and " + put + ".",
		"When an update brings a new version of this mod, I'll ask which one you want.",
	}, nil, nil, "Use it", func(m *model) tea.Cmd {
		m.closeDialog()
		if m.gameRunning(in) {
			return nil
		}
		if err := m.addOverride(in, c.Src, replaces); err != nil {
			m.errorDialog(err, "Nothing was changed.")
			return nil
		}
		m.refresh()
		m.savedRow = "overrides"
		return nil
	}, func(m *model) tea.Cmd {
		m.askOverrideJar(in, typed)
		return nil
	})
}

// confirmRemoveOverride asks before stopping to use the override o.
func (m *model) confirmRemoveOverride(in prism.Instance, o update.Override) {
	m.confirmDialog("Stop using "+o.Jar+"?", []string{
		"I'll switch GTNH's " + jarList(o.Replaces) + " back on and move " + o.Jar +
			" to the replaced-mods folder inside the instance's .gtnh-updater folder.",
	}, nil, nil, "Stop using it", func(m *model) tea.Cmd {
		m.closeDialog()
		if m.gameRunning(in) {
			return nil
		}
		if err := m.removeOverride(in, o.Jar); err != nil {
			m.refresh()
			m.errorDialog(err, cmp.Or(overrideErr(err), "Some files may have moved: have a look at the mods folder."))
			return nil
		}
		m.refresh()
		m.savedRow = "overrides"
		return nil
	}, closeOnlyCmd)
}

// gameRunning says so and reports true when the game runs from in.
func (m *model) gameRunning(in prism.Instance) bool {
	if running, err := m.isRunning(in); err == nil && running {
		m.notify("The game is running", in.Name+" is running right now. Close Minecraft, then try again.")
		return true
	}
	return false
}

// overrideErr is what a failed add or remove means; the game-running refusal changed nothing.
func overrideErr(err error) string {
	if errors.Is(err, update.ErrGameRunning) {
		return "Nothing was changed."
	}
	return ""
}

// asksOverrides are the override changes of s the player decides about.
func asksOverrides(s *update.Session) []update.OverrideChange {
	var out []update.OverrideChange
	for _, c := range s.Overrides {
		if c.Asks() {
			out = append(out, c)
		}
	}
	return out
}

// overridesDialog asks which version of each overridden mod the update changes the
// player wants: theirs or GTNH's new one (space switches). esc cancels the update.
func (m *model) overridesDialog() {
	s := m.session
	changes := asksOverrides(s)
	items := make([]ditem, len(changes))
	for i, c := range changes {
		items[i] = ditem{title: c.Jar, desc: overrideChoiceDesc(c, s.OverrideChoice(c.Jar)), key: c.Jar}
	}
	title := "Which version of these mods do you want?"
	if len(changes) == 1 {
		title = "Which version of this mod do you want?"
	}
	m.openList(title, "GTNH "+m.target+" changed "+plural(len(changes), "a mod", "mods")+
		" you use your own jar for. Space switches one, enter when you're done.", items, items[0].key,
		func(m *model, _ string) tea.Cmd {
			m.closeDialog()
			return m.afterOverrides()
		})
	l := m.dialog.list
	l.toggle = func(m *model, jar string) {
		c := update.TakeNew
		if s.OverrideChoice(jar) == update.TakeNew {
			c = update.KeepMine
		}
		s.ChooseOverride(jar, c)
		for i := range l.items {
			l.items[i].desc = overrideChoiceDesc(changes[i], s.OverrideChoice(changes[i].Jar))
		}
	}
	m.dialog.buttons = []string{"Done"}
	m.dialog.onButton = func(m *model, label string) tea.Cmd {
		if label == "" {
			return cancelUpdate(m)
		}
		m.closeDialog()
		return m.afterOverrides()
	}
}

// overrideChoiceDesc is what choice c means for the override change ch.
func overrideChoiceDesc(ch update.OverrideChange, c update.Choice) string {
	switch {
	case c == update.KeepMine && len(ch.New) == 0:
		return "keep mine · GTNH dropped this mod"
	case c == update.KeepMine:
		return "keep mine · GTNH has " + jarList(ch.New)
	case len(ch.New) == 0:
		return "take mine out"
	}
	return "use GTNH's " + jarList(ch.New)
}

// overrideLines are the confirmation's lines about the overrides the update changes.
func overrideLines(s *update.Session, target string) []string {
	var out []string
	for _, c := range s.Overrides {
		gtnh := c.New
		if len(gtnh) == 0 {
			gtnh = c.Old
		}
		keep := s.OverrideChoice(c.Jar) == update.KeepMine
		switch {
		case c.Gone:
			out = append(out, "GTNH's "+jarList(gtnh)+" comes back on: your "+c.Jar+" isn't in the mods folder any more")
		case c.Shipped:
			out = append(out, "GTNH "+target+" ships your "+c.Jar+" itself, so it's GTNH's from now on")
		case keep && len(c.New) == 0:
			out = append(out, "Your "+c.Jar+" stays as a mod of your own — GTNH "+target+" dropped that mod")
		case keep:
			out = append(out, "Your "+c.Jar+" stays instead of GTNH's "+jarList(c.New))
		case len(c.New) == 0:
			out = append(out, "Your "+c.Jar+" is taken out — it goes to the backup")
		default:
			out = append(out, "GTNH's "+jarList(c.New)+" replaces your "+c.Jar+" — yours goes to the backup")
		}
	}
	return out
}
