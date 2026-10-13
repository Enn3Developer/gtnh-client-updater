package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Right after the instances are loaded the launcher syncs the server mods of every GTNH
// instance with a link, one after the other, without being asked: the player never has
// to open anything to get the server's new mods. It is the same sync Play runs
// (journaled, rolled back on failure, never deleting the player's jars), and it changes
// nothing while the game runs from the instance: the next Play syncs then. The sync is
// quiet: a dim line in the Update section while it runs, then a notice there with what
// it did ("Server mods synced just now · 2 new", "up to date", or why not). Play on an
// instance waits for its sync, or stops a download for another one, instead of being
// refused.

// startAutoMods queues the startup syncs: the selected instance first (with the command
// line's link, when given), then every other GTNH instance with a link.
func (m *model) startAutoMods() tea.Cmd {
	if m.job != nil || len(m.autoQueue) > 0 {
		return nil
	}
	link := m.cfg.ServerMods
	cur, ok := m.current()
	var queue []prism.Instance
	if info := m.home[cur.Dir]; ok && info.gtnh && (link != "" || info.modsURL != "") {
		queue = append(queue, cur)
	} else {
		link = ""
	}
	for _, in := range m.insts {
		if info := m.home[in.Dir]; info.gtnh && info.modsURL != "" && (!ok || in.Dir != cur.Dir) {
			queue = append(queue, in)
		}
	}
	m.autoQueue, m.autoLink = queue, link
	return m.nextAutoMods()
}

// nextAutoMods starts the sync of the next queued instance; nil when none is left or
// something else runs.
func (m *model) nextAutoMods() tea.Cmd {
	for m.job == nil && len(m.autoQueue) > 0 {
		in, link := m.autoQueue[0], m.autoLink
		m.autoQueue, m.autoLink = m.autoQueue[1:], ""
		if info := m.home[in.Dir]; info.gtnh && (link != "" || info.modsURL != "") && !m.gameUnderway(in.Dir) {
			return m.startAutoSync(in, link)
		}
	}
	m.autoQueue, m.autoLink = nil, ""
	return nil
}

// startAutoSync starts the auto sync of in from link (a ModsSyncOptions.URL).
func (m *model) startAutoSync(in prism.Instance, link string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.autoCancel = cancel
	cmd := m.startModsSync(in, link, ctx, (*model).nextAutoMods)
	m.job.auto = true
	m.job.title = "Checking your server's mods"
	m.job.tryLink = link != "" && link != "none" && link != m.home[in.Dir].modsURL
	return cmd
}

// waitAutoMods makes the Play of inst (its sync from override, then then) wait for the
// sync the launcher runs on its own, which is all the syncing it needs when it's on
// inst; the syncs still queued are left to their own Play. A download for another
// instance is stopped, so the game doesn't wait for it.
func (m *model) waitAutoMods(inst prism.Instance, override string, then func(*model) tea.Cmd) tea.Cmd {
	m.autoQueue, m.autoLink = nil, ""
	if m.job.dir == inst.Dir && override == "" {
		m.afterMods = then
		return nil
	}
	m.afterMods = func(m *model) tea.Cmd {
		m.modsOverride = override
		return m.syncMods(inst, then)
	}
	if m.job.phase == "prepare" && m.autoCancel != nil {
		m.autoCancel()
	}
	return nil
}

// autoTried ends an auto sync whose link from the command line couldn't be fetched: the
// link isn't saved and nothing changes. false when s isn't such a sync.
func (m *model) autoTried(s *update.ModsSync) (tea.Cmd, bool) {
	if !m.job.auto || !m.job.tryLink || s.FetchErr == nil {
		return nil, false
	}
	s.Close()
	return m.endAutoMods(notice{warn: []string{"I couldn't use the server mods link you gave me, so I didn't save it: " +
		s.FetchErr.Error() + "."}}, true), true
}

// autoSynced is the notice of an auto sync that ran: what it did, or that the mods are
// up to date.
func autoSynced(msg modsSyncedMsg, info homeInfo) notice {
	if n, ok := modsNotice(msg.p, msg.fetchErr, info.modsURL != ""); ok {
		return n
	}
	return notice{info: []string{"Your server's mods are up to date."}}
}

// autoFailed is the notice of an auto sync that failed (false: say nothing, Play
// stopped its download).
func autoFailed(err error) (notice, bool) {
	switch {
	case errors.Is(err, context.Canceled):
		return notice{}, false
	case errors.Is(err, update.ErrGameRunning):
		return notice{info: []string{"The game is running, so I'll sync your server's mods the next time you play."}}, true
	}
	return notice{warn: []string{"I couldn't sync your server's mods: " + err.Error() + "."}}, true
}

// endAutoMods ends the running auto sync with n (when say) on its instance and runs what
// waits for it: the next sync, or a Play.
func (m *model) endAutoMods(n notice, say bool) tea.Cmd {
	j, then := m.job, m.afterMods
	if m.autoCancel != nil {
		m.autoCancel()
		m.autoCancel = nil
	}
	m.job, m.afterMods = nil, nil
	m.refresh()
	if say {
		m.notices[j.dir] = n
		m.holdOpen = m.holdOpen || len(n.warn) > 0
	}
	return then(m)
}

// autoLine is the dim line the Update section of dir shows while the launcher syncs its
// server mods on its own; "" when it doesn't.
func (m *model) autoLine(dir string) string {
	if !m.jobOn(dir) || !m.job.auto {
		return ""
	}
	what := "Checking your server's mods…"
	if m.busyApplying() {
		what = "Installing your server's mods…"
	}
	return m.spin.View() + " " + what
}
