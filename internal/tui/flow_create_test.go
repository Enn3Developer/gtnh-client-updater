package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Fixtures of the new-instance flow: hand-built Creations and CreateResults; the real
// download (prepareCreate's command) and write (applyCreate's) are never run.

const (
	pickTitle = "Which GTNH version do you want to install?"
	nameTitle = "What should the new instance be called?"
	modsTitle = "Does your server have extra mods?"
)

// createModel is a loaded 80x24 model under a fresh Prism root with real instances
// named names (GTNH, fullSpec).
func createModel(t *testing.T, cfg Config, names ...string) (*model, *fakes, string) {
	t.Helper()
	root := t.TempDir()
	var insts []prism.Instance
	for _, n := range names {
		insts = append(insts, makeInst(t, root, fullSpec(n)))
	}
	cfg.PrismDirs = []string{root}
	m, f := newTestModel(cfg, 80, 24)
	m.Update(loadedMsg{m: testManifest(), insts: insts})
	return m, f, root
}

// creating is createModel with the creation of "My Pack" on 2.8.1 downloading.
func creating(t *testing.T, names ...string) (*model, *fakes, string) {
	t.Helper()
	m, f, root := createModel(t, Config{ServerMods: "none"}, names...)
	if cmd := m.beginCreate("2.8.1", "My Pack"); cmd == nil {
		t.Fatalf("beginCreate returned no command")
	}
	return m, f, root
}

// createDir is a folder standing in for a downloaded, unapplied new instance.
func createDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "My Pack")
	writeFile(t, filepath.Join(dir, ".minecraft", "mods", "a.jar"), "jar")
	return dir
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func itemTexts(l *dlist) [][2]string {
	out := make([][2]string, len(l.items))
	for i, it := range l.items {
		out[i] = [2]string{it.title, it.desc}
	}
	return out
}

func visibleHas(m *model, dir string) bool {
	for _, in := range m.visible() {
		if in.Dir == dir {
			return true
		}
	}
	return false
}

// ---- C5 newInstance ----

// C5
func TestC5NKeyOpensTheVersionPicker(t *testing.T) {
	m, _, _ := createModel(t, Config{}, "Home")

	cmd := press(m, "n")

	if cmd != nil || dialogTitle(m) != pickTitle {
		t.Errorf("cmd %v dialog %q, want the picker", cmd != nil, dialogTitle(m))
	}
}

// C5
func TestC5PickerListsTheReleasesNewestFirst(t *testing.T) {
	m, _, _ := createModel(t, Config{}, "Home")

	m.pickCreateVersion()

	if dialogTitle(m) != pickTitle {
		t.Fatalf("dialog %q, want %q", dialogTitle(m), pickTitle)
	}
	if got := m.dialog.body(200); got != "" {
		t.Errorf("intro %q, want none", got)
	}
	l := listOf(t, m)
	want := [][2]string{
		{"2.8.4", "stable release · 5 days ago · recommended"},
		{"2.8.1", "stable release · 5 weeks ago"},
		{"2.8.0", "stable release · 2 months ago"},
	}
	if got := itemTexts(l); !equalPairs(got, want) || l.cursor != 0 {
		t.Errorf("items %q cursor %d, want %q at 0", got, l.cursor, want)
	}
}

func equalPairs(a, b [][2]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// C5: a beta first, a Java 8-only release without a date; the cursor on the
// recommended one.
func TestC5PickerMarksJava8OnlyReleasesAndStartsOnTheRecommended(t *testing.T) {
	now := time.Now()
	man := &manifest.Manifest{Releases: []manifest.Release{
		{Version: "2.9.0-beta-1", Title: "Beta release", ReleaseDate: now.Add(-2*day - time.Hour),
			Java17URL: "https://downloads.gtnewhorizons.com/b17.zip", Java8URL: "https://downloads.gtnewhorizons.com/b8.zip"},
		{Version: "2.8.4", Title: "Stable release", ReleaseDate: now.Add(-5*day - time.Hour),
			Java17URL: "https://downloads.gtnewhorizons.com/a17.zip", Java8URL: "https://downloads.gtnewhorizons.com/a8.zip"},
		{Version: "2.8.0", Title: "Stable release", Java8URL: "https://downloads.gtnewhorizons.com/c8.zip"},
	}}
	m, _ := newTestModel(Config{PrismDirs: []string{t.TempDir()}}, 80, 24)
	m.Update(loadedMsg{m: man})

	m.pickCreateVersion()

	l := listOf(t, m)
	want := [][2]string{
		{"2.9.0-beta-1", "beta · 2 days ago"},
		{"2.8.4", "stable release · 5 days ago · recommended"},
		{"2.8.0", "stable release · Java 8 only"},
	}
	if got := itemTexts(l); !equalPairs(got, want) || l.cursor != 1 {
		t.Errorf("items %q cursor %d, want %q at 1", got, l.cursor, want)
	}
}

// C5/C7: picking a version (server mods settled) asks for the name.
func TestC5PickingAVersionGoesOnToTheName(t *testing.T) {
	m, _, _ := createModel(t, Config{ServerMods: "none"}, "Home")
	m.pickCreateVersion()

	cmd := press(m, "down", "enter")

	if cmd != nil || m.job != nil {
		t.Errorf("cmd %v job %+v, want nothing started yet", cmd != nil, m.job)
	}
	if dialogTitle(m) != nameTitle || m.dialog.input == nil {
		t.Fatalf("dialog %q, want the name question", dialogTitle(m))
	}
	if got := m.dialog.input.Value(); got != "GT New Horizons 2.8.1" {
		t.Errorf("name %q, want GT New Horizons 2.8.1", got)
	}
}

// C5
func TestC5NewInstanceRefusesWhileAJobRuns(t *testing.T) {
	m, _, _ := createModel(t, Config{}, "Home")
	m.job = jobOn(m.insts[0].Dir, checking284, "prepare")

	cmd := press(m, "n")

	if cmd != nil || dialogTitle(m) != "One thing at a time" {
		t.Fatalf("cmd %v dialog %q, want One thing at a time", cmd != nil, dialogTitle(m))
	}
	if m.job.title != checking284 {
		t.Errorf("job replaced: %+v", *m.job)
	}
}

// C5: the command line's version is used once, then the picker.
func TestC5TargetFromTheCommandLineSkipsThePickerOnce(t *testing.T) {
	m, _, _ := createModel(t, Config{Target: "latest", ServerMods: "none"})

	press(m, "n")
	first, value := dialogTitle(m), ""
	if m.dialog != nil && m.dialog.input != nil {
		value = m.dialog.input.Value()
	}
	press(m, "esc")
	press(m, "n")

	if first != nameTitle || value != "GT New Horizons 2.8.4" {
		t.Errorf("first n: dialog %q value %q, want the name question for 2.8.4", first, value)
	}
	if dialogTitle(m) != pickTitle {
		t.Errorf("second n: dialog %q, want the picker", dialogTitle(m))
	}
}

// C5
func TestC5UnknownTargetFromTheCommandLineIsExplained(t *testing.T) {
	m, _, _ := createModel(t, Config{Target: "9.9.9", ServerMods: "none"})

	cmd := press(m, "n")

	if cmd != nil || m.job != nil || dialogTitle(m) != "No such version" {
		t.Fatalf("cmd %v job %+v dialog %q, want No such version and nothing started", cmd != nil, m.job, dialogTitle(m))
	}
	if got := flat(m.dialog.body(200)); got != `GTNH has no version called "9.9.9".` {
		t.Errorf("body %q", got)
	}
}

// ---- C6 server mods ----

// C6: a creation never asks about server mods: they're asked with the server address.
func TestC6CreationNeverAsksAboutServerMods(t *testing.T) {
	m, _, _ := createModel(t, Config{}, "Home")
	m.pickCreateVersion()

	press(m, "enter")

	if dialogTitle(m) != nameTitle {
		t.Fatalf("dialog %q, want the name question", dialogTitle(m))
	}
}

// C6: the command line's link is the new instance's, and counts as the player's answer.
func TestC6CreationTakesTheServerModsOfTheCommandLine(t *testing.T) {
	cases := []struct {
		cfg, want string
		given     bool
	}{
		{"", "", false},
		{"none", "", true},
		{"https://cfg.example.org/x.zip", "https://cfg.example.org/x.zip", true},
	}
	for _, c := range cases {
		t.Run(c.cfg, func(t *testing.T) {
			m, _, _ := createModel(t, Config{ServerMods: c.cfg}, "Home")
			m.serverMods = "https://stale.example.org/old.zip"

			m.beginCreate("2.8.1", "My Pack")

			if m.serverMods != c.want || m.serverModsGiven != c.given {
				t.Errorf("serverMods %q given %v, want %q %v", m.serverMods, m.serverModsGiven, c.want, c.given)
			}
		})
	}
}

// ---- C7 askCreateName ----

// C7
func TestC7NameQuestionOffersTheDefaultName(t *testing.T) {
	m, _, root := createModel(t, Config{}, "Home")

	m.askCreateName("2.8.1")

	if dialogTitle(m) != nameTitle || m.dialog.input == nil {
		t.Fatalf("dialog %q, want the name question", dialogTitle(m))
	}
	d := m.dialog
	if got := d.body(200); got != "That's the name you'll see in Prism; it's also the folder name." {
		t.Errorf("intro %q", got)
	}
	if d.input.Value() != "GT New Horizons 2.8.1" || d.input.Placeholder != "" {
		t.Errorf("value %q placeholder %q", d.input.Value(), d.input.Placeholder)
	}
	if want := "It'll be created in " + filepath.Join(root, "instances"); d.note != want {
		t.Errorf("note %q, want %q", d.note, want)
	}
	if !eq(d.buttons, []string{"Create"}) {
		t.Errorf("buttons %q", d.buttons)
	}
}

// C7: the command line's name is offered the first time only.
func TestC7NameFromTheCommandLineIsOfferedOnce(t *testing.T) {
	m, _, _ := createModel(t, Config{Name: "My Pack"}, "Home")

	m.askCreateName("2.8.1")
	first := m.dialog.input.Value()
	press(m, "esc")
	m.askCreateName("2.8.1")

	if first != "My Pack" || m.dialog.input.Value() != "GT New Horizons 2.8.1" {
		t.Errorf("values %q then %q, want My Pack then GT New Horizons 2.8.1", first, m.dialog.input.Value())
	}
}

// C7
func TestC7EscOnTheNameCancelsTheCreation(t *testing.T) {
	m, _, _ := createModel(t, Config{}, "Home")
	m.askCreateName("2.8.1")

	cmd := press(m, "esc")

	if cmd != nil || m.dialog != nil || m.job != nil {
		t.Errorf("cmd %v dialog %q job %+v, want nothing", cmd != nil, dialogTitle(m), m.job)
	}
}

// C7
func TestC7AnInvalidNameKeepsTheQuestion(t *testing.T) {
	for _, name := range []string{"", "a/b", "Home"} {
		t.Run(name, func(t *testing.T) {
			m, _, root := createModel(t, Config{}, "Home")
			m.askCreateName("2.8.1")
			m.dialog.input.SetValue(name)

			cmd := press(m, "enter")

			want := update.CheckInstanceName(filepath.Join(root, "instances"), name)
			if want == nil {
				t.Fatalf("%q is a valid name", name)
			}
			if cmd != nil || m.job != nil || dialogTitle(m) != nameTitle {
				t.Fatalf("cmd %v job %+v dialog %q, want the question kept", cmd != nil, m.job, dialogTitle(m))
			}
			if m.dialog.inputErr != want.Error() {
				t.Errorf("inputErr %q, want %q", m.dialog.inputErr, want.Error())
			}
		})
	}
}

// C7/C8
func TestC7AValidNameStartsTheCreation(t *testing.T) {
	m, _, root := createModel(t, Config{}, "Home")
	m.askCreateName("2.8.1")
	m.dialog.input.SetValue("My Pack")

	cmd := press(m, "enter")

	if cmd == nil || m.dialog != nil {
		t.Errorf("cmd %v dialog %q, want the download command and no dialog", cmd != nil, dialogTitle(m))
	}
	want := job{kind: jobCreate, dir: filepath.Join(root, "instances", "My Pack"), title: "Getting GTNH 2.8.1 ready", phase: "prepare"}
	if m.job == nil || *m.job != want {
		t.Fatalf("job %+v, want %+v", m.job, want)
	}
	if m.target != "2.8.1" || m.newName != "My Pack" || m.cancelCreate == nil {
		t.Errorf("target %q newName %q cancelCreate set %v", m.target, m.newName, m.cancelCreate != nil)
	}
}

// ---- C8 the pending instance ----

// C8
func TestC8BeginCreateStartsTheJob(t *testing.T) {
	m, _, root := createModel(t, Config{}, "Home")

	cmd := m.beginCreate("2.8.1", "My Pack")

	want := job{kind: jobCreate, dir: filepath.Join(root, "instances", "My Pack"), title: "Getting GTNH 2.8.1 ready", phase: "prepare"}
	if cmd == nil || m.job == nil || *m.job != want {
		t.Fatalf("cmd %v job %+v, want %+v", cmd != nil, m.job, want)
	}
	if m.target != "2.8.1" || m.newName != "My Pack" || m.cancelCreate == nil {
		t.Errorf("target %q newName %q cancelCreate set %v", m.target, m.newName, m.cancelCreate != nil)
	}
}

// C8
func TestC8PendingIsTheInstanceBeingCreated(t *testing.T) {
	m, _, root := creating(t, "Home")

	p, ok := m.pending()

	want := prism.Instance{Dir: filepath.Join(root, "instances", "My Pack"), Name: "My Pack", GTNH: true}
	if !ok || p.Dir != want.Dir || p.Name != want.Name || !p.GTNH {
		t.Errorf("pending = %+v %v, want %+v true", p, ok, want)
	}
}

// C8
func TestC8NothingIsPendingWithoutACreateJob(t *testing.T) {
	cases := []struct {
		name string
		job  func(m *model) *job
	}{
		{"no job", func(*model) *job { return nil }},
		{"update job", func(m *model) *job { return jobOn(m.insts[0].Dir, checking284, "prepare") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _ := createModel(t, Config{}, "Home")
			m.job = c.job(m)

			p, ok := m.pending()

			if ok || p != (prism.Instance{}) {
				t.Errorf("pending = %+v %v, want none", p, ok)
			}
		})
	}
}

// C8: listed last, selected, the page focused; the sidebar shows with one real instance.
func TestC8ThePendingInstanceIsListedLastAndSelected(t *testing.T) {
	m, _, root := creating(t, "Home")
	dir := filepath.Join(root, "instances", "My Pack")

	vis := m.visible()

	if len(vis) != 2 || vis[1].Dir != dir || vis[0].Name != "Home" {
		t.Fatalf("visible %s, want Home then My Pack", names(vis))
	}
	if cur, ok := m.current(); !ok || cur.Dir != dir || m.focus != focusPage {
		t.Errorf("current %q focus %v, want My Pack and the page", cur.Name, m.focus)
	}
	if !m.sidebarShows() {
		t.Errorf("sidebar hidden with one real and one pending instance")
	}
	if side := trimLines(m.sidebarView(10)); !containsLine(side, "◐ My Pack") {
		t.Errorf("sidebar %q, want ◐ My Pack", side)
	}
}

// C8
func TestC8ThePendingInstanceCanBeSelectedAgain(t *testing.T) {
	m, _, root := creating(t, "Home")
	m.focus = focusSidebar

	press(m, "up")
	away, _ := m.current()
	press(m, "down")
	back, _ := m.current()

	if away.Name != "Home" || back.Dir != filepath.Join(root, "instances", "My Pack") {
		t.Errorf("up selected %q, down %q; want Home then My Pack", away.Name, back.Dir)
	}
}

// C8: the page of the pending instance is its hero and the job block, nothing else.
func TestC8ThePendingPageIsTheHeroAndTheJob(t *testing.T) {
	m, _, _ := creating(t, "Home")

	got := pageLines(m, 60)

	want := []string{"My Pack", "GTNH 2.8.1 · being created", "", "▸ ◐ Getting GTNH 2.8.1 ready"}
	if !eq(got, want) {
		t.Errorf("page\n%q\nwant\n%q", got, want)
	}
	if ids := rowIDs(m); !eq(ids, []string{"job"}) {
		t.Errorf("rows %v, want [job]", ids)
	}
}

// C8
func TestC8ThePendingPageShowsTheProgress(t *testing.T) {
	m, _, _ := creating(t, "Home")
	m.Update(stepMsg("Downloading GTNH 2.8.1"))
	m.Update(progressMsg{500, 1000})

	got := pageLines(m, 100)

	for _, want := range []string{"◐ Getting GTNH 2.8.1 ready", "  50%", "  500 of 1,000 files", "Downloading GTNH 2.8.1"} {
		if !containsLine(got, want) {
			t.Errorf("page lacks %q:\n%s", want, strings.Join(got, "\n"))
		}
	}
	for _, not := range []string{"Play", "Update", "Settings", "Launcher"} {
		if containsLine(got, not) {
			t.Errorf("page has %q:\n%s", not, strings.Join(got, "\n"))
		}
	}
}

// C8: without real instances the pending page replaces the empty workspace.
func TestC8WithoutInstancesThePendingPageShows(t *testing.T) {
	m, _, _ := creating(t)

	lines := screen(m)

	if containsLine(lines, "No GTNH instances") || !containsLine(lines, "GTNH 2.8.1 · being created") {
		t.Errorf("screen\n%s\nwant the pending page", strings.Join(lines, "\n"))
	}
}

// C8: the page info comes from the manifest, the folder doesn't exist yet.
func TestC8PendingInfoComesFromTheRelease(t *testing.T) {
	cases := []struct {
		name   string
		drop   func(r *manifest.Release)
		flavor manifest.Flavor
	}{
		{"java 17", func(*manifest.Release) {}, manifest.Java17},
		{"java 8 only", func(r *manifest.Release) { r.Java17URL = "" }, manifest.Java8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			man := testManifest()
			c.drop(&man.Releases[1])
			m, _ := newTestModel(Config{PrismDirs: []string{root}}, 80, 24)
			m.Update(loadedMsg{m: man, insts: []prism.Instance{makeInst(t, root, fullSpec("Home"))}})
			m.beginCreate("2.8.1", "My Pack")
			p, _ := m.pending()

			info := m.home[p.Dir]

			if info.version != "2.8.1" || !info.gtnh || info.flavor != c.flavor {
				t.Errorf("home %+v, want version 2.8.1, gtnh, flavor %v", info, c.flavor)
			}
		})
	}
}

// C8: the pending instance can't be played.
func TestC8ThePendingInstanceIsNeverLaunched(t *testing.T) {
	m, f, _ := creating(t, "Home")

	runCmd(press(m, "p"))
	m.focus = focusSidebar
	runCmd(press(m, "enter"))

	if len(f.findCalls) != 0 || len(f.launches) != 0 {
		t.Errorf("findLauncher %d launches %d, want none", len(f.findCalls), len(f.launches))
	}
}

// ---- C9 createReady, createdMsg, errors ----

// C9
func TestC9ReadyAsksToConfirmTheCreation(t *testing.T) {
	cases := []struct {
		name       string
		serverMods string
		mods       *update.ModsPlan
		warns      []string
		flavor     manifest.Flavor
		files      int
		want       func(dir string) []string
	}{
		{"server mods, java 17", "https://mods.example.org/m.zip", modsPlanOf(update.ModAdd, "a.jar", update.ModAdd, "b.jar"), nil,
			manifest.Java17, 1234, func(dir string) []string {
				return []string{"1,234 files will be installed", "In " + dir, "Server mods from mods.example.org: 2 new",
					"Java 17+ pack", "Your other instances aren't touched"}
			}},
		{"server mods out of reach", "https://mods.example.org/m.zip", nil, []string{"I couldn't get your server's mods: the link doesn't lead to a file anymore."},
			manifest.Java17, 2, func(dir string) []string {
				return []string{"2 files will be installed", "In " + dir, "Java 17+ pack", "Your other instances aren't touched", "",
					"Heads up: I couldn't get your server's mods: the link doesn't lead to a file anymore."}
			}},
		{"java 8", "", nil, nil, manifest.Java8, 2, func(dir string) []string {
			return []string{"2 files will be installed", "In " + dir, "This version only comes as a Java 8 pack",
				"Your other instances aren't touched"}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _ := creating(t, "Home")
			m.serverMods, m.warns = c.serverMods, c.warns
			cr := &update.Creation{Dir: createDir(t), Flavor: c.flavor, Files: c.files, ModsPlan: c.mods}

			m.Update(createReady{cr})

			if m.creation != cr {
				t.Errorf("creation not kept")
			}
			if dialogTitle(m) != "Create My Pack with GTNH 2.8.1?" || !eq(dialogButtons(m), []string{"Create", "Cancel"}) {
				t.Fatalf("dialog %q buttons %q", dialogTitle(m), dialogButtons(m))
			}
			if got, want := bodyLines(t, m), c.want(cr.Dir); !eq(got, want) {
				t.Errorf("body\n%q\nwant\n%q", got, want)
			}
		})
	}
}

// ready is creating with the download done and the confirmation open.
func ready(t *testing.T, names ...string) (*model, *fakes, string, *update.Creation) {
	t.Helper()
	m, f, root := creating(t, names...)
	cr := &update.Creation{Dir: createDir(t), Flavor: manifest.Java17, Files: 3}
	m.Update(createReady{cr})
	return m, f, root, cr
}

// C9
func TestC9CreateStartsWritingTheInstance(t *testing.T) {
	m, _, _, _ := ready(t, "Home")

	cmd := press(m, "enter")

	if cmd == nil || m.dialog != nil || m.creation != nil {
		t.Errorf("cmd %v dialog %q creation %v, want the apply command", cmd != nil, dialogTitle(m), m.creation != nil)
	}
	if m.job == nil || m.job.phase != "apply" || m.job.title != "Creating My Pack" || !m.busyApplying() {
		t.Errorf("job %+v, want phase apply titled Creating My Pack", m.job)
	}
}

// C9, kills K2: Cancel throws the download away.
func TestC9CancelThrowsTheDownloadAway(t *testing.T) {
	for _, keys := range [][]string{{"right", "enter"}, {"esc"}} {
		t.Run(strings.Join(keys, "+"), func(t *testing.T) {
			m, _, root, cr := ready(t, "Home")

			press(m, keys...)

			if m.dialog != nil || m.job != nil {
				t.Errorf("dialog %q job %+v, want both gone", dialogTitle(m), m.job)
			}
			if exists(cr.Dir) {
				t.Errorf("the downloaded folder %s is still there", cr.Dir)
			}
			if visibleHas(m, filepath.Join(root, "instances", "My Pack")) {
				t.Errorf("visible %s still lists the pending instance", names(m.visible()))
			}
			if cur, ok := m.current(); !ok || cur.Name != "Home" {
				t.Errorf("current %q %v, want Home", cur.Name, ok)
			}
		})
	}
}

// C9
func TestC9CreatedEndsTheJobWithANotice(t *testing.T) {
	cases := []struct {
		name string
		r    func(in prism.Instance) *update.CreateResult
		want notice
	}{
		{"plain", func(in prism.Instance) *update.CreateResult {
			return &update.CreateResult{Instance: in, Files: 1234}
		}, notice{text: "Created just now with GTNH 2.8.1 · 1,234 files installed"}},
		{"server mods failed", func(in prism.Instance) *update.CreateResult {
			return &update.CreateResult{Instance: in, Files: 2, ModsErr: errors.New("the link doesn't lead to a file anymore")}
		}, notice{text: "Created just now with GTNH 2.8.1 · 2 files installed",
			warn: []string{"I couldn't sync your server's mods: the link doesn't lead to a file anymore."}}},
		{"one server mod", func(in prism.Instance) *update.CreateResult {
			return &update.CreateResult{Instance: in, Files: 2, Mods: modsPlanOf(update.ModAdd, "a.jar")}
		}, notice{text: "Created just now with GTNH 2.8.1 · 2 files installed", info: []string{"Server mods: 1 new"}}},
		{"server mods, one GTNH has", func(in prism.Instance) *update.CreateResult {
			return &update.CreateResult{Instance: in, Files: 2, Mods: modsPlanOf(update.ModAdd, "a.jar", update.ModAdd, "b.jar", update.ModSkip, "core.jar")}
		}, notice{text: "Created just now with GTNH 2.8.1 · 2 files installed",
			info: []string{"Server mods: 2 new", "Left out, this instance has it already: core.jar"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, root, _ := ready(t, "Home")
			press(m, "enter")
			in := makeInst(t, root, instSpec{name: "My Pack", gtnh: true, version: "2.8.1", java17: true})

			_, cmd := m.Update(createdMsg{c.r(in)})

			if m.job != nil {
				t.Errorf("job %+v, want none", m.job)
			}
			if got := m.notices[in.Dir]; !reflect.DeepEqual(got, c.want) {
				t.Errorf("notice\n%+v\nwant\n%+v", got, c.want)
			}
			msgs := runCmd(cmd)
			if len(msgs) != 1 {
				t.Fatalf("command yielded %v, want one reloadedMsg", msgs)
			}
			if _, ok := msgs[0].(reloadedMsg); !ok {
				t.Errorf("command yielded %T, want reloadedMsg", msgs[0])
			}
		})
	}
}

// C9: after the reload the new instance is selected and shows its notice.
func TestC9TheCreatedInstanceIsSelectedAfterTheReload(t *testing.T) {
	m, _, root, _ := ready(t, "Home")
	press(m, "enter")
	in := makeInst(t, root, instSpec{name: "My Pack", gtnh: true, version: "2.8.1", java17: true})
	_, cmd := m.Update(createdMsg{&update.CreateResult{Instance: in, Files: 3}})
	msgs := runCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("command yielded %v, want one reloadedMsg", msgs)
	}

	m.Update(msgs[0])

	if cur, ok := m.current(); !ok || cur.Dir != in.Dir {
		t.Fatalf("current %q, want My Pack", cur.Dir)
	}
	if page := flat(strings.Join(pageLines(m, 78), "\n")); !strings.Contains(page, "Created just now with GTNH 2.8.1 · 3 files installed") {
		t.Errorf("page lacks the notice:\n%s", page)
	}
}

// C9: errors end the job, drop the pending instance and say what's left.
func TestC9CreateErrorsSayWhatHappened(t *testing.T) {
	cases := []struct {
		name    string
		applied bool
		outcome string
	}{
		{"while downloading", false, "Nothing was created."},
		{"while writing", true, "I removed the half-made instance, so there's nothing to clean up."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, root := creating(t, "Home")
			if c.applied {
				m.Update(createReady{&update.Creation{Dir: createDir(t), Flavor: manifest.Java17, Files: 3}})
				press(m, "enter")
			}
			err := errors.New("the download broke")

			m.Update(errMsg{err})

			if m.job != nil || visibleHas(m, filepath.Join(root, "instances", "My Pack")) {
				t.Errorf("job %+v visible %s, want the job and the pending instance gone", m.job, names(m.visible()))
			}
			if _, ok := m.current(); !ok {
				t.Errorf("sel %d outside visible %s", m.sel, names(m.visible()))
			}
			if dialogTitle(m) != "Something went wrong" {
				t.Fatalf("dialog %q, want Something went wrong", dialogTitle(m))
			}
			if got, want := bodyLines(t, m), []string{err.Error(), "", c.outcome}; !eq(got, want) {
				t.Errorf("body %q, want %q", got, want)
			}
		})
	}
}

// C9: a folder that couldn't be removed is said by the error alone.
func TestC9ALeftoverFolderNeedsNoOutcome(t *testing.T) {
	m, _, _, _ := ready(t, "Home")
	press(m, "enter")
	err := errors.Join(errors.New("disk full"), &update.LeftoverError{Dir: filepath.Join(t.TempDir(), "My Pack"), Err: errors.New("in use")})

	m.Update(errMsg{err})

	if dialogTitle(m) != "Something went wrong" {
		t.Fatalf("dialog %q, want Something went wrong", dialogTitle(m))
	}
	if got := flat(m.dialog.body(200)); got != flat(err.Error()) {
		t.Errorf("body %q, want the error alone %q", got, flat(err.Error()))
	}
}

// C9
func TestC9JobOutcomeOfACreation(t *testing.T) {
	leftover := &update.LeftoverError{Dir: "x", Err: errors.New("in use")}
	cases := []struct {
		name, phase string
		err         error
		want        string
	}{
		{"prepare", "prepare", errors.New("broke"), "Nothing was created."},
		{"apply", "apply", errors.New("broke"), "I removed the half-made instance, so there's nothing to clean up."},
		{"leftover while preparing", "prepare", fmt.Errorf("broke: %w", leftover), ""},
		{"leftover while writing", "apply", errors.Join(errors.New("broke"), leftover), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _ := createModel(t, Config{}, "Home")
			m.job = &job{kind: jobCreate, dir: filepath.Join(t.TempDir(), "My Pack"), title: "Creating My Pack", phase: c.phase}

			if got := m.jobOutcome(c.err); got != c.want {
				t.Errorf("jobOutcome = %q, want %q", got, c.want)
			}
		})
	}
}

// ---- C10 quitting during the download ----

// C10: q and ctrl+c stop the download and wait for it to clean up.
func TestC10QuitDuringTheDownloadCancelsFirst(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		t.Run(k, func(t *testing.T) {
			m, _, _ := creating(t, "Home")
			cancels := 0
			orig := m.cancelCreate
			m.cancelCreate = func() { cancels++; orig() }

			cmd := press(m, k)

			if m.quitting || hasQuit(runCmd(cmd)) {
				t.Errorf("%s quit before the download was cleaned up", k)
			}
			if cancels == 0 || !m.quitAfterCancel {
				t.Errorf("cancels %d quitAfterCancel %v, want the download cancelled", cancels, m.quitAfterCancel)
			}
			pairs := m.statusPairs()
			if n := len(pairs); n < 2 || pairs[n-2] != "stopping" || pairs[n-1] != "cleaning up…" {
				t.Errorf("statusPairs %q, want them to end with stopping, cleaning up…", pairs)
			}
		})
	}
}

// C10: the cancelled download's error quits.
func TestC10TheCancelledDownloadsErrorQuits(t *testing.T) {
	m, _, _ := creating(t, "Home")
	press(m, "q")

	_, cmd := m.Update(errMsg{fmt.Errorf("download: %w", context.Canceled)})

	if !m.quitting || !hasQuit(runCmd(cmd)) {
		t.Errorf("quitting %v, want the program to quit", m.quitting)
	}
	if m.dialog != nil {
		t.Errorf("dialog %q, want none", dialogTitle(m))
	}
}

// C10: a download that finished anyway is thrown away, then the program quits.
func TestC10ADownloadReadyAfterQuitIsThrownAway(t *testing.T) {
	m, _, _ := creating(t, "Home")
	press(m, "q")
	cr := &update.Creation{Dir: createDir(t), Flavor: manifest.Java17, Files: 3}

	_, cmd := m.Update(createReady{cr})

	if exists(cr.Dir) {
		t.Errorf("the downloaded folder %s is still there", cr.Dir)
	}
	if !m.quitting || !hasQuit(runCmd(cmd)) {
		t.Errorf("quitting %v, want the program to quit", m.quitting)
	}
}

// C10: while the instance is written, quit keys do nothing.
func TestC10QuitKeysDoNothingWhileWriting(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		t.Run(k, func(t *testing.T) {
			m, _, _, _ := ready(t, "Home")
			press(m, "enter")

			cmd := press(m, k)

			if m.quitting || hasQuit(runCmd(cmd)) || m.quitAfterCancel {
				t.Errorf("%s acted while writing: quitting %v quitAfterCancel %v", k, m.quitting, m.quitAfterCancel)
			}
			pairs := m.statusPairs()
			if n := len(pairs); n < 2 || pairs[n-2] != "" || pairs[n-1] != "updating — please don't close this window" {
				t.Errorf("statusPairs %q, want them to end with the keyless updating — please don't close this window", pairs)
			}
		})
	}
}

// ---- C11 afterLoad ----

// C11
func TestC11CreateFromTheCommandLineOpensThePickerAfterLoad(t *testing.T) {
	for _, n := range []int{0, 1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			root := t.TempDir()
			var insts []prism.Instance
			if n > 0 {
				insts = append(insts, makeInst(t, root, fullSpec("Home")))
			}
			m, _ := newTestModel(Config{PrismDirs: []string{root}, Create: true}, 80, 24)

			m.Update(loadedMsg{m: testManifest(), insts: insts})

			if dialogTitle(m) != pickTitle {
				t.Errorf("dialog %q, want the picker", dialogTitle(m))
			}
		})
	}
}

// C11: creating wins over the command line's update.
func TestC11CreateWithATargetAsksTheNameInsteadOfUpdating(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, fullSpec("Home"))
	markAsked(t, in.Dir, modsURL)
	m, _ := newTestModel(Config{PrismDirs: []string{root}, Create: true, Target: "2.8.1", ServerMods: "none"}, 80, 24)

	m.Update(loadedMsg{m: testManifest(), insts: []prism.Instance{in}})

	if dialogTitle(m) != nameTitle || m.dialog.input.Value() != "GT New Horizons 2.8.1" {
		t.Errorf("dialog %q, want the name question for 2.8.1", dialogTitle(m))
	}
	if m.job != nil {
		t.Errorf("job %+v started, want none", m.job)
	}
}
