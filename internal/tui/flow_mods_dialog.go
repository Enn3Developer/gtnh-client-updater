package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// The Server mods row and the question about a server's mods run the sync in a dialog:
// a new link is checked before it's saved, and the dialog shows the progress and then
// what the sync did.

// modsDialogTitle heads the Server mods dialog and the sync's progress and result in it.
const modsDialogTitle = "Your server's mods"

// modsFlow is a sync the player started in a dialog (the Server mods row, or the
// question): the dialog shows its progress and its result, so the player sees what
// happened where they asked for it, and a new link that the server can't give is never
// saved.
type modsFlow struct {
	try       bool                       // link is new: if it can't be fetched, it isn't saved
	next      func(*model) tea.Cmd       // nil: the result in a dialog; else a notice, then next
	reopen    func(m *model, err string) // back to the field the link was typed in, with why
	cancel    context.CancelFunc         // stops the download
	cancelled bool                       // esc while it downloaded: back to the field, quietly
}

// askModsLink opens the field for the server-mods link of inst, holding value. Continue
// with a link checks it by syncing from it, in a progress dialog: a link the server
// can't give comes back to the field with the reason and isn't saved. Empty means none:
// saved as the answer, the mods of an earlier link taken out. esc just closes.
func (m *model) askModsLink(inst prism.Instance, title, intro, value, note string, next func(*model) tea.Cmd) {
	m.openInput(title, intro, value, "https://…/custom_mods.zip", note, []string{"Continue"},
		func(m *model, label, value string) tea.Cmd {
			if label == "" {
				m.closeDialog()
				return nil
			}
			value = cleanLink(value)
			if value != "" && update.CheckCustomModsURL(value) != nil {
				m.dialog.inputErr = msgBadModsLink
				return nil
			}
			info := m.home[inst.Dir]
			if value == "" && info.modsURL == "" && info.mods == 0 || m.job != nil {
				// nothing to take out, or busy: save the answer, the sync waits for Play
				m.closeDialog()
				err := update.UpdateState(inst.Dir, func(st *update.State) { st.CustomModsURL, st.CustomModsAsked = value, true })
				if err != nil {
					m.notify(couldntSaveSetting, err.Error())
					return nil
				}
				m.refresh()
				return m.syncMods(inst, next)
			}
			url := value
			if url == "" {
				url = "none"
			}
			return m.startModsFlow(inst, &modsFlow{try: value != "" && value != info.modsURL, next: next,
				reopen: func(m *model, err string) {
					m.askModsLink(inst, title, intro, value, note, next)
					m.dialog.inputErr = err
				}}, url)
		})
}

// cleanLink is a pasted link without the spaces and quotes around it.
func cleanLink(v string) string {
	return strings.Trim(strings.TrimSpace(v), `"'<> `)
}

// manageMods is the Server mods row: without a link the field for one; with a link a
// dialog that shows it and the mods it installed, and offers to check for new mods now,
// to use another link, or to stop using them.
func (m *model) manageMods() tea.Cmd {
	in, ok := m.current()
	if m.dialog != nil || !ok {
		return nil
	}
	if m.job != nil {
		m.notifyBusy()
		return nil
	}
	info := m.home[in.Dir]
	if info.modsURL == "" {
		m.askModsLink(in, modsDialogTitle, serverModsIntro, "", "Leave it empty if there's none.", nil)
		return nil
	}
	m.openList(modsDialogTitle, modsSummary(info, m.now())+"\n", []ditem{
		{"Check for new mods", "get what the server has now", "sync"},
		{"Use another link", "", "link"},
		{"Stop using them", "take the server's mods out; your own mods stay", "none"},
	}, "sync", func(m *model, key string) tea.Cmd {
		switch key {
		case "sync":
			return m.startModsFlow(in, &modsFlow{}, "")
		case "link":
			m.askModsLink(in, modsDialogTitle, "Paste the link the server owner gave you.", info.modsURL,
				"Leave it empty to stop using the server's mods.", nil)
			return nil
		}
		return m.startModsFlow(in, &modsFlow{}, "none")
	})
	return nil
}

// modsSummary is the intro of the Server mods dialog: the link, then which mods it
// installed and when they were last synced.
func modsSummary(info homeInfo, now time.Time) string {
	s := "From " + info.modsURL + "\n"
	switch {
	case info.synced.IsZero() && len(info.modNames) == 0:
		return s + "Not synced yet."
	case len(info.modNames) == 0:
		return s + "The server has no mods for you right now. Synced " + ago(info.synced, now) + "."
	case info.synced.IsZero():
		return s + jarList(info.modNames)
	}
	return s + fmt.Sprintf("%d %s, synced %s: %s", len(info.modNames), plural(len(info.modNames), "mod", "mods"),
		ago(info.synced, now), jarList(info.modNames))
}

// startModsFlow starts the sync f is about from url (a ModsSyncOptions.URL), in a
// progress dialog where esc stops the download.
func (m *model) startModsFlow(inst prism.Instance, f *modsFlow, url string) tea.Cmd {
	m.closeDialog()
	if m.job != nil {
		m.notifyBusy()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	m.modsFlow = f
	title := "Checking your server's mods"
	if url == "none" {
		title = "Taking out your server's mods"
	}
	m.progressDialog(title)
	m.dialog.pairs = func(m *model) []string {
		switch {
		case m.busyApplying() && m.quitRefused:
			return []string{"", cantQuitNow}
		case m.busyApplying():
			return []string{"", "updating your mods — please don't close this window"}
		}
		return []string{"esc", "cancel"}
	}
	m.dialog.onButton = func(m *model, label string) tea.Cmd {
		if label == "" && m.modsFlow == f && !m.busyApplying() {
			f.cancelled = true
			f.cancel()
		}
		return nil
	}
	return m.startModsSync(inst, url, ctx, f.next)
}

// endModsFlow ends the sync job of a flow and closes its progress dialog.
func (m *model) endModsFlow() {
	if m.modsFlow != nil && m.modsFlow.cancel != nil {
		m.modsFlow.cancel()
	}
	m.job, m.afterMods, m.modsFlow = nil, nil, nil
	m.closeDialog()
}

// modsResult is what the result dialog of a sync says: what it did, what went wrong and
// what to know, then which of the server's mods the instance has now (info: read after
// the sync).
func modsResult(p *update.ModsPlan, fetchErr error, info homeInfo) (lines, warn, dim []string) {
	counts := modsCounts(p)
	switch {
	case info.modsURL == "" && counts != "":
		k := p.Count(update.ModRemove)
		lines = append(lines, fmt.Sprintf("I took out %d %s of your server.", k, plural(k, "mod", "mods")))
	case info.modsURL == "":
		lines = append(lines, "Done. This instance doesn't use a server's mods now.")
	case counts != "":
		lines = append(lines, "Done: "+counts+".")
	case fetchErr == nil:
		lines = append(lines, "You have all of the server's mods already.")
	}
	if fetchErr != nil {
		warn = append(warn, "I couldn't check your server's mods: "+fetchErr.Error()+".")
	}
	w, i := modsLines(p, true)
	warn, dim = append(warn, w...), append(dim, i...)
	if info.modsURL != "" && len(info.modNames) > 0 {
		dim = append(dim, "From "+hostOf(info.modsURL)+" now: "+jarList(info.modNames))
	}
	return lines, warn, dim
}

// resultDialog says what a job did: lines, then the warn lines and the dim lines, each
// block after a blank line; OK closes it.
func (m *model) resultDialog(title string, lines, warn, dim []string) {
	body := strings.Join(lines, "\n")
	for _, block := range []struct {
		lines []string
		sty   lipgloss.Style
	}{{warn, warnSty}, {dim, dimSty}} {
		if len(block.lines) == 0 {
			continue
		}
		if body != "" {
			body += "\n"
		}
		for _, l := range block.lines {
			body += "\n" + block.sty.Render(l)
		}
	}
	body = strings.TrimPrefix(body, "\n")
	m.openDialog(&dialog{
		title: title, body: func(int) string { return body }, buttons: []string{"OK"}, width: 72,
		onButton: closeOnly,
	})
}
