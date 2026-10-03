package tui

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	body, footer, scroll := m.page()
	return m.chrome(body, footer, scroll)
}

// page builds the current screen for chrome: body, the pinned footer and the scroll
// offset to show.
func (m *model) page() (body, footer string, scroll int) {
	scroll = m.scroll
	switch m.screen {
	case scHome:
		body = m.list.View()
		if m.homeCardShows(scHome) {
			body = lipgloss.JoinHorizontal(lipgloss.Top, body, m.cardView())
		}
		footer = m.homeHelp()
	case scInstalled, scTarget, scSettings, scConflicts, scResolve:
		body, footer = m.titledList(), keyBar(m.width, m.listKeyPairs()...)
	case scLoading:
		body, scroll = m.spin.View()+" Looking for your GTNH instances…", math.MaxInt32
	case scServerMods:
		body, footer = m.serverModsView()
	case scName:
		body, footer = m.nameView()
	case scSettingEdit:
		body, footer = m.settingEditView()
	case scBackups:
		if len(m.backups) > 0 {
			body, footer = m.titledList(), keyBar(m.width, m.listKeyPairs()...)
		} else {
			body, footer = m.backupsView()
		}
	case scRestoreConfirm:
		body, footer = m.restoreConfirmView()
	case scRestored:
		body, footer = m.restoredView()
	case scPreparing, scApplying, scRestoring, scSelfUpdate:
		body, footer = m.busyView()
		scroll = math.MaxInt32 // keep the newest step in view
	case scConfirm:
		if m.creating {
			body, footer = m.confirmCreateView()
		} else {
			body, footer = m.confirmView()
		}
	case scDone:
		if m.creating {
			body, footer = m.createdView()
		} else {
			body, footer = m.doneView()
		}
	case scLaunching:
		body, footer = m.launchingView()
	case scPlaying:
		body, footer = m.playingView()
	case scError:
		body, footer = m.errorView()
	case scSelfUpdated:
		b := m.blocks()
		b.success("The launcher is now version " + m.newer.Version + ".")
		body = b.String()
		footer = m.buttonFooter("enter", "restart now", "q", "quit")
	}
	return strings.TrimRight(body, "\n"), footer, scroll
}

// titledList is a non-home list under its wrapped title. The list's own title bar,
// empty but for the filter prompt, is the blank line between them.
func (m *model) titledList() string {
	return titleSty.Render(m.list.Title) + "\n" + m.list.View()
}

func (m *model) launchingView() (body, footer string) {
	return m.spin.View() + " Starting " + m.inst.Name + " in Prism Launcher…", ""
}

func (m *model) playingView() (body, footer string) {
	var state string
	switch m.playState {
	case "starting":
		state = m.spin.View() + " Prism Launcher is starting the game…"
	case "slow":
		state = warnSty.Render(wrap("I haven't seen the game start yet. Maybe Prism is asking you something — have a look at its window.", m.bodyWidth()))
	case "running":
		state = okSty.Render("The game is running (since "+m.runningSince.Format("15:04")+").") + "\n" +
			dimSty.Render("Leave me open or press q — the game keeps running either way.")
	case "closed":
		state = "The game closed. Have fun next time!"
	case "unknown":
		state = dimSty.Render(wrap("The game should be starting from Prism now. I can't tell on this computer whether it's running.", m.bodyWidth()))
	}
	return titleSty.Render(wrap(m.inst.Name, m.bodyWidth())) + "\n\n" + state, m.buttonFooter("enter", "back", "q", "quit")
}

// serverModsIntro explains the server extra mods link wherever the player is asked for it.
const serverModsIntro = "Some servers add a few mods on top of GTNH. If the server owner gave you a link for " +
	"them, paste it here — I'll install them now and keep them in sync every time you update."

// msgBadModsLink is shown when a server extra mods link is rejected.
const msgBadModsLink = "That doesn't look like a download link — it should start with https://"

func (m *model) serverModsView() (body, footer string) {
	return m.inputView("Does your server have its own extra mods?", serverModsIntro,
		m.input, m.inputEr, "No link? Leave it empty — you can add one later with m on the version list.")
}

func (m *model) nameView() (body, footer string) {
	return m.inputView("What should the new instance be called?",
		"That's the name you'll see in Prism; it's also the folder name.",
		m.nameIn, m.nameEr, "It'll be created in "+m.instancesDir())
}

// inputView is a text-input screen: title, intro, the input, its error (if any) and a
// dim footnote.
func (m *model) inputView(title, intro string, input textinput.Model, errText, footnote string) (body, footer string) {
	var b strings.Builder
	b.WriteString(titleSty.Render(title) + "\n\n")
	b.WriteString(wrap(intro, m.bodyWidth()) + "\n\n")
	b.WriteString(input.View() + "\n")
	if errText != "" {
		b.WriteString(badSty.Render(wrap(errText, m.bodyWidth())) + "\n")
	}
	b.WriteString("\n" + dimSty.Render(wrap(footnote, m.bodyWidth())))
	return b.String(), hint("enter", "continue", "esc", "back")
}

func (m *model) busyView() (body, footer string) {
	var title string
	switch {
	case m.screen == scSelfUpdate:
		title = "Updating the launcher"
	case m.screen == scRestoring:
		title = fmt.Sprintf("Putting %s back on GTNH %s", m.inst.Name, m.backup.Info.From)
	case m.creating && m.screen == scApplying:
		title = "Creating " + m.newName
	case m.creating:
		title = "Getting GTNH " + m.target + " ready"
	case m.screen == scApplying:
		title = fmt.Sprintf("Updating %s to GTNH %s", m.inst.Name, m.target)
	default:
		title = fmt.Sprintf("Getting GTNH %s ready for %s", m.target, m.inst.Name)
	}
	var steps []string
	for _, s := range m.steps {
		steps = append(steps, okSty.Render("✓ ")+dimSty.Render(indentWrap(s, m.bodyWidth()-6, 2)))
	}
	if m.step != "" {
		steps = append(steps, m.spin.View()+indentWrap(m.step, m.bodyWidth()-6, 2))
	}
	if m.total > 0 {
		pct := float64(m.done) / float64(m.total)
		steps = append(steps, "", m.bar.ViewAs(pct), dimSty.Render(m.progressDetail()))
	}
	body = titleSty.Render(wrap(title, m.bodyWidth())) + "\n\n" + panel("", strings.Join(steps, "\n"), m.bodyWidth()) + "\n" + m.headsUp()
	switch {
	case m.screen != scPreparing:
		footer = warnSty.Render(indentWrap("Please don't close this window until I'm done.", m.bodyWidth(), 0))
	case m.quitAfterCancel:
		footer = dimSty.Render(wrap("Stopping and cleaning up…", m.bodyWidth()))
	default:
		footer = hint("ctrl+c", "cancel")
	}
	return body, footer
}

func (m *model) progressDetail() string {
	var s string
	if m.total > 1_000_000 {
		s = fmt.Sprintf("%s of %s", mb(m.done), mb(m.total))
	} else {
		s = fmt.Sprintf("%s of %s files", num(m.done), num(m.total))
	}
	if left := eta(m.done, m.total, time.Since(m.stepStart)); left != "" {
		s += " · " + left
	}
	return s
}

func (m *model) confirmView() (body, footer string) {
	s, pl := m.session, m.session.Plan
	b := m.blocks()
	title := fmt.Sprintf("Ready to refresh %s on GTNH %s", m.inst.Name, m.target)
	if m.target != m.detect.Version {
		title = fmt.Sprintf("Ready to update %s from %s to %s", m.inst.Name, m.detect.Version, m.target)
	}
	b.title(title)
	b.raw(m.confirmWarnings())
	if ins, rem := pl.Count(update.Install), pl.Count(update.Remove); ins+rem == 0 {
		b.bullet("Your GTNH files are already exactly as they should be.")
	} else {
		b.bullet(fmt.Sprintf("%s %s will be updated and %s removed.", num(int64(ins)), plural(ins, "file", "files"), num(int64(rem))))
	}
	b.bullet(worldsStayBullet)
	if n := len(pl.Kept); n > 0 {
		b.bullet(fmt.Sprintf("%s config %s you changed will be kept as you have %s.", num(int64(n)), plural(n, "file", "files"), plural(n, "it", "them")))
	}
	if n := len(pl.Chosen(update.TakeNew)); n > 0 {
		b.bullet(fmt.Sprintf("%d config %s you changed %s the new version. Your old %s %s to the backup folder.",
			n, plural(n, "file", "files"), plural(n, "gets", "get"), plural(n, "one", "ones"), plural(n, "goes", "go")))
	}
	if n := len(pl.Chosen(update.KeepMine)); n > 0 {
		b.bullet(fmt.Sprintf("%d config %s you changed %s as you have %s; the new %s saved next to %s with .mcnew at the end.",
			n, plural(n, "file", "files"), plural(n, "stays", "stay"), plural(n, "it", "them"),
			plural(n, "one is", "ones are"), plural(n, "it", "them")))
	}
	if len(pl.ExtraMods) > 0 {
		b.bullet(fmt.Sprintf("Mods you added yourself stay: %s. Make sure they work with %s.",
			ansi.Truncate(strings.Join(pl.ExtraMods, ", "), 120, "…"), m.target))
	}
	if m.serverMods != "" {
		b.bullet("Your server's extra mods will be synced from " + hostOf(m.serverMods) + ".")
	}
	if s.Flavor == manifest.Java8 {
		b.bullet("This instance uses the Java 8 version of the pack, so that's what you'll get.")
	}
	b.bullet("Everything that gets replaced is backed up first, just in case.")
	b.closeMinecraftNote()
	b.headsUp()
	return b.String(), m.buttonFooter("enter", "update now", "esc", "back")
}

// confirmWarnings are the red warnings above the update summary: a downgrade, or an
// installed version that's probably wrong.
func (m *model) confirmWarnings() string {
	pl := m.session.Plan
	b := m.blocks()
	if update.IsDowngrade(m.target, m.detect.Version) {
		b.danger(downgradeWarning(m.detect.Version))
	}
	if pl.BaselineSuspect() {
		b.danger(fmt.Sprintf("Only %.0f%% of the mods that come with %s are in this instance, so it's "+
			"probably not on %s. Press esc, then i to tell me the right version — otherwise old mods could be left behind.",
			pl.BaselineMatch*100, m.detect.Version, m.detect.Version))
	}
	return b.String()
}

// doneFooter is the footer of the screens after a finished update, creation or restore.
func (m *model) doneFooter() string {
	return m.buttonFooter("enter", "back", "p", "play now", "q", "quit")
}

func (m *model) doneView() (body, footer string) {
	r, pl := m.result, m.session.Plan
	b := m.blocks()
	b.success(fmt.Sprintf("All done! %s is now on GTNH %s.", m.inst.Name, r.To))
	if ins, rem := pl.Count(update.Install), pl.Count(update.Remove); ins+rem == 0 {
		b.bullet("Your GTNH files were already up to date.")
	} else {
		b.bullet(fmt.Sprintf("%s %s updated, %s removed.", num(int64(ins)), plural(ins, "file", "files"), num(int64(rem))))
	}
	if r.Renamed != "" {
		b.bullet(renamedBullet(r.Renamed))
	}
	b.raw(m.customModsSummary(r.CustomMods, r.CustomErr))
	if n := len(pl.Chosen(update.TakeNew)); n > 0 {
		b.bullet(fmt.Sprintf("%d config %s you had changed %s replaced with the new version; the old %s in the backup folder.",
			n, plural(n, "file", "files"), plural(n, "was", "were"), plural(n, "one is", "ones are")))
	}
	if c := pl.Chosen(update.KeepMine); len(c) > 0 {
		b.blank()
		b.raw(wrap(fmt.Sprintf("%d config %s changed on your side and in the new version. "+
			"I kept yours and saved the new %s next to %s as .mcnew. It's usually fine to ignore this — "+
			"many mods rewrite their own config when the game starts.",
			len(c), plural(len(c), "file was", "files were"), plural(len(c), "one", "ones"), plural(len(c), "it", "them")), m.bodyWidth()) + "\n")
		const limit = 20
		for i, p := range c {
			if i == limit {
				b.raw(dimSty.Render(fmt.Sprintf("    … and %d more", len(c)-limit)) + "\n")
				break
			}
			b.raw(dimSty.Render("    "+strings.TrimPrefix(p, ".minecraft/")) + "\n")
		}
	}
	if r.BackupDir != "" {
		b.blank()
		b.raw(dimSty.Render(wrap("If something's wrong, the old files are in "+r.BackupDir, m.bodyWidth())) + "\n")
	}
	b.headsUp()
	return b.String(), m.doneFooter()
}

// customModsSummary describes what the server-mods sync did (cm nil = it didn't run).
func (m *model) customModsSummary(cm *update.CustomModsResult, cerr error) string {
	b := m.blocks()
	if cm != nil {
		switch {
		case m.serverMods == "" && len(cm.Removed) > 0:
			b.bullet(fmt.Sprintf("Removed %d extra %s from your old server.", len(cm.Removed), plural(len(cm.Removed), "mod", "mods")))
		case len(cm.Installed) == 0:
			b.bullet("Your server has no extra mods right now.")
		default:
			line := fmt.Sprintf("%d extra %s from your server installed", len(cm.Installed), plural(len(cm.Installed), "mod", "mods"))
			if len(cm.Added)+len(cm.Removed) > 0 {
				line += fmt.Sprintf(" (%d new or updated, %d removed)", len(cm.Added), len(cm.Removed))
			}
			b.bullet(line + ".")
		}
		if len(cm.Skipped) > 0 {
			b.bullet("Skipped " + strings.Join(cm.Skipped, ", ") + " — GTNH already ships a mod with that name.")
		}
	}
	if cerr != nil {
		b.warnBullet("Your server's extra mods couldn't be synced this time (" + cerr.Error() + "). Run me again later to retry.")
	}
	return b.String()
}

func (m *model) confirmCreateView() (body, footer string) {
	c := m.creation
	b := m.blocks()
	b.title(fmt.Sprintf("Ready to create %s with GTNH %s", m.newName, m.target))
	b.bullet("It'll be a new instance in Prism, in " + c.Dir + ".")
	b.bullet(num(int64(c.Files)) + " files will be installed.")
	b.bullet("Your other instances aren't touched.")
	if m.serverMods != "" {
		b.bullet("Your server's extra mods will be installed from " + hostOf(m.serverMods) + ".")
	}
	if c.Flavor == manifest.Java8 {
		b.bullet("This version only comes as a Java 8 pack, so that's what you'll get.")
	} else {
		b.bullet("It uses the Java 17+ version of the pack.")
	}
	b.headsUp()
	return b.String(), m.buttonFooter("enter", "create it", "esc", "back")
}

func (m *model) createdView() (body, footer string) {
	r := m.created
	b := m.blocks()
	b.success(fmt.Sprintf("All done! %s is ready in Prism.", r.Instance.Name))
	b.bullet(num(int64(r.Files)) + " files installed.")
	b.raw(m.customModsSummary(r.CustomMods, r.CustomErr))
	b.bullet("If Prism is already open and doesn't show it, restart Prism.")
	b.headsUp()
	return b.String(), m.doneFooter()
}

func (m *model) errorView() (body, footer string) {
	b := m.blocks()
	b.note(badSty, "Something went wrong")
	b.para(m.err.Error())
	var leftover *update.LeftoverError
	switch {
	case m.creating && (m.errPhase == scPreparing || m.errPhase == scApplying) && errors.As(m.err, &leftover):
		// The error itself says the folder is still there and what to do.
	case m.creating && m.errPhase == scPreparing:
		b.note(okSty, "Nothing was created.")
	case m.creating && m.errPhase == scApplying:
		b.note(okSty, "I removed the half-made instance, so there's nothing to clean up.")
	case m.errPhase == scPreparing:
		b.note(okSty, "Nothing was changed.")
	case m.errPhase == scApplying && errors.Is(m.err, update.ErrRolledBack):
		b.note(okSty, "Everything was put back the way it was, so your instance is exactly as before.")
	case m.errPhase == scApplying:
		b.note(badSty, "Some files may have changed. The originals are in the .gtnh-updater folder inside the instance.")
	case m.errPhase == scRestoring:
		b.raw(m.restoreErrorNote())
	}
	if m.canGoBack() {
		return b.String(), m.buttonFooter("enter", "back", "q", "quit")
	}
	return b.String(), m.buttonFooter("enter", "quit")
}
