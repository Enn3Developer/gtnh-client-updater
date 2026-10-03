package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

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
