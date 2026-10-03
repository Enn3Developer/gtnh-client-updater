// Package tui is the interactive bubbletea front end: pick an instance, pick a version,
// review what will happen, apply. It talks to players, not developers: plain sentences,
// no jargon, and always a clear next key.
package tui

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Enn3Developer/gtnh-client-updater/internal/appcfg"
	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Config is what the command line pre-selects. Empty fields are asked interactively.
type Config struct {
	Client     *http.Client
	AppVersion string
	PrismDirs  []string // Prism data dirs to scan
	Instance   string   // path or display name
	Installed  string   // override version detection
	Target     string   // version to install
	// ServerMods: "" = use what the instance remembers (or ask), "none" = off, or a URL.
	ServerMods  string
	UpdateCheck bool   // look for a newer gtnh-update on GitHub
	Create      bool   // start by creating a new instance instead of updating one
	Name        string // preset name for a new instance
	Play        bool   // start the chosen instance right away
	// Configs preselects what happens to config files changed both by the player and by
	// the new version: "new", "mine", or "" = update.Recommended.
	Configs string
}

// Outcome tells the caller what to do after the TUI exits.
type Outcome struct {
	Restart bool // gtnh-update replaced itself; start the new binary
}

// Run starts the TUI and blocks until the player quits.
func Run(cfg Config) (Outcome, error) {
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	m.send = p.Send
	_, err := p.Run()
	return Outcome{Restart: m.restart}, err
}

type screen int

const (
	scLoading screen = iota
	scHome
	scLaunching
	scPlaying
	scInstalled
	scTarget
	scServerMods
	scName // name of a new instance
	scPreparing
	scConflicts // what to do with config files the player and the new version both changed
	scResolve   // the same, decided file by file
	scConfirm
	scApplying
	scDone
	scError
	scSelfUpdate
	scSelfUpdated
	scSettings       // per-instance and launcher-wide settings list
	scSettingEdit    // text field for one setting
	scBackups        // backups of the instance that can be restored
	scRestoreConfirm // what restoring the chosen backup will do
	scRestoring
	scRestored
)

type model struct {
	cfg  Config
	send func(tea.Msg)

	screen  screen
	scroll  int // body scroll of the current non-list screen; reset on screen change
	btn     int // selected button of the current screen; reset on screen change
	width   int
	height  int
	spin    spinner.Model
	bar     progress.Model
	list    list.Model
	hasList bool
	// listKeys are the help bindings of the list on screen, for its key bar.
	listKeys  []key.Binding
	listTitle string // unwrapped title of the list on screen; re-wrapped on resize
	input     textinput.Model
	inputEr   string

	manifest *manifest.Manifest
	insts    []prism.Instance
	showAll  bool
	inst     prism.Instance
	detect   update.Detection
	target   string

	serverMods      string // URL, "" = none
	serverModsAsked bool
	modsThenPrepare bool // server-mods screen continues into the update (vs. back to the list)

	steps     []string // finished steps of the current busy screen
	step      string
	stepStart time.Time
	done      int64
	total     int64
	warns     []string
	session   *update.Session
	result    *update.Result
	err       error
	errPhase  screen

	// Create mode: making a new instance instead of updating m.inst.
	creating bool
	nameIn   textinput.Model
	nameEr   string
	nameUsed bool // cfg.Name was offered already
	newName  string
	creation *update.Creation
	created  *update.CreateResult
	// cancelCreate stops the running PrepareCreate; nil when none runs.
	cancelCreate context.CancelFunc
	// quitAfterCancel: the player quit during a create download; quit once PrepareCreate
	// has returned (it removes the half-made folder before it does).
	quitAfterCancel bool

	newer    *selfupdate.Release
	restart  bool
	quitting bool

	app          appcfg.Config
	saveApp      func(appcfg.Config) error
	setting      string          // key of the settings row being edited or last chosen
	setIn        textinput.Model // value of the setting being edited
	setEr        string          // why the typed setting was rejected
	savedRow     string          // key of the row whose value shows "✓ saved"; "" = none
	findLauncher func(dataDir, override string) (prism.Launcher, error)
	launch       func(l prism.Launcher, dataDir string, inst prism.Instance, server string) error
	isRunning    func(inst prism.Instance) (bool, error)
	runUnknown   bool                // pickInstance couldn't tell whether the game runs
	home         map[string]homeInfo // card data per instance Dir, filled by showHome
	playGen      int
	playState    string // "starting" | "slow" | "running" | "closed" | "unknown"
	playStart    time.Time
	runningSince time.Time

	backups  []update.Backup       // restorable backups of m.inst, newest first
	backup   update.Backup         // the backup being restored
	restored *update.RestoreResult // what the last restore did
	restore  func(prism.Instance, update.Backup, update.Reporter) (*update.RestoreResult, error)
}

func newModel(cfg Config) *model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	ti := textinput.New()
	ti.Placeholder = "https://…/custom_mods.zip"
	ti.CharLimit = 500
	ni := textinput.New()
	ni.CharLimit = 100
	si := textinput.New()
	si.CharLimit = 500
	return &model{
		cfg: cfg, spin: sp, input: ti, nameIn: ni, setIn: si, width: 80, height: 24,
		bar:          progress.New(progress.WithGradient("#7FB4CA", "#98BB6C")),
		findLauncher: prism.FindLauncher, launch: prism.Launch, isRunning: prism.IsRunning,
		saveApp: appcfg.Save, restore: update.Restore,
	}
}

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
	reloadedMsg struct{ insts []prism.Instance }
	restoredMsg struct{ r *update.RestoreResult }
)

func errCmd(err error) tea.Cmd { return func() tea.Msg { return errMsg{err} } }

// orErr is the message a background command sends: errMsg if it failed, msg otherwise.
func orErr(err error, msg tea.Msg) tea.Msg {
	if err != nil {
		return errMsg{err}
	}
	return msg
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick, m.load}
	if m.cfg.UpdateCheck {
		cmds = append(cmds, m.checkSelf)
	}
	if c, err := appcfg.Load(); err == nil {
		m.app = c
	}
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prev := m.screen
	md, cmd := m.update(msg)
	if m.screen != prev {
		m.scroll, m.btn = 0, 0
	}
	return md, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.onResize(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case loadedMsg:
		m.manifest, m.insts = msg.m, msg.insts
		return m.afterLoad()
	case newerMsg:
		return m.onNewer(msg)
	case stepMsg:
		return m.onStep(msg)
	case progressMsg:
		m.done, m.total = msg.done, msg.total
		return m, nil
	case warnMsg:
		m.warns = append(m.warns, string(msg))
		return m, nil
	case preparedMsg:
		return m.onPrepared(msg)
	case appliedMsg:
		m.result, m.screen = msg.r, scDone
		return m, nil
	case createReady:
		return m.onCreateReady(msg)
	case createdMsg:
		m.created, m.screen = msg.r, scDone
		return m, nil
	case restoredMsg:
		m.restored, m.screen = msg.r, scRestored
		return m, nil
	case selfDoneMsg:
		m.screen = scSelfUpdated
		return m, nil
	case launchedMsg:
		return m.onLaunched()
	case pollTick:
		return m.onPollTick(msg)
	case pollMsg:
		return m.onPoll(msg)
	case reloadedMsg:
		return m.onReloaded(msg)
	case errMsg:
		return m.onError(msg)
	case tea.KeyMsg:
		return m.key(msg)
	}
	if m.isListScreen() {
		return m.updateList(msg)
	}
	return m, nil
}

func (m *model) onResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = msg.Width, msg.Height
	m.bar.Width = max(min(msg.Width-8, 64), 10)
	m.input.Width = m.inputWidth()
	m.nameIn.Width = m.inputWidth()
	m.setIn.Width = m.inputWidth()
	m.resizeList()
	return m, nil
}

func (m *model) onNewer(msg newerMsg) (tea.Model, tea.Cmd) {
	m.newer = msg.r
	m.resizeList() // make room for the banner
	return m, nil
}

func (m *model) onStep(msg stepMsg) (tea.Model, tea.Cmd) {
	if m.step != "" {
		m.steps = append(m.steps, m.step)
	}
	m.step, m.stepStart, m.done, m.total = string(msg), time.Now(), 0, 0
	return m, nil
}

func (m *model) onPrepared(msg preparedMsg) (tea.Model, tea.Cmd) {
	m.session, m.screen = msg.s, scConfirm
	if msg.s.Plan.Count(update.Conflict) > 0 {
		msg.s.Plan.ChooseAll(m.configsChoice()) // preselected on the next screen
		return m.showConflicts()
	}
	return m, nil
}

func (m *model) onCreateReady(msg createReady) (tea.Model, tea.Cmd) {
	m.creation, m.screen = msg.c, scConfirm
	if m.createDone() {
		return m.quit() // finished just before the cancel landed; quit closes it
	}
	return m, nil
}

func (m *model) onError(msg errMsg) (tea.Model, tea.Cmd) {
	var leftover *update.LeftoverError
	if m.createDone() && !errors.As(msg.err, &leftover) {
		return m.quit() // the cancelled download cleaned up after itself
	}
	m.fail(msg.err, m.screen)
	return m, nil
}

// fail shows err on the error screen; phase is the screen it happened on.
func (m *model) fail(err error, phase screen) {
	m.err, m.errPhase, m.screen = err, phase, scError
}

func (m *model) isListScreen() bool {
	return m.screen == scHome || m.screen == scInstalled || m.screen == scTarget || m.screen == scSettings ||
		(m.screen == scBackups && len(m.backups) > 0) || m.choosingConfigs()
}

// choosingConfigs reports whether a config-choice list is on screen; those belong to a
// prepared session, so the self-update offer is hidden there.
func (m *model) choosingConfigs() bool {
	return m.screen == scConflicts || m.screen == scResolve
}

func (m *model) quit() (tea.Model, tea.Cmd) {
	if m.screen == scApplying || m.screen == scRestoring || m.screen == scSelfUpdate {
		return m, nil // never abandon a half-applied update; rollback needs this process
	}
	if m.cancelCreate != nil {
		// The download goroutine removes the new folder when it sees the cancel; quitting
		// now would kill it first. Its errMsg (or createReady) quits.
		m.cancelCreate()
		m.quitAfterCancel = true
		return m, nil
	}
	if m.session != nil {
		m.session.Close()
	}
	if err := m.closeCreation(); err != nil {
		return m.showLeftover(err)
	}
	m.quitting = true
	return m, tea.Quit
}

// startBusy enters a busy screen with a fresh step log.
func (m *model) startBusy(sc screen) {
	m.screen, m.warns, m.steps, m.step = sc, nil, nil, ""
}
