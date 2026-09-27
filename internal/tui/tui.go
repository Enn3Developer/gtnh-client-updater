// Package tui is the interactive bubbletea front end: pick an instance, pick a version,
// review what will happen, apply. It talks to players, not developers: plain sentences,
// no jargon, and always a clear next key.
package tui

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
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
	UpdateCheck bool // look for a newer gtnh-update on GitHub
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
	scPreparing
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
	pageSty   = lipgloss.NewStyle().Padding(1, 2)
)

var (
	keyNotMine   = key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "wrong version?"))
	keyServer    = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "server mods"))
	keyOther     = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	keySelfUpd   = key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "update me"))
	keyShowOther = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all instances"))
)

type item struct{ title, desc, key string }

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title + " " + i.desc }

type model struct {
	cfg  Config
	send func(tea.Msg)

	screen  screen
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
	return &model{
		cfg: cfg, spin: sp, input: ti, width: 80, height: 24,
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
	if len(insts) == 0 {
		return errMsg{fmt.Errorf("Prism Launcher is installed, but it has no instances yet.\n\n"+
			"Install GTNH in Prism first (looked in %s).", strings.Join(m.cfg.PrismDirs, ", "))}
	}
	return loadedMsg{man, insts}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.bar.Width = max(min(msg.Width-8, 64), 10)
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
		return m, nil
	case appliedMsg:
		m.result, m.screen = msg.r, scDone
		return m, nil
	case selfDoneMsg:
		m.screen = scSelfUpdated
		return m, nil
	case errMsg:
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
	return m.screen == scInstance || m.screen == scInstalled || m.screen == scTarget
}

func (m *model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m.quit()
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
			if m.list.FilterState() == list.Unfiltered && m.screen != scInstance {
				return m.showInstances()
			}
		case "i":
			if m.screen == scTarget {
				return m.showInstalled()
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
	case scConfirm:
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
			m.session.Close()
			m.session = nil
			return m.showTargets()
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

func (m *model) quit() (tea.Model, tea.Cmd) {
	if m.screen == scApplying || m.screen == scSelfUpdate {
		return m, nil // never abandon a half-applied update; rollback needs this process
	}
	if m.session != nil {
		m.session.Close()
	}
	m.quitting = true
	return m, tea.Quit
}

func (m *model) afterLoad() (tea.Model, tea.Cmd) {
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
		return m.prepare()
	}
	return m, nil
}

func (m *model) pickInstance(in prism.Instance) (tea.Model, tea.Cmd) {
	m.inst, m.target = in, ""
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

func (m *model) listWidth() int { return max(m.width-4, 20) }
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
	l := list.New(items, d, m.listWidth(), m.listHeight())
	l.Title = title
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
		if m.newer != nil {
			out = append(out, keySelfUpd)
		}
		return out
	}
	m.list, m.screen, m.hasList = l, sc, true
	return m, nil
}

func (m *model) showInstances() (tea.Model, tea.Cmd) {
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
	var help []key.Binding
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
		return m.prepare()
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
	if len(m.insts) > 1 {
		help = append(help, keyOther)
	}
	return m.showList(scTarget, title, items, sel, help...)
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
	m.input.Width = max(min(m.width-10, 70), 20)
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
	var body string
	switch m.screen {
	case scInstance, scInstalled, scTarget:
		return lipgloss.NewStyle().Padding(1, 2, 0, 1).Render("   " + m.header() + m.banner() + m.list.View())
	case scLoading:
		body = m.spin.View() + " Looking for your GTNH instances…"
	case scServerMods:
		body = m.serverModsView()
	case scPreparing, scApplying, scSelfUpdate:
		body = m.busyView()
	case scConfirm:
		body = m.confirmView()
	case scDone:
		body = m.doneView()
	case scError:
		body = m.errorView()
	case scSelfUpdated:
		body = okSty.Bold(true).Render("gtnh-update is now version "+m.newer.Version+".") +
			"\n\n" + hint("enter", "restart it now", "q", "quit")
	}
	return pageSty.Render(m.header() + body)
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

func hint(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keySty.Render(pairs[i])+" "+dimSty.Render(pairs[i+1]))
	}
	return strings.Join(parts, "    ")
}

func (m *model) serverModsView() string {
	var b strings.Builder
	b.WriteString(titleSty.Render("Does your server have its own extra mods?") + "\n\n")
	b.WriteString(wrap("Some servers add a few mods on top of GTNH. If the server owner gave you a link for "+
		"them, paste it here — I'll install them now and keep them in sync every time you update.", m.width-6) + "\n\n")
	b.WriteString(m.input.View() + "\n")
	if m.inputEr != "" {
		b.WriteString(badSty.Render(m.inputEr) + "\n")
	}
	b.WriteString("\n" + dimSty.Render("No link? Leave it empty — you can add one later with m on the version list.") + "\n\n")
	b.WriteString(hint("enter", "continue", "esc", "back"))
	return b.String()
}

func (m *model) busyView() string {
	var b strings.Builder
	switch m.screen {
	case scApplying:
		b.WriteString(titleSty.Render(fmt.Sprintf("Updating %s to GTNH %s", m.inst.Name, m.target)) + "\n\n")
	case scSelfUpdate:
		b.WriteString(titleSty.Render("Updating gtnh-update itself") + "\n\n")
	default:
		b.WriteString(titleSty.Render(fmt.Sprintf("Getting GTNH %s ready for %s", m.target, m.inst.Name)) + "\n\n")
	}
	for _, s := range m.steps {
		b.WriteString(okSty.Render("  ✓ ") + dimSty.Render(s) + "\n")
	}
	if m.step != "" {
		b.WriteString("  " + m.spin.View() + m.step + "\n")
	}
	if m.total > 0 {
		pct := float64(m.done) / float64(m.total)
		b.WriteString("\n    " + m.bar.ViewAs(pct) + "\n    " + dimSty.Render(m.progressDetail()) + "\n")
	}
	for _, w := range m.warns {
		b.WriteString("\n" + warnSty.Render("  Heads up: "+w))
	}
	if m.screen != scPreparing {
		b.WriteString("\n\n" + warnSty.Render("  Please don't close this window until I'm done."))
	}
	return b.String()
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

func (m *model) confirmView() string {
	s, pl := m.session, m.session.Plan
	var b strings.Builder
	if m.target == m.detect.Version {
		b.WriteString(titleSty.Render(fmt.Sprintf("Ready to refresh %s on GTNH %s", m.inst.Name, m.target)) + "\n\n")
	} else {
		b.WriteString(titleSty.Render(fmt.Sprintf("Ready to update %s from %s to %s", m.inst.Name, m.detect.Version, m.target)) + "\n\n")
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
		b.WriteString(badSty.Render(wrap("! "+w, m.width-6)) + "\n\n")
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
	if n := pl.Count(update.Conflict); n > 0 {
		bullet(fmt.Sprintf("%d config %s changed both on your side and in the new version. Yours stay; the new "+
			"%s saved next to %s with .mcnew at the end, so you can compare.",
			n, plural(n, "file was", "files were"), plural(n, "one is", "ones are"), plural(n, "it", "them")))
	}
	if len(pl.ExtraMods) > 0 {
		bullet(fmt.Sprintf("Mods you added yourself stay: %s. Make sure they work with %s.",
			clip(strings.Join(pl.ExtraMods, ", "), 120), m.target))
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
		b.WriteString("\n" + warnSty.Render(wrap("  Heads up: "+w, m.width-6)) + "\n")
	}
	b.WriteString("\n" + hint("enter", "update now", "esc", "go back"))
	return b.String()
}

func (m *model) doneView() string {
	r, pl := m.result, m.session.Plan
	var b strings.Builder
	b.WriteString(okSty.Bold(true).Render(fmt.Sprintf("All done! %s is now on GTNH %s.", m.inst.Name, r.To)) + "\n\n")
	bullet := func(s string) { b.WriteString(m.bullet(s)) }
	if n := pl.Count(update.Install) + pl.Count(update.Remove); n == 0 {
		bullet("Your GTNH files were already up to date.")
	} else {
		bullet(fmt.Sprintf("%s files updated, %s removed.", num(int64(pl.Count(update.Install))), num(int64(pl.Count(update.Remove)))))
	}
	if r.Renamed != "" {
		bullet("Renamed the instance in Prism to \"" + r.Renamed + "\".")
	}
	if cm := r.CustomMods; cm != nil {
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
	if r.CustomErr != nil {
		b.WriteString(warnSty.Render(m.bullet("Your server's extra mods couldn't be synced this time (" + r.CustomErr.Error() + "). Run me again later to retry.")))
	}
	if c := pl.Conflicts(); len(c) > 0 {
		b.WriteString("\n" + wrap(fmt.Sprintf("%d config %s changed on your side and in the new version. "+
			"I kept yours and saved the new %s next to %s as .mcnew. It's usually fine to ignore this — "+
			"many mods rewrite their own config when the game starts.",
			len(c), plural(len(c), "file was", "files were"), plural(len(c), "one", "ones"), plural(len(c), "it", "them")), m.width-6) + "\n")
		sort.Strings(c)
		limit := max(m.height-24, 3)
		for i, p := range c {
			if i == limit {
				b.WriteString(dimSty.Render(fmt.Sprintf("    … and %d more", len(c)-limit)) + "\n")
				break
			}
			b.WriteString(dimSty.Render("    "+strings.TrimPrefix(p, ".minecraft/")) + "\n")
		}
	}
	if r.BackupDir != "" {
		b.WriteString("\n" + dimSty.Render(wrap("If something's wrong, the old files are in "+r.BackupDir, m.width-6)) + "\n")
	}
	for _, w := range m.warns {
		b.WriteString("\n" + warnSty.Render(wrap("  Heads up: "+w, m.width-6)))
	}
	b.WriteString("\nYou can start the game from Prism now. Have fun!\n\n")
	b.WriteString(hint("enter", "exit"))
	return b.String()
}

func (m *model) errorView() string {
	var b strings.Builder
	b.WriteString(badSty.Render("Something went wrong") + "\n\n")
	b.WriteString(wrap(m.err.Error(), m.width-6) + "\n\n")
	switch {
	case m.errPhase == scPreparing:
		b.WriteString(okSty.Render("Nothing in your instance was changed.") + "\n\n")
	case m.errPhase == scApplying && strings.Contains(m.err.Error(), "rolled back"):
		b.WriteString(okSty.Render("Everything was put back the way it was, so your instance is exactly as before.") + "\n\n")
	case m.errPhase == scApplying:
		b.WriteString(badSty.Render("Some files may have changed. The originals are in the .gtnh-updater folder inside the instance.") + "\n\n")
	}
	if m.canGoBack() {
		b.WriteString(hint("esc", "go back", "enter", "exit"))
	} else {
		b.WriteString(hint("enter", "exit"))
	}
	return b.String()
}

// canGoBack reports whether the error screen may return to a list: not after a failed
// apply (the player must read what happened) and not before anything was loaded.
func (m *model) canGoBack() bool {
	return m.manifest != nil && m.errPhase != scApplying && m.errPhase != scLoading
}

// goBackFromError returns to the version list after a failed download or self-update,
// otherwise to the instance list (e.g. the picked instance was running).
func (m *model) goBackFromError() (tea.Model, tea.Cmd) {
	if m.inst.Dir != "" && (m.errPhase == scPreparing || m.errPhase == scSelfUpdate) {
		return m.showTargets()
	}
	return m.showInstances()
}

// bullet renders "  • text" with wrapped lines indented under the text.
func (m *model) bullet(s string) string {
	lines := strings.Split(wrap(s, m.width-10), "\n")
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

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func wrap(s string, width int) string {
	if width < 20 {
		return s
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}
