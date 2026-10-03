package tui

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/appcfg"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Tests for Play: starting Prism, watching the game, and coming back home (home spec
// C6-C11). Clause numbers in the comments refer to the home spec.

const (
	notFoundText = "I couldn't find Prism Launcher on this computer. Start the game from Prism yourself, or tell me where Prism is in the settings."
	startingText = "Prism Launcher is starting the game…"
	slowText     = "I haven't seen the game start yet. Maybe Prism is asking you something — have a look at its window."
	keepsRunning = "Leave me open or press q — the game keeps running either way."
	closedText   = "The game closed. Have fun next time!"
	unknownText  = "The game should be starting from Prism now. I can't tell on this computer whether it's running."
)

// launching is a home whose Older instance was just started (screen scLaunching).
func launching(t *testing.T) *home {
	t.Helper()
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	press(h.m, keyEnter)
	if h.m.screen != scLaunching {
		t.Fatalf("setup: enter on home gave screen %d, want scLaunching (%d)", h.m.screen, scLaunching)
	}
	return h
}

// playing is launching after Prism started (screen scPlaying, the launcher stays open).
func playing(t *testing.T) *home {
	t.Helper()
	h := launching(t)
	press(h.m, launchedMsg{})
	if h.m.screen != scPlaying {
		t.Fatalf("setup: launchedMsg gave screen %d, want scPlaying (%d)", h.m.screen, scPlaying)
	}
	return h
}

// pollOnce delivers the current poll tick, runs the isRunning command it returns and
// feeds the answer back; it returns the command the model gives for that answer.
func pollOnce(t *testing.T, m *model) tea.Cmd {
	t.Helper()
	gen := m.playGen
	cmd := press(m, pollTick{gen: gen})
	if cmd == nil {
		t.Fatal("pollTick for the current gen: no command, want one asking whether the game runs")
	}
	msg, ok := cmd().(pollMsg)
	if !ok || msg.gen != gen {
		t.Fatalf("pollTick command gave %#v, want pollMsg for gen %d", msg, gen)
	}
	return press(m, msg)
}

// runningSeq makes isRunning answer from answers in order, recording what it was asked.
func runningSeq(m *model, answers ...bool) *[]prism.Instance {
	var asked []prism.Instance
	m.isRunning = func(in prism.Instance) (bool, error) {
		asked = append(asked, in)
		r := answers[0]
		answers = answers[1:]
		return r, nil
	}
	return &asked
}

func pageFooter(m *model) string {
	_, footer, _ := m.page()
	return words(footer)
}

// ---- C6 starting Prism ----

func TestPlayUsesFirstPrismDirForInstanceOutsideAll(t *testing.T) { // C6
	h := newHome(t, termW, update.State{})
	elsewhere := gtnhInstance(t, filepath.Join(t.TempDir(), "instances", "Away"), "Away", update.State{Version: "2.8.4"})
	h.m.insts = append(h.m.insts, elsewhere)
	h.m.inst = elsewhere
	h.m.showHome()
	msgOf[launchedMsg](t, press(h.m, keyEnter))
	if len(h.prism.launches) != 1 || h.prism.launches[0].dataDir != h.dir1 || h.prism.launches[0].inst.Dir != elsewhere.Dir {
		t.Errorf("launches = %+v, want Away started with the first Prism dir %q", h.prism.launches, h.dir1)
	}
}

func TestPlayIgnoresSiblingFolderThatOnlySharesThePrefix(t *testing.T) { // C6
	h := newHome(t, termW, update.State{})
	sibling := gtnhInstance(t, filepath.Join(h.dir2, "instances-old", "Old"), "Old", update.State{Version: "2.8.4"})
	h.m.insts = append(h.m.insts, sibling)
	h.m.inst = sibling
	h.m.showHome()
	msgOf[launchedMsg](t, press(h.m, keyEnter))
	if len(h.prism.launches) != 1 || h.prism.launches[0].dataDir != h.dir1 {
		t.Errorf("launches = %+v, want the first Prism dir %q (instances-old is not inside instances)", h.prism.launches, h.dir1)
	}
}

func TestPlayWithoutPrismShowsPlayerTextAndEscGoesHome(t *testing.T) { // C6, C14
	h := newHome(t, termW, update.State{})
	h.prism.findErr = fmt.Errorf("%w: looked everywhere", prism.ErrLauncherNotFound)
	h.m.showHome()
	press(h.m, keyEnter)
	screen, phase, body := h.m.screen, h.m.errPhase, pageBody(h.m)
	press(h.m, keyEsc)
	if screen != scError || phase != scLaunching || !strings.Contains(body, notFoundText) || len(h.prism.launches) != 0 || h.m.screen != scHome {
		t.Errorf("Prism not found: screen %d, phase %d, body %q, launches %d, after esc %d; want scError, scLaunching, the not-found text, 0, scHome",
			screen, phase, body, len(h.prism.launches), h.m.screen)
	}
}

func TestPlayWithOtherFindErrorSaysCouldNotStart(t *testing.T) { // C6
	h := newHome(t, termW, update.State{})
	h.prism.findErr = errors.New("flatpak is broken")
	h.m.showHome()
	press(h.m, keyEnter)
	if body := pageBody(h.m); h.m.screen != scError || h.m.errPhase != scLaunching || !strings.Contains(body, "I couldn't start Prism Launcher: flatpak is broken") {
		t.Errorf("find error: screen %d, phase %d, body %q; want scError, scLaunching, \"I couldn't start Prism Launcher: flatpak is broken\"", h.m.screen, h.m.errPhase, body)
	}
}

func TestLaunchFailureBecomesErrorScreen(t *testing.T) { // C6, C14
	h := newHome(t, termW, update.State{})
	h.prism.launchErr = errors.New("exec: permission denied")
	h.m.showHome()
	em := msgOf[errMsg](t, press(h.m, keyEnter))
	press(h.m, em)
	if body := pageBody(h.m); h.m.screen != scError || h.m.errPhase != scLaunching || !strings.Contains(body, "I couldn't start Prism Launcher: exec: permission denied") {
		t.Errorf("launch error: screen %d, phase %d, body %q; want scError, scLaunching, the couldn't-start text", h.m.screen, h.m.errPhase, body)
	}
}

// screens C3: a launch error can go back, so enter activates the default Back button;
// q still quits.
func TestLaunchErrorScreenEnterGoesHomeAndQQuits(t *testing.T) { // C6
	launchError := func(t *testing.T) *model {
		h := newHome(t, termW, update.State{})
		h.prism.launchErr = errors.New("nope")
		h.m.showHome()
		press(h.m, msgOf[errMsg](t, press(h.m, keyEnter)))
		return h.m
	}
	m := launchError(t)
	if got := m.buttonLabels(); m.screen != scError || !slices.Equal(got, []string{"Back", "Quit"}) {
		t.Fatalf("launch error: screen %d, buttonLabels() %q; want scError, [Back Quit]", m.screen, got)
	}
	cmd := press(m, keyEnter)
	if m.screen != scHome || m.quitting || isQuit(cmd) {
		t.Errorf("enter on the launch error: screen %d, quitting %v, quit cmd %v; want scHome, false, false", m.screen, m.quitting, isQuit(cmd))
	}
	q := launchError(t)
	cmd = press(q, runes("q"))
	if !q.quitting || !isQuit(cmd) {
		t.Errorf("q on the launch error: quitting %v, quit cmd %v; want true, true", q.quitting, isQuit(cmd))
	}
}

// ---- C9 the launching page ----

func TestLaunchingPageSaysStartingAndHasNoFooter(t *testing.T) { // C9
	h := launching(t)
	if body, footer := pageBody(h.m), pageFooter(h.m); !strings.Contains(body, "Starting Older in Prism Launcher…") || footer != "" {
		t.Errorf("launching page: body %q, footer %q; want the starting line and no footer", body, footer)
	}
}

func TestLaunchingIgnoresKeysButCtrlC(t *testing.T) { // C9
	h := launching(t)
	press(h.m, runes("q"), keyEsc, keyEnter)
	still := h.m.screen
	cmd := press(h.m, keyCtrlC)
	if still != scLaunching || !h.m.quitting || !isQuit(cmd) {
		t.Errorf("keys on launching: screen after q/esc/enter %d, ctrl+c quitting %v quit cmd %v; want scLaunching, true, true", still, h.m.quitting, isQuit(cmd))
	}
}

// ---- C7 after Prism started ----

func TestLaunchedQuitsWhenSettingSaysSo(t *testing.T) { // C7, C14, K2
	h := launching(t)
	h.m.app.AfterPlay = appcfg.AfterPlayQuit
	cmd := press(h.m, launchedMsg{})
	if !h.m.quitting || !isQuit(cmd) || h.m.View() != "" {
		t.Errorf("launchedMsg with AfterPlay quit: quitting %v, quit cmd %v, view %q; want true, true, empty", h.m.quitting, isQuit(cmd), h.m.View())
	}
}

func TestLaunchedStaysOpenAndWatchesTheGame(t *testing.T) { // C7, C9, C14, K2
	h := launching(t)
	gen := h.m.playGen
	cmd := press(h.m, launchedMsg{})
	if h.m.quitting || h.m.screen != scPlaying || h.m.playState != "starting" || h.m.playGen != gen+1 || cmd == nil {
		t.Errorf("launchedMsg staying open: quitting %v, screen %d, state %q, gen %d -> %d, cmd nil %v; want false, scPlaying, starting, +1, a cmd",
			h.m.quitting, h.m.screen, h.m.playState, gen, h.m.playGen, cmd == nil)
	}
}

func TestLaunchedStartsTheClock(t *testing.T) { // C7
	h := launching(t)
	before := time.Now()
	press(h.m, launchedMsg{})
	after := time.Now()
	if h.m.playStart.Before(before) || h.m.playStart.After(after) {
		t.Errorf("playStart %v, want between %v and %v", h.m.playStart, before, after)
	}
}

func TestLaunchedWithExplicitStaySettingStaysOpen(t *testing.T) { // C7
	h := launching(t)
	h.m.app.AfterPlay = appcfg.AfterPlayStay
	press(h.m, launchedMsg{})
	if h.m.quitting || h.m.screen != scPlaying {
		t.Errorf("launchedMsg with AfterPlay stay: quitting %v, screen %d; want false, scPlaying", h.m.quitting, h.m.screen)
	}
}

// ---- C8, C9 watching the game ----

func TestPlayingPageWhileStarting(t *testing.T) { // C9
	h := playing(t)
	const wantFooter = "[ Back ] [ Quit ] ←→ choose enter back q quit" // screens C4
	if body, footer := pageBody(h.m), pageFooter(h.m); !strings.HasPrefix(body, "Older ") || !strings.Contains(body, startingText) || footer != wantFooter {
		t.Errorf("playing page: body %q, footer %q; want the name, %q and %q", body, footer, startingText, wantFooter)
	}
}

func TestPlayingTitleWrapsALongInstanceName(t *testing.T) { // screens C8
	h := playing(t)
	long := strings.TrimSpace(strings.Repeat("Long Name ", 12))
	h.m.inst.Name = long
	if got := h.m.buttonLabels(); !slices.Equal(got, []string{"Back", "Quit"}) {
		t.Fatalf("buttonLabels() on playing = %q, want [Back Quit]", got)
	}
	out := h.m.View()
	if _, widest := fit(out); widest > termW || !strings.Contains(words(out), long) || !strings.Contains(words(out), startingText) {
		t.Errorf("playing view with a %d-column name (widest line %d) = %q; want the whole name then %q", len(long), widest, words(out), startingText)
	}
}

func TestPollSequenceStartingRunningClosed(t *testing.T) { // C8, C9, C14
	h := playing(t)
	m := h.m
	asked := runningSeq(m, true, false)
	before := time.Now()
	runningCmd := pollOnce(t, m)
	after := time.Now()
	runningBody := pageBody(m)
	since := m.runningSince
	closedCmd := pollOnce(t, m)
	closedBody := pageBody(m)
	wantRunning := "The game is running (since " + since.Format("15:04") + "). " + keepsRunning
	if since.Before(before) || since.After(after) {
		t.Errorf("runningSince %v, want between %v and %v", since, before, after)
	}
	if runningCmd == nil || !strings.Contains(runningBody, wantRunning) {
		t.Errorf("running: cmd nil %v, body %q; want a cmd and %q", runningCmd == nil, runningBody, wantRunning)
	}
	if closedCmd != nil || m.playState != "closed" || !strings.Contains(closedBody, closedText) {
		t.Errorf("closed: cmd nil %v, state %q, body %q; want nil, closed, %q", closedCmd == nil, m.playState, closedBody, closedText)
	}
	if len(*asked) != 2 || (*asked)[0].Dir != h.older.Dir || (*asked)[1].Dir != h.older.Dir {
		t.Errorf("isRunning asked about %+v, want Older twice", *asked)
	}
}

func TestRunningSinceIsKeptWhileStillRunning(t *testing.T) { // C8
	h := playing(t)
	runningSeq(h.m, true, true)
	pollOnce(t, h.m)
	first := time.Date(2026, 1, 2, 7, 5, 0, 0, time.Local)
	h.m.runningSince = first
	cmd := pollOnce(t, h.m)
	if !h.m.runningSince.Equal(first) || h.m.playState != "running" || cmd == nil || !strings.Contains(pageBody(h.m), "(since 07:05)") {
		t.Errorf("second running poll: since %v, state %q, cmd nil %v, body %q; want unchanged 07:05, running, a cmd",
			h.m.runningSince, h.m.playState, cmd == nil, pageBody(h.m))
	}
}

func TestPollErrorMeansUnknownAndStops(t *testing.T) { // C8, C9, C14
	h := playing(t)
	h.m.isRunning = func(prism.Instance) (bool, error) { return false, errors.New("ps not found") }
	cmd := pollOnce(t, h.m)
	if body := pageBody(h.m); cmd != nil || h.m.playState != "unknown" || !strings.Contains(body, unknownText) {
		t.Errorf("isRunning error: cmd nil %v, state %q, body %q; want nil, unknown, %q", cmd == nil, h.m.playState, body, unknownText)
	}
}

func TestNotRunningAfterTwoMinutesIsSlowAndKeepsPolling(t *testing.T) { // C8, C9, C14
	h := playing(t)
	h.m.playStart = time.Now().Add(-2 * time.Minute)
	cmd := press(h.m, pollMsg{gen: h.m.playGen, running: false})
	if body := pageBody(h.m); cmd == nil || h.m.playState != "slow" || !strings.Contains(body, slowText) {
		t.Errorf("not running after 2 minutes: cmd nil %v, state %q, body %q; want a cmd, slow, %q", cmd == nil, h.m.playState, body, slowText)
	}
}

// C9: the slow and unknown sentences are wrapped to the page, so the framed view shows
// them whole (frame clips lines wider than the terminal).
func TestSlowAndUnknownSentencesFitTheTerminal(t *testing.T) { // C9
	cases := []struct {
		state, want string
	}{
		{"slow", slowText},
		{"unknown", unknownText},
	}
	for _, c := range cases {
		for _, width := range []int{50, 60, 70, 82} {
			t.Run(fmt.Sprintf("%s at %d", c.state, width), func(t *testing.T) {
				h := playing(t)
				h.m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
				h.m.playState = c.state
				if v := words(h.m.View()); !strings.Contains(v, c.want) {
					t.Errorf("playing view at %d columns = %q, want the whole sentence %q", width, v, c.want)
				}
			})
		}
	}
}

func TestNotRunningAfterOneMinuteIsStillStarting(t *testing.T) { // C8
	h := playing(t)
	h.m.playStart = time.Now().Add(-60 * time.Second)
	cmd := press(h.m, pollMsg{gen: h.m.playGen, running: false})
	if cmd == nil || h.m.playState != "starting" || !strings.Contains(pageBody(h.m), startingText) {
		t.Errorf("not running after 1 minute: cmd nil %v, state %q; want a cmd, starting", cmd == nil, h.m.playState)
	}
}

func TestSlowThenRunningIsRunning(t *testing.T) { // C8
	h := playing(t)
	h.m.playState = "slow"
	cmd := press(h.m, pollMsg{gen: h.m.playGen, running: true})
	if cmd == nil || h.m.playState != "running" {
		t.Errorf("running after slow: cmd nil %v, state %q; want a cmd, running", cmd == nil, h.m.playState)
	}
}

func TestStalePollMsgIsIgnored(t *testing.T) { // C7, C14
	h := playing(t)
	cmd := press(h.m, pollMsg{gen: h.m.playGen - 1, running: true})
	if cmd != nil || h.m.playState != "starting" || !h.m.runningSince.IsZero() {
		t.Errorf("stale pollMsg: cmd nil %v, state %q, since %v; want nil, starting, zero", cmd == nil, h.m.playState, h.m.runningSince)
	}
}

func TestStalePollTickIsIgnored(t *testing.T) { // C7
	h := playing(t)
	asked := runningSeq(h.m, true)
	cmd := press(h.m, pollTick{gen: h.m.playGen + 1})
	if cmd != nil || len(*asked) != 0 {
		t.Errorf("stale pollTick: cmd nil %v, isRunning calls %d; want nil, 0", cmd == nil, len(*asked))
	}
}

func TestPollAfterLeavingPlayingIsIgnored(t *testing.T) { // C7
	h := playing(t)
	gen := h.m.playGen
	press(h.m, keyEnter)
	tickCmd := press(h.m, pollTick{gen: gen})
	msgCmd := press(h.m, pollMsg{gen: gen, running: true})
	if tickCmd != nil || msgCmd != nil || h.m.playState != "starting" || h.m.screen != scLoading {
		t.Errorf("poll after enter: tick cmd nil %v, msg cmd nil %v, state %q, screen %d; want nil, nil, starting, scLoading",
			tickCmd == nil, msgCmd == nil, h.m.playState, h.m.screen)
	}
}

// ---- C9 playing keys, C10 back home ----

func TestPlayingQQuits(t *testing.T) { // C9
	h := playing(t)
	cmd := press(h.m, runes("q"))
	if !h.m.quitting || !isQuit(cmd) {
		t.Errorf("q on playing: quitting %v, quit cmd %v; want true, true", h.m.quitting, isQuit(cmd))
	}
}

func TestPlayingEnterAndEscReloadHome(t *testing.T) { // C9, C10
	for _, k := range []tea.KeyMsg{keyEnter, keyEsc} {
		t.Run(k.String(), func(t *testing.T) {
			h := playing(t)
			cmd := press(h.m, k)
			if h.m.screen != scLoading || cmd == nil || h.m.quitting {
				t.Errorf("%s on playing: screen %d, cmd nil %v, quitting %v; want scLoading, a cmd, false", k, h.m.screen, cmd == nil, h.m.quitting)
			}
		})
	}
}

func TestReloadListsInstancesWithoutRefetchingManifest(t *testing.T) { // C10
	m := sizedModel(t)
	d1, d2 := t.TempDir(), t.TempDir()
	one := prismInstance(t, d1, "one", "One")
	two := prismInstance(t, d2, "two", "Two")
	m.cfg.PrismDirs = []string{d1, d2}
	m.cfg.Client = &http.Client{Transport: offlineTransport{}}
	m.manifest = routeManifest(t)
	_, cmd := m.reloadHome()
	msg := msgOf[reloadedMsg](t, cmd)
	if m.screen != scLoading || len(msg.insts) != 2 || msg.insts[0].Dir != one || msg.insts[1].Dir != two {
		t.Errorf("reloadHome: screen %d, reloaded %+v; want scLoading and One, Two", m.screen, msg.insts)
	}
}

func TestReloadedShowsHomeWithCurrentInstanceSelected(t *testing.T) { // C10
	h := playing(t)
	press(h.m, keyEnter)
	press(h.m, reloadedMsg{[]prism.Instance{h.newest, h.older}})
	if h.m.screen != scHome || selectedKey(h.m) != h.older.Dir || len(h.m.insts) != 2 || h.m.insts[0].Dir != h.newest.Dir {
		t.Errorf("reloadedMsg: screen %d, selected %q, insts %+v; want scHome, Older, the reloaded list", h.m.screen, selectedKey(h.m), h.m.insts)
	}
}

// ---- C11 done screens ----

// doneUpdate is a home after updating Older (screen scDone).
func doneUpdate(t *testing.T) *home {
	t.Helper()
	h := newHome(t, termW, update.State{})
	h.m.inst, h.m.target = h.older, "2.8.4"
	h.m.session = &update.Session{Plan: &update.Plan{}}
	h.m.warns = []string{"w"}
	press(h.m, appliedMsg{&update.Result{From: "2.8.1", To: "2.8.4"}})
	if h.m.screen != scDone {
		t.Fatalf("setup: screen %d after appliedMsg, want scDone", h.m.screen)
	}
	return h
}

// doneCreate is a home after creating Made (screen scDone).
func doneCreate(t *testing.T) (*home, prism.Instance) {
	t.Helper()
	h := newHome(t, termW, update.State{})
	made := gtnhInstance(t, filepath.Join(h.dir2, "instances", "Made"), "Made", update.State{Version: "2.8.4"})
	h.m.startCreate()
	h.m.target, h.m.newName = "2.8.4", "Made"
	press(h.m, createdMsg{&update.CreateResult{Instance: made, Files: 3}})
	if h.m.screen != scDone || !h.m.creating {
		t.Fatalf("setup: screen %d, creating %v after createdMsg; want scDone, true", h.m.screen, h.m.creating)
	}
	return h, made
}

func TestDoneUpdateEnterReloadsAndSelectsInstance(t *testing.T) { // C10, C11, C14
	h := doneUpdate(t)
	cmd := press(h.m, keyEnter)
	screen, session, result, warns := h.m.screen, h.m.session, h.m.result, h.m.warns
	press(h.m, reloadedMsg{[]prism.Instance{h.newest, h.older}})
	if screen != scLoading || cmd == nil || session != nil || result != nil || warns != nil {
		t.Errorf("enter on done: screen %d, cmd nil %v, session nil %v, result nil %v, warns %v; want scLoading, a cmd, all cleared",
			screen, cmd == nil, session == nil, result == nil, warns)
	}
	if h.m.screen != scHome || selectedKey(h.m) != h.older.Dir {
		t.Errorf("after reload: screen %d, selected %q; want scHome, Older", h.m.screen, selectedKey(h.m))
	}
}

func TestDoneUpdateEscReloadsLikeEnter(t *testing.T) { // C11
	h := doneUpdate(t)
	cmd := press(h.m, keyEsc)
	if h.m.screen != scLoading || cmd == nil || h.m.quitting {
		t.Errorf("esc on done: screen %d, cmd nil %v, quitting %v; want scLoading, a cmd, false", h.m.screen, cmd == nil, h.m.quitting)
	}
}

func TestDoneUpdatePPlaysTheUpdatedInstance(t *testing.T) { // C11, C14
	h := doneUpdate(t)
	msgOf[launchedMsg](t, press(h.m, runes("p")))
	if h.m.screen != scLaunching || len(h.prism.launches) != 1 || h.prism.launches[0].inst.Dir != h.older.Dir || h.prism.launches[0].dataDir != h.dir2 {
		t.Errorf("p on done: screen %d, launches %+v; want scLaunching, Older from %q", h.m.screen, h.prism.launches, h.dir2)
	}
}

func TestDoneCreateEnterReloadsAndSelectsNewInstance(t *testing.T) { // C10, C11
	h, made := doneCreate(t)
	cmd := press(h.m, keyEnter)
	creating, created := h.m.creating, h.m.created
	press(h.m, reloadedMsg{[]prism.Instance{h.older, h.newest, made}})
	if cmd == nil || creating || created != nil || h.m.screen != scHome || selectedKey(h.m) != made.Dir {
		t.Errorf("enter on created: cmd nil %v, creating %v, created nil %v, screen %d, selected %q; want a cmd, false, nil, scHome, Made",
			cmd == nil, creating, created == nil, h.m.screen, selectedKey(h.m))
	}
}

func TestDoneCreatePPlaysTheNewInstance(t *testing.T) { // C11
	h, made := doneCreate(t)
	msgOf[launchedMsg](t, press(h.m, runes("p")))
	if h.m.screen != scLaunching || len(h.prism.launches) != 1 || h.prism.launches[0].inst.Dir != made.Dir || h.m.inst.Dir != made.Dir {
		t.Errorf("p on created: screen %d, launches %+v; want scLaunching, Made", h.m.screen, h.prism.launches)
	}
}
