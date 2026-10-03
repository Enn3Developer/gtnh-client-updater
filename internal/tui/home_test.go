package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Tests for the launcher home screen (home spec C1-C5, C12). Clause numbers in the
// comments refer to the home spec.

// ---- fixtures ----

type findCall struct{ dataDir, override string }

type launchCall struct {
	l       prism.Launcher
	dataDir string
	inst    prism.Instance
	server  string
}

// fakePrism stands in for Prism Launcher: it records what the model asked of it.
type fakePrism struct {
	finds     []findCall
	launches  []launchCall
	findErr   error
	launchErr error
}

var testLauncher = prism.Launcher{Exe: "prism-test", Kind: "custom"}

func (f *fakePrism) install(m *model) {
	m.findLauncher = func(dataDir, override string) (prism.Launcher, error) {
		f.finds = append(f.finds, findCall{dataDir, override})
		if f.findErr != nil {
			return prism.Launcher{}, f.findErr
		}
		return testLauncher, nil
	}
	m.launch = func(l prism.Launcher, dataDir string, inst prism.Instance, server string) error {
		f.launches = append(f.launches, launchCall{l, dataDir, inst, server})
		return f.launchErr
	}
}

// home is a model with two Prism data dirs; both GTNH instances live in the second one.
// older (2.8.1) comes first in m.insts, newest (2.8.4, the newest stable) second.
type home struct {
	m          *model
	prism      *fakePrism
	dir1, dir2 string
	older      prism.Instance
	newest     prism.Instance
}

func gtnhInstance(t *testing.T, dir, name string, st update.State) prism.Instance {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := update.SaveState(dir, &st); err != nil {
		t.Fatal(err)
	}
	return prism.Instance{Dir: dir, Name: name, GTNH: true}
}

func newHome(t *testing.T, width int, olderState update.State) *home {
	t.Helper()
	m := sizedModel(t)
	m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	h := &home{m: m, prism: &fakePrism{}, dir1: t.TempDir(), dir2: t.TempDir()}
	m.cfg.PrismDirs = []string{h.dir1, h.dir2}
	m.manifest = routeManifest(t)
	olderState.Version = "2.8.1"
	h.older = gtnhInstance(t, filepath.Join(h.dir2, "instances", "Older"), "Older", olderState)
	h.newest = gtnhInstance(t, filepath.Join(h.dir2, "instances", "Newest"), "Newest", update.State{Version: "2.8.4"})
	m.insts = []prism.Instance{h.older, h.newest}
	h.prism.install(m)
	return h
}

// msgsOf runs cmd and returns its messages, unpacking batches.
func msgsOf(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, msgsOf(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// msgOf returns the first message of type T that cmd produces.
func msgOf[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	msgs := msgsOf(cmd)
	for _, msg := range msgs {
		if v, ok := msg.(T); ok {
			return v
		}
	}
	var zero T
	t.Fatalf("command produced %#v, want a %T", msgs, zero)
	return zero
}

func view(m *model) string { return words(m.View()) }

func itemDescs(m *model) map[string]string {
	out := map[string]string{}
	for _, it := range m.list.Items() {
		out[it.(item).key] = it.(item).desc
	}
	return out
}

// ---- C1 header ----

func TestHeaderSaysGTNHLauncherAndVersion(t *testing.T) { // C1
	m := sizedModel(t)
	if got := words(m.header()); got != "GTNH Launcher 1.2.3" {
		t.Errorf("header = %q, want %q", got, "GTNH Launcher 1.2.3")
	}
}

// ---- C2 after load ----

func TestLoadWithOneGTNHInstanceLandsOnHome(t *testing.T) { // C2
	m := sizedModel(t)
	inst := prism.Instance{Dir: t.TempDir(), Name: "Pack", GTNH: true}
	press(m, loadedMsg{routeManifest(t), []prism.Instance{inst}})
	if m.screen != scHome || selectedKey(m) != inst.Dir {
		t.Errorf("loaded with one GTNH instance: screen %d, selected %q; want scHome (%d), %q", m.screen, selectedKey(m), scHome, inst.Dir)
	}
}

func TestLoadSelectsFirstGTNHInstanceAndHidesOthers(t *testing.T) { // C2, C3
	m := sizedModel(t)
	other := prism.Instance{Dir: t.TempDir(), Name: "Vanilla"}
	a := prism.Instance{Dir: t.TempDir(), Name: "Alpha", GTNH: true}
	b := prism.Instance{Dir: t.TempDir(), Name: "Beta", GTNH: true}
	press(m, loadedMsg{routeManifest(t), []prism.Instance{other, a, b}})
	if m.screen != scHome || m.list.Title != "Which instance do you want to play?" || selectedKey(m) != a.Dir || m.showAll || len(m.list.Items()) != 2 {
		t.Errorf("loaded with Vanilla, Alpha, Beta: screen %d, title %q, selected %q, showAll %v, %d items; want scHome, the home title, Alpha, false, 2",
			m.screen, m.list.Title, selectedKey(m), m.showAll, len(m.list.Items()))
	}
}

func TestLoadWithInstanceFlagSelectsItOnHome(t *testing.T) { // C2
	m := sizedModel(t)
	a := prism.Instance{Dir: t.TempDir(), Name: "Alpha", GTNH: true}
	b := prism.Instance{Dir: t.TempDir(), Name: "Beta", GTNH: true}
	m.cfg.Instance = "Beta"
	press(m, loadedMsg{routeManifest(t), []prism.Instance{a, b}})
	if m.screen != scHome || selectedKey(m) != b.Dir {
		t.Errorf("-instance Beta: screen %d, selected %q; want scHome (%d), %q", m.screen, selectedKey(m), scHome, b.Dir)
	}
}

func TestLoadWithPlayAndInstanceStartsIt(t *testing.T) { // C2, C6
	h := newHome(t, termW, update.State{})
	m := h.m
	m.cfg.Instance, m.cfg.Play = "Newest", true
	cmd := press(m, loadedMsg{routeManifest(t), m.insts})
	launched := msgOf[launchedMsg](t, cmd)
	if m.screen != scLaunching || len(h.prism.launches) != 1 || h.prism.launches[0].inst.Dir != h.newest.Dir {
		t.Errorf("-play -instance Newest: screen %d, launches %+v, msg %#v; want scLaunching (%d), Newest launched",
			m.screen, h.prism.launches, launched, scLaunching)
	}
}

func TestLoadWithPlayButNoInstanceLandsOnHome(t *testing.T) { // C2
	h := newHome(t, termW, update.State{})
	m := h.m
	m.cfg.Play = true
	press(m, loadedMsg{routeManifest(t), []prism.Instance{h.older}})
	if m.screen != scHome || len(h.prism.finds) != 0 || len(h.prism.launches) != 0 {
		t.Errorf("-play without -instance: screen %d, finds %d, launches %d; want scHome (%d), nothing started",
			m.screen, len(h.prism.finds), len(h.prism.launches), scHome)
	}
}

// ---- C3 home list ----

func TestHomeListTitleAndDescriptions(t *testing.T) { // C3, C14
	h := newHome(t, 100, update.State{})
	other := prism.Instance{Dir: t.TempDir(), Name: "Vanilla"}
	h.m.insts = append(h.m.insts, other)
	h.m.showAll = true
	h.m.showHome()
	want := map[string]string{
		h.older.Dir:  "GTNH 2.8.1 · never played · update available",
		h.newest.Dir: "GTNH 2.8.4 · never played · up to date",
		other.Dir:    "not a GTNH instance · never played",
	}
	got := itemDescs(h.m)
	if h.m.list.Title != "Which instance do you want to play?" || got[h.older.Dir] != want[h.older.Dir] ||
		got[h.newest.Dir] != want[h.newest.Dir] || got[other.Dir] != want[other.Dir] || len(got) != 3 {
		t.Errorf("home list: title %q, descs %q; want %q, %q", h.m.list.Title, got, "Which instance do you want to play?", want)
	}
	items := h.m.list.Items()
	if items[0].(item).title != "Older" || items[1].(item).title != "Newest" || items[2].(item).title != "Vanilla" {
		t.Errorf("home item titles = %#v, want the instance names Older, Newest, Vanilla", items)
	}
}

func TestHomeListDescSaysWhenPlayed(t *testing.T) { // C3
	h := newHome(t, termW, update.State{})
	h.m.insts[0].LastLaunch = time.Now().Add(-3 * 24 * time.Hour)
	h.m.showHome()
	if got, want := itemDescs(h.m)[h.older.Dir], "GTNH 2.8.1 · played 3 days ago · update available"; got != want {
		t.Errorf("played desc = %q, want %q", got, want)
	}
}

func TestHomeJAndKDoNotMoveTheCursor(t *testing.T) { // C3
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	press(h.m, runes("j"))
	afterJ := selectedKey(h.m)
	press(h.m, keyDown, runes("k"))
	afterK := selectedKey(h.m)
	if afterJ != h.older.Dir || afterK != h.newest.Dir {
		t.Errorf("j then down, k: selected %q then %q; want Older then Newest (only arrows move)", afterJ, afterK)
	}
}

// ---- C4 card ----

func TestHomeCardAtWidth100ShowsSelectedInstance(t *testing.T) { // C4, C14
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	v := view(h.m)
	for _, want := range []string{"Installed GTNH 2.8.1", "Update 2.8.4 is out — press u", "Played never", "Server none set", "Server mods none", "Pack Java 8"} {
		if !strings.Contains(v, want) {
			t.Errorf("home view at 100 columns lacks %q:\n%s", want, ansi.Strip(h.m.View()))
		}
	}
}

func TestHomeCardSaysUpToDateForNewestStable(t *testing.T) { // C4
	h := newHome(t, 100, update.State{})
	h.m.inst = h.newest
	h.m.showHome()
	if v := view(h.m); !strings.Contains(v, "Installed GTNH 2.8.4") || !strings.Contains(v, "Update up to date") || strings.Contains(v, "is out") {
		t.Errorf("card for Newest = %q, want Installed GTNH 2.8.4 and Update up to date", v)
	}
}

func TestHomeCardShowsServerAndServerModsHost(t *testing.T) { // C4
	h := newHome(t, 100, update.State{ServerAddress: "mc.x:1", CustomModsURL: "https://mods.example.com/a/custom.zip", CustomModsAsked: true})
	h.m.insts[0].LastLaunch = time.Now().Add(-3 * 24 * time.Hour)
	h.m.showHome()
	v := view(h.m)
	for _, want := range []string{"Server mc.x:1 — j joins it", "Server mods mods.example.com", "Played 3 days ago"} {
		if !strings.Contains(v, want) {
			t.Errorf("home view lacks %q:\n%s", want, ansi.Strip(h.m.View()))
		}
	}
}

func TestHomeCardOfOtherInstanceHasNoUpdateOrPack(t *testing.T) { // C4
	h := newHome(t, 100, update.State{})
	other := prism.Instance{Dir: t.TempDir(), Name: "Vanilla"}
	h.m.insts = append(h.m.insts, other)
	h.m.showAll, h.m.inst = true, other
	h.m.showHome()
	if v := view(h.m); !strings.Contains(v, "Update —") || strings.Contains(v, "Pack Java") {
		t.Errorf("card for a non-GTNH instance = %q, want \"Update —\" and no Pack line", v)
	}
}

func TestHomeCardLabelsArePadded(t *testing.T) { // C4
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	v := ansi.Strip(h.m.View())
	for _, want := range []string{"Installed  GTNH 2.8.1", "Update     2.8.4 is out — press u", "Played     never", "Server     none set", "Server mods  none", "Pack       Java 8"} {
		if !strings.Contains(v, want) {
			t.Errorf("home view lacks the padded line %q:\n%s", want, v)
		}
	}
}

func TestHomeCardIsComputedWhenHomeIsShownNotInView(t *testing.T) { // C4
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	if err := update.SaveState(h.older.Dir, &update.State{Version: "2.8.4"}); err != nil {
		t.Fatal(err)
	}
	if v := view(h.m); !strings.Contains(v, "Update 2.8.4 is out — press u") {
		t.Errorf("card after the state changed on disk = %q, want the values from showHome", v)
	}
}

func TestHomeCardNeedsNinetyColumns(t *testing.T) { // C4, C14
	at90 := newHome(t, 90, update.State{})
	at90.m.showHome()
	at89 := newHome(t, 89, update.State{})
	at89.m.showHome()
	at80 := newHome(t, 80, update.State{})
	at80.m.showHome()
	if !strings.Contains(view(at90.m), "Installed") || strings.Contains(view(at89.m), "Installed") || strings.Contains(view(at80.m), "Installed") {
		t.Errorf("card shown at 90/89/80 columns: %v/%v/%v; want true/false/false",
			strings.Contains(view(at90.m), "Installed"), strings.Contains(view(at89.m), "Installed"), strings.Contains(view(at80.m), "Installed"))
	}
}

// C4: list (width-40) + card (36) leave the view inside the terminal; without the card
// the list is width-4 as on every list screen.
func TestHomeViewFitsTerminalWithAndWithoutCard(t *testing.T) { // C4
	longName := strings.Repeat("Long name ", 20) // wider than any terminal here: the list clips it
	wide := newHome(t, 100, update.State{ServerAddress: "mc.x:1"})
	wide.m.insts[0].Name = longName
	wide.m.showHome()
	narrow := newHome(t, 80, update.State{ServerAddress: "mc.x:1"})
	narrow.m.insts[0].Name = longName
	narrow.m.showHome()
	_, wideW := fit(wide.m.View())
	_, narrowW := fit(narrow.m.View())
	if wideW > 100 || narrowW > 80 {
		t.Errorf("home view widths: %d at 100 columns, %d at 80; want <= 100 and <= 80", wideW, narrowW)
	}
}

// ---- C5 home keys and help ----

func TestHomeHelpLineBasic(t *testing.T) { // C5
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	if v := view(h.m); !strings.Contains(v, "enter play u update s settings b undo n new q quit") || strings.Contains(v, "j join") || strings.Contains(v, "a all") {
		t.Errorf("home view %q: want the help \"enter play u update s settings b undo n new q quit\" without j or a", v)
	}
}

func TestHomeHelpLineOffersJoinWithServer(t *testing.T) { // C5
	h := newHome(t, termW, update.State{ServerAddress: "mc.x:1"})
	h.m.showHome()
	if v := view(h.m); !strings.Contains(v, "enter play j join u update s settings b undo n new q quit") {
		t.Errorf("home view %q: want the help with j join", v)
	}
}

func TestHomeHelpLineOffersAllAndNewVersion(t *testing.T) { // C5
	h := newHome(t, termW, update.State{})
	h.m.insts = append(h.m.insts, prism.Instance{Dir: t.TempDir(), Name: "Vanilla"})
	h.m.newer = &selfupdate.Release{Version: "9.9.9"}
	h.m.showHome()
	if v := view(h.m); !strings.Contains(v, "enter play u update s settings b undo n new a all q quit v new version") {
		t.Errorf("home view %q: want the help with a all and v new version", v)
	}
}

func TestHomeEnterLaunchesSelectedInstance(t *testing.T) { // C5, C6, C14
	h := newHome(t, termW, update.State{})
	h.m.app.PrismExe = "/opt/prism/prismlauncher"
	h.m.showHome()
	cmd := press(h.m, keyEnter)
	if h.m.screen != scLaunching || cmd == nil {
		t.Fatalf("enter on home: screen %d, cmd nil %v; want scLaunching (%d) with a cmd", h.m.screen, cmd == nil, scLaunching)
	}
	msgOf[launchedMsg](t, cmd)
	want := launchCall{testLauncher, h.dir2, h.older, ""}
	if len(h.prism.launches) != 1 || h.prism.launches[0].dataDir != want.dataDir || h.prism.launches[0].inst.Dir != want.inst.Dir ||
		h.prism.launches[0].server != "" || h.prism.launches[0].l.Exe != testLauncher.Exe {
		t.Errorf("launches = %+v, want one %+v", h.prism.launches, want)
	}
	if len(h.prism.finds) != 1 || h.prism.finds[0] != (findCall{h.dir2, "/opt/prism/prismlauncher"}) {
		t.Errorf("findLauncher calls = %+v, want one with (%q, the PrismExe setting)", h.prism.finds, h.dir2)
	}
}

func TestHomePLaunchesLikeEnter(t *testing.T) { // C5
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	press(h.m, keyDown)
	msgOf[launchedMsg](t, press(h.m, runes("p")))
	if h.m.screen != scLaunching || len(h.prism.launches) != 1 || h.prism.launches[0].inst.Dir != h.newest.Dir || h.m.inst.Dir != h.newest.Dir {
		t.Errorf("p on Newest: screen %d, launches %+v; want scLaunching, Newest", h.m.screen, h.prism.launches)
	}
}

func TestHomeJJoinsSavedServer(t *testing.T) { // C5, C6, C14
	h := newHome(t, termW, update.State{ServerAddress: "mc.example.com:25565"})
	h.m.showHome()
	msgOf[launchedMsg](t, press(h.m, runes("j")))
	if h.m.screen != scLaunching || len(h.prism.launches) != 1 || h.prism.launches[0].server != "mc.example.com:25565" || h.prism.launches[0].inst.Dir != h.older.Dir {
		t.Errorf("j with a server: screen %d, launches %+v; want scLaunching, Older with server mc.example.com:25565", h.m.screen, h.prism.launches)
	}
}

func TestHomeJWithoutServerIsIgnored(t *testing.T) { // C5, C14, K1
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	cmd := press(h.m, runes("j"))
	if h.m.screen != scHome || cmd != nil || len(h.prism.finds)+len(h.prism.launches) != 0 || selectedKey(h.m) != h.older.Dir {
		t.Errorf("j without a server: screen %d, cmd nil %v, finds %d, launches %d, selected %q; want scHome, nil, 0, 0, Older",
			h.m.screen, cmd == nil, len(h.prism.finds), len(h.prism.launches), selectedKey(h.m))
	}
}

func TestHomeUGoesToVersionList(t *testing.T) { // C5
	h := newHome(t, termW, update.State{})
	h.m.serverModsAsked = true
	h.m.showHome()
	press(h.m, runes("u"))
	if h.m.screen != scTarget || h.m.inst.Dir != h.older.Dir || h.m.detect.Version != "2.8.1" {
		t.Errorf("u on Older: screen %d, inst %q, detected %q; want scTarget (%d), Older, 2.8.1", h.m.screen, h.m.inst.Name, h.m.detect.Version, scTarget)
	}
}

func TestHomeQQuits(t *testing.T) { // C5
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	cmd := press(h.m, runes("q"))
	if !h.m.quitting || !isQuit(cmd) {
		t.Errorf("q on home: quitting %v, quit cmd %v; want true, true", h.m.quitting, isQuit(cmd))
	}
}

func TestHomeVSelfUpdatesOnlyWithNewer(t *testing.T) { // C5, C14
	without := newHome(t, termW, update.State{})
	without.m.showHome()
	cmdWithout := press(without.m, runes("v"))
	with := newHome(t, termW, update.State{})
	with.m.newer = &selfupdate.Release{Version: "9.9.9"}
	with.m.showHome()
	cmdWith := press(with.m, runes("v"))
	if without.m.screen != scHome || cmdWithout != nil || with.m.screen != scSelfUpdate || cmdWith == nil {
		t.Errorf("v on home: without newer screen %d cmd nil %v, with newer screen %d cmd nil %v; want scHome nil, scSelfUpdate a cmd",
			without.m.screen, cmdWithout == nil, with.m.screen, cmdWith == nil)
	}
}

func TestHomeKeysGoToFilterWhileTyping(t *testing.T) { // C5
	h := newHome(t, termW, update.State{ServerAddress: "mc.x:1"})
	h.m.showHome()
	press(h.m, runes("/"), runes("p"), runes("j"), runes("q"))
	screen, quitting, filter, finds := h.m.screen, h.m.quitting, h.m.list.FilterValue(), len(h.prism.finds)
	press(h.m, keyEsc, runes("p")) // filter closed: p plays again
	if screen != scHome || quitting || filter != "pjq" || finds != 0 {
		t.Errorf("typing pjq into the filter: screen %d, quitting %v, filter %q, finds %d; want scHome, false, \"pjq\", 0",
			screen, quitting, filter, finds)
	}
	if h.m.screen != scLaunching {
		t.Errorf("p after closing the filter: screen %d, want scLaunching (%d)", h.m.screen, scLaunching)
	}
}

// ---- C12 is the game running? ----

func TestPickInstanceWhenGameRunsShowsError(t *testing.T) { // C12, C14
	m := routeModel(t)
	var asked []prism.Instance
	m.isRunning = func(in prism.Instance) (bool, error) { asked = append(asked, in); return true, nil }
	in := prism.Instance{Dir: t.TempDir(), Name: "Other", GTNH: true}
	_, cmd := m.pickInstance(in)
	press(m, msgOf[errMsg](t, cmd))
	want := "Other is running right now. Close Minecraft, then start me again."
	if m.screen != scError || m.err.Error() != want || len(asked) != 1 || asked[0].Dir != in.Dir {
		t.Errorf("pickInstance while running: screen %d, err %v, asked %+v; want scError, %q, one call for Other", m.screen, m.err, asked, want)
	}
}

func TestPickInstanceWhenRunningIsUnknownWarnsOnConfirm(t *testing.T) { // C12, C14
	m := routeModel(t)
	m.isRunning = func(prism.Instance) (bool, error) { return false, errors.New("can't tell") }
	in := prism.Instance{Dir: t.TempDir(), Name: "Other", GTNH: true}
	writeState(t, in.Dir, update.State{Version: "2.8.4"})
	m.pickInstance(in)
	unknown := m.runUnknown
	m.screen, m.target = scConfirm, "2.9.0-RC-1"
	m.session = &update.Session{Plan: onePlan(), Flavor: manifest.Java17}
	if body := pageBody(m); !unknown || m.screen != scConfirm || !strings.Contains(body, "Make sure Minecraft is closed before you continue.") {
		t.Errorf("isRunning error: runUnknown %v, confirm body %q; want true and the close-Minecraft line", unknown, body)
	}
}

func TestPickInstanceResetsRunUnknown(t *testing.T) { // C12
	m := routeModel(t)
	m.runUnknown = true
	in := prism.Instance{Dir: t.TempDir(), Name: "Other", GTNH: true}
	writeState(t, in.Dir, update.State{Version: "2.8.4"})
	m.pickInstance(in)
	m.screen, m.target = scConfirm, "2.9.0-RC-1"
	m.session = &update.Session{Plan: onePlan(), Flavor: manifest.Java17}
	if body := pageBody(m); m.runUnknown || strings.Contains(body, "Make sure Minecraft is closed") {
		t.Errorf("isRunning ok after an unknown: runUnknown %v, body %q; want false, no close-Minecraft line", m.runUnknown, body)
	}
}

func TestConfirmCloseMinecraftLineOnlyWhenRunUnknown(t *testing.T) { // C12
	const line = "Make sure Minecraft is closed before you continue."
	build := func(unknown bool) string {
		m := goldenModel(t)
		m.screen, m.target = scConfirm, "2.8.4"
		m.session = &update.Session{Plan: onePlan(), Flavor: manifest.Java17}
		m.runUnknown = unknown
		return pageText(m)
	}
	withLine, without := build(true), build(false)
	if !strings.Contains(withLine, "just in case.\n\n  "+line+"\n") || strings.Contains(without, line) {
		t.Errorf("confirm page: runUnknown=true\n%s\nrunUnknown=false\n%s\nwant the line after the backup bullet only in the first", withLine, without)
	}
}
