package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// dropSession closes the prepared update.
func (m *model) dropSession() tea.Cmd {
	m.session.Close()
	m.session = nil
	return nil
}

// prepare downloads and plans the update of inst to m.target.
func (m *model) prepare(inst prism.Instance) tea.Cmd {
	m.startBusy()
	opts := update.Options{
		Client: m.cfg.Client, Manifest: m.manifest, Instance: inst,
		Installed: m.detect.Version, Target: m.target, CustomModsURL: m.serverMods,
	}
	rep := m.reporter()
	return func() tea.Msg {
		s, err := update.Prepare(opts, rep)
		return orErr(err, preparedMsg{s})
	}
}

// apply runs the prepared update.
func (m *model) apply() tea.Cmd {
	m.startBusy()
	s, rep := m.session, m.reporter()
	return func() tea.Msg {
		r, err := s.Apply(rep)
		return orErr(err, appliedMsg{r})
	}
}

// serverModsSetting is the server-mods URL to use and whether it's settled: the command
// line ("none" = off) wins over what the instance remembers (st may be nil).
func serverModsSetting(cfg string, st *update.State) (url string, asked bool) {
	switch {
	case cfg == "none":
		return "", true
	case cfg != "":
		return cfg, true
	case st != nil && st.CustomModsAsked:
		return st.CustomModsURL, true
	default:
		return "", false
	}
}

const serverModsIntro = "Some servers add a few mods on top of GTNH. If the server owner gave you a link for them, paste it here — I'll install them now and keep them in sync every time you update."

const msgBadModsLink = "That doesn't look like a download link — it should start with https://"

// startUpdate updates the current instance to target (C3): refuses while another job or
// the game runs, asks for the installed version and the server mods when they aren't
// known, then starts preparing.
func (m *model) startUpdate(target string) tea.Cmd {
	if m.job != nil {
		m.notifyBusy()
		return nil
	}
	inst, ok := m.current()
	if !ok {
		return nil
	}
	running, err := m.isRunning(inst)
	if err == nil && running {
		m.notify("The game is running", inst.Name+" is running right now. Close Minecraft, then try again.")
		return nil
	}
	m.runUnknown = err != nil
	st, _ := update.LoadState(inst.Dir)
	if m.cfg.Installed != "" {
		m.detect = update.Detection{Version: m.cfg.Installed, Source: "command line"}
	} else {
		m.detect = update.DetectVersion(inst, st, m.manifest)
	}
	if m.detect.Version != "" {
		return m.askServerMods(inst, st, target)
	}
	now := m.now()
	items := make([]ditem, len(m.manifest.Releases))
	for i, r := range m.manifest.Releases {
		items[i] = ditem{title: r.Version, desc: releaseDesc(r, now), key: r.Version}
	}
	m.openList("Which GTNH version is "+inst.Name+" on right now?", "", items, "",
		func(m *model, v string) tea.Cmd {
			m.detect = update.Detection{Version: v, Source: "you told me"}
			m.closeDialog()
			return m.askServerMods(inst, st, target)
		})
	return nil
}

// askServerMods settles the server-mods link (C3d), asking when it never was, then
// starts preparing.
func (m *model) askServerMods(inst prism.Instance, st *update.State, target string) tea.Cmd {
	return m.askServerModsThen(st, func(m *model) tea.Cmd { return m.beginPrepare(inst, target) })
}

// askServerModsThen settles the server-mods link, asking when it never was (esc stops
// there), then runs then.
func (m *model) askServerModsThen(st *update.State, then func(m *model) tea.Cmd) tea.Cmd {
	url, asked := serverModsSetting(m.cfg.ServerMods, st)
	if asked {
		m.serverMods, m.serverModsAsked = url, true
		return then(m)
	}
	m.openInput("Does your server have extra mods?", serverModsIntro, url, "https://…/custom_mods.zip",
		"Leave it empty if there's none. You can change it later in Settings.", []string{"Continue"},
		func(m *model, label, value string) tea.Cmd {
			if label == "" {
				m.closeDialog()
				return nil
			}
			if value != "" && update.CheckCustomModsURL(value) != nil {
				m.dialog.inputErr = msgBadModsLink
				return nil
			}
			m.serverMods, m.serverModsAsked = value, true
			m.closeDialog()
			return then(m)
		})
	return nil
}

// beginPrepare starts the job that prepares the update of inst to target (C3e).
func (m *model) beginPrepare(inst prism.Instance, target string) tea.Cmd {
	m.target = target
	m.job = &job{kind: jobUpdate, dir: inst.Dir, title: "Checking what GTNH " + target + " changes", phase: "prepare"}
	delete(m.notices, inst.Dir)
	return m.prepare(inst)
}

// onPrepared keeps the prepared session and asks about conflicting configs, if any,
// else for confirmation (C4).
func (m *model) onPrepared(s *update.Session) tea.Cmd {
	m.session = s
	if s.Plan.Count(update.Conflict) == 0 {
		return m.confirmUpdate()
	}
	c := update.Recommended
	if ch, err := update.ParseChoice(m.cfg.Configs); err == nil {
		c = ch
	}
	s.Plan.ChooseAll(c)
	m.conflictsDialog()
	return nil
}

// onApplied ends the job, keeps its notice for the instance and reloads (C4).
func (m *model) onApplied(r *update.Result) tea.Cmd {
	dir := m.job.dir
	n := noticeOf(r, m.session.Plan)
	m.job = nil
	m.notices[dir] = n
	m.dropSession()
	return m.reload()
}

// onJobErr ends the running job with err and says what it means (C4, C9, C13); a
// cancelled download the player is waiting on quits instead (C10).
func (m *model) onJobErr(err error) tea.Cmd {
	j := m.job
	outcome := m.jobOutcome(err)
	if j.kind == jobCreate && m.createDone() {
		m.job = nil
		_, cmd := m.quit()
		return cmd
	}
	m.job = nil
	if m.session != nil {
		m.dropSession()
	}
	switch j.kind {
	case jobSelf:
		m.closeDialog() // the progress dialog
	case jobCreate:
		m.refresh() // the pending row is gone
	}
	if outcome == "" {
		m.notify("Something went wrong", err.Error())
	} else {
		m.errorDialog(err, outcome)
	}
	return nil
}

// updateOutcome is what a failed update means for the instance (C4).
func (m *model) updateOutcome(err error) string {
	switch {
	case m.job.phase == "prepare":
		return "Nothing was changed."
	case errors.Is(err, update.ErrRolledBack):
		return "Everything was put back the way it was, so your instance is exactly as before."
	}
	return "Some files may have changed. The originals are in the .gtnh-updater folder inside the instance."
}

// cancelUpdate drops the prepared update before anything was changed.
func cancelUpdate(m *model) tea.Cmd {
	m.dropSession()
	m.job = nil
	m.closeDialog()
	return nil
}

// cancelOnEsc is the button handler of the conflict dialogs: esc cancels the update.
func cancelOnEsc(m *model, label string) tea.Cmd {
	if label == "" {
		return cancelUpdate(m)
	}
	return m.confirmUpdate()
}

// confirmUpdate opens the confirmation of the update in m.session (C7).
func (m *model) confirmUpdate() tea.Cmd {
	pl, target, installed, name := m.session.Plan, m.target, m.detect.Version, m.jobName()
	title, ok, applying := "Update "+name+" to GTNH "+target+"?", "Update", "Updating to GTNH "+target
	down := update.IsDowngrade(target, installed)
	switch {
	case target == installed:
		title, ok, applying = "Refresh "+name+" on GTNH "+target+"?", "Refresh", "Refreshing GTNH "+target
	case down:
		title, ok = "Go back to GTNH "+target+"?", "Go back"
	}
	var lines, warn, bad []string
	if counts := changeCounts(pl); counts != "" {
		lines = append(lines, counts)
	} else {
		lines = append(lines, "Your files already match this version")
	}
	if n := len(pl.Kept); n > 0 {
		lines = append(lines, fmt.Sprintf("%d %s", n, plural(n,
			"config file you changed stays as it is", "config files you changed stay as they are")))
	}
	if n := len(pl.Chosen(update.TakeNew)); n > 0 {
		lines = append(lines, fmt.Sprintf("%d %s", n, plural(n,
			"config file you changed gets the new version — your old one goes to the backup",
			"config files you changed get the new version — your old ones go to the backup")))
	}
	if n := len(pl.Chosen(update.KeepMine)); n > 0 {
		lines = append(lines, fmt.Sprintf("%d %s", n, plural(n,
			"config file you changed stays yours — the new one is saved next to it as .mcnew",
			"config files you changed stay yours — the new ones are saved next to them as .mcnew")))
	}
	if len(pl.ExtraMods) > 0 {
		lines = append(lines, "Mods you added yourself stay: "+ansi.Truncate(strings.Join(pl.ExtraMods, ", "), 100, "…"))
	}
	if m.serverMods != "" {
		lines = append(lines, "Server mods synced from "+hostOf(m.serverMods))
	}
	if m.session.Flavor == manifest.Java8 {
		lines = append(lines, "This instance uses the Java 8 pack")
	}
	lines = append(lines, "Worlds, maps and settings aren't touched. Everything replaced is backed up first.")
	if m.runUnknown {
		warn = append(warn, "Make sure Minecraft is closed before you continue.")
	}
	for _, w := range m.warns {
		warn = append(warn, "Heads up: "+w)
	}
	if down {
		bad = append(bad, downgradeWarning(installed))
	}
	if pl.BaselineSuspect() {
		bad = append(bad, fmt.Sprintf("Only %.0f%% of the mods that come with %s are in this instance, so it's probably not on %s. "+
			"Cancel and use o to tell me the right version — otherwise old mods could be left behind.",
			pl.BaselineMatch*100, installed, installed))
	}
	m.confirmDialog(title, lines, warn, bad, ok, func(m *model) tea.Cmd {
		m.job.phase, m.job.title = "apply", applying
		m.closeDialog()
		return m.apply()
	}, cancelUpdate)
	return nil
}

// changeCounts is "<n> files updated, <n> removed"; "" when the plan changes no file.
func changeCounts(pl *update.Plan) string {
	ins, rem := pl.Count(update.Install), pl.Count(update.Remove)
	if ins+rem == 0 {
		return ""
	}
	return num(int64(ins)) + " " + plural(ins, "file", "files") + " updated, " + num(int64(rem)) + " removed"
}

// conflictsDialog asks what to do with the configs changed both by the player and by
// the new version (C6), preselecting the plan's current choice.
func (m *model) conflictsDialog() {
	pl := m.session.Plan
	n := pl.Count(update.Conflict)
	selected := "new"
	if cs := pl.Conflicts(); len(cs) > 0 && pl.ChoiceOf(cs[0]) == update.KeepMine {
		selected = "mine"
	}
	m.openList(fmt.Sprintf("%d %s changed both on your side and in the new version", n, plural(n, "config file", "config files")),
		"What should I do with them?", []ditem{
			{title: "Use the new versions", desc: "Recommended — your old copies go to the backup folder.", key: "new"},
			{title: "Keep mine", desc: "The new ones are saved next to yours with .mcnew at the end.", key: "mine"},
			{title: "Decide file by file", key: "pick"},
		}, selected, func(m *model, key string) tea.Cmd {
			switch key {
			case "new":
				m.session.Plan.ChooseAll(update.TakeNew)
			case "mine":
				m.session.Plan.ChooseAll(update.KeepMine)
			default:
				m.resolveDialog()
				return nil
			}
			m.closeDialog()
			return m.confirmUpdate()
		})
	m.dialog.onButton = cancelOnEsc
}

// resolveDialog lets the player pick a side for each conflicting config (C6 "pick").
func (m *model) resolveDialog() {
	pl := m.session.Plan
	paths := pl.Conflicts()
	items := make([]ditem, len(paths))
	for i, p := range paths {
		items[i] = ditem{title: strings.TrimPrefix(p, ".minecraft/"), desc: choiceDesc(pl.ChoiceOf(p)), key: p}
	}
	first := ""
	if len(paths) > 0 {
		first = paths[0]
	}
	m.openList("Which version of each file do you want?", "Space switches a file, enter when you're done.",
		items, first, func(m *model, _ string) tea.Cmd {
			m.closeDialog()
			return m.confirmUpdate()
		})
	l := m.dialog.list
	l.toggle = func(m *model, path string) {
		c := update.TakeNew
		if pl.ChoiceOf(path) == update.TakeNew {
			c = update.KeepMine
		}
		pl.Choose(path, c)
		for i := range l.items {
			l.items[i].desc = choiceDesc(pl.ChoiceOf(l.items[i].key))
		}
	}
	m.dialog.buttons = []string{"Done"}
	m.dialog.onButton = cancelOnEsc
}

// choiceDesc is what a conflict's choice means, for the resolve list.
func choiceDesc(c update.Choice) string {
	if c == update.TakeNew {
		return "new version"
	}
	return "keep mine"
}

// downgradeWarning warns that going back from ver may break worlds played on it.
func downgradeWarning(ver string) string {
	return "This goes BACK to an older version. Worlds you played on " + ver +
		" may lose blocks and items or not load at all. Copy your saves folder somewhere safe first."
}

// noticeOf is the notice the Update section shows after the update r of pl (C8).
func noticeOf(r *update.Result, pl *update.Plan) notice {
	counts := changeCounts(pl)
	if counts == "" {
		counts = "your files already matched"
	}
	n := notice{text: "Updated to GTNH " + r.To + " just now · " + counts}
	if r.Renamed != "" {
		n.text += ` · renamed to "` + r.Renamed + `"`
	}
	if k := len(pl.Chosen(update.KeepMine)); k > 0 {
		n.text += fmt.Sprintf(" · %d new config %s saved as .mcnew", k, plural(k, "version", "versions"))
	}
	n.warn, n.info = serverModsNotice(r.CustomErr, r.CustomMods)
	return n
}

// serverModsNotice is a notice's warning and info about the server's extra mods after a
// job synced them (customErr: the sync failed; cm: what it did, nil = nothing).
func serverModsNotice(customErr error, cm *update.CustomModsResult) (warn, info string) {
	if customErr != nil {
		warn = "Your server's extra mods couldn't be synced (" + customErr.Error() + "). I'll try again next time."
	}
	if cm != nil {
		switch {
		case len(cm.Installed) > 0:
			info = fmt.Sprintf("%d extra %s from your server installed", len(cm.Installed), plural(len(cm.Installed), "mod", "mods"))
		case len(cm.Removed) > 0:
			info = fmt.Sprintf("Removed %d extra %s from your old server", len(cm.Removed), plural(len(cm.Removed), "mod", "mods"))
		}
	}
	return warn, info
}
