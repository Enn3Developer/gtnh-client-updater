package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/appcfg"
	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Shared fixtures of the workspace tests: fake instances on disk, an in-memory manifest,
// fake launcher boundaries and key helpers.

const testAppVersion = "1.2.3"

const day = 24 * time.Hour

// testManifest is newest first: 2.8.4 (out 5 days ago) is the newest stable release.
func testManifest() *manifest.Manifest {
	now := time.Now()
	return &manifest.Manifest{Releases: []manifest.Release{
		{Version: "2.8.4", Title: "Stable release", ReleaseDate: now.Add(-5*day - time.Hour),
			Java17URL: "https://downloads.gtnewhorizons.com/a17.zip", Java8URL: "https://downloads.gtnewhorizons.com/a8.zip"},
		{Version: "2.8.1", Title: "Stable release", ReleaseDate: now.Add(-40 * day),
			Java17URL: "https://downloads.gtnewhorizons.com/b17.zip", Java8URL: "https://downloads.gtnewhorizons.com/b8.zip"},
		{Version: "2.8.0", Title: "Stable release", ReleaseDate: now.Add(-80 * day),
			Java17URL: "https://downloads.gtnewhorizons.com/c17.zip", Java8URL: "https://downloads.gtnewhorizons.com/c8.zip"},
	}}
}

// instSpec describes a fake Prism instance written under <dataDir>/instances/<name>.
type instSpec struct {
	name       string
	gtnh       bool
	version    string // state.json version; "" = unknown
	server     string
	mods       string
	modsAsked  bool // answered the server-mods question (implied by mods)
	backup     *update.BackupInfo
	java17     bool
	noCfg      bool   // no instance.cfg: settings unreadable
	cfg        string // extra instance.cfg lines
	lastLaunch time.Time
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeInst(t *testing.T, dataDir string, s instSpec) prism.Instance {
	t.Helper()
	dir := filepath.Join(dataDir, "instances", s.name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !s.noCfg {
		writeFile(t, filepath.Join(dir, "instance.cfg"), "[General]\nname="+s.name+"\n"+s.cfg)
	}
	if s.java17 {
		writeFile(t, filepath.Join(dir, "mmc-pack.json"),
			`{"components":[{"uid":"net.minecraft","version":"1.7.10"},{"uid":"me.eigenraven.lwjgl3ify.forgepatches"}]}`)
	}
	if s.version != "" || s.server != "" || s.mods != "" || s.modsAsked {
		saveState(t, dir, s.version, s.server, s.mods)
		if s.modsAsked {
			if err := update.UpdateState(dir, func(st *update.State) { st.CustomModsAsked = true }); err != nil {
				t.Fatal(err)
			}
		}
	}
	if s.backup != nil {
		data, err := json.Marshal(s.backup)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, update.StateDir, "backup-20260101-000000", update.BackupManifest), string(data))
	}
	return prism.Instance{Dir: dir, Name: s.name, GameDir: filepath.Join(dir, ".minecraft"), GTNH: s.gtnh, LastLaunch: s.lastLaunch}
}

func saveState(t *testing.T, dir, version, server, mods string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, update.StateDir), 0o755); err != nil {
		t.Fatal(err)
	}
	st := &update.State{Version: version, ServerAddress: server, CustomModsURL: mods}
	if err := update.SaveState(dir, st); err != nil {
		t.Fatal(err)
	}
}

// fullSpec is a GTNH instance with everything: outdated (2.8.1, 2.8.4 is out), a server,
// server mods, an undoable backup, a memory override, Java 17, played 2 days ago.
func fullSpec(name string) instSpec {
	now := time.Now()
	return instSpec{
		name: name, gtnh: true, version: "2.8.1", java17: true,
		server: "play.example.org", mods: "https://mods.example.org/pack/extra.zip",
		backup:     &update.BackupInfo{From: "2.8.0", To: "2.8.1", When: now.Add(-3*day - time.Hour)},
		cfg:        "OverrideMemory=true\nMinMemAlloc=4096\nMaxMemAlloc=8192\n",
		lastLaunch: now.Add(-2*day - time.Hour),
	}
}

type launchCall struct {
	l       prism.Launcher
	dataDir string
	inst    prism.Instance
	server  string
}

// fakes records the launcher boundary calls of a model.
type fakes struct {
	findCalls [][2]string // dataDir, override
	findErr   error
	launches  []launchCall
	launchErr error
	running   bool
	runErr    error
	runChecks []prism.Instance
	saved     []appcfg.Config
	// the server-mods sync: what it was asked, what preparing gives, what applying does
	modsSyncs    []update.ModsSyncOptions
	modsPrepErr  error
	modsSync     *update.ModsSync // nil = one with an empty plan
	modsApplied  int
	modsApplyErr error
}

var fakeLauncher = prism.Launcher{Exe: "prism-fake", Kind: "custom"}

func newTestModel(cfg Config, width, height int) (*model, *fakes) {
	if cfg.AppVersion == "" {
		cfg.AppVersion = testAppVersion
	}
	f := &fakes{}
	m := newModel(cfg)
	m.findLauncher = func(dataDir, override string) (prism.Launcher, error) {
		f.findCalls = append(f.findCalls, [2]string{dataDir, override})
		if f.findErr != nil {
			return prism.Launcher{}, f.findErr
		}
		return fakeLauncher, nil
	}
	m.launch = func(l prism.Launcher, dataDir string, inst prism.Instance, server string) error {
		f.launches = append(f.launches, launchCall{l, dataDir, inst, server})
		return f.launchErr
	}
	m.isRunning = func(inst prism.Instance) (bool, error) {
		f.runChecks = append(f.runChecks, inst)
		return f.running, f.runErr
	}
	m.saveApp = func(c appcfg.Config) error {
		f.saved = append(f.saved, c)
		return nil
	}
	m.prepareMods = func(o update.ModsSyncOptions, _ update.Reporter) (*update.ModsSync, error) {
		f.modsSyncs = append(f.modsSyncs, o)
		if f.modsPrepErr != nil {
			return nil, f.modsPrepErr
		}
		if f.modsSync == nil {
			return &update.ModsSync{Plan: &update.ModsPlan{}}, nil
		}
		return f.modsSync, nil
	}
	m.applyMods = func(s *update.ModsSync, _ update.Reporter) (*update.ModsPlan, error) {
		f.modsApplied++
		if f.modsApplyErr != nil {
			return nil, f.modsApplyErr
		}
		return s.Plan, nil
	}
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m, f
}

// loadedModel is a model at width x height that has loaded insts with testManifest.
func loadedModel(dataDir string, width, height int, insts ...prism.Instance) (*model, *fakes) {
	m, f := newTestModel(Config{PrismDirs: []string{dataDir}}, width, height)
	m.Update(loadedMsg{m: testManifest(), insts: insts})
	return m, f
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// press sends keys through Update and returns the last command.
func press(m *model, keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.Update(keyMsg(k))
	}
	return cmd
}

var cmdType = reflect.TypeOf(tea.Cmd(nil))

// runCmd executes cmd and every command of the batches/sequences it yields, returning
// the plain messages.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	v := reflect.ValueOf(msg)
	if v.IsValid() && v.Kind() == reflect.Slice && v.Type().Elem() == cmdType {
		var out []tea.Msg
		for i := range v.Len() {
			out = append(out, runCmd(v.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func hasQuit(msgs []tea.Msg) bool {
	for _, msg := range msgs {
		if _, ok := msg.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

// screen is View() without styling, split into lines with trailing spaces trimmed.
func screen(m *model) []string {
	return trimLines(m.View())
}

func trimLines(s string) []string {
	lines := strings.Split(ansi.Strip(s), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

// pageLines is the page at width with room for everything.
func pageLines(m *model, width int) []string {
	return trimLines(m.pageView(width, 200))
}

func rowIDs(m *model) []string {
	var ids []string
	for _, r := range m.rows() {
		ids = append(ids, r.id)
	}
	return ids
}

func indexOf(ids []string, id string) int {
	for i, s := range ids {
		if s == id {
			return i
		}
	}
	return -1
}

// hinted is a row line as C4 fixes it: prefix + text, hint flush right at width.
func hinted(prefix, text, hint string, width int) string {
	s := prefix + text
	return s + strings.Repeat(" ", width-ansi.StringWidth(s)-ansi.StringWidth(hint)) + hint
}

// hintedAt is a page row line as C2 fixes it: prefix + text, hint starting at column col.
func hintedAt(prefix, text, hint string, col int) string {
	s := prefix + text
	return s + strings.Repeat(" ", col-ansi.StringWidth(s)) + hint
}

// hintColumn is the 0-based column where hint (the last occurrence) starts in line.
func hintColumn(line, hint string) int {
	i := strings.LastIndex(line, hint)
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(line[:i])
}

func containsLine(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func lineWith(lines []string, sub string) string {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

// flat collapses all whitespace runs of a (wrapped, padded) text to single spaces.
func flat(s string) string {
	return strings.Join(strings.Fields(ansi.Strip(s)), " ")
}
