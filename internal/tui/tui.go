// Package tui is the interactive bubbletea front end: pick an instance, pick a version,
// review what will happen, apply. It talks to players, not developers: plain sentences,
// no jargon, and always a clear next key.
package tui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
	scInstance
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
)

var (
	accent    = lipgloss.Color("#7FB4CA")
	titleSty  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	okSty     = lipgloss.NewStyle().Foreground(lipgloss.Color("#98BB6C"))
	warnSty   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6C384"))
	badSty    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E46876")).Bold(true)
	dimSty    = lipgloss.NewStyle().Foreground(lipgloss.Color("#727169"))
	keySty    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	bannerSty = lipgloss.NewStyle().Foreground(lipgloss.Color("#1F1F28")).Background(lipgloss.Color("#E6C384")).Padding(0, 1)
)

var (
	keyNotMine   = key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "not this one"))
	keyServer    = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mods"))
	keyOther     = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	keySelfUpd   = key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "update me"))
	keyShowOther = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all instances"))
	keyNew       = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new instance"))
	keyPick      = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "choose"))
	keySwitch    = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "switch"))
	keyAllNew    = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "all new"))
	keyDone      = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done"))
)

type item struct{ title, desc, key string }

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title + " " + i.desc }

type model struct {
	cfg  Config
	send func(tea.Msg)

	screen  screen
	scroll  int // body scroll of the current non-list screen; reset on screen change
	width   int
	height  int
	spin    spinner.Model
	bar     progress.Model
	list    list.Model
	hasList bool
	input   textinput.Model
	inputEr string

	manifest *manifest.Manifest
	insts    []prism.Instance
	showAll  bool
	inst     prism.Instance
	auto     bool // instance picked automatically (the only GTNH one)
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
	return &model{
		cfg: cfg, spin: sp, input: ti, nameIn: ni, width: 80, height: 24,
		bar: progress.New(progress.WithGradient("#7FB4CA", "#98BB6C")),
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
)

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick, m.load}
	if m.cfg.UpdateCheck {
		cmds = append(cmds, m.checkSelf)
	}
	return tea.Batch(cmds...)
}

func (m *model) checkSelf() tea.Msg {
	client := *m.cfg.Client
	client.Timeout = 10 * time.Second
	r, err := selfupdate.Latest(&client)
	if err != nil || !selfupdate.Newer(m.cfg.AppVersion, r.Version) {
		return nil // never bother the player about a failed check
	}
	return newerMsg{r}
}

func (m *model) load() tea.Msg {
	if len(m.cfg.PrismDirs) == 0 {
		return errMsg{errors.New("I couldn't find Prism Launcher on this computer.\n\n" +
			"If it's installed somewhere unusual, start me with\n  -prism-dir <the folder that contains prismlauncher.cfg>")}
	}
	man, err := manifest.Fetch(m.cfg.Client)
	if err != nil {
		return errMsg{fmt.Errorf("I couldn't reach the GTNH download server to see which versions exist. "+
			"Check your internet connection and try again.\n\n(%w)", err)}
	}
	var insts []prism.Instance
	for _, d := range m.cfg.PrismDirs {
		if found, err := prism.ListInstances(prism.InstancesDir(d)); err == nil {
			insts = append(insts, found...)
		}
	}
	return loadedMsg{man, insts} // no instances at all: afterLoad offers to create one
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prev := m.screen
	md, cmd := m.update(msg)
	if m.screen != prev {
		m.scroll = 0
	}
	return md, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.bar.Width = max(min(msg.Width-8, 64), 10)
		m.input.Width = m.inputWidth()
		m.nameIn.Width = m.inputWidth()
		if m.hasList {
			m.list.SetSize(m.listWidth(), m.listHeight())
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case loadedMsg:
		m.manifest, m.insts = msg.m, msg.insts
		return m.afterLoad()
	case newerMsg:
		m.newer = msg.r
		if m.hasList {
			m.list.SetSize(m.listWidth(), m.listHeight()) // make room for the banner
		}
		return m, nil
	case stepMsg:
		if m.step != "" {
			m.steps = append(m.steps, m.step)
		}
		m.step, m.stepStart, m.done, m.total = string(msg), time.Now(), 0, 0
		return m, nil
	case progressMsg:
		m.done, m.total = msg.done, msg.total
		return m, nil
	case warnMsg:
		m.warns = append(m.warns, string(msg))
		return m, nil
	case preparedMsg:
		m.session, m.screen = msg.s, scConfirm
		if msg.s.Plan.Count(update.Conflict) > 0 {
			msg.s.Plan.ChooseAll(m.configsChoice()) // preselected on the next screen
			return m.showConflicts()
		}
		return m, nil
	case appliedMsg:
		m.result, m.screen = msg.r, scDone
		return m, nil
	case createReady:
		m.creation, m.screen = msg.c, scConfirm
		if m.createDone() {
			return m.quit() // finished just before the cancel landed; quit closes it
		}
		return m, nil
	case createdMsg:
		m.created, m.screen = msg.r, scDone
		return m, nil
	case selfDoneMsg:
		m.screen = scSelfUpdated
		return m, nil
	case errMsg:
		var leftover *update.LeftoverError
		if m.createDone() && !errors.As(msg.err, &leftover) {
			return m.quit() // the cancelled download cleaned up after itself
		}
		m.err, m.errPhase, m.screen = msg.err, m.screen, scError
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	if m.isListScreen() {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) isListScreen() bool {
	return m.screen == scInstance || m.screen == scInstalled || m.screen == scTarget || m.choosingConfigs()
}

// choosingConfigs reports whether a config-choice list is on screen; those belong to a
// prepared session, so the self-update offer is hidden there.
func (m *model) choosingConfigs() bool {
	return m.screen == scConflicts || m.screen == scResolve
}

func (m *model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m.quit()
	}
	if m.scrollable() && m.scrollKey(k.String()) {
		return m, nil
	}
	switch m.screen {
	case scInstance, scInstalled, scTarget:
		if m.list.FilterState() == list.Filtering {
			break // typing into the filter: let the list have every key
		}
		switch k.String() {
		case "enter":
			if sel, ok := m.list.SelectedItem().(item); ok {
				return m.choose(sel.key)
			}
			return m, nil
		case "u":
			if m.newer != nil {
				return m.startSelfUpdate()
			}
		case "esc":
			if m.list.FilterState() == list.Unfiltered && m.screen != scInstance && len(m.insts) > 0 {
				return m.showInstances()
			}
		case "i":
			if m.screen == scTarget && !m.creating {
				return m.showInstalled()
			}
		case "n":
			if m.screen == scInstance {
				return m.startCreate()
			}
		case "m":
			if m.screen == scTarget {
				return m.askServerMods(false)
			}
		case "a":
			if m.screen == scInstance {
				m.showAll = !m.showAll
				return m.showInstances()
			}
		}
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(k)
		return m, cmd
	case scServerMods:
		switch k.String() {
		case "enter":
			v := strings.TrimSpace(m.input.Value())
			if v != "" {
				if err := update.CheckCustomModsURL(v); err != nil {
					m.inputEr = "That doesn't look like a download link — it should start with https://"
					return m, nil
				}
			}
			m.serverMods, m.serverModsAsked, m.inputEr = v, true, ""
			if m.modsThenPrepare && m.creating {
				return m.askName()
			}
			if m.modsThenPrepare {
				return m.prepare()
			}
			return m.showTargets()
		case "esc":
			m.inputEr = ""
			return m.showTargets()
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(k)
		return m, cmd
	case scName:
		switch k.String() {
		case "enter":
			v := strings.TrimSpace(m.nameIn.Value())
			if err := update.CheckInstanceName(m.instancesDir(), v); err != nil {
				m.nameEr = err.Error()
				return m, nil
			}
			m.newName, m.nameEr = v, ""
			return m.prepareCreate()
		case "esc":
			m.nameEr = ""
			return m.showTargets()
		}
		var cmd tea.Cmd
		m.nameIn, cmd = m.nameIn.Update(k)
		return m, cmd
	case scConflicts, scResolve:
		if m.list.FilterState() == list.Filtering {
			break // typing into the filter: let the list have every key
		}
		if m.screen == scConflicts {
			return m.conflictsKey(k)
		}
		return m.resolveKey(k)
	case scConfirm:
		if m.creating {
			return m.confirmCreateKey(k)
		}
		switch k.String() {
		case "enter", "y":
			m.screen, m.warns, m.steps, m.step = scApplying, nil, nil, ""
			s := m.session
			return m, func() tea.Msg {
				r, err := s.Apply(m.reporter())
				if err != nil {
					return errMsg{err}
				}
				return appliedMsg{r}
			}
		case "esc", "n", "q":
			return m.dropSession()
		}
	case scError:
		switch k.String() {
		case "esc":
			if m.canGoBack() {
				return m.goBackFromError()
			}
			return m.quit()
		case "q", "enter":
			return m.quit()
		}
	case scDone:
		switch k.String() {
		case "q", "enter", "esc":
			return m.quit()
		}
	case scSelfUpdated:
		switch k.String() {
		case "enter":
			m.restart = true
			return m.quit()
		case "q", "esc":
			return m.quit()
		}
	case scLoading, scPreparing, scApplying, scSelfUpdate:
		// Busy: only ctrl+c (handled above) interrupts.
	}
	if m.isListScreen() {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(k)
		return m, cmd
	}
	return m, nil
}

// dropSession closes the prepared update and returns to the version list.
func (m *model) dropSession() (tea.Model, tea.Cmd) {
	m.session.Close()
	m.session = nil
	return m.showTargets()
}

func (m *model) confirmCreateKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter", "y":
		m.screen, m.warns, m.steps, m.step = scApplying, nil, nil, ""
		c := m.creation
		m.creation = nil // Apply always closes it
		return m, func() tea.Msg {
			r, err := c.Apply(m.reporter())
			if err != nil {
				return errMsg{err}
			}
			return createdMsg{r}
		}
	case "esc", "n", "q":
		if err := m.closeCreation(); err != nil {
			return m.showLeftover(err)
		}
		return m.showTargets()
	}
	return m, nil
}

func (m *model) conflictsKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	pl := m.session.Plan
	switch k.String() {
	case "enter":
		sel, _ := m.list.SelectedItem().(item)
		switch sel.key {
		case "new":
			pl.ChooseAll(update.TakeNew)
		case "mine":
			pl.ChooseAll(update.KeepMine)
		case "pick":
			return m.showResolve()
		default:
			return m, nil
		}
		m.screen = scConfirm
		return m, nil
	case "esc":
		if m.list.FilterState() == list.Unfiltered {
			return m.dropSession()
		}
	case "q":
		return m.quit()
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

func (m *model) resolveKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	pl := m.session.Plan
	switch k.String() {
	case " ":
		if sel, ok := m.list.SelectedItem().(item); ok {
			c := update.TakeNew
			if pl.ChoiceOf(sel.key) == update.TakeNew {
				c = update.KeepMine
			}
			pl.Choose(sel.key, c)
			return m, m.list.SetItems(m.resolveItems())
		}
		return m, nil
	case "n":
		pl.ChooseAll(update.TakeNew)
		return m, m.list.SetItems(m.resolveItems())
	case "k":
		pl.ChooseAll(update.KeepMine)
		return m, m.list.SetItems(m.resolveItems())
	case "enter":
		m.screen = scConfirm
		return m, nil
	case "esc":
		if m.list.FilterState() == list.Unfiltered {
			return m.showConflicts()
		}
	case "q":
		return m.quit()
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

// closeCreation throws away the prepared, unapplied creation, if any. The error is a
// folder it couldn't remove.
func (m *model) closeCreation() error {
	c := m.creation
	m.creation = nil
	if c == nil {
		return nil
	}
	return c.Close()
}

// showLeftover shows the error of a thrown-away creation whose folder is still there.
// The creation never got past its download, hence the preparing phase.
func (m *model) showLeftover(err error) (tea.Model, tea.Cmd) {
	m.err, m.errPhase, m.screen = err, scPreparing, scError
	return m, nil
}

// createDone marks the running PrepareCreate as returned and reports whether the
// player is waiting for it to quit.
func (m *model) createDone() bool {
	if m.cancelCreate == nil {
		return false
	}
	m.cancelCreate()
	m.cancelCreate = nil
	return m.quitAfterCancel
}

func (m *model) quit() (tea.Model, tea.Cmd) {
	if m.screen == scApplying || m.screen == scSelfUpdate {
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

func (m *model) afterLoad() (tea.Model, tea.Cmd) {
	if m.cfg.Target != "" {
		v, err := manifest.Resolve(m.manifest, m.cfg.Target)
		if err != nil {
			if m.cfg.Target != "latest-stable" {
				err = fmt.Errorf("GTNH has no version called %q.", m.cfg.Target)
			}
			return m, errCmd(err)
		}
		m.cfg.Target = v
	}
	if m.cfg.Create {
		return m.startCreate()
	}
	if m.cfg.Instance != "" {
		for _, in := range m.insts {
			if in.Name == m.cfg.Instance || sameDir(in.Dir, m.cfg.Instance) {
				return m.pickInstance(in)
			}
		}
		if in, err := prism.LoadInstance(m.cfg.Instance); err == nil {
			m.insts = append(m.insts, in)
			return m.pickInstance(in)
		}
		return m, errCmd(fmt.Errorf("I couldn't find an instance called %q.", m.cfg.Instance))
	}
	var gtnh []prism.Instance
	for _, in := range m.insts {
		if in.GTNH {
			gtnh = append(gtnh, in)
		}
	}
	if len(m.insts) == 0 {
		return m.startCreate()
	}
	if len(gtnh) == 1 {
		m.auto = true
		return m.pickInstance(gtnh[0])
	}
	m.showAll = len(gtnh) == 0
	return m.showInstances()
}

func errCmd(err error) tea.Cmd { return func() tea.Msg { return errMsg{err} } }

func sameDir(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
}

// choose handles Enter on a list screen.
func (m *model) choose(key string) (tea.Model, tea.Cmd) {
	switch m.screen {
	case scInstance:
		for _, in := range m.insts {
			if in.Dir == key {
				m.auto = false
				return m.pickInstance(in)
			}
		}
	case scInstalled:
		m.detect = update.Detection{Version: key, Source: "you told me"}
		return m.showTargets()
	case scTarget:
		m.target = key
		if !m.serverModsAsked {
			return m.askServerMods(true)
		}
		if m.creating {
			return m.askName()
		}
		return m.prepare()
	}
	return m, nil
}

// startCreate enters the create flow: pick a version, server mods, a name, then create.
func (m *model) startCreate() (tea.Model, tea.Cmd) {
	m.creating, m.inst, m.target, m.auto = true, prism.Instance{}, "", false
	switch m.cfg.ServerMods {
	case "":
		m.serverMods, m.serverModsAsked = "", false
	case "none":
		m.serverMods, m.serverModsAsked = "", true
	default:
		m.serverMods, m.serverModsAsked = m.cfg.ServerMods, true
	}
	return m.showTargets()
}

func (m *model) instancesDir() string { return prism.InstancesDir(m.cfg.PrismDirs[0]) }

func (m *model) pickInstance(in prism.Instance) (tea.Model, tea.Cmd) {
	m.inst, m.target, m.creating = in, "", false
	if prism.Running(in) {
		return m, errCmd(fmt.Errorf("%s is running right now. Close Minecraft, then start me again.", in.Name))
	}
	st, err := update.LoadState(in.Dir)
	if err != nil {
		return m, errCmd(fmt.Errorf("I couldn't read my notes about this instance: %w", err))
	}
	m.detect = update.DetectVersion(in, st, m.manifest)
	if m.cfg.Installed != "" {
		m.detect = update.Detection{Version: m.cfg.Installed, Source: "command line"}
	}
	switch {
	case m.cfg.ServerMods == "none":
		m.serverMods, m.serverModsAsked = "", true
	case m.cfg.ServerMods != "":
		m.serverMods, m.serverModsAsked = m.cfg.ServerMods, true
	case st != nil && st.CustomModsAsked:
		m.serverMods, m.serverModsAsked = st.CustomModsURL, true
	default:
		m.serverMods, m.serverModsAsked = "", false
	}
	if m.detect.Version == "" {
		return m.showInstalled()
	}
	return m.showTargets()
}

func (m *model) inputWidth() int { return max(min(m.width-10, 70), 20) }
func (m *model) listWidth() int  { return max(m.width-4, 20) }
func (m *model) listHeight() int {
	h := m.height - 5 // padding + header
	if m.newer != nil {
		h -= 2
	}
	return max(h, 5)
}

func (m *model) showList(sc screen, title string, items []list.Item, selected string, help ...key.Binding) (tea.Model, tea.Cmd) {
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(accent).BorderForeground(accent)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(lipgloss.Color("#98BB6C")).BorderForeground(accent)
	l := list.New(items, d, 0, 0)
	l.SetSize(m.listWidth(), m.listHeight()) // New doesn't size the help line, SetSize does
	l.Title = ansi.Truncate(title, m.listWidth()-2, "…")
	l.Styles.Title = titleSty.Padding(0, 1)
	l.SetStatusBarItemName("choice", "choices")
	l.SetShowStatusBar(len(items) > 8)
	for i, it := range items {
		if it.(item).key == selected {
			l.Select(i)
		}
	}
	l.AdditionalShortHelpKeys = func() []key.Binding {
		out := append([]key.Binding{}, help...)
		if m.newer != nil && !m.choosingConfigs() {
			out = append(out, keySelfUpd)
		}
		return out
	}
	m.list, m.screen, m.hasList = l, sc, true
	return m, nil
}

func (m *model) showConflicts() (tea.Model, tea.Cmd) {
	n := m.session.Plan.Count(update.Conflict)
	title := fmt.Sprintf("%d config %s changed both on your side and in the new version. What should I do?",
		n, plural(n, "file was", "files were"))
	items := []list.Item{
		item{"Use the new versions", "Recommended. Your old copies go to the backup folder.", "new"},
		item{"Keep mine", "The new ones are saved next to them with .mcnew at the end, so you can compare.", "mine"},
		item{"Let me choose file by file", "You decide for each file.", "pick"},
	}
	return m.showList(scConflicts, title, items, choiceKey(m.configsChoice()), keyPick, keyOther)
}

// configsChoice is the conflict answer to preselect, from Config.Configs.
func (m *model) configsChoice() update.Choice {
	switch m.cfg.Configs {
	case "new":
		return update.TakeNew
	case "mine":
		return update.KeepMine
	}
	return update.Recommended
}

// choiceKey is the scConflicts item that stands for c.
func choiceKey(c update.Choice) string {
	if c == update.KeepMine {
		return "mine"
	}
	return "new"
}

func (m *model) showResolve() (tea.Model, tea.Cmd) {
	title := "Which version of each file do you want? Space switches, enter when you're done."
	// k (keep all) works but stays out of the help: with it the line passes ~100 columns.
	md, cmd := m.showList(scResolve, title, m.resolveItems(), "", keySwitch, keyAllNew, keyDone, keyOther)
	// k means "keep all" here, so it no longer moves the cursor up.
	m.list.KeyMap.CursorUp = key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "up"))
	return md, cmd
}

func (m *model) resolveItems() []list.Item {
	pl := m.session.Plan
	var items []list.Item
	for _, p := range pl.Conflicts() {
		desc := "keep mine"
		if pl.ChoiceOf(p) == update.TakeNew {
			desc = "new version"
		}
		items = append(items, item{strings.TrimPrefix(p, ".minecraft/"), desc, p})
	}
	return items
}

func (m *model) showInstances() (tea.Model, tea.Cmd) {
	m.creating = false
	var items []list.Item
	sel, hidden := "", 0
	for _, in := range m.insts {
		if !in.GTNH && !m.showAll {
			hidden++
			continue
		}
		var desc string
		if in.GTNH {
			st, _ := update.LoadState(in.Dir)
			v := update.DetectVersion(in, st, m.manifest).Version
			if v == "" {
				v = "version unknown"
			}
			desc = "GTNH " + v
			if sel == "" {
				sel = in.Dir // the list is sorted most recently played first
			}
		} else {
			desc = "not a GTNH instance"
		}
		if a := ago(in.LastLaunch, time.Now()); a != "" {
			desc += " · played " + a
		} else {
			desc += " · never played"
		}
		items = append(items, item{in.Name, desc, in.Dir})
	}
	if m.inst.Dir != "" {
		sel = m.inst.Dir
	}
	help := []key.Binding{keyNew}
	if hidden > 0 || m.showAll {
		help = append(help, keyShowOther)
	}
	return m.showList(scInstance, "Which modpack do you want to update?", items, sel, help...)
}

// kindOf turns a manifest title plus version into words a player knows.
func kindOf(r manifest.Release) string {
	v := strings.ToLower(r.Version)
	switch {
	case r.Stable():
		return "Stable release"
	case strings.Contains(v, "rc"):
		return "Release candidate"
	case strings.Contains(v, "beta"):
		return "Beta"
	case strings.Contains(v, "pre"):
		return "Pre-release"
	}
	return r.Title
}

func (m *model) releaseItems(mark func(manifest.Release) []string) []list.Item {
	var items []list.Item
	now := time.Now()
	for _, r := range m.manifest.Releases {
		parts := []string{kindOf(r)}
		if a := ago(r.ReleaseDate, now); a != "" {
			parts = append(parts, a)
		}
		parts = append(parts, mark(r)...)
		items = append(items, item{r.Version, strings.Join(parts, " · "), r.Version})
	}
	return items
}

func (m *model) showInstalled() (tea.Model, tea.Cmd) {
	items := m.releaseItems(func(manifest.Release) []string { return nil })
	title := "Which GTNH version is " + m.inst.Name + " on right now?"
	return m.showList(scInstalled, title, items, m.detect.Version, keyOther)
}

func (m *model) showTargets() (tea.Model, tea.Cmd) {
	if m.cfg.Target != "" && m.target == "" && m.screen != scConfirm && m.screen != scError {
		m.target = m.cfg.Target
		if !m.serverModsAsked {
			return m.askServerMods(true)
		}
		if m.creating {
			return m.askName()
		}
		return m.prepare()
	}
	if m.creating {
		return m.showCreateTargets()
	}
	flavor := update.FlavorOf(m.inst)
	rec := defaultTarget(m.manifest, m.detect.Version)
	items := m.releaseItems(func(r manifest.Release) []string {
		var tags []string
		if r.Version == rec && r.Version != m.detect.Version {
			tags = append(tags, "recommended")
		}
		if r.Version == m.detect.Version {
			tags = append(tags, "you have this one")
		}
		if _, err := r.URL(flavor); err != nil {
			tags = append(tags, "not available for your Java")
		}
		return tags
	})
	sel := m.target
	if sel == "" {
		sel = rec
	}
	title := fmt.Sprintf("%s is on GTNH %s. Which version do you want?", m.inst.Name, m.detect.Version)
	help := []key.Binding{keyNotMine, keyServer}
	if len(m.insts) > 0 {
		help = append(help, keyOther)
	}
	return m.showList(scTarget, title, items, sel, help...)
}

// showCreateTargets is the version list of the create flow.
func (m *model) showCreateTargets() (tea.Model, tea.Cmd) {
	rec := defaultTarget(m.manifest, "")
	items := m.releaseItems(func(r manifest.Release) []string {
		var tags []string
		if r.Version == rec {
			tags = append(tags, "recommended")
		}
		if update.NewInstanceFlavor(r) == manifest.Java8 {
			tags = append(tags, "Java 8 only")
		}
		return tags
	})
	sel := m.target
	if sel == "" {
		sel = rec
	}
	title := "Which GTNH version do you want to install?"
	if len(m.insts) == 0 {
		title = "Prism has no instances yet. " + title
	}
	help := []key.Binding{keyServer}
	if len(m.insts) > 0 {
		help = append(help, keyOther)
	}
	return m.showList(scTarget, title, items, sel, help...)
}

func (m *model) askName() (tea.Model, tea.Cmd) {
	name := update.DefaultInstanceName(m.target)
	if m.cfg.Name != "" && !m.nameUsed {
		name, m.nameUsed = m.cfg.Name, true
	}
	m.nameIn.SetValue(name)
	m.nameIn.CursorEnd()
	m.nameIn.Width = m.inputWidth()
	m.screen = scName
	return m, m.nameIn.Focus()
}

func (m *model) prepareCreate() (tea.Model, tea.Cmd) {
	m.screen, m.warns, m.steps, m.step = scPreparing, nil, nil, ""
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelCreate, m.quitAfterCancel = cancel, false
	opts := update.CreateOptions{
		Context: ctx, Client: m.cfg.Client, Manifest: m.manifest, InstancesDir: m.instancesDir(), Name: m.newName,
		Target: m.target, CustomModsURL: m.serverMods, CustomModsAsked: true,
	}
	return m, func() tea.Msg {
		c, err := update.PrepareCreate(opts, m.reporter())
		if err != nil {
			return errMsg{err}
		}
		return createReady{c}
	}
}

// defaultTarget preselects the newest stable release, unless the player already runs
// something newer (a beta or RC): then the newest release overall, so a blind Enter
// never downgrades.
func defaultTarget(man *manifest.Manifest, installed string) string {
	for _, r := range man.Releases {
		if r.Stable() && manifest.CompareVersions(r.Version, installed) >= 0 {
			return r.Version
		}
	}
	return man.Releases[0].Version
}

func (m *model) askServerMods(thenPrepare bool) (tea.Model, tea.Cmd) {
	m.modsThenPrepare = thenPrepare
	m.input.SetValue(m.serverMods)
	m.input.CursorEnd()
	m.input.Width = m.inputWidth()
	m.screen = scServerMods
	return m, m.input.Focus()
}

func (m *model) prepare() (tea.Model, tea.Cmd) {
	m.screen, m.warns, m.steps, m.step = scPreparing, nil, nil, ""
	opts := update.Options{
		Client: m.cfg.Client, Manifest: m.manifest, Instance: m.inst,
		Installed: m.detect.Version, Target: m.target, CustomModsURL: m.serverMods,
	}
	return m, func() tea.Msg {
		s, err := update.Prepare(opts, m.reporter())
		if err != nil {
			return errMsg{err}
		}
		return preparedMsg{s}
	}
}

func (m *model) startSelfUpdate() (tea.Model, tea.Cmd) {
	m.screen, m.warns, m.steps = scSelfUpdate, nil, nil
	m.step, m.stepStart, m.done, m.total = "Downloading gtnh-update "+m.newer.Version, time.Now(), 0, 0
	rel, rep := m.newer, m.reporter()
	return m, func() tea.Msg {
		if err := rel.Apply(m.cfg.Client, rep.Progress); err != nil {
			if errors.Is(err, selfupdate.ErrNotWritable) {
				err = fmt.Errorf("I can't replace myself in this folder. Download the new version yourself from\n%s\n\n(%w)", rel.Page, err)
			}
			return errMsg{err}
		}
		return selfDoneMsg{}
	}
}

// reporter forwards progress to the program, throttling the chatty byte counter.
func (m *model) reporter() update.Reporter { return &teaReporter{send: m.send} }

type teaReporter struct {
	send func(tea.Msg)
	mu   sync.Mutex
	last time.Time
}

func (r *teaReporter) Step(s string) { r.send(stepMsg(s)) }
func (r *teaReporter) Warn(s string) { r.send(warnMsg(s)) }
func (r *teaReporter) Progress(done, total int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if done != total && time.Since(r.last) < 80*time.Millisecond {
		return
	}
	r.last = time.Now()
	r.send(progressMsg{done, total})
}

// ---- views ----

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	switch m.screen {
	case scInstance, scInstalled, scTarget:
		return lipgloss.NewStyle().Padding(1, 2, 0, 1).Render("   " + m.header() + m.banner() + m.list.View())
	case scConflicts, scResolve:
		return lipgloss.NewStyle().Padding(1, 2, 0, 1).Render("   " + m.header() + m.list.View())
	}
	header, body, footer, scroll := m.page()
	return frame(header, body, footer, m.width, m.height, scroll)
}

// page builds a non-list screen for frame: header, body, the pinned footer and the
// scroll offset to show.
func (m *model) page() (header, body, footer string, scroll int) {
	scroll = m.scroll
	switch m.screen {
	case scLoading:
		body, scroll = m.spin.View()+" Looking for your GTNH instances…", math.MaxInt32
	case scServerMods:
		body, footer = m.serverModsView()
	case scName:
		body, footer = m.nameView()
	case scPreparing, scApplying, scSelfUpdate:
		body, footer = m.busyView()
		scroll = math.MaxInt32 // keep the newest step in view
	case scConfirm:
		if m.creating {
			body, footer = m.confirmCreateView()
		} else {
			body, footer = m.confirmView()
		}
	case scDone:
		if m.creating {
			body, footer = m.createdView()
		} else {
			body, footer = m.doneView()
		}
	case scError:
		body, footer = m.errorView()
	case scSelfUpdated:
		body = okSty.Bold(true).Render(wrap("gtnh-update is now version "+m.newer.Version+".", m.width-4))
		footer = hint("enter", "restart it now", "q", "quit")
	}
	if footer != "" {
		footer = "\n" + footer // a blank line between body and keys
	}
	return strings.TrimSuffix(m.header(), "\n"), strings.TrimRight(body, "\n"), footer, scroll
}

// scrollable reports whether the current screen scrolls with the arrow keys.
func (m *model) scrollable() bool {
	switch m.screen {
	case scConfirm, scDone, scError, scServerMods, scName:
		return true
	}
	return false
}

// scrollKey moves the body of a scrollable screen; ok is false for other keys.
// Home and end stay with the text field on the input screens.
func (m *model) scrollKey(k string) (ok bool) {
	header, body, footer, _ := m.page()
	lines := len(strings.Split(body, "\n"))
	rows := fitBodyRows(m.height, len(strings.Split(header, "\n")), len(strings.Split(footer, "\n")))
	page := max(rows/2, 1)
	input := m.screen == scServerMods || m.screen == scName
	switch {
	case k == "up":
		m.scroll--
	case k == "down":
		m.scroll++
	case k == "pgup":
		m.scroll -= page
	case k == "pgdown":
		m.scroll += page
	case k == "home" && !input:
		m.scroll = 0
	case k == "end" && !input:
		m.scroll = lines
	default:
		return false
	}
	m.scroll = max(min(m.scroll, max(lines-rows, 0)), 0)
	return true
}

func (m *model) header() string {
	return dimSty.Render("GTNH Updater "+m.cfg.AppVersion) + "\n\n"
}

func (m *model) banner() string {
	if m.newer == nil {
		return ""
	}
	return bannerSty.Render(fmt.Sprintf("A new version of this updater is out (%s) — press u to get it", m.newer.Version)) + "\n\n"
}

// fitBodyRows is how many body lines fit under the blank top line, header and footer.
func fitBodyRows(height, headerLines, footerLines int) int {
	return max(height-1-headerLines-footerLines, 1)
}

// bodyWindow picks the rows of body that fit on screen, scrolled to scroll (clamped).
// When body overflows, the first/last visible row is replaced by a "more" marker.
func bodyWindow(body []string, rows, scroll int) (visible []string, offset int) {
	rows = max(rows, 1)
	if len(body) <= rows {
		return body, 0
	}
	maxOff := len(body) - rows
	offset = max(min(scroll, maxOff), 0)
	visible = append([]string{}, body[offset:offset+rows]...)
	if offset < maxOff {
		visible[rows-1] = dimSty.Render(fmt.Sprintf("↓ %d more (↑/↓ to scroll)", maxOff-offset))
	}
	if offset > 0 {
		visible[0] = dimSty.Render(fmt.Sprintf("↑ %d more", offset))
	}
	return visible, offset
}

// frame lays out a non-list screen: header on top, footer pinned at the bottom, body
// scrolled in between, every line indented and clipped to width.
func frame(header, body, footer string, width, height, scroll int) string {
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(s, "\n")
	}
	head, foot := split(header), split(footer)
	visible, _ := bodyWindow(split(body), fitBodyRows(height, len(head), len(foot)), scroll)
	out := []string{""}
	for _, part := range [][]string{head, visible, foot} {
		for _, l := range part {
			out = append(out, ansi.Truncate("  "+l, width, "…"))
		}
	}
	return strings.Join(out, "\n")
}

func hint(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keySty.Render(pairs[i])+" "+dimSty.Render(pairs[i+1]))
	}
	return strings.Join(parts, "    ")
}

func (m *model) serverModsView() (body, footer string) {
	var b strings.Builder
	b.WriteString(titleSty.Render("Does your server have its own extra mods?") + "\n\n")
	b.WriteString(wrap("Some servers add a few mods on top of GTNH. If the server owner gave you a link for "+
		"them, paste it here — I'll install them now and keep them in sync every time you update.", m.width-4) + "\n\n")
	b.WriteString(m.input.View() + "\n")
	if m.inputEr != "" {
		b.WriteString(badSty.Render(wrap(m.inputEr, m.width-4)) + "\n")
	}
	b.WriteString("\n" + dimSty.Render(wrap("No link? Leave it empty — you can add one later with m on the version list.", m.width-4)))
	return b.String(), hint("enter", "continue", "esc", "back")
}

func (m *model) nameView() (body, footer string) {
	var b strings.Builder
	b.WriteString(titleSty.Render("What should the new instance be called?") + "\n\n")
	b.WriteString(wrap("That's the name you'll see in Prism; it's also the folder name.", m.width-4) + "\n\n")
	b.WriteString(m.nameIn.View() + "\n")
	if m.nameEr != "" {
		b.WriteString(badSty.Render(wrap(m.nameEr, m.width-4)) + "\n")
	}
	b.WriteString("\n" + dimSty.Render(wrap("It'll be created in "+m.instancesDir(), m.width-4)))
	return b.String(), hint("enter", "continue", "esc", "back")
}

func (m *model) busyView() (body, footer string) {
	var b strings.Builder
	title := func(s string) { b.WriteString(titleSty.Render(wrap(s, m.width-4)) + "\n\n") }
	switch {
	case m.screen == scSelfUpdate:
		title("Updating gtnh-update itself")
	case m.creating && m.screen == scApplying:
		title("Creating " + m.newName)
	case m.creating:
		title("Getting GTNH " + m.target + " ready")
	case m.screen == scApplying:
		title(fmt.Sprintf("Updating %s to GTNH %s", m.inst.Name, m.target))
	default:
		title(fmt.Sprintf("Getting GTNH %s ready for %s", m.target, m.inst.Name))
	}
	for _, s := range m.steps {
		b.WriteString(okSty.Render("  ✓ ") + dimSty.Render(indentWrap(s, m.width-8, 4)) + "\n")
	}
	if m.step != "" {
		b.WriteString("  " + m.spin.View() + indentWrap(m.step, m.width-8, 4) + "\n")
	}
	if m.total > 0 {
		pct := float64(m.done) / float64(m.total)
		b.WriteString("\n    " + m.bar.ViewAs(pct) + "\n    " + dimSty.Render(m.progressDetail()) + "\n")
	}
	for _, w := range m.warns {
		b.WriteString("\n" + warnSty.Render(wrap("  Heads up: "+w, m.width-4)))
	}
	switch {
	case m.screen != scPreparing:
		footer = warnSty.Render(wrap("  Please don't close this window until I'm done.", m.width-4))
	case m.quitAfterCancel:
		footer = dimSty.Render(wrap("Stopping and cleaning up…", m.width-4))
	default:
		footer = hint("ctrl+c", "cancel")
	}
	return b.String(), footer
}

func (m *model) progressDetail() string {
	var s string
	if strings.HasPrefix(m.step, "Downloading") {
		s = fmt.Sprintf("%s of %s", mb(m.done), mb(m.total))
	} else {
		s = fmt.Sprintf("%s of %s files", num(m.done), num(m.total))
	}
	if left := eta(m.done, m.total, time.Since(m.stepStart)); left != "" {
		s += " · " + left
	}
	return s
}

func (m *model) confirmView() (body, footer string) {
	s, pl := m.session, m.session.Plan
	var b strings.Builder
	if m.target == m.detect.Version {
		b.WriteString(titleSty.Render(wrap(fmt.Sprintf("Ready to refresh %s on GTNH %s", m.inst.Name, m.target), m.width-4)) + "\n\n")
	} else {
		b.WriteString(titleSty.Render(wrap(fmt.Sprintf("Ready to update %s from %s to %s", m.inst.Name, m.detect.Version, m.target), m.width-4)) + "\n\n")
	}

	var bad []string
	if manifest.CompareVersions(m.target, m.detect.Version) < 0 {
		bad = append(bad, fmt.Sprintf("This goes BACK to an older version. Worlds you played on %s may lose "+
			"blocks and items or not load at all. Copy your saves folder somewhere safe first.", m.detect.Version))
	}
	if pl.BaselineMatch < 0.8 {
		bad = append(bad, fmt.Sprintf("Only %.0f%% of the mods that come with %s are in this instance, so it's "+
			"probably not on %s. Press esc, then i to tell me the right version — otherwise old mods could be left behind.",
			pl.BaselineMatch*100, m.detect.Version, m.detect.Version))
	}
	for _, w := range bad {
		b.WriteString(badSty.Render(wrap("! "+w, m.width-4)) + "\n\n")
	}

	bullet := func(s string) { b.WriteString(m.bullet(s)) }
	if n := pl.Count(update.Install) + pl.Count(update.Remove); n == 0 {
		bullet("Your GTNH files are already exactly as they should be.")
	} else {
		bullet(fmt.Sprintf("%s files will be updated and %s removed.", num(int64(pl.Count(update.Install))), num(int64(pl.Count(update.Remove)))))
	}
	bullet("Your worlds, screenshots, maps and game settings stay exactly as they are.")
	if n := len(pl.Kept); n > 0 {
		bullet(fmt.Sprintf("%s config %s you changed will be kept as you have %s.", num(int64(n)), plural(n, "file", "files"), plural(n, "it", "them")))
	}
	if n := len(pl.Chosen(update.TakeNew)); n > 0 {
		bullet(fmt.Sprintf("%d config %s you changed %s the new version. Your old %s %s to the backup folder.",
			n, plural(n, "file", "files"), plural(n, "gets", "get"), plural(n, "one", "ones"), plural(n, "goes", "go")))
	}
	if n := len(pl.Chosen(update.KeepMine)); n > 0 {
		bullet(fmt.Sprintf("%d config %s you changed %s as you have %s; the new %s saved next to %s with .mcnew at the end.",
			n, plural(n, "file", "files"), plural(n, "stays", "stay"), plural(n, "it", "them"),
			plural(n, "one is", "ones are"), plural(n, "it", "them")))
	}
	if len(pl.ExtraMods) > 0 {
		bullet(fmt.Sprintf("Mods you added yourself stay: %s. Make sure they work with %s.",
			ansi.Truncate(strings.Join(pl.ExtraMods, ", "), 120, "…"), m.target))
	}
	if m.serverMods != "" {
		bullet("Your server's extra mods will be synced from " + hostOf(m.serverMods) + ".")
	}
	if s.Flavor == manifest.Java8 {
		bullet("This instance uses the Java 8 version of the pack, so that's what you'll get.")
	}
	bullet("Everything that gets replaced is backed up first, just in case.")

	if !prism.CanDetectRunning {
		b.WriteString("\n" + warnSty.Render("  Make sure Minecraft is closed before you continue.") + "\n")
	}
	for _, w := range m.warns {
		b.WriteString("\n" + warnSty.Render(wrap("  Heads up: "+w, m.width-4)) + "\n")
	}
	return b.String(), hint("enter", "update now", "esc", "go back")
}

func (m *model) doneView() (body, footer string) {
	r, pl := m.result, m.session.Plan
	var b strings.Builder
	b.WriteString(okSty.Bold(true).Render(wrap(fmt.Sprintf("All done! %s is now on GTNH %s.", m.inst.Name, r.To), m.width-4)) + "\n\n")
	bullet := func(s string) { b.WriteString(m.bullet(s)) }
	if n := pl.Count(update.Install) + pl.Count(update.Remove); n == 0 {
		bullet("Your GTNH files were already up to date.")
	} else {
		bullet(fmt.Sprintf("%s files updated, %s removed.", num(int64(pl.Count(update.Install))), num(int64(pl.Count(update.Remove)))))
	}
	if r.Renamed != "" {
		bullet("Renamed the instance in Prism to \"" + r.Renamed + "\".")
	}
	b.WriteString(m.customModsSummary(r.CustomMods, r.CustomErr))
	if n := len(pl.Chosen(update.TakeNew)); n > 0 {
		bullet(fmt.Sprintf("%d config %s you had changed %s replaced with the new version; the old %s in the backup folder.",
			n, plural(n, "file", "files"), plural(n, "was", "were"), plural(n, "one is", "ones are")))
	}
	if c := pl.Chosen(update.KeepMine); len(c) > 0 {
		b.WriteString("\n" + wrap(fmt.Sprintf("%d config %s changed on your side and in the new version. "+
			"I kept yours and saved the new %s next to %s as .mcnew. It's usually fine to ignore this — "+
			"many mods rewrite their own config when the game starts.",
			len(c), plural(len(c), "file was", "files were"), plural(len(c), "one", "ones"), plural(len(c), "it", "them")), m.width-4) + "\n")
		const limit = 20
		for i, p := range c {
			if i == limit {
				b.WriteString(dimSty.Render(fmt.Sprintf("    … and %d more", len(c)-limit)) + "\n")
				break
			}
			b.WriteString(dimSty.Render("    "+strings.TrimPrefix(p, ".minecraft/")) + "\n")
		}
	}
	if r.BackupDir != "" {
		b.WriteString("\n" + dimSty.Render(wrap("If something's wrong, the old files are in "+r.BackupDir, m.width-4)) + "\n")
	}
	for _, w := range m.warns {
		b.WriteString("\n" + warnSty.Render(wrap("  Heads up: "+w, m.width-4)))
	}
	b.WriteString("\nYou can start the game from Prism now. Have fun!")
	return b.String(), hint("enter", "exit")
}

// customModsSummary describes what the server-mods sync did (cm nil = it didn't run).
func (m *model) customModsSummary(cm *update.CustomModsResult, cerr error) string {
	var b strings.Builder
	bullet := func(s string) { b.WriteString(m.bullet(s)) }
	if cm != nil {
		switch {
		case m.serverMods == "" && len(cm.Removed) > 0:
			bullet(fmt.Sprintf("Removed %d extra %s from your old server.", len(cm.Removed), plural(len(cm.Removed), "mod", "mods")))
		case len(cm.Installed) == 0:
			bullet("Your server has no extra mods right now.")
		default:
			line := fmt.Sprintf("%d extra %s from your server installed", len(cm.Installed), plural(len(cm.Installed), "mod", "mods"))
			if len(cm.Added)+len(cm.Removed) > 0 {
				line += fmt.Sprintf(" (%d new or updated, %d removed)", len(cm.Added), len(cm.Removed))
			}
			bullet(line + ".")
		}
		if len(cm.Skipped) > 0 {
			bullet("Skipped " + strings.Join(cm.Skipped, ", ") + " — GTNH already ships a mod with that name.")
		}
	}
	if cerr != nil {
		b.WriteString(warnSty.Render(m.bullet("Your server's extra mods couldn't be synced this time (" + cerr.Error() + "). Run me again later to retry.")))
	}
	return b.String()
}

func (m *model) confirmCreateView() (body, footer string) {
	c := m.creation
	var b strings.Builder
	b.WriteString(titleSty.Render(wrap(fmt.Sprintf("Ready to create %s with GTNH %s", m.newName, m.target), m.width-4)) + "\n\n")
	bullet := func(s string) { b.WriteString(m.bullet(s)) }
	bullet("It'll be a new instance in Prism, in " + c.Dir + ".")
	bullet(num(int64(c.Files)) + " files will be installed.")
	bullet("Your other instances aren't touched.")
	if m.serverMods != "" {
		bullet("Your server's extra mods will be installed from " + hostOf(m.serverMods) + ".")
	}
	if c.Flavor == manifest.Java8 {
		bullet("This version only comes as a Java 8 pack, so that's what you'll get.")
	} else {
		bullet("It uses the Java 17+ version of the pack.")
	}
	for _, w := range m.warns {
		b.WriteString("\n" + warnSty.Render(wrap("  Heads up: "+w, m.width-4)) + "\n")
	}
	return b.String(), hint("enter", "create it", "esc", "go back")
}

func (m *model) createdView() (body, footer string) {
	r := m.created
	var b strings.Builder
	b.WriteString(okSty.Bold(true).Render(wrap(fmt.Sprintf("All done! %s is ready in Prism.", r.Instance.Name), m.width-4)) + "\n\n")
	b.WriteString(m.bullet(num(int64(r.Files)) + " files installed."))
	b.WriteString(m.customModsSummary(r.CustomMods, r.CustomErr))
	b.WriteString(m.bullet("If Prism is already open and doesn't show it, restart Prism."))
	for _, w := range m.warns {
		b.WriteString("\n" + warnSty.Render(wrap("  Heads up: "+w, m.width-4)))
	}
	b.WriteString("\nYou can start the game from Prism now. Have fun!")
	return b.String(), hint("enter", "exit")
}

func (m *model) errorView() (body, footer string) {
	var b strings.Builder
	b.WriteString(badSty.Render("Something went wrong") + "\n\n")
	b.WriteString(wrap(m.err.Error(), m.width-4) + "\n\n")
	var leftover *update.LeftoverError
	switch {
	case m.creating && (m.errPhase == scPreparing || m.errPhase == scApplying) && errors.As(m.err, &leftover):
		// The error itself says the folder is still there and what to do.
	case m.creating && m.errPhase == scPreparing:
		b.WriteString(okSty.Render(wrap("Nothing was created.", m.width-4)) + "\n\n")
	case m.creating && m.errPhase == scApplying:
		b.WriteString(okSty.Render(wrap("I removed the half-made instance, so there's nothing to clean up.", m.width-4)) + "\n\n")
	case m.errPhase == scPreparing:
		b.WriteString(okSty.Render(wrap("Nothing in your instance was changed.", m.width-4)) + "\n\n")
	case m.errPhase == scApplying && strings.Contains(m.err.Error(), "rolled back"):
		b.WriteString(okSty.Render(wrap("Everything was put back the way it was, so your instance is exactly as before.", m.width-4)) + "\n\n")
	case m.errPhase == scApplying:
		b.WriteString(badSty.Render(wrap("Some files may have changed. The originals are in the .gtnh-updater folder inside the instance.", m.width-4)) + "\n\n")
	}
	if m.canGoBack() {
		return b.String(), hint("esc", "go back", "enter", "exit")
	}
	return b.String(), hint("enter", "exit")
}

// canGoBack reports whether the error screen may return to a list: not after a failed
// apply (the player must read what happened) and not before anything was loaded.
func (m *model) canGoBack() bool {
	return m.manifest != nil && m.errPhase != scApplying && m.errPhase != scLoading
}

// goBackFromError returns to the version list after a failed download or self-update,
// otherwise to the instance list (e.g. the picked instance was running).
func (m *model) goBackFromError() (tea.Model, tea.Cmd) {
	if (m.inst.Dir != "" || m.creating) && (m.errPhase == scPreparing || m.errPhase == scSelfUpdate) {
		return m.showTargets()
	}
	return m.showInstances()
}

// bullet renders "  • text" with wrapped lines indented under the text.
func (m *model) bullet(s string) string {
	lines := strings.Split(wrap(s, m.width-8), "\n")
	for i, l := range lines {
		prefix := "    "
		if i == 0 {
			prefix = "  • "
		}
		lines[i] = prefix + strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

// ---- formatting helpers ----

func ago(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	days := int(d.Hours() / 24)
	switch {
	case d < 0:
		return "just now"
	case days == 0 && now.YearDay() == t.YearDay():
		return "today"
	case days <= 1:
		return "yesterday"
	case days < 14:
		return fmt.Sprintf("%d days ago", days)
	case days < 60:
		return fmt.Sprintf("%d weeks ago", days/7)
	case days < 365:
		return fmt.Sprintf("%d months ago", days/30)
	case days < 730:
		return "a year ago"
	}
	return fmt.Sprintf("%d years ago", days/365)
}

func eta(done, total int64, elapsed time.Duration) string {
	if done <= 0 || total <= 0 || done >= total || elapsed < 2*time.Second {
		return ""
	}
	left := time.Duration(float64(elapsed) * float64(total-done) / float64(done))
	switch {
	case left < 10*time.Second:
		return "almost done"
	case left < time.Minute:
		return fmt.Sprintf("about %d seconds left", int(left.Seconds()+5)/10*10)
	case left < 2*time.Minute:
		return "about a minute left"
	}
	return fmt.Sprintf("about %d minutes left", int(left.Minutes()+0.5))
}

func mb(n int64) string {
	if n < 1<<30 {
		return fmt.Sprintf("%d MB", n/1_000_000)
	}
	return fmt.Sprintf("%.1f GB", float64(n)/1e9)
}

func num(n int64) string {
	s := fmt.Sprint(n)
	var out []byte
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func hostOf(u string) string {
	u = strings.TrimPrefix(u, "https://")
	if i := strings.IndexByte(u, '/'); i >= 0 {
		return u[:i]
	}
	return u
}

// indentWrap wraps s to width and indents the continuation lines by indent spaces.
func indentWrap(s string, width, indent int) string {
	return strings.ReplaceAll(strings.TrimRight(wrap(s, width), " "), "\n", "\n"+strings.Repeat(" ", indent))
}

func wrap(s string, width int) string {
	if width < 20 {
		return s
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}
