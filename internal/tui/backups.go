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
	if md, cmd, ok := m.listKey(k, m.showHome); ok {
		return md, cmd
	}
	if k.String() == "enter" {
		dir, ok := m.selectedKey()
		if !ok {
			return m, nil
		}
		for _, b := range m.backups {
			if b.Dir == dir {
				m.backup, m.screen = b, scRestoreConfirm
				return m, nil
			}
		}
		return m, nil
	}
	return m.updateList(k)
}

// keyRestoreConfirm handles keys on the screen that asks before restoring m.backup.
func (m *model) keyRestoreConfirm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y":
		return m.startRestore()
	case "esc", "n":
		return m.showBackups()
	case "q":
		return m.quit()
	}
	return m, nil
}

// keyRestored handles keys on the screen after a finished restore.
func (m *model) keyRestored(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
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
	b := m.blocks()
	b.title("Nothing to undo for " + m.inst.Name)
	b.para("I only keep the files from the last update I did here, and there isn't one.")
	return b.String(), m.buttonFooter("esc", "back")
}

// restoreConfirmView tells the player what restoring m.backup will do and asks first.
func (m *model) restoreConfirmView() (body, footer string) {
	info := m.backup.Info
	b := m.blocks()
	b.title(fmt.Sprintf("Ready to put %s back on GTNH %s", m.inst.Name, info.From))
	if update.IsDowngrade(info.From, info.To) {
		b.danger(downgradeWarning(info.To))
	}
	b.bullet("Files that update added are removed and the files it replaced are put back exactly as they were.")
	b.bullet(worldsStayBullet)
	b.bullet("If you changed settings since, they're kept (server address, server mods link, Java and memory).")
	if info.To != info.From && strings.Contains(m.inst.Name, info.To) {
		b.bullet("The instance name in Prism goes back too.")
	}
	b.closeMinecraftNote()
	b.headsUp()
	return b.String(), m.buttonFooter("enter", "undo now", "esc", "back")
}

// restoredView tells the player what the restore did.
func (m *model) restoredView() (body, footer string) {
	r := m.restored
	b := m.blocks()
	b.success(fmt.Sprintf("All done! %s is back on GTNH %s.", m.inst.Name, r.To))
	b.bullet(fmt.Sprintf("%s %s put back, %s removed.", num(int64(r.MovedBack)), plural(r.MovedBack, "file", "files"), num(int64(r.Removed))))
	if r.Renamed != "" {
		b.bullet(renamedBullet(r.Renamed))
	}
	if len(r.Skipped) > 0 {
		b.warnBullet("I couldn't put these back myself (they live outside the instance folder): " +
			strings.Join(r.Skipped, ", ") + ". They're still in " + m.backup.Dir + ".")
	}
	b.headsUp()
	return b.String(), m.doneFooter()
}

// restoreErrorNote is what the error screen adds after a failed restore: what state the
// instance is in and what to do next.
func (m *model) restoreErrorNote() string {
	b := m.blocks()
	if errors.Is(m.err, update.ErrGameRunning) {
		b.note(okSty, nothingChangedNote)
	} else {
		b.note(badSty, "Some files may have changed. The backup folder is still there, so you can try again: "+m.backup.Dir)
	}
	return b.String()
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
