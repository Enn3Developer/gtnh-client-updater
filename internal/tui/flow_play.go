package tui

import (
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// slowStart is how long the game may take to show up before the player is told to look
// at Prism.
const slowStart = 90 * time.Second

// launchFailedTitle heads every dialog about a game that couldn't be started.
const launchFailedTitle = "I couldn't start the game"

// playCmd starts the current instance through Prism Launcher, joining its saved server
// when join is set. A launcher that can't be found or started is told in a dialog
// titled "I couldn't start the game", and nil is returned. While the current
// instance's game is starting or running it does nothing.
func (m *model) playCmd(join bool) tea.Cmd {
	inst, ok := m.current()
	if !ok || m.gameUnderway(inst.Dir) {
		return nil
	}
	if p, ok := m.pending(); ok && p.Dir == inst.Dir {
		return nil
	}
	dataDir := prism.DataDirOf(m.cfg.PrismDirs, inst)
	server := ""
	if join {
		server = m.home[inst.Dir].server
	}
	l, err := m.findLauncher(dataDir, m.app.PrismExe)
	if errors.Is(err, prism.ErrLauncherNotFound) {
		m.notify(launchFailedTitle, "I couldn't find Prism Launcher on this computer. Start the game from Prism yourself, or tell me where Prism is in the settings.")
		return nil
	}
	if err != nil {
		m.notify(launchFailedTitle, "I couldn't start Prism Launcher: "+err.Error())
		return nil
	}
	m.launchDir = inst.Dir
	launch := m.launch
	return func() tea.Msg {
		if err := launch(l, dataDir, inst, server); err != nil {
			return launchFailedMsg{err}
		}
		return launchedMsg{}
	}
}

// gameUnderway reports whether the game of the instance in dir is starting or running.
func (m *model) gameUnderway(dir string) bool {
	return m.play.dir == dir && (m.play.state == "starting" || m.play.state == "running")
}

// gameShown reports whether the play row of the instance in dir shows the watched game.
func (m *model) gameShown(dir string) bool {
	return m.play.dir == dir && m.play.state != ""
}

// dismissPlay puts the current instance's play row back to "▶ Play" once the watch
// has ended (closed, slow or unknown).
func (m *model) dismissPlay() {
	in, ok := m.current()
	if ok && m.gameShown(in.Dir) && !m.gameUnderway(in.Dir) {
		m.play.state = ""
	}
}

// onLaunched quits when the launcher shouldn't stay open, else starts watching the game
// in m.play.
func (m *model) onLaunched() tea.Cmd {
	if !m.app.StaysOpen() {
		_, cmd := m.quit()
		return cmd
	}
	dir := m.launchDir
	if in, ok := m.current(); dir == "" && ok {
		dir = in.Dir
	}
	m.play = playMonitor{gen: m.play.gen + 1, dir: dir, state: "starting", start: time.Now()}
	return m.pollLater(m.play.gen)
}

func (m *model) pollLater(gen int) tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return pollTick{gen} })
}

// polling reports whether a poll of generation gen is still wanted.
func (m *model) polling(gen int) bool {
	return gen == m.play.gen && m.play.watching()
}

// watching reports whether the game is still being watched: starting, slow or running.
func (p playMonitor) watching() bool {
	return p.state == "starting" || p.state == "slow" || p.state == "running"
}

func (m *model) onPollTick(msg pollTick) tea.Cmd {
	if !m.polling(msg.gen) {
		return nil
	}
	inst := prism.Instance{Dir: m.play.dir}
	for _, in := range m.insts {
		if in.Dir == m.play.dir {
			inst = in
		}
	}
	isRunning := m.isRunning
	return func() tea.Msg {
		r, err := isRunning(inst)
		return pollMsg{msg.gen, r, err}
	}
}

// onPoll moves m.play.state on: an error means unknown (stop), seen means running, gone
// after running means closed (stop), not seen after slowStart means slow (keep polling).
func (m *model) onPoll(msg pollMsg) tea.Cmd {
	if !m.polling(msg.gen) {
		return nil
	}
	switch {
	case msg.err != nil:
		m.play.state = "unknown"
		return nil
	case msg.running:
		if m.play.state != "running" {
			m.play.since = time.Now()
		}
		m.play.state = "running"
	case m.play.state == "running":
		m.play.state = "closed"
		return nil
	case time.Since(m.play.start) > slowStart:
		m.play.state = "slow"
	}
	return m.pollLater(msg.gen)
}
