package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Messages from background work.
type (
	loadedMsg struct {
		m     *manifest.Manifest
		insts []prism.Instance
	}
	stepMsg     string
	progressMsg struct{ done, total int64 }
	warnMsg     string
	preparedMsg struct{ s *update.Session }
	createReady struct{ c *update.Creation }
	createdMsg  struct{ r *update.CreateResult }
	appliedMsg  struct{ r *update.Result }
	errMsg      struct{ err error }
	newerMsg    struct{ r *selfupdate.Release }
	selfDoneMsg struct{}
	launchedMsg struct{}
	pollTick    struct{ gen int }
	pollMsg     struct {
		gen     int
		running bool
		err     error
	}
	// launchFailedMsg: Prism Launcher couldn't be started for the game.
	launchFailedMsg struct{ err error }
	reloadedMsg     struct{ insts []prism.Instance }
	restoredMsg     struct{ r *update.RestoreResult }
	// modsPreparedMsg: the server's mods of the job's instance are fetched and the sync
	// planned; modsSyncedMsg: the sync is done (fetchErr: why the server's archive
	// couldn't be fetched, so the copy of the last one or nothing was used).
	modsPreparedMsg struct{ s *update.ModsSync }
	modsSyncedMsg   struct {
		p        *update.ModsPlan
		fetchErr error
	}
)

func errCmd(err error) tea.Cmd { return func() tea.Msg { return errMsg{err} } }

// orErr is the message a background command sends: errMsg if it failed, msg otherwise.
func orErr(err error, msg tea.Msg) tea.Msg {
	if err != nil {
		return errMsg{err}
	}
	return msg
}
