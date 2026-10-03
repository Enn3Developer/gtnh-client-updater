package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Fixtures of the inline update flow: an instance whose server mods were settled already,
// hand-built plans and sessions (never applied, never downloaded).

const modsURL = "https://mods.example.org/pack/extra.zip"

const (
	alwaysLast    = "Worlds, maps and settings aren't touched. Everything replaced is backed up first."
	syncedFromEx  = "Server mods synced from mods.example.org"
	checking284   = "Checking what GTNH 2.8.4 changes"
	confirmTitle  = "Update Home to GTNH 2.8.4?"
	conflictZeta  = ".minecraft/config/zeta.cfg"
	conflictAlpha = ".minecraft/config/alpha.cfg"
)

// markAsked records in state.json that the player answered the server-mods question
// with url ("" = none).
func markAsked(t *testing.T, dir, url string) {
	t.Helper()
	err := update.UpdateState(dir, func(st *update.State) {
		st.CustomModsAsked = true
		st.CustomModsURL = url
	})
	if err != nil {
		t.Fatal(err)
	}
}

// askedHome is a loaded 80x24 model with one fullSpec("Home") instance (2.8.1, Java 17)
// whose server mods were asked already (modsURL), page focused.
func askedHome(t *testing.T, cfg Config) (*model, *fakes, prism.Instance) {
	t.Helper()
	root := t.TempDir()
	in := makeInst(t, root, fullSpec("Home"))
	markAsked(t, in.Dir, modsURL)
	cfg.PrismDirs = []string{root}
	m, f := newTestModel(cfg, 80, 24)
	m.Update(loadedMsg{m: testManifest(), insts: []prism.Instance{in}})
	m.focus = focusPage
	return m, f, in
}

// startedHome is askedHome with an update to target under way (step e of C3 done).
func startedHome(t *testing.T, cfg Config, target string) (*model, *fakes, prism.Instance) {
	t.Helper()
	m, f, in := askedHome(t, cfg)
	if cmd := m.startUpdate(target); cmd == nil {
		t.Fatalf("startUpdate(%s) returned no command (dialog %+v)", target, m.dialog)
	}
	return m, f, in
}

// planOf is a plan with install installs, remove removes and the conflicts, in that
// order, all of the instance's mods found (BaselineMatch 1).
func planOf(install, remove int, conflicts ...string) *update.Plan {
	pl := &update.Plan{BaselineMatch: 1}
	for i := range install {
		pl.Actions = append(pl.Actions, update.Action{Kind: update.Install, Path: fmt.Sprintf(".minecraft/mods/new-%04d.jar", i)})
	}
	for i := range remove {
		pl.Actions = append(pl.Actions, update.Action{Kind: update.Remove, Path: fmt.Sprintf(".minecraft/mods/old-%04d.jar", i)})
	}
	for _, c := range conflicts {
		pl.Actions = append(pl.Actions, update.Action{Kind: update.Conflict, Path: c})
	}
	return pl
}

func sessionOf(pl *update.Plan) *update.Session {
	return &update.Session{Plan: pl, Flavor: manifest.Java17}
}

// withColors renders styles with colours for the rest of the test, so styles can be
// told apart.
func withColors(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// bodyLines is the open dialog's body at a width nothing wraps at, unstyled and trimmed.
func bodyLines(t *testing.T, m *model) []string {
	t.Helper()
	if m.dialog == nil {
		t.Fatalf("no dialog open")
	}
	return trimLines(m.dialog.body(200))
}

func listOf(t *testing.T, m *model) *dlist {
	t.Helper()
	if m.dialog == nil || m.dialog.list == nil {
		t.Fatalf("no list dialog open: %+v", m.dialog)
	}
	return m.dialog.list
}

func dialogTitle(m *model) string {
	if m.dialog == nil {
		return "<no dialog>"
	}
	return m.dialog.title
}

func eq(a, b []string) bool { return reflect.DeepEqual(a, b) }

// ---- C3 startUpdate ----

// C3a: another job runs: notify, no second job and the game isn't even checked.
func TestC3BusyJobRefusesASecondUpdate(t *testing.T) {
	m, f, _ := askedHome(t, Config{})
	other := filepath.Join(t.TempDir(), "Other Pack")
	m.job = &job{kind: jobUpdate, dir: other, title: "Busy elsewhere", phase: "prepare"}

	cmd := m.startUpdate("2.8.4")

	if cmd != nil || dialogTitle(m) != "One thing at a time" {
		t.Fatalf("cmd %v dialog %q, want nil and One thing at a time", cmd != nil, dialogTitle(m))
	}
	if got := flat(m.dialog.body(200)); got != "I'm still busy with Other Pack. Let it finish first." {
		t.Errorf("body = %q", got)
	}
	if m.job.dir != other || m.job.title != "Busy elsewhere" {
		t.Errorf("job replaced: %+v", *m.job)
	}
	if len(f.runChecks) != 0 {
		t.Errorf("isRunning checked %d times before refusing, want 0", len(f.runChecks))
	}
}

// C3b
func TestC3RunningGameRefusesTheUpdate(t *testing.T) {
	m, f, in := askedHome(t, Config{})
	f.running = true

	cmd := m.startUpdate("2.8.4")

	if cmd != nil || m.job != nil || m.jobShown(in.Dir) {
		t.Errorf("cmd %v job %+v: want no job", cmd != nil, m.job)
	}
	if dialogTitle(m) != "The game is running" {
		t.Fatalf("dialog %q, want The game is running", dialogTitle(m))
	}
	if got := flat(m.dialog.body(200)); got != "Home is running right now. Close Minecraft, then try again." {
		t.Errorf("body = %q", got)
	}
}

// C3b: a failed check goes on and remembers that it couldn't tell.
func TestC3UnknownRunStateContinuesAndIsRemembered(t *testing.T) {
	m, f, in := askedHome(t, Config{})
	f.runErr = errors.New("process list unavailable")

	cmd := m.startUpdate("2.8.4")

	if cmd == nil || !m.jobShown(in.Dir) {
		t.Errorf("cmd %v job %+v: want the update started", cmd != nil, m.job)
	}
	if !m.runUnknown {
		t.Errorf("runUnknown false after an isRunning error")
	}
}

// C3b
func TestC3KnownRunStateClearsRunUnknown(t *testing.T) {
	m, _, in := askedHome(t, Config{})
	m.runUnknown = true

	m.startUpdate("2.8.4")

	if m.runUnknown || !m.jobShown(in.Dir) {
		t.Errorf("runUnknown %v job %+v, want false and a job", m.runUnknown, m.job)
	}
}

// C3c
func TestC3DetectionPrefersTheCommandLine(t *testing.T) {
	cases := []struct {
		name      string
		installed string
		want      update.Detection
	}{
		{"command line", "2.8.0", update.Detection{Version: "2.8.0", Source: "command line"}},
		{"saved state", "", update.Detection{Version: "2.8.1", Source: "updater state"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _ := askedHome(t, Config{Installed: c.installed})

			m.startUpdate("2.8.4")

			if m.detect != c.want {
				t.Errorf("detect = %+v, want %+v", m.detect, c.want)
			}
		})
	}
}

// unknownHome is a loaded model with a GTNH instance whose version can't be detected.
func unknownHome(t *testing.T, asked bool) (*model, prism.Instance) {
	t.Helper()
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true})
	if asked {
		markAsked(t, in.Dir, "")
	}
	m, _ := loadedModel(root, 80, 24, in)
	m.focus = focusPage
	return m, in
}

// C3c
func TestC3UnknownVersionAsksWhichVersionItIsOn(t *testing.T) {
	m, in := unknownHome(t, true)

	cmd := m.startUpdate("2.8.4")

	if cmd != nil || m.job != nil || m.jobShown(in.Dir) {
		t.Errorf("cmd %v job %+v: want neither before the version is known", cmd != nil, m.job)
	}
	if dialogTitle(m) != "Which GTNH version is Home on right now?" {
		t.Fatalf("dialog %q", dialogTitle(m))
	}
	l := listOf(t, m)
	want := []ditem{
		{title: "2.8.4", desc: "stable release · 5 days ago", key: "2.8.4"},
		{title: "2.8.1", desc: "stable release · 5 weeks ago", key: "2.8.1"},
		{title: "2.8.0", desc: "stable release · 2 months ago", key: "2.8.0"},
	}
	if !reflect.DeepEqual(l.items, want) || l.cursor != 0 {
		t.Errorf("items %+v cursor %d, want %+v at 0", l.items, l.cursor, want)
	}
}

// C3c: the picked version is remembered and the update goes on to the same target.
func TestC3PickingTheInstalledVersionContinuesWithTheSameTarget(t *testing.T) {
	m, in := unknownHome(t, true)
	m.startUpdate("2.8.4")

	cmd := press(m, "down", "enter")

	if m.detect != (update.Detection{Version: "2.8.1", Source: "you told me"}) {
		t.Errorf("detect = %+v", m.detect)
	}
	if m.dialog != nil || cmd == nil {
		t.Errorf("dialog %q cmd %v, want closed and the prepare command", dialogTitle(m), cmd != nil)
	}
	if !m.jobShown(in.Dir) || m.job.title != checking284 || m.target != "2.8.4" {
		t.Errorf("job %+v target %q, want %q for 2.8.4", m.job, m.target, checking284)
	}
}

// C3c/d: after the version, the never-asked server-mods question comes.
func TestC3PickedVersionThenAsksAboutServerMods(t *testing.T) {
	m, in := unknownHome(t, false)
	m.startUpdate("2.8.4")

	press(m, "enter")

	if dialogTitle(m) != "Does your server have extra mods?" || m.detect.Version != "2.8.4" {
		t.Errorf("dialog %q detect %+v, want the server-mods question after 2.8.4", dialogTitle(m), m.detect)
	}
	if m.jobShown(in.Dir) {
		t.Errorf("job started before the server-mods answer")
	}
}

// notAskedHome is a loaded model with a 2.8.1 instance never asked about server mods.
func notAskedHome(t *testing.T) (*model, prism.Instance) {
	t.Helper()
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.1", java17: true})
	m, _ := loadedModel(root, 80, 24, in)
	m.focus = focusPage
	return m, in
}

// C3d, kills K2: never asked → the question, and nothing starts yet.
func TestC3ServerModsAreAskedWhenNeverAsked(t *testing.T) {
	m, in := notAskedHome(t)

	cmd := m.startUpdate("2.8.4")

	if cmd != nil || m.jobShown(in.Dir) {
		t.Errorf("cmd %v job %+v, want nothing started while asking", cmd != nil, m.job)
	}
	if dialogTitle(m) != "Does your server have extra mods?" || m.dialog.input == nil {
		t.Fatalf("dialog %q, want the server-mods input", dialogTitle(m))
	}
	d := m.dialog
	if d.input.Value() != "" || d.input.Placeholder != "https://…/custom_mods.zip" {
		t.Errorf("value %q placeholder %q", d.input.Value(), d.input.Placeholder)
	}
	if d.note != "Leave it empty if there's none. You can change it later in Settings." || !eq(d.buttons, []string{"Continue"}) {
		t.Errorf("note %q buttons %q", d.note, d.buttons)
	}
	if !containsLine(screen(m), "Some servers add a few mods") {
		t.Errorf("intro missing:\n%s", strings.Join(screen(m), "\n"))
	}
}

// C3d
func TestC3EscOnTheServerModsQuestionCancelsTheUpdate(t *testing.T) {
	m, in := notAskedHome(t)
	m.startUpdate("2.8.4")

	cmd := press(m, "esc")

	if cmd != nil || m.dialog != nil || m.job != nil || m.jobShown(in.Dir) {
		t.Errorf("cmd %v dialog %q job %+v, want everything cancelled", cmd != nil, dialogTitle(m), m.job)
	}
}

// C3d
func TestC3AnInvalidServerModsLinkKeepsTheQuestionOpen(t *testing.T) {
	m, in := notAskedHome(t)
	m.startUpdate("2.8.4")

	cmd := press(m, "http://x", "enter")

	if dialogTitle(m) != "Does your server have extra mods?" || m.dialog.inputErr != msgBadModsLink {
		t.Fatalf("dialog %q inputErr %q, want the question with msgBadModsLink", dialogTitle(m), m.dialog.inputErr)
	}
	if cmd != nil || m.jobShown(in.Dir) {
		t.Errorf("cmd %v job %+v, want nothing started", cmd != nil, m.job)
	}
}

// C3d
func TestC3AnswersToTheServerModsQuestionStartTheUpdate(t *testing.T) {
	cases := []struct {
		name  string
		typed []string
		want  string
	}{
		{"a link", []string{"https://mods.example.org/m.zip"}, "https://mods.example.org/m.zip"},
		{"a link with spaces around", []string{"  https://mods.example.org/m.zip  "}, "https://mods.example.org/m.zip"},
		{"nothing", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, in := notAskedHome(t)
			m.startUpdate("2.8.4")

			cmd := press(m, append(c.typed, "enter")...)

			if cmd == nil || m.dialog != nil {
				t.Errorf("cmd %v dialog %q, want the prepare command and no dialog", cmd != nil, dialogTitle(m))
			}
			if m.serverMods != c.want || !m.serverModsAsked {
				t.Errorf("serverMods %q asked %v, want %q true", m.serverMods, m.serverModsAsked, c.want)
			}
			if !m.jobShown(in.Dir) || m.job.title != checking284 {
				t.Errorf("job %+v, want %q", m.job, checking284)
			}
		})
	}
}

// C3d, kills K2: a settled answer (state or command line) skips the question.
func TestC3SettledServerModsSkipTheQuestion(t *testing.T) {
	cases := []struct {
		name     string
		cfgMods  string
		asked    bool
		stateURL string
		want     string
	}{
		{"asked with a link", "", true, modsURL, modsURL},
		{"asked, none", "", true, "", ""},
		{"command line none", "none", false, "", ""},
		{"command line link", "https://cfg.example.org/x.zip", false, "", "https://cfg.example.org/x.zip"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.1"})
			if c.asked {
				markAsked(t, in.Dir, c.stateURL)
			}
			m, _ := newTestModel(Config{PrismDirs: []string{root}, ServerMods: c.cfgMods}, 80, 24)
			m.Update(loadedMsg{m: testManifest(), insts: []prism.Instance{in}})

			cmd := m.startUpdate("2.8.4")

			if cmd == nil || m.dialog != nil || !m.jobShown(in.Dir) {
				t.Errorf("cmd %v dialog %q job %+v, want the update started without asking", cmd != nil, dialogTitle(m), m.job)
			}
			if m.serverMods != c.want || !m.serverModsAsked {
				t.Errorf("serverMods %q asked %v, want %q true", m.serverMods, m.serverModsAsked, c.want)
			}
		})
	}
}

// C3e
func TestC3StartUpdateStartsPreparingAndClearsTheNotice(t *testing.T) {
	m, _, in := askedHome(t, Config{})
	m.notices[in.Dir] = notice{text: "Updated to GTNH 2.8.1 just now · your files already matched"}
	m.notices["elsewhere"] = notice{text: "kept"}

	cmd := m.startUpdate("2.8.4")

	if cmd == nil || m.dialog != nil {
		t.Errorf("cmd %v dialog %q, want the prepare command and no dialog", cmd != nil, dialogTitle(m))
	}
	want := job{kind: jobUpdate, dir: in.Dir, title: checking284, phase: "prepare"}
	if m.job == nil || *m.job != want || m.target != "2.8.4" {
		t.Fatalf("job %+v target %q, want %+v 2.8.4", m.job, m.target, want)
	}
	if _, ok := m.notices[in.Dir]; ok {
		t.Errorf("notice of the instance kept after starting an update")
	}
	if m.notices["elsewhere"].text != "kept" {
		t.Errorf("another instance's notice was cleared")
	}
	if m.serverMods != modsURL || !m.serverModsAsked {
		t.Errorf("serverMods %q asked %v, want the saved link", m.serverMods, m.serverModsAsked)
	}
}

// ---- C4 messages ----

// C4
func TestC4PreparedWithoutConflictsAsksToConfirm(t *testing.T) {
	m, _, _ := startedHome(t, Config{}, "2.8.4")
	s := sessionOf(planOf(2, 1))

	m.Update(preparedMsg{s})

	if m.session != s {
		t.Errorf("session not kept")
	}
	if dialogTitle(m) != confirmTitle || !eq(m.dialog.buttons, []string{"Update", "Cancel"}) {
		t.Errorf("dialog %q buttons %q, want the confirmation", dialogTitle(m), m.dialog.buttons)
	}
}

// C4/C6
func TestC4ConflictsArePreselectedFromConfigs(t *testing.T) {
	cases := []struct {
		configs string
		want    update.Choice
		cursor  int
	}{
		{"", update.TakeNew, 0},
		{"new", update.TakeNew, 0},
		{"mine", update.KeepMine, 1},
		{"bogus", update.TakeNew, 0},
	}
	for _, c := range cases {
		t.Run(c.configs, func(t *testing.T) {
			m, _, _ := startedHome(t, Config{Configs: c.configs}, "2.8.4")
			pl := planOf(1, 0, conflictZeta, conflictAlpha)

			m.Update(preparedMsg{sessionOf(pl)})

			if pl.ChoiceOf(conflictZeta) != c.want || pl.ChoiceOf(conflictAlpha) != c.want {
				t.Errorf("choices %v %v, want %v", pl.ChoiceOf(conflictZeta), pl.ChoiceOf(conflictAlpha), c.want)
			}
			if dialogTitle(m) != "2 config files changed both on your side and in the new version" {
				t.Fatalf("dialog %q", dialogTitle(m))
			}
			if l := listOf(t, m); l.cursor != c.cursor {
				t.Errorf("cursor %d, want %d", l.cursor, c.cursor)
			}
		})
	}
}

// C4
func TestC4AppliedEndsTheJobStoresTheNoticeAndReloads(t *testing.T) {
	m, _, in := startedHome(t, Config{}, "2.8.4")
	m.Update(preparedMsg{sessionOf(planOf(2, 1))})
	press(m, "enter")

	_, cmd := m.Update(appliedMsg{&update.Result{From: "2.8.1", To: "2.8.4"}})

	if m.job != nil || m.session != nil || m.jobShown(in.Dir) {
		t.Errorf("job %+v session %v, want both gone", m.job, m.session != nil)
	}
	want := notice{text: "Updated to GTNH 2.8.4 just now · 2 files updated, 1 removed"}
	if got := m.notices[in.Dir]; got != want {
		t.Errorf("notice %+v, want %+v", got, want)
	}
	msgs := runCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("command yielded %v, want one reloadedMsg", msgs)
	}
	if _, ok := msgs[0].(reloadedMsg); !ok {
		t.Errorf("command yielded %T, want reloadedMsg", msgs[0])
	}
}

// C4: errors of a job end it and say what it means for the instance.
func TestC4JobErrorsSayWhatHappenedToTheInstance(t *testing.T) {
	const (
		nothing  = "Nothing was changed."
		putBack  = "Everything was put back the way it was, so your instance is exactly as before."
		mayHaveC = "Some files may have changed. The originals are in the .gtnh-updater folder inside the instance."
	)
	rolled := fmt.Errorf("couldn't write a file: %w", update.ErrRolledBack)
	cases := []struct {
		name    string
		applied bool
		err     error
		outcome string
		good    bool
	}{
		{"while preparing", false, errors.New("the download broke"), nothing, true},
		{"rolled back", true, rolled, putBack, true},
		{"apply failed", true, errors.New("disk full"), mayHaveC, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withColors(t)
			m, _, in := startedHome(t, Config{}, "2.8.4")
			if c.applied {
				m.Update(preparedMsg{sessionOf(planOf(1, 0))})
				press(m, "enter")
			}

			m.Update(errMsg{c.err})

			if m.job != nil || m.session != nil || m.jobShown(in.Dir) {
				t.Errorf("job %+v session %v, want both gone", m.job, m.session != nil)
			}
			if dialogTitle(m) != "Something went wrong" || !eq(m.dialog.buttons, []string{"OK"}) {
				t.Fatalf("dialog %q, want Something went wrong with OK", dialogTitle(m))
			}
			if got := bodyLines(t, m); !eq(got, []string{c.err.Error(), "", c.outcome}) {
				t.Errorf("body %q", got)
			}
			sty, other := okSty, badSty
			if !c.good {
				sty, other = badSty, okSty
			}
			body := m.dialog.body(200)
			if !strings.Contains(body, sty.Render(c.outcome)) || strings.Contains(body, other.Render(c.outcome)) {
				t.Errorf("outcome %q not in the right style: %q", c.outcome, body)
			}
		})
	}
}

// ---- C6 conflicts ----

func conflictPaths(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf(".minecraft/config/c%d.cfg", i)
	}
	return out
}

// C6
func TestC6ConflictsDialogCountsTheFilesAndOffersThreeWays(t *testing.T) {
	cases := []struct {
		n     int
		title string
	}{
		{1, "1 config file changed both on your side and in the new version"},
		{3, "3 config files changed both on your side and in the new version"},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.n), func(t *testing.T) {
			m, _, _ := startedHome(t, Config{}, "2.8.4")

			m.Update(preparedMsg{sessionOf(planOf(1, 0, conflictPaths(c.n)...))})

			if dialogTitle(m) != c.title {
				t.Errorf("title %q, want %q", dialogTitle(m), c.title)
			}
			want := []ditem{
				{title: "Use the new versions", desc: "Recommended — your old copies go to the backup folder.", key: "new"},
				{title: "Keep mine", desc: "The new ones are saved next to yours with .mcnew at the end.", key: "mine"},
				{title: "Decide file by file", key: "pick"},
			}
			if got := listOf(t, m).items; !reflect.DeepEqual(got, want) {
				t.Errorf("items %+v", got)
			}
			if got := boxText(m); len(got) == 0 || got[0] != "What should I do with them?" {
				t.Errorf("box %q, want the intro first", got)
			}
		})
	}
}

// C6
func TestC6ChoosingForAllFilesGoesToTheConfirmation(t *testing.T) {
	cases := []struct {
		name    string
		configs string
		keys    []string
		want    update.Choice
	}{
		{"new", "mine", []string{"up", "enter"}, update.TakeNew},
		{"mine", "new", []string{"down", "enter"}, update.KeepMine},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _ := startedHome(t, Config{Configs: c.configs}, "2.8.4")
			pl := planOf(1, 0, conflictZeta, conflictAlpha)
			m.Update(preparedMsg{sessionOf(pl)})

			press(m, c.keys...)

			if pl.ChoiceOf(conflictZeta) != c.want || pl.ChoiceOf(conflictAlpha) != c.want {
				t.Errorf("choices %v %v, want %v", pl.ChoiceOf(conflictZeta), pl.ChoiceOf(conflictAlpha), c.want)
			}
			if dialogTitle(m) != confirmTitle {
				t.Errorf("dialog %q, want the confirmation", dialogTitle(m))
			}
		})
	}
}

// resolving opens the resolve dialog for zeta then alpha, preselected by configs.
func resolving(t *testing.T, configs string) (*model, *update.Plan) {
	t.Helper()
	m, _, _ := startedHome(t, Config{Configs: configs}, "2.8.4")
	pl := planOf(1, 0, conflictZeta, conflictAlpha)
	m.Update(preparedMsg{sessionOf(pl)})
	press(m, "down", "down", "enter")
	return m, pl
}

// C6: one item per conflict, in plan order, named without .minecraft/.
func TestC6DecideFileByFileListsTheConflictsInPlanOrder(t *testing.T) {
	m, _ := resolving(t, "mine")

	if dialogTitle(m) != "Which version of each file do you want?" {
		t.Fatalf("dialog %q", dialogTitle(m))
	}
	l := listOf(t, m)
	want := []ditem{
		{title: "config/zeta.cfg", desc: "keep mine", key: conflictZeta},
		{title: "config/alpha.cfg", desc: "keep mine", key: conflictAlpha},
	}
	if !reflect.DeepEqual(l.items, want) || l.cursor != 0 {
		t.Errorf("items %+v cursor %d, want %+v at 0", l.items, l.cursor, want)
	}
	if l.toggle == nil || !eq(m.dialog.buttons, []string{"Done"}) {
		t.Errorf("toggle set %v buttons %q, want a toggle and Done", l.toggle != nil, m.dialog.buttons)
	}
	if got := boxText(m); len(got) == 0 || got[0] != "Space switches a file, enter when you're done." {
		t.Errorf("box %q, want the intro first", got)
	}
}

// C6
func TestC6SpaceSwitchesTheFileUnderTheCursor(t *testing.T) {
	m, pl := resolving(t, "")
	press(m, "down")

	m.Update(spaceKey)
	afterOne := []string{listOf(t, m).items[0].desc, listOf(t, m).items[1].desc}
	alphaOnce := pl.ChoiceOf(conflictAlpha)
	m.Update(spaceKey)

	if alphaOnce != update.KeepMine || !eq(afterOne, []string{"new version", "keep mine"}) {
		t.Errorf("after one space: alpha %v descs %q, want KeepMine [new version keep mine]", alphaOnce, afterOne)
	}
	if pl.ChoiceOf(conflictAlpha) != update.TakeNew || listOf(t, m).items[1].desc != "new version" {
		t.Errorf("after two spaces: alpha %v desc %q, want TakeNew new version", pl.ChoiceOf(conflictAlpha), listOf(t, m).items[1].desc)
	}
	if pl.ChoiceOf(conflictZeta) != update.TakeNew || listOf(t, m).cursor != 1 {
		t.Errorf("zeta %v cursor %d, want TakeNew and the cursor kept on 1", pl.ChoiceOf(conflictZeta), listOf(t, m).cursor)
	}
}

// C6/C7
func TestC6DoneGoesToTheConfirmationWithThePerFileChoices(t *testing.T) {
	m, pl := resolving(t, "")
	m.Update(spaceKey)
	press(m, "down")

	press(m, "enter")

	if dialogTitle(m) != confirmTitle {
		t.Fatalf("dialog %q, want the confirmation", dialogTitle(m))
	}
	if pl.ChoiceOf(conflictZeta) != update.KeepMine || pl.ChoiceOf(conflictAlpha) != update.TakeNew {
		t.Errorf("choices zeta %v alpha %v, want KeepMine TakeNew", pl.ChoiceOf(conflictZeta), pl.ChoiceOf(conflictAlpha))
	}
	got := bodyLines(t, m)
	if !containsLine(got, "1 config file you changed gets the new version — your old one goes to the backup") ||
		!containsLine(got, "1 config file you changed stays yours — the new one is saved next to it as .mcnew") {
		t.Errorf("confirmation body %q lacks the per-file choices", got)
	}
}

// C6: esc on either dialog cancels the update.
func TestC6EscCancelsTheUpdate(t *testing.T) {
	cases := []struct {
		name string
		keys []string
	}{
		{"conflicts", []string{"esc"}},
		{"resolve", []string{"down", "down", "enter", "esc"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, in := startedHome(t, Config{}, "2.8.4")
			m.Update(preparedMsg{sessionOf(planOf(1, 0, conflictZeta))})

			cmd := press(m, c.keys...)

			if cmd != nil || m.dialog != nil || m.session != nil || m.job != nil || m.jobShown(in.Dir) {
				t.Errorf("cmd %v dialog %q session %v job %+v, want all gone", cmd != nil, dialogTitle(m), m.session != nil, m.job)
			}
		})
	}
}

// ---- C7 confirmation ----

// C7: every line that applies, singular forms, in order.
func TestC7ConfirmationListsWhatTheUpdateDoes(t *testing.T) {
	m, _, _ := startedHome(t, Config{}, "2.8.4")
	pl := planOf(2, 1, ".minecraft/config/c1.cfg", ".minecraft/config/c2.cfg")
	pl.Kept = []string{".minecraft/config/kept.cfg"}
	pl.ExtraMods = []string{"extra-a.jar", "extra-b.jar"}
	m.Update(preparedMsg{sessionOf(pl)})
	pl.Choose(".minecraft/config/c1.cfg", update.TakeNew)
	pl.Choose(".minecraft/config/c2.cfg", update.KeepMine)

	m.confirmUpdate()

	want := []string{
		"2 files updated, 1 removed",
		"1 config file you changed stays as it is",
		"1 config file you changed gets the new version — your old one goes to the backup",
		"1 config file you changed stays yours — the new one is saved next to it as .mcnew",
		"Mods you added yourself stay: extra-a.jar, extra-b.jar",
		syncedFromEx,
		alwaysLast,
	}
	if got := bodyLines(t, m); !eq(got, want) {
		t.Errorf("body\n%q\nwant\n%q", got, want)
	}
}

// C7: plural forms, thousands separators, Java 8, no server mods.
func TestC7ConfirmationUsesPluralsAndSaysJava8(t *testing.T) {
	m, _, _ := startedHome(t, Config{ServerMods: "none"}, "2.8.4")
	pl := planOf(1234, 5, conflictPaths(4)...)
	pl.Kept = []string{"a", "b", "c"}
	s := sessionOf(pl)
	s.Flavor = manifest.Java8
	m.Update(preparedMsg{s})
	pl.Choose(".minecraft/config/c0.cfg", update.TakeNew)
	pl.Choose(".minecraft/config/c1.cfg", update.TakeNew)
	pl.Choose(".minecraft/config/c2.cfg", update.KeepMine)
	pl.Choose(".minecraft/config/c3.cfg", update.KeepMine)

	m.confirmUpdate()

	want := []string{
		"1,234 files updated, 5 removed",
		"3 config files you changed stay as they are",
		"2 config files you changed get the new version — your old ones go to the backup",
		"2 config files you changed stay yours — the new ones are saved next to them as .mcnew",
		"This instance uses the Java 8 pack",
		alwaysLast,
	}
	if got := bodyLines(t, m); !eq(got, want) {
		t.Errorf("body\n%q\nwant\n%q", got, want)
	}
}

// C7
func TestC7ConfirmationOfSmallPlans(t *testing.T) {
	cases := []struct {
		name    string
		install int
		first   string
	}{
		{"nothing to do", 0, "Your files already match this version"},
		{"one file", 1, "1 file updated, 0 removed"},
	}
	t.Run("as many removed as updated", func(t *testing.T) {
		m, _, _ := startedHome(t, Config{ServerMods: "none"}, "2.8.4")

		m.Update(preparedMsg{sessionOf(planOf(1, 1))})

		if got := bodyLines(t, m); !eq(got, []string{"1 file updated, 1 removed", alwaysLast}) {
			t.Errorf("body %q", got)
		}
	})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _ := startedHome(t, Config{ServerMods: "none"}, "2.8.4")

			m.Update(preparedMsg{sessionOf(planOf(c.install, 0))})

			if got := bodyLines(t, m); !eq(got, []string{c.first, alwaysLast}) {
				t.Errorf("body %q", got)
			}
		})
	}
}

// C7: a long list of extra mods is cut at 100 columns with …
func TestC7LongExtraModListIsCut(t *testing.T) {
	m, _, _ := startedHome(t, Config{ServerMods: "none"}, "2.8.4")
	pl := planOf(1, 0)
	for i := range 20 {
		pl.ExtraMods = append(pl.ExtraMods, fmt.Sprintf("mod-%02d.jar", i))
	}
	joined := strings.Join(pl.ExtraMods, ", ")

	m.Update(preparedMsg{sessionOf(pl)})

	want := "Mods you added yourself stay: " + joined[:99] + "…"
	if got := bodyLines(t, m); len(got) != 3 || got[1] != want {
		t.Errorf("body %q, want line 2 %q", got, want)
	}
}

// C7
func TestC7TitleAndButtonFollowTheDirection(t *testing.T) {
	downgrade := "This goes BACK to an older version. Worlds you played on 2.8.1 may lose blocks and items or not load at all. Copy your saves folder somewhere safe first."
	cases := []struct {
		target, title, ok string
		tail              []string
	}{
		{"2.8.4", "Update Home to GTNH 2.8.4?", "Update", []string{alwaysLast}},
		{"2.8.1", "Refresh Home on GTNH 2.8.1?", "Refresh", []string{alwaysLast}},
		{"2.8.0", "Go back to GTNH 2.8.0?", "Go back", []string{alwaysLast, "", downgrade}},
	}
	for _, c := range cases {
		t.Run(c.target, func(t *testing.T) {
			m, _, _ := startedHome(t, Config{ServerMods: "none"}, c.target)

			m.Update(preparedMsg{sessionOf(planOf(1, 0))})

			if dialogTitle(m) != c.title || !eq(m.dialog.buttons, []string{c.ok, "Cancel"}) || m.dialog.btn != 0 {
				t.Errorf("dialog %q buttons %q btn %d, want %q [%s Cancel] 0", dialogTitle(m), m.dialog.buttons, m.dialog.btn, c.title, c.ok)
			}
			if got := bodyLines(t, m); !eq(got, append([]string{"1 file updated, 0 removed"}, c.tail...)) {
				t.Errorf("body %q", got)
			}
		})
	}
}

// C7
func TestC7DowngradeWarningText(t *testing.T) {
	got := downgradeWarning("2.8.4")

	if got != "This goes BACK to an older version. Worlds you played on 2.8.4 may lose blocks and items or not load at all. Copy your saves folder somewhere safe first." {
		t.Errorf("downgradeWarning = %q", got)
	}
}

// C7: below 80% of the baseline's mods the guess is suspect (red line); 80% is fine.
func TestC7SuspectBaselineIsWarned(t *testing.T) {
	suspect := func(pct string) string {
		return "Only " + pct + "% of the mods that come with 2.8.1 are in this instance, so it's probably not on 2.8.1. Cancel and use o to tell me the right version — otherwise old mods could be left behind."
	}
	cases := []struct {
		match float64
		tail  []string
	}{
		{0.5, []string{alwaysLast, "", suspect("50")}},
		{0.79, []string{alwaysLast, "", suspect("79")}},
		{0.8, []string{alwaysLast}},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.match), func(t *testing.T) {
			m, _, _ := startedHome(t, Config{ServerMods: "none"}, "2.8.4")
			pl := planOf(1, 0)
			pl.BaselineMatch = c.match

			m.Update(preparedMsg{sessionOf(pl)})

			if got := bodyLines(t, m); !eq(got, append([]string{"1 file updated, 0 removed"}, c.tail...)) {
				t.Errorf("body %q", got)
			}
		})
	}
}

// C7: downgrade first, then the suspect line, in one red block.
func TestC7DowngradeAndSuspectAreBothListed(t *testing.T) {
	m, _, _ := startedHome(t, Config{ServerMods: "none"}, "2.8.0")
	pl := planOf(1, 0)
	pl.BaselineMatch = 0.5

	m.Update(preparedMsg{sessionOf(pl)})

	got := bodyLines(t, m)
	want := []string{
		"1 file updated, 0 removed", alwaysLast, "",
		"This goes BACK to an older version. Worlds you played on 2.8.1 may lose blocks and items or not load at all. Copy your saves folder somewhere safe first.",
		"Only 50% of the mods that come with 2.8.1 are in this instance, so it's probably not on 2.8.1. Cancel and use o to tell me the right version — otherwise old mods could be left behind.",
	}
	if !eq(got, want) {
		t.Errorf("body\n%q\nwant\n%q", got, want)
	}
}

// C7: an unknown run state and the prepare's heads-ups are warnings.
func TestC7UnknownRunStateAndHeadsUpsAreWarnings(t *testing.T) {
	m, f, _ := askedHome(t, Config{})
	f.runErr = errors.New("process list unavailable")
	m.startUpdate("2.8.4")
	m.Update(warnMsg("The server mods link is down"))

	m.Update(preparedMsg{sessionOf(planOf(1, 0))})

	want := []string{
		"1 file updated, 0 removed", syncedFromEx, alwaysLast, "",
		"Make sure Minecraft is closed before you continue.",
		"Heads up: The server mods link is down",
	}
	if got := bodyLines(t, m); !eq(got, want) {
		t.Errorf("body\n%q\nwant\n%q", got, want)
	}
}

// C7: OK starts applying.
func TestC7ConfirmingStartsApplying(t *testing.T) {
	cases := []struct{ target, title string }{
		{"2.8.4", "Updating to GTNH 2.8.4"},
		{"2.8.1", "Refreshing GTNH 2.8.1"},
	}
	for _, c := range cases {
		t.Run(c.target, func(t *testing.T) {
			m, _, in := startedHome(t, Config{}, c.target)
			m.Update(preparedMsg{sessionOf(planOf(1, 0))})

			cmd := press(m, "enter")

			if cmd == nil || m.dialog != nil || m.session == nil {
				t.Errorf("cmd %v dialog %q session %v, want the apply command", cmd != nil, dialogTitle(m), m.session != nil)
			}
			if !m.jobShown(in.Dir) || m.job.phase != "apply" || m.job.title != c.title || !m.busyApplying() {
				t.Errorf("job %+v, want phase apply titled %q", m.job, c.title)
			}
		})
	}
}

// C7
func TestC7CancelDropsTheUpdate(t *testing.T) {
	for _, keys := range [][]string{{"right", "enter"}, {"esc"}} {
		t.Run(strings.Join(keys, "+"), func(t *testing.T) {
			m, _, in := startedHome(t, Config{}, "2.8.4")
			m.Update(preparedMsg{sessionOf(planOf(1, 0))})

			cmd := press(m, keys...)

			if cmd != nil || m.dialog != nil || m.session != nil || m.job != nil || m.jobShown(in.Dir) {
				t.Errorf("cmd %v dialog %q session %v job %+v, want all gone", cmd != nil, dialogTitle(m), m.session != nil, m.job)
			}
		})
	}
}

// ---- C10 afterLoad ----

// C10
func TestC10TargetFromTheCommandLineStartsTheUpdateAfterLoad(t *testing.T) {
	cases := []struct{ target, want string }{
		{"latest", "2.8.4"},
		{"latest-stable", "2.8.4"},
		{"2.8.0", "2.8.0"},
	}
	for _, c := range cases {
		t.Run(c.target, func(t *testing.T) {
			root := t.TempDir()
			in := makeInst(t, root, fullSpec("Home"))
			markAsked(t, in.Dir, modsURL)
			m, _ := newTestModel(Config{PrismDirs: []string{root}, Target: c.target}, 80, 24)

			_, cmd := m.Update(loadedMsg{m: testManifest(), insts: []prism.Instance{in}})

			want := job{kind: jobUpdate, dir: in.Dir, title: "Checking what GTNH " + c.want + " changes", phase: "prepare"}
			if !m.jobShown(in.Dir) || cmd == nil || *m.job != want || m.target != c.want {
				t.Errorf("cmd %v job %+v target %q, want %+v %s", cmd != nil, m.job, m.target, want, c.want)
			}
			if m.dialog != nil {
				t.Errorf("dialog %q open, want none", dialogTitle(m))
			}
		})
	}
}

// C10
func TestC10UnknownTargetIsExplained(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, fullSpec("Home"))
	markAsked(t, in.Dir, modsURL)
	m, _ := newTestModel(Config{PrismDirs: []string{root}, Target: "9.9.9"}, 80, 24)

	_, cmd := m.Update(loadedMsg{m: testManifest(), insts: []prism.Instance{in}})

	if cmd != nil || m.jobShown(in.Dir) {
		t.Errorf("cmd %v job %+v, want nothing started", cmd != nil, m.job)
	}
	if dialogTitle(m) != "No such version" {
		t.Fatalf("dialog %q, want No such version", dialogTitle(m))
	}
	if got := flat(m.dialog.body(200)); got != `GTNH has no version called "9.9.9".` {
		t.Errorf("body %q", got)
	}
}
