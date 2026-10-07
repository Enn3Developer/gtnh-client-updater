package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// startUndo offers to restore the current instance's newest backup (C1): refuses while
// another job or the game runs.
func (m *model) startUndo() tea.Cmd {
	if m.job != nil {
		m.notifyBusy()
		return nil
	}
	inst, ok := m.current()
	if !ok {
		return nil
	}
	b := m.home[inst.Dir].backup
	if b == nil {
		return nil
	}
	running, err := m.isRunning(inst)
	if err == nil && running {
		m.notify("The game is running", inst.Name+" is running right now. Close Minecraft, then try again.")
		return nil
	}
	m.runUnknown = err != nil
	m.confirmUndo(inst, *b)
	return nil
}

// confirmUndo asks before going back to the version b was taken on (C2).
func (m *model) confirmUndo(inst prism.Instance, b update.Backup) {
	from, to := b.Info.From, b.Info.To
	lines := []string{
		"Files the update added are removed and the files it replaced are put back exactly as they were",
		"Worlds, maps and settings aren't touched",
		"Settings you changed since then are kept (server, mods link, memory, Java)",
	}
	if info := m.home[inst.Dir]; info.modsURL != "" || info.mods > 0 {
		lines = append(lines, "Your server's mods stay as they are: they follow the server, not the GTNH version")
	}
	// mirrors the engine's rename condition (prism.RenameVersion)
	if to != from && strings.Contains(inst.Name, to) {
		lines = append(lines, "The instance name in Prism goes back too")
	}
	var warn, bad []string
	if m.runUnknown {
		warn = append(warn, "Make sure Minecraft is closed before you continue.")
	}
	if update.IsDowngrade(from, to) {
		bad = append(bad, downgradeWarning(to))
	}
	m.confirmDialog("Go back to GTNH "+from+"?", lines, warn, bad, "Undo", func(m *model) tea.Cmd {
		m.job = &job{kind: jobRestore, dir: inst.Dir, title: "Going back to GTNH " + from, phase: "apply", backup: b.Dir}
		delete(m.notices, inst.Dir)
		m.startBusy()
		m.closeDialog()
		restore, rep := m.restore, m.reporter()
		return func() tea.Msg {
			r, err := restore(inst, b, rep)
			return orErr(err, restoredMsg{r})
		}
	}, closeOnlyCmd)
}

// closeOnlyCmd is the cancel handler of confirmations that just close.
func closeOnlyCmd(m *model) tea.Cmd {
	m.closeDialog()
	return nil
}

// onRestored ends the restore job, keeps its notice for the instance and reloads (C3).
func (m *model) onRestored(r *update.RestoreResult) tea.Cmd {
	dir, backup := m.job.dir, m.job.backup
	m.job = nil
	n := notice{text: "Back on GTNH " + r.To + " just now · " + num(int64(r.MovedBack)) + " " +
		plural(r.MovedBack, "file", "files") + " put back, " + num(int64(r.Removed)) + " removed"}
	if r.Renamed != "" {
		n.text += ` · renamed to "` + r.Renamed + `"`
	}
	if len(r.Skipped) > 0 {
		n.warn = []string{"I couldn't put these back myself — they live outside the instance folder: " +
			strings.Join(r.Skipped, ", ") + ". They're still in " + backup + "."}
	}
	m.notices[dir] = n
	return m.reload()
}
