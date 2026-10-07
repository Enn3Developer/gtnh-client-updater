// Package tui is the interactive bubbletea front end: one persistent workspace with the
// instances in a sidebar, the selected instance's page of rows (play, update, settings,
// launcher), a status bar of keys and dialogs drawn over it. It talks to players, not
// developers: plain sentences, no jargon, and always a clear next key.
package tui

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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

// focus is the part of the workspace the arrow keys move in.
type focus int

const (
	focusSidebar focus = iota
	focusPage
)

// playMonitor watches the game started from the launcher.
type playMonitor struct {
	gen   int    // generation of the current watch; stale polls carry an older one
	dir   string // Dir of the launched instance
	state string // "" none | "starting" | "slow" | "running" | "closed" | "unknown"
	start time.Time
	since time.Time // when the game was first seen running
}

// homeInfo is what the page shows about one instance, read once by refresh so the view
// never does I/O.
type homeInfo struct {
	gtnh                                  bool
	version, rec, played, server, modsURL string // played = ago(LastLaunch, now); "" = never
	flavor                                manifest.Flavor
	backup                                *update.Backup // newest restorable backup; nil = nothing to undo
	settings                              prism.Settings
	settingsErr                           error
	mods                                  int       // server mods the sync manages
	synced                                time.Time // when they were last synced with the server; zero = never
	modsAsked                             bool      // the player answered whether the server has extra mods
}

type model struct {
	cfg  Config
	send func(tea.Msg)

	width, height int
	spin          spinner.Model

	manifest   *manifest.Manifest
	insts      []prism.Instance
	showAll    bool
	sel        int // index into visible()
	focus      focus
	row        int // index into rows()
	pageScroll int
	newer      *selfupdate.Release
	app        appcfg.Config
	dialog     *dialog
	play       playMonitor
	launchDir  string // Dir of the instance whose launch is under way
	loaded     bool
	loadErr    error
	quitting   bool
	restart    bool
	home       map[string]homeInfo // per instance Dir, filled by refresh
	edit       *settingEdit        // nil = not editing
	savedRow   string              // id of the row to mark "✓ saved"; "" none

	findLauncher func(dataDir, override string) (prism.Launcher, error)
	launch       func(l prism.Launcher, dataDir string, inst prism.Instance, server string) error
	isRunning    func(inst prism.Instance) (bool, error)
	saveApp      func(appcfg.Config) error
	restore      func(prism.Instance, update.Backup, update.Reporter) (*update.RestoreResult, error)
	prepareMods  func(update.ModsSyncOptions, update.Reporter) (*update.ModsSync, error)
	applyMods    func(*update.ModsSync, update.Reporter) (*update.ModsPlan, error)
	now          func() time.Time

	// job state
	steps           []string // finished steps of the current job
	step            string
	stepStart       time.Time
	done, total     int64
	warns           []string
	session         *update.Session
	creation        *update.Creation
	cancelCreate    context.CancelFunc // stops the running PrepareCreate; nil when none runs
	quitAfterCancel bool               // quit once the cancelled PrepareCreate has returned
	target          string
	serverMods      string                 // the server-mods link of the update or creation, "" = none
	serverModsGiven bool                   // serverMods came from the command line: it's the player's answer
	holdOpen        bool                   // the launch came with a warning: stay open even if set to quit
	modsOverride    string                 // the server-mods link the next sync uses and saves ("none" = none); "" = the instance's
	afterMods       func(m *model) tea.Cmd // runs when the jobMods is done, whatever its outcome
	newName         string
	detect          update.Detection
	nameUsed        bool              // cfg.Name was offered already
	targetUsed      bool              // cfg.Target was consumed by a creation
	job             *job              // the running job shown inline; nil when none runs
	notices         map[string]notice // outcome of the last job per instance Dir
	runUnknown      bool              // the last check couldn't tell whether the game runs
}

func newModel(cfg Config) *model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	return &model{
		cfg: cfg, spin: sp, width: 80, height: 24,
		findLauncher: prism.FindLauncher, launch: prism.Launch, isRunning: prism.IsRunning,
		saveApp: appcfg.Save, restore: update.Restore, now: time.Now,
		prepareMods: update.PrepareModsSync, applyMods: (*update.ModsSync).Apply,
		notices: map[string]notice{},
	}
}

// Init sets the window title, starts the spinner, the load and (when asked) the
// self-update check, and loads the launcher settings into m.app.
func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.SetWindowTitle("GTNH Launcher"), m.spin.Tick, m.load}
	if m.cfg.UpdateCheck {
		cmds = append(cmds, m.checkSelf)
	}
	if c, err := appcfg.Load(); err == nil {
		m.app = c
	}
	return tea.Batch(cmds...)
}

// Update routes messages: background results, window size, keys (a dialog takes every
// key while open, C8; otherwise C5). An errMsg ends the running job; otherwise before
// load it sets loadErr, after load it becomes notify("Something went wrong", err).
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case loadedMsg:
		m.manifest, m.insts = msg.m, msg.insts
		return m, m.afterLoad()
	case newerMsg:
		m.newer = msg.r
	case stepMsg:
		m.onStep(msg)
	case progressMsg:
		m.done, m.total = msg.done, msg.total
	case warnMsg:
		m.warns = append(m.warns, string(msg))
	case launchedMsg:
		return m, m.onLaunched()
	case launchFailedMsg:
		m.notify(launchFailedTitle, "I couldn't start Prism Launcher: "+msg.err.Error())
	case pollTick:
		return m, m.onPollTick(msg)
	case pollMsg:
		return m, m.onPoll(msg)
	case reloadedMsg:
		m.onReloaded(msg)
	case preparedMsg:
		if m.job != nil {
			return m, m.onPrepared(msg.s)
		}
	case appliedMsg:
		if m.job != nil && m.session != nil {
			return m, m.onApplied(msg.r)
		}
	case createReady:
		if m.job != nil {
			return m, m.onCreateReady(msg.c)
		}
	case createdMsg:
		if m.job != nil {
			return m, m.onCreated(msg.r)
		}
	case restoredMsg:
		if m.job != nil {
			return m, m.onRestored(msg.r)
		}
	case modsPreparedMsg:
		return m, m.onModsPrepared(msg.s)
	case modsSyncedMsg:
		if m.job != nil {
			return m, m.onModsSynced(msg)
		}
	case selfDoneMsg:
		if m.job != nil {
			return m, m.onSelfDone()
		}
	case errMsg:
		if m.job != nil {
			return m, m.onJobErr(msg.err)
		}
		if !m.loaded {
			m.loadErr = msg.err
			return m, nil
		}
		m.notify("Something went wrong", msg.err.Error())
	case tea.KeyMsg:
		if cmd, ok := m.keyEdit(msg); ok {
			return m, cmd
		}
		if cmd, ok := m.keyDialog(msg); ok {
			return m, cmd
		}
		return m.key(msg)
	}
	return m, nil
}

// View is the title bar, a blank line, the workspace (sidebar | page) and the status
// bar: exactly height lines, none wider than width, with the dialog overlaid (C1, C8).
func (m *model) View() string {
	right := ""
	if m.newer != nil {
		right = "v  launcher " + m.newer.Version + " is out"
	}
	out := []string{titleBar("GTNH Launcher "+m.cfg.AppVersion, right, m.width), ""}
	out = append(out, m.workspace(m.height-3)...)
	out = append(out, statusBar(m.width, m.statusPairs()...))
	for i, l := range out {
		out[i] = ansi.Truncate(l, m.width, "")
	}
	view := strings.Join(out, "\n")
	if m.dialog != nil {
		view = overlay(view, m.dialogView(), m.width, m.height)
	}
	return view
}

// workspace is the h lines between the title and the status bar: the sidebar, its rule
// and the page, or the page alone behind a 2-column margin.
func (m *model) workspace(h int) []string {
	h = max(h, 0)
	page := strings.Split(m.pageView(m.pageWidth(), h), "\n")
	out := make([]string, h)
	if !m.sidebarShows() {
		for i := range out {
			if i < len(page) && page[i] != "" {
				out[i] = "  " + page[i]
			}
		}
		return out
	}
	sw := m.sidebarWidth()
	side := strings.Split(m.sidebarView(h), "\n")
	rule := dimSty.Render(" │ ")
	for i := range out {
		s, p := "", ""
		if i < len(side) {
			s = side[i]
		}
		if i < len(page) {
			p = page[i]
		}
		out[i] = padTo(s, sw) + rule + p
	}
	return out
}

// padTo pads s with spaces to width columns.
func padTo(s string, width int) string {
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}

// visible is the instances the sidebar lists: GTNH ones, or all with showAll, then the
// one being created.
func (m *model) visible() []prism.Instance {
	var out []prism.Instance
	for _, in := range m.insts {
		if in.GTNH || m.showAll {
			out = append(out, in)
		}
	}
	if p, ok := m.pending(); ok {
		out = append(out, p)
	}
	return out
}

// current is the selected visible instance; false when there is none.
func (m *model) current() (prism.Instance, bool) {
	v := m.visible()
	if m.sel < 0 || m.sel >= len(v) {
		return prism.Instance{}, false
	}
	return v[m.sel], true
}

// selectDir selects the visible instance with folder dir, else the first one.
func (m *model) selectDir(dir string) {
	m.sel = 0
	for i, in := range m.visible() {
		if in.Dir == dir {
			m.sel = i
		}
	}
}

// refresh rereads everything the page shows about each instance into m.home.
func (m *model) refresh() {
	m.home = make(map[string]homeInfo, len(m.insts))
	for _, in := range m.insts {
		m.home[in.Dir] = m.homeInfoOf(in)
	}
	if p, ok := m.pending(); ok {
		m.home[p.Dir] = m.pendingInfo()
	}
	if _, ok := m.current(); !ok {
		m.sel = 0
	}
	if !m.sidebarShows() {
		m.focus = focusPage
	}
}

func (m *model) homeInfoOf(in prism.Instance) homeInfo {
	st, _ := update.LoadState(in.Dir)
	info := homeInfo{
		gtnh:    in.GTNH,
		version: update.DetectVersion(in, st, m.manifest).Version,
		played:  ago(in.LastLaunch, m.now()),
		flavor:  update.FlavorOf(in),
	}
	if m.manifest != nil && len(m.manifest.Releases) > 0 {
		info.rec = defaultTarget(m.manifest, info.version)
	}
	if st != nil {
		info.server, info.modsURL = st.ServerAddress, st.CustomModsURL
		info.mods, info.synced, info.modsAsked = len(st.CustomMods), st.CustomModsSynced, st.CustomModsAsked
	}
	if in.GTNH {
		if bs, _ := update.ListBackups(in.Dir); len(bs) > 0 {
			info.backup = &bs[0]
		}
	}
	info.settings, info.settingsErr = prism.ReadSettings(in.Dir)
	return info
}

// quit leaves the program: m.quitting and tea.Quit.
func (m *model) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	return m, tea.Quit
}

// quitKey handles q and ctrl+c: never quits mid-apply, cancels a create download and
// quits once it has cleaned up, else quits.
func (m *model) quitKey() tea.Cmd {
	if m.busyApplying() {
		return nil // never quit mid-apply
	}
	if j := m.job; j != nil && j.kind == jobCreate && j.phase == "prepare" && m.cancelCreate != nil {
		m.cancelCreate() // quit once the download has cleaned up (C10)
		m.quitAfterCancel = true
		return nil
	}
	_, cmd := m.quit()
	return cmd
}

// onStep starts the next step of the running job. Unused until slice "updateflow".
func (m *model) onStep(msg stepMsg) {
	if m.step != "" {
		m.steps = append(m.steps, m.step)
	}
	m.step, m.stepStart, m.done, m.total = string(msg), m.now(), 0, 0
}

// startBusy starts a job with a fresh step log.
func (m *model) startBusy() {
	m.warns, m.steps, m.step = nil, nil, ""
}
