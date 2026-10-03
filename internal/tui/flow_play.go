package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// slowStart is how long the game may take to show up before the player is told to look
// at Prism.
const slowStart = 90 * time.Second

// dataDirOf is the Prism data dir whose instances folder holds inst, else the first one.
func (m *model) dataDirOf(inst prism.Instance) string {
	for _, d := range m.cfg.PrismDirs {
		rel, err := filepath.Rel(prism.InstancesDir(d), inst.Dir)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "." {
			return d
		}
	}
	if len(m.cfg.PrismDirs) == 0 {
		return ""
	}
	return m.cfg.PrismDirs[0]
}

// play starts m.inst through Prism Launcher, joining its saved server when join is set.
func (m *model) play(join bool) (tea.Model, tea.Cmd) {
	dataDir := m.dataDirOf(m.inst)
	server := ""
	if join {
		if st, _ := update.LoadState(m.inst.Dir); st != nil {
			server = st.ServerAddress
		}
	}
	l, err := m.findLauncher(dataDir, m.app.PrismExe)
	if errors.Is(err, prism.ErrLauncherNotFound) {
		return m.launchFailed(errors.New("I couldn't find Prism Launcher on this computer. Start the game from Prism yourself, or tell me where Prism is in the settings."))
	}
	if err != nil {
		return m.launchFailed(fmt.Errorf("I couldn't start Prism Launcher: %w", err))
	}
	m.screen = scLaunching
	launch, inst := m.launch, m.inst
	return m, func() tea.Msg {
		if err := launch(l, dataDir, inst, server); err != nil {
			return errMsg{fmt.Errorf("I couldn't start Prism Launcher: %w", err)}
		}
		return launchedMsg{}
	}
}

func (m *model) launchFailed(err error) (tea.Model, tea.Cmd) {
	m.err, m.errPhase, m.screen = err, scLaunching, scError
	return m, nil
}

func (m *model) onLaunched() (tea.Model, tea.Cmd) {
	if !m.app.StaysOpen() {
		return m.quit()
	}
	m.screen = scPlaying
	m.playGen++
	m.playState, m.playStart, m.runningSince = "starting", time.Now(), time.Time{}
	return m, m.pollLater(m.playGen)
}

func (m *model) pollLater(gen int) tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return pollTick{gen} })
}

// polling reports whether a poll of generation gen is still wanted.
func (m *model) polling(gen int) bool { return gen == m.playGen && m.screen == scPlaying }

func (m *model) onPollTick(msg pollTick) (tea.Model, tea.Cmd) {
	if !m.polling(msg.gen) {
		return m, nil
	}
	inst, isRunning := m.inst, m.isRunning
	return m, func() tea.Msg {
		r, err := isRunning(inst)
		return pollMsg{msg.gen, r, err}
	}
}

func (m *model) onPoll(msg pollMsg) (tea.Model, tea.Cmd) {
	if !m.polling(msg.gen) {
		return m, nil
	}
	switch {
	case msg.err != nil:
		m.playState = "unknown"
		return m, nil
	case msg.running:
		if m.playState != "running" {
			m.runningSince = time.Now()
		}
		m.playState = "running"
	case m.playState == "running":
		m.playState = "closed"
		return m, nil
	case time.Since(m.playStart) > slowStart:
		m.playState = "slow"
	}
	return m, m.pollLater(msg.gen)
}

// reloadHome drops the finished run and lists the instances again (the manifest is kept).
func (m *model) reloadHome() (tea.Model, tea.Cmd) {
	if m.session != nil {
		m.session.Close()
		m.session = nil
	}
	m.result, m.created, m.creating, m.warns = nil, nil, false, nil
	m.screen = scLoading
	dirs := m.cfg.PrismDirs
	return m, func() tea.Msg { return reloadedMsg{listInstances(dirs)} }
}

func (m *model) onReloaded(msg reloadedMsg) (tea.Model, tea.Cmd) {
	m.insts = msg.insts
	return m.showHome()
}

func (m *model) keyPlaying(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter", "esc":
		return m.reloadHome()
	case "q":
		return m.quit()
	}
	return m, nil
}
