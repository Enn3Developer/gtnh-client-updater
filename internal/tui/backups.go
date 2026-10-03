package tui

// The undo screens: list an instance's update backups, confirm one, put it back.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// showBackups lists the restorable backups of m.inst, newest first, or explains that
// there is nothing to undo.
func (m *model) showBackups() (tea.Model, tea.Cmd) {
	bs, _ := update.ListBackups(m.inst.Dir)
	m.backups = bs
	if len(bs) == 0 {
		m.screen = scBackups
		return m, nil
	}
	running, err := m.isRunning(m.inst)
	m.runUnknown = err != nil
	if running {
		return m, errCmd(fmt.Errorf("%s is running right now. Close Minecraft, then try again.", m.inst.Name))
	}
	items := make([]list.Item, 0, len(bs))
	now := time.Now()
	for _, b := range bs {
		items = append(items, item{"Go back to GTNH " + b.Info.From, backupDesc(b, now), b.Dir})
	}
	return m.showList(scBackups, "Undo the last update of "+m.inst.Name+"?", items, bs[0].Dir, keyPick, keyOther)
}

// keyBackups handles keys on the backup list (or the "nothing to undo" screen).
func (m *model) keyBackups(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.backups) == 0 {
		switch k.String() {
		case "esc":
			return m.showHome()
		case "q":
			return m.quit()
		}
		return m, nil
	}
	if m.list.FilterState() == list.Filtering {
		return m.updateList(k) // typing into the filter: let the list have every key
	}
	switch k.String() {
	case "enter":
		sel, ok := m.list.SelectedItem().(item)
		if !ok {
			return m, nil
		}
		for _, b := range m.backups {
			if b.Dir == sel.key {
				m.backup, m.screen = b, scRestoreConfirm
				return m, nil
			}
		}
		return m, nil
	case "esc":
		if m.list.FilterState() == list.Unfiltered {
			return m.showHome()
		}
	case "q":
		return m.quit()
	}
	return m.updateList(k)
}

// keyRestoreConfirm handles keys on the screen that asks before restoring m.backup.
func (m *model) keyRestoreConfirm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter", "y":
		return m.startRestore()
	case "esc", "n", "q":
		return m.showBackups()
	}
	return m, nil
}

// keyRestored handles keys on the screen after a finished restore.
func (m *model) keyRestored(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter", "esc":
		return m.reloadHome()
	case "p":
		return m.play(false)
	case "q":
		return m.quit()
	}
	return m, nil
}

// startRestore enters the busy screen and puts m.backup back into m.inst in the
// background; it ends with restoredMsg or errMsg.
func (m *model) startRestore() (tea.Model, tea.Cmd) {
	m.startBusy(scRestoring)
	inst, b, restore, rep := m.inst, m.backup, m.restore, m.reporter()
	return m, func() tea.Msg {
		r, err := restore(inst, b, rep)
		return orErr(err, restoredMsg{r})
	}
}

// backupsView is the screen shown when m.inst has no backups to restore.
func (m *model) backupsView() (body, footer string) {
	body = titleSty.Render(wrap("Nothing to undo for "+m.inst.Name, m.width-4)) + "\n\n" +
		wrap("I only keep the files from the last update I did here, and there isn't one.", m.width-4)
	return body, hint("esc", "back")
}

// restoreConfirmView tells the player what restoring m.backup will do and asks first.
func (m *model) restoreConfirmView() (body, footer string) {
	info := m.backup.Info
	var b strings.Builder
	b.WriteString(titleSty.Render(wrap(fmt.Sprintf("Ready to put %s back on GTNH %s", m.inst.Name, info.From), m.width-4)) + "\n\n")
	if update.IsDowngrade(info.From, info.To) {
		b.WriteString(badSty.Render(wrap(fmt.Sprintf("! This goes BACK to an older version. Worlds you played on %s may lose "+
			"blocks and items or not load at all. Copy your saves folder somewhere safe first.", info.To), m.width-4)) + "\n\n")
	}
	bullet := func(s string) { b.WriteString(m.bullet(s)) }
	bullet("Files that update added are removed and the files it replaced are put back exactly as they were.")
	bullet("Your worlds, screenshots, maps and game settings stay exactly as they are.")
	bullet("If you changed settings since, they're kept (server address, server mods link, Java and memory).")
	if info.PrevName != "" || strings.Contains(m.inst.Name, info.To) {
		bullet("The instance name in Prism goes back too.")
	}
	b.WriteString(m.closeMinecraftNote())
	b.WriteString(m.headsUp("\n"))
	return b.String(), hint("enter", "undo now", "esc", "go back")
}

// restoredView tells the player what the restore did.
func (m *model) restoredView() (body, footer string) {
	r := m.restored
	var b strings.Builder
	b.WriteString(okSty.Bold(true).Render(wrap(fmt.Sprintf("All done! %s is back on GTNH %s.", m.inst.Name, r.To), m.width-4)) + "\n\n")
	b.WriteString(m.bullet(fmt.Sprintf("%s %s put back, %s removed.", num(int64(r.MovedBack)), plural(r.MovedBack, "file", "files"), num(int64(r.Removed)))))
	if r.Renamed != "" {
		b.WriteString(m.bullet("Renamed the instance in Prism to \"" + r.Renamed + "\"."))
	}
	if len(r.Skipped) > 0 {
		text := m.bullet("I couldn't put these back myself (they live outside the instance folder): " +
			strings.Join(r.Skipped, ", ") + ". They're still in " + m.backup.Dir + ".")
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		for i, l := range lines {
			lines[i] = warnSty.Render(l) // per line, so the bullet and wrapped lines keep the colour
		}
		b.WriteString(strings.Join(lines, "\n") + "\n")
	}
	b.WriteString(m.headsUp(""))
	b.WriteString(haveFun)
	return b.String(), doneFooter()
}

// restoreErrorNote is what the error screen adds after a failed restore: what state the
// instance is in and what to do next.
func (m *model) restoreErrorNote() string {
	if errors.Is(m.err, update.ErrGameRunning) {
		return okSty.Render(wrap("Nothing was changed.", m.width-4)) + "\n\n"
	}
	return badSty.Render(wrap("Some files may have changed. The backup folder is still there, so you can try again: "+m.backup.Dir, m.width-4)) + "\n\n"
}

// backupDesc is the line under a backup on the list: which update it undoes and when
// it happened, relative to now.
func backupDesc(b update.Backup, now time.Time) string {
	s := "updated to " + b.Info.To
	if a := ago(b.Info.When, now); a != "" {
		s += " " + a
	}
	n := len(b.Info.Added) + len(b.Info.AddedMods)
	return s + fmt.Sprintf(" · %d added %s removed, replaced files put back", n, plural(n, "file", "files"))
}
