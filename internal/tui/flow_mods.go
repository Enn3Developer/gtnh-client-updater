package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// A server's extra mods follow the server, not the GTNH version: they're synced before
// every Play and right after the player changes the link, by a short job of its own
// (prepare = ask the server, apply = change mods/) that then runs whatever waited for
// it, like starting the game. A sync that fails never keeps the game from starting: it
// ends in a notice.

const serverModsIntro = "Some servers add a few mods on top of GTNH. If the server owner gave you a link for them, paste it here — I'll install them and keep them in sync every time you play."

const msgBadModsLink = "That doesn't look like a download link — it should start with https://"

// serverModsLink is the server-mods link an update or a creation uses: the command
// line's ("none" = none) over what the instance remembers (st may be nil). given reports
// a link from the command line, which counts as the player's answer.
func serverModsLink(cfg string, st *update.State) (url string, given bool) {
	switch {
	case cfg == "none":
		return "", true
	case cfg != "":
		return cfg, true
	case st != nil:
		return st.CustomModsURL, false
	}
	return "", false
}

// askServerMods asks whether the server of inst has extra mods and saves the answer,
// then runs next, or without one syncs the mods the answer named. esc just closes.
func (m *model) askServerMods(inst prism.Instance, next func(*model) tea.Cmd) tea.Cmd {
	m.openInput("Does your server have extra mods?", serverModsIntro, "", "https://…/custom_mods.zip",
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
			m.closeDialog()
			err := update.UpdateState(inst.Dir, func(st *update.State) { st.CustomModsURL, st.CustomModsAsked = value, true })
			if err != nil {
				m.notify(couldntSaveSetting, err.Error())
				return nil
			}
			m.refresh()
			if next != nil {
				return next(m)
			}
			return m.syncMods(inst, nil)
		})
	return nil
}

// syncMods syncs the server mods of inst as a job, then runs then (nil = nothing more).
// With nothing to sync (no link, no jars from an old one) then runs right away; so it
// does while another job runs, with a notice that the sync waits for the next Play.
func (m *model) syncMods(inst prism.Instance, then func(*model) tea.Cmd) tea.Cmd {
	if then == nil {
		then = func(*model) tea.Cmd { return nil }
	}
	override := m.modsOverride
	m.modsOverride = ""
	if info := m.home[inst.Dir]; !info.gtnh || info.modsURL == "" && info.mods == 0 && override == "" {
		return then(m)
	}
	if m.job != nil {
		m.notices[inst.Dir] = notice{warn: []string{"I didn't check your server's mods because I'm busy with " +
			m.jobName() + ". I'll do it the next time you play."}}
		m.holdOpen = true
		return then(m)
	}
	m.job = &job{kind: jobMods, dir: inst.Dir, title: "Syncing your server's mods", phase: "prepare"}
	m.afterMods = then
	m.startBusy()
	opts := update.ModsSyncOptions{Client: m.cfg.Client, Instance: inst, URL: override}
	prepare, rep := m.prepareMods, m.reporter()
	return func() tea.Msg {
		s, err := prepare(opts, rep)
		return orErr(err, modsPreparedMsg{s})
	}
}

// onModsPrepared carries out the planned sync right away: the player asked for these
// mods, there's nothing to confirm.
func (m *model) onModsPrepared(s *update.ModsSync) tea.Cmd {
	if m.job == nil || m.job.kind != jobMods {
		s.Close()
		return nil
	}
	m.job.phase = "apply"
	apply, rep := m.applyMods, m.reporter()
	return func() tea.Msg {
		p, err := apply(s, rep)
		return orErr(err, modsSyncedMsg{p, s.FetchErr})
	}
}

// onModsSynced ends the sync with its notice, when it has something to say, and runs
// what waited for it.
func (m *model) onModsSynced(msg modsSyncedMsg) tea.Cmd {
	j, then := m.job, m.afterMods
	m.job, m.afterMods = nil, nil
	m.refresh()
	if n, ok := modsNotice(msg.p, msg.fetchErr, m.home[j.dir].modsURL != ""); ok {
		m.notices[j.dir] = n
		m.holdOpen = m.holdOpen || len(n.warn) > 0
	}
	return then(m)
}

// onModsErr ends a sync that failed with a warning; what waited for it still runs.
func (m *model) onModsErr(err error) tea.Cmd {
	j, then := m.job, m.afterMods
	m.job, m.afterMods = nil, nil
	m.refresh()
	m.notices[j.dir] = notice{warn: []string{"I couldn't sync your server's mods: " + err.Error() + "."}}
	m.holdOpen = true
	return then(m)
}

// modsNotice is the notice after a sync on its own (linked: the instance has a link, so
// it isn't just taking out an old server's mods); false when it changed nothing and the
// server answered.
func modsNotice(p *update.ModsPlan, fetchErr error, linked bool) (notice, bool) {
	counts := modsCounts(p)
	if counts == "" && fetchErr == nil {
		return notice{}, false
	}
	var n notice
	switch {
	case counts == "":
	case linked:
		n.text = "Server mods synced just now · " + counts
	default:
		k := p.Count(update.ModRemove)
		n.text = fmt.Sprintf("Removed %d %s of your old server just now", k, plural(k, "mod", "mods"))
	}
	n.addMods(p, fetchErr, counts != "")
	return n, true
}

// addMods adds to n what the server-mods sync of a job did (p nil: none ran; err: why
// the server couldn't be asked or the sync failed): a dim line with its counts unless
// the notice already says them (counted), then what the player should know.
func (n *notice) addMods(p *update.ModsPlan, err error, counted bool) {
	switch {
	case err != nil && p == nil:
		n.warn = append(n.warn, "I couldn't sync your server's mods: "+err.Error()+".")
	case err != nil:
		n.warn = append(n.warn, "I couldn't check your server's mods: "+err.Error()+".")
	}
	if c := modsCounts(p); c != "" && !counted {
		n.info = append(n.info, "Server mods: "+c)
	}
	warn, info := modsLines(p, true)
	n.warn, n.info = append(n.warn, warn...), append(n.info, info...)
}

// modsPreview are a confirmation's lines about the server-mods sync p from link ("" =
// none, so p takes out what an old link installed): what changes and what to know.
func modsPreview(p *update.ModsPlan, link string) (lines, warn []string) {
	if p == nil {
		return nil, nil
	}
	head := "Server mods from " + hostOf(link)
	if link == "" {
		head = "Mods of your old server"
	}
	counts := modsCounts(p)
	if counts == "" {
		counts = "nothing changes"
	}
	warn, info := modsLines(p, false)
	return append([]string{head + ": " + counts}, info...), warn
}

// modsCounts is "2 new, 1 updated, 1 removed" for what p changes in mods/; "" for
// nothing.
func modsCounts(p *update.ModsPlan) string {
	if p == nil {
		return ""
	}
	var add, upd, rem int
	for _, c := range p.Changes {
		switch {
		case c.Kind == update.ModAdd:
			add++
		case c.Kind == update.ModUpdate || c.Kind == update.ModReplace:
			upd++
		case c.Kind == update.ModRemove || c.Kind == update.ModSkip && c.Disk != "":
			rem++
		}
	}
	var parts []string
	for _, part := range []struct {
		n    int
		what string
	}{{add, "new"}, {upd, "updated"}, {rem, "removed"}} {
		if part.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", part.n, part.what))
		}
	}
	return strings.Join(parts, ", ")
}

// modsLines are what the player should know about the sync p, which ran (done) or is
// about to: warnings (their own jars moved aside, files of the zip left out) and dim
// lines (server jars left out, jars kept).
func modsLines(p *update.ModsPlan, done bool) (warn, info []string) {
	if p == nil {
		return nil, nil
	}
	var mine, skipped []string
	for _, c := range p.Changes {
		if c.Preserve {
			mine = append(mine, c.Disk)
		}
		if c.Kind == update.ModSkip {
			skipped = append(skipped, c.Name)
		}
	}
	if len(mine) > 0 {
		verb := "I'll move"
		if done {
			verb = "I moved"
		}
		warn = append(warn, verb+" your own "+jarList(mine)+" to .gtnh-updater/replaced-mods inside the instance to make way for the server's")
	}
	if len(p.Ignored) > 0 {
		warn = append(warn, "Left out of the server's zip (inside a folder, or a name Windows doesn't allow): "+jarList(p.Ignored))
	}
	if len(skipped) > 0 {
		info = append(info, "Left out, this instance has "+plural(len(skipped), "it", "them")+" already: "+jarList(skipped))
	}
	if kept := p.Names(update.ModKeep); len(kept) > 0 {
		info = append(info, "The server dropped "+jarList(kept)+", but you changed "+plural(len(kept), "it", "them")+
			", so "+plural(len(kept), "it stays", "they stay"))
	}
	return warn, info
}

// jarList lists jar names, cut at 100 columns.
func jarList(ns []string) string {
	return ansi.Truncate(strings.Join(ns, ", "), 100, "…")
}
