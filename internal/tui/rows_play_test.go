package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/appcfg"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// playModel is a loaded model whose only instance lives in the second of two Prism
// data dirs, so DataDirOf has to pick it rather than fall back to the first. Its server
// has no extra mods, so Play launches right away.
func playModel(t *testing.T) (*model, *fakes, prism.Instance, string) {
	other, root := t.TempDir(), t.TempDir()
	spec := fullSpec("Home")
	spec.mods, spec.modsAsked = "", true
	in := makeInst(t, root, spec)
	m, f := newTestModel(Config{PrismDirs: []string{other, root}}, 80, 30)
	m.Update(loadedMsg{m: testManifest(), insts: []prism.Instance{in}})
	m.focus = focusPage
	return m, f, in, root
}

func sameLaunch(c launchCall, dataDir string, in prism.Instance, server string) bool {
	return c.l.Exe == fakeLauncher.Exe && c.dataDir == dataDir && c.inst.Dir == in.Dir && c.server == server
}

// playRow is the stripped lines of the current page's play row.
func playRow(m *model) []string {
	return trimLines(strings.Join(m.rows()[0].lines(78, false), "\n"))
}

// C6
func TestC6PlayFindsTheLauncherOfTheInstancesDataDir(t *testing.T) {
	m, f, in, root := playModel(t)
	m.app.PrismExe = "/opt/prism/prismlauncher"

	msgs := runCmd(press(m, "p"))

	if len(f.findCalls) != 1 || f.findCalls[0] != [2]string{root, "/opt/prism/prismlauncher"} {
		t.Errorf("findLauncher calls %v, want [%s /opt/prism/prismlauncher]", f.findCalls, root)
	}
	if len(f.launches) != 1 || !sameLaunch(f.launches[0], root, in, "") {
		t.Errorf("launches %+v", f.launches)
	}
	if len(msgs) != 1 || msgs[0] != (launchedMsg{}) {
		t.Errorf("play cmd messages %v, want launchedMsg", msgs)
	}
}

// C6
func TestC6JoinPassesTheServerToLaunch(t *testing.T) {
	m, f, in, root := playModel(t)

	runCmd(press(m, "j"))

	if len(f.launches) != 1 || !sameLaunch(f.launches[0], root, in, "play.example.org") {
		t.Errorf("launches %+v", f.launches)
	}
}

// C6
func TestC6MissingLauncherIsExplainedInADialog(t *testing.T) {
	m, f, _, _ := playModel(t)
	f.findErr = fmt.Errorf("looking around: %w", prism.ErrLauncherNotFound)

	runCmd(press(m, "p"))

	if m.dialog == nil {
		t.Fatalf("no dialog")
	}
	if m.dialog.title != "I couldn't start the game" || strings.Join(m.dialog.buttons, ",") != "OK" {
		t.Errorf("dialog %q with buttons %v", m.dialog.title, m.dialog.buttons)
	}
	want := "I couldn't find Prism Launcher on this computer. Start the game from Prism yourself, or tell me where Prism is in the settings."
	if got := flat(m.dialog.body(200)); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if len(f.launches) != 0 {
		t.Errorf("launched without a launcher")
	}
}

// C6
func TestC6OtherLauncherErrorsAreShownWithTheError(t *testing.T) {
	m, f, _, _ := playModel(t)
	f.findErr = errors.New("permission denied")

	runCmd(press(m, "p"))

	if m.dialog == nil || m.dialog.title != "I couldn't start the game" {
		t.Fatalf("dialog %+v, want I couldn't start the game", m.dialog)
	}
	if got := flat(m.dialog.body(200)); got != "I couldn't start Prism Launcher: permission denied" {
		t.Errorf("body = %q", got)
	}
}

// C6: the launch error comes back as launchFailedMsg and ends in a dialog that names it.
func TestC6LaunchErrorsEndInADialog(t *testing.T) {
	m, f, _, _ := playModel(t)
	f.launchErr = errors.New("exec format error")

	msgs := runCmd(press(m, "p"))
	if len(msgs) != 1 {
		t.Fatalf("play cmd messages %v, want one launchFailedMsg", msgs)
	}
	lf, ok := msgs[0].(launchFailedMsg)
	if !ok {
		t.Fatalf("play cmd message %T, want launchFailedMsg", msgs[0])
	}
	m.Update(lf)

	if m.dialog == nil || m.dialog.title != "I couldn't start the game" {
		t.Fatalf("dialog %+v, want I couldn't start the game", m.dialog)
	}
	if got := flat(m.dialog.body(200)); got != "I couldn't start Prism Launcher: exec format error" {
		t.Errorf("body = %q", got)
	}
}

// C6
func TestC6LaunchedQuitsWhenTheLauncherShouldNotStayOpen(t *testing.T) {
	m, _, _, _ := playModel(t)
	m.app.AfterPlay = appcfg.AfterPlayQuit

	_, cmd := m.Update(launchedMsg{})

	if !m.quitting || !hasQuit(runCmd(cmd)) {
		t.Errorf("after launch with AfterPlay quit: quitting %v", m.quitting)
	}
}

// C6
func TestC6LaunchedStartsWatchingTheGame(t *testing.T) {
	m, _, in, _ := playModel(t)
	runCmd(press(m, "p"))
	gen := m.play.gen

	_, cmd := m.Update(launchedMsg{})

	if m.quitting || m.play.gen != gen+1 || m.play.dir != in.Dir || m.play.state != "starting" || cmd == nil {
		t.Errorf("play = %+v quitting %v cmd %v; want starting, gen %d, dir %s, a poll", m.play, m.quitting, cmd != nil, gen+1, in.Dir)
	}
	if got := playRow(m); !strings.Contains(got[0], "◐ Prism Launcher is starting the game…") {
		t.Errorf("play row = %q", got)
	}
}

// startedModel is a model watching its game: launched and "starting".
func startedModel(t *testing.T) (*model, *fakes, prism.Instance) {
	m, f, in, _ := playModel(t)
	runCmd(press(m, "p"))
	m.Update(launchedMsg{})
	return m, f, in
}

// C6
func TestC6PollTickAsksWhetherTheGameRuns(t *testing.T) {
	m, f, in := startedModel(t)
	f.running = true
	gen := m.play.gen

	_, cmd := m.Update(pollTick{gen: gen})
	msgs := runCmd(cmd)

	// the whole instance, not just its folder (kills flow_play.go:106)
	if len(f.runChecks) != 1 || f.runChecks[0].Dir != in.Dir || f.runChecks[0].Name != "Home" || f.runChecks[0].GameDir != in.GameDir {
		t.Errorf("isRunning calls %+v, want one for %s", f.runChecks, in.Dir)
	}
	if len(msgs) != 1 || msgs[0] != (pollMsg{gen: gen, running: true}) {
		t.Errorf("poll messages %v, want pollMsg{%d true}", msgs, gen)
	}
}

// C6
func TestC6RunningIsShownWithItsStartTime(t *testing.T) {
	m, _, _ := startedModel(t)

	m.Update(pollMsg{gen: m.play.gen, running: true})
	first := m.play.since
	m.Update(pollMsg{gen: m.play.gen, running: true})

	if m.play.state != "running" || first.IsZero() || !m.play.since.Equal(first) {
		t.Errorf("play = %+v, want running since the first sighting %v", m.play, first)
	}
}

// C6
func TestC6RunningPlayRowText(t *testing.T) {
	m, _, in := startedModel(t)
	m.play = playMonitor{gen: m.play.gen, dir: in.Dir, state: "running", start: time.Now(),
		since: time.Date(2026, 3, 4, 14, 5, 0, 0, time.Local)}

	got := playRow(m)

	if len(got) != 2 || !strings.Contains(got[0], "● The game is running since 14:05") || !strings.HasSuffix(got[0], "running") {
		t.Fatalf("play row = %q", got)
	}
	if strings.TrimSpace(got[1]) != "Leave me open or quit — the game keeps running either way." {
		t.Errorf("second line = %q", got[1])
	}
	if w := ansi.StringWidth(got[0]); w != 78 {
		t.Errorf("running line is %d columns, want the hint flush at 78", w)
	}
}

// C4/C6: on a narrow page the state sentences word-wrap within the row width and keep
// every word. Kills rows_play.go:46 and :61 (wrap width).
func TestC6StateSentencesWrapToANarrowRow(t *testing.T) {
	// widths where a word ends just past width-2: a wrap two columns too wide overflows
	cases := []struct {
		state string
		width int
		want  string
	}{
		{"running", 53, "● The game is running since 14:05 running Leave me open or quit — the game keeps running either way."},
		{"unknown", 44, "The game should be starting from Prism now; I can't tell on this computer whether it's running."},
	}
	for _, c := range cases {
		t.Run(c.state, func(t *testing.T) {
			m, _, in := startedModel(t)
			m.play = playMonitor{gen: m.play.gen, dir: in.Dir, state: c.state, start: time.Now(),
				since: time.Date(2026, 3, 4, 14, 5, 0, 0, time.Local)}

			got := trimLines(strings.Join(m.rows()[0].lines(c.width, false), "\n"))

			if flat(strings.Join(got, "\n")) != c.want {
				t.Errorf("play row = %q, want the words %q", got, c.want)
			}
			for i, l := range got {
				if w := ansi.StringWidth(l); w > c.width {
					t.Errorf("line %d is %d columns, want ≤ %d", i+1, w, c.width)
				}
			}
		})
	}
}

// C6
func TestC6GameGoneAfterRunningIsClosedAndPollingStops(t *testing.T) {
	m, _, _ := startedModel(t)
	m.Update(pollMsg{gen: m.play.gen, running: true})

	_, cmd := m.Update(pollMsg{gen: m.play.gen, running: false})

	if m.play.state != "closed" || cmd != nil {
		t.Errorf("state %q cmd %v, want closed and no poll", m.play.state, cmd != nil)
	}
	if got := playRow(m); strings.TrimSpace(got[0]) != "The game closed." {
		t.Errorf("play row = %q", got)
	}
}

// C6
func TestC6PollErrorMeansUnknownAndPollingStops(t *testing.T) {
	m, _, _ := startedModel(t)

	_, cmd := m.Update(pollMsg{gen: m.play.gen, err: errors.New("no process list")})

	if m.play.state != "unknown" || cmd != nil {
		t.Errorf("state %q cmd %v, want unknown and no poll", m.play.state, cmd != nil)
	}
	want := "The game should be starting from Prism now; I can't tell on this computer whether it's running."
	got := playRow(m)
	if flat(strings.Join(got, "\n")) != want {
		t.Errorf("play row = %q, want the sentence %q", got, want)
	}
	for i, l := range got {
		if w := ansi.StringWidth(l); w > 78 {
			t.Errorf("play row line %d is %d columns, want ≤ 78", i+1, w)
		}
	}
}

// C6: not seen for more than slowStart: slow, still polling.
func TestC6NotSeenAfterSlowStartIsSlow(t *testing.T) {
	m, _, _ := startedModel(t)
	m.play.start = time.Now().Add(-slowStart - 5*time.Second)

	_, cmd := m.Update(pollMsg{gen: m.play.gen, running: false})

	if m.play.state != "slow" || cmd == nil {
		t.Errorf("state %q cmd %v, want slow and a poll", m.play.state, cmd != nil)
	}
	want := "I haven't seen the game start yet — have a look at Prism's window."
	if got := playRow(m); strings.TrimSpace(got[0]) != want {
		t.Errorf("play row = %q", got)
	}
}

// C6: not seen yet but within slowStart: still starting.
func TestC6NotSeenWithinSlowStartKeepsStarting(t *testing.T) {
	m, _, _ := startedModel(t)
	m.play.start = time.Now().Add(-slowStart + 30*time.Second)

	_, cmd := m.Update(pollMsg{gen: m.play.gen, running: false})

	if m.play.state != "starting" || cmd == nil {
		t.Errorf("state %q cmd %v, want starting and a poll", m.play.state, cmd != nil)
	}
}

// C6
func TestC6StalePollsAreDropped(t *testing.T) {
	m, _, _ := startedModel(t)

	m.Update(pollMsg{gen: m.play.gen - 1, running: true})

	if m.play.state != "starting" {
		t.Errorf("stale poll moved the state to %q", m.play.state)
	}
}

// C6
func TestC6EnterOrEscOnAFinishedPlayRowResetsIt(t *testing.T) {
	for _, state := range []string{"closed", "slow", "unknown"} {
		for _, k := range []string{"enter", "esc"} {
			t.Run(state+"/"+k, func(t *testing.T) {
				m, _, _ := startedModel(t)
				m.play.state = state
				m.row = 0

				press(m, k)

				if m.play.state != "" || playRow(m)[0] != hinted("  ", "Play", "enter", 78) {
					t.Errorf("state %q row %q, want back to Play", m.play.state, playRow(m))
				}
			})
		}
	}
}

// C6
func TestC6PlayingAgainWhileStartingOrRunningDoesNotLaunch(t *testing.T) {
	for _, state := range []string{"starting", "running"} {
		for _, k := range []string{"enter", "p"} {
			t.Run(state+"/"+k, func(t *testing.T) {
				m, f, _ := startedModel(t)
				m.play.state = state
				m.row = 0

				runCmd(press(m, k))

				if len(f.launches) != 1 {
					t.Errorf("%d launches, want only the first", len(f.launches))
				}
			})
		}
	}
}

// C6
func TestC6AnotherInstanceShowsPlainPlay(t *testing.T) {
	root := t.TempDir()
	a := makeInst(t, root, instSpec{name: "Alpha", gtnh: true, version: "2.8.4"})
	b := makeInst(t, root, instSpec{name: "Bravo", gtnh: true, version: "2.8.4"})
	m, _ := loadedModel(root, 80, 24, a, b)
	m.play = playMonitor{gen: 1, dir: a.Dir, state: "running", since: time.Now()}
	m.focus = focusSidebar

	press(m, "down")

	if got := playRow(m); got[0] != hinted("  ", "Play", "enter", 78) {
		t.Errorf("Bravo's play row = %q", got)
	}
}
