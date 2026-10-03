package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
)

func (m *model) checkSelf() tea.Msg {
	client := *m.cfg.Client
	client.Timeout = 10 * time.Second
	r, err := selfupdate.Latest(&client)
	if err != nil || !selfupdate.Newer(m.cfg.AppVersion, r.Version) {
		return nil // never bother the player about a failed check
	}
	return newerMsg{r}
}

// selfUpdate asks to replace the launcher with m.newer (C12), then downloads it behind
// a progress dialog.
func (m *model) selfUpdate() tea.Cmd {
	if m.newer == nil {
		return nil
	}
	if m.job != nil {
		m.notifyBusy()
		return nil
	}
	v := m.newer.Version
	m.confirmDialog("Update the launcher to "+v+"?", []string{
		"I'll download GTNH Launcher " + v + ", replace myself and restart.",
		"Your instances aren't touched.",
	}, nil, nil, "Update", func(m *model) tea.Cmd {
		m.job = &job{kind: jobSelf, title: "Downloading GTNH Launcher " + v, phase: "apply"}
		m.closeDialog()
		m.progressDialog("Updating the launcher")
		return m.startSelfUpdate()
	}, closeOnlyCmd)
	return nil
}

// onSelfDone ends the self-update job and offers a restart (C13).
func (m *model) onSelfDone() tea.Cmd {
	v := m.newer.Version
	m.job, m.newer = nil, nil
	m.openDialog(&dialog{
		title: "The launcher is now version " + v + ".", body: func(int) string { return "" },
		buttons: []string{"Restart now", "Later"},
		onButton: func(m *model, label string) tea.Cmd {
			if label == "Restart now" {
				m.restart = true
				_, cmd := m.quit()
				return cmd
			}
			m.closeDialog()
			return nil
		},
	})
	return nil
}

// progressDialog opens a buttonless dialog showing the running job's progress, read
// from the model on every View; it takes and ignores every key (C12).
func (m *model) progressDialog(title string) {
	m.openDialog(&dialog{
		title: title, width: 60,
		onButton: func(*model, string) tea.Cmd { return nil },
		body: func(w int) string {
			if m.total > 0 {
				return strings.Join(m.progressLines(w, w-8), "\n") + "\n" + m.stepText()
			}
			return m.stepText()
		},
	})
}

// startSelfUpdate downloads m.newer over the running binary.
func (m *model) startSelfUpdate() tea.Cmd {
	m.startBusy()
	m.step, m.stepStart, m.done, m.total = "Downloading GTNH Launcher "+m.newer.Version, m.now(), 0, 0
	rel, rep, client := m.newer, m.reporter(), m.cfg.Client
	return func() tea.Msg {
		if err := rel.Apply(client, rep.Progress); err != nil {
			if errors.Is(err, selfupdate.ErrNotWritable) {
				err = fmt.Errorf("I can't replace myself in this folder. Download the new version yourself from\n%s\n\n(%w)", rel.Page, err)
			}
			return errMsg{err}
		}
		return selfDoneMsg{}
	}
}
