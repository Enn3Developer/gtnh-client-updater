package tui

import (
	"errors"
	"fmt"
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

func (m *model) startSelfUpdate() (tea.Model, tea.Cmd) {
	m.startBusy(scSelfUpdate)
	m.step, m.stepStart, m.done, m.total = "Downloading gtnh-update "+m.newer.Version, time.Now(), 0, 0
	rel, rep, client := m.newer, m.reporter(), m.cfg.Client
	return m, func() tea.Msg {
		if err := rel.Apply(client, rep.Progress); err != nil {
			if errors.Is(err, selfupdate.ErrNotWritable) {
				err = fmt.Errorf("I can't replace myself in this folder. Download the new version yourself from\n%s\n\n(%w)", rel.Page, err)
			}
			return errMsg{err}
		}
		return selfDoneMsg{}
	}
}
