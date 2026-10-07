package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// ---- C8 notices ----

// C8
func TestC8NoticeOfAnUpdate(t *testing.T) {
	keepMine := func(pl *update.Plan) *update.Plan {
		pl.ChooseAll(update.KeepMine)
		return pl
	}
	takeNew := func(pl *update.Plan) *update.Plan {
		pl.ChooseAll(update.TakeNew)
		return pl
	}
	cases := []struct {
		name string
		r    *update.Result
		pl   *update.Plan
		want notice
	}{
		{"files", &update.Result{To: "2.8.4"}, planOf(2, 1),
			notice{text: "Updated to GTNH 2.8.4 just now · 2 files updated, 1 removed"}},
		{"one file", &update.Result{To: "2.8.4"}, planOf(1, 0),
			notice{text: "Updated to GTNH 2.8.4 just now · 1 file updated, 0 removed"}},
		{"thousands", &update.Result{To: "2.8.4"}, planOf(1234, 5),
			notice{text: "Updated to GTNH 2.8.4 just now · 1,234 files updated, 5 removed"}},
		{"nothing to do", &update.Result{To: "2.8.4"}, planOf(0, 0),
			notice{text: "Updated to GTNH 2.8.4 just now · your files already matched"}},
		{"renamed and one .mcnew", &update.Result{To: "2.8.4", Renamed: "GTNH 2.8.4"}, keepMine(planOf(1, 0, conflictZeta)),
			notice{text: `Updated to GTNH 2.8.4 just now · 1 file updated, 0 removed · renamed to "GTNH 2.8.4" · 1 new config version saved as .mcnew`}},
		{"two .mcnew", &update.Result{To: "2.8.4"}, keepMine(planOf(0, 0, conflictZeta, conflictAlpha)),
			notice{text: "Updated to GTNH 2.8.4 just now · your files already matched · 2 new config versions saved as .mcnew"}},
		{"taken configs say nothing", &update.Result{To: "2.8.4"}, takeNew(planOf(0, 0, conflictZeta)),
			notice{text: "Updated to GTNH 2.8.4 just now · your files already matched"}},
		{"server mods failed", &update.Result{To: "2.8.4", ModsErr: errors.New("I couldn't install a.jar: disk full (your mods are as they were)")}, planOf(0, 0),
			notice{text: "Updated to GTNH 2.8.4 just now · your files already matched",
				warn: []string{"I couldn't sync your server's mods: I couldn't install a.jar: disk full (your mods are as they were)."}}},
		{"server out of reach", &update.Result{To: "2.8.4", Mods: modsPlanOf(), ModsErr: errors.New("the link doesn't lead to a file anymore")}, planOf(0, 0),
			notice{text: "Updated to GTNH 2.8.4 just now · your files already matched",
				warn: []string{"I couldn't check your server's mods: the link doesn't lead to a file anymore."}}},
		{"one server mod", &update.Result{To: "2.8.4", Mods: modsPlanOf(update.ModAdd, "a.jar")}, planOf(0, 0),
			notice{text: "Updated to GTNH 2.8.4 just now · your files already matched", info: []string{"Server mods: 1 new"}}},
		{"server mods counted", &update.Result{To: "2.8.4", Mods: modsPlanOf(update.ModAdd, "a.jar", update.ModUpdate, "b.jar",
			update.ModReplace, "c.jar", update.ModRemove, "x.jar", update.ModAdopt, "y.jar")}, planOf(0, 0),
			notice{text: "Updated to GTNH 2.8.4 just now · your files already matched", info: []string{"Server mods: 1 new, 2 updated, 1 removed"}}},
		{"server mods unchanged", &update.Result{To: "2.8.4", Mods: modsPlanOf(update.ModAdopt, "y.jar")}, planOf(0, 0),
			notice{text: "Updated to GTNH 2.8.4 just now · your files already matched"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := noticeOf(c.r, c.pl); !reflect.DeepEqual(got, c.want) {
				t.Errorf("noticeOf =\n%+v\nwant\n%+v", got, c.want)
			}
		})
	}
}

// C8: one line per part, each styled.
func TestC8NoticeLinesStyleEachPart(t *testing.T) {
	withColors(t)
	m, _ := oneFull(t)
	cases := []struct {
		name string
		n    notice
		want []string
	}{
		{"text only", notice{text: "Updated to GTNH 2.8.4 just now"},
			[]string{"  " + okSty.Render("Updated to GTNH 2.8.4 just now")}},
		{"all parts", notice{text: "Updated", warn: []string{"Mods failed.", "Look here."}, info: []string{"Server mods: 1 new", "Left out"}},
			[]string{"  " + okSty.Render("Updated"), "  " + warnSty.Render("Mods failed."), "  " + warnSty.Render("Look here."),
				"  " + dimSty.Render("Server mods: 1 new"), "  " + dimSty.Render("Left out")}},
		{"info without warn", notice{text: "Updated", info: []string{"Server mods: 1 removed"}},
			[]string{"  " + okSty.Render("Updated"), "  " + dimSty.Render("Server mods: 1 removed")}},
		{"warn without text", notice{warn: []string{"I couldn't sync your server's mods."}},
			[]string{"  " + warnSty.Render("I couldn't sync your server's mods.")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := m.noticeLines(60, c.n); !reflect.DeepEqual(got, c.want) {
				t.Errorf("noticeLines =\n%q\nwant\n%q", got, c.want)
			}
		})
	}
}

// C8
func TestC8NoticeLinesWrapToTheWidth(t *testing.T) {
	m, _ := oneFull(t)

	got := plainLines(m.noticeLines(30, notice{text: "Updated to GTNH 2.8.4 just now · 1 file updated, 0 removed"}))

	want := []string{"  Updated to GTNH 2.8.4 just", "  now · 1 file updated, 0", "  removed"}
	if !eq(got, want) {
		t.Errorf("noticeLines = %q, want %q", got, want)
	}
}

// applyUpdate drives an update of the current instance to 2.8.4 with pl through to
// appliedMsg{r}, then saves 2.8.4 as installed and delivers the reload with insts.
func applyUpdate(t *testing.T, m *model, in prism.Instance, pl *update.Plan, r *update.Result, insts []prism.Instance) {
	t.Helper()
	if cmd := m.startUpdate("2.8.4"); cmd == nil {
		t.Fatalf("startUpdate returned no command (dialog %q)", dialogTitle(m))
	}
	m.Update(preparedMsg{sessionOf(pl)})
	press(m, "enter")
	m.Update(appliedMsg{r})
	if err := update.UpdateState(in.Dir, func(st *update.State) { st.Version = "2.8.4" }); err != nil {
		t.Fatal(err)
	}
	m.Update(reloadedMsg{insts: insts})
}

// C8: the notice sits right under the Update heading, before the rows, and survives
// a refresh.
func TestC8NoticeShowsUnderTheUpdateHeading(t *testing.T) {
	m, _, in := askedHome(t, Config{})
	r := &update.Result{To: "2.8.4", ModsErr: errors.New("the link doesn't lead to a file anymore"),
		Mods: modsPlanOf(update.ModAdd, "a.jar")}
	applyUpdate(t, m, in, planOf(2, 1), r, []prism.Instance{in})

	before := pageLines(m, 78)
	m.refresh()
	after := pageLines(m, 78)

	want := []string{
		"Update",
		"  Updated to GTNH 2.8.4 just now · 2 files updated, 1 removed",
		"  I couldn't check your server's mods: the link doesn't lead to a file",
		"  anymore.",
		"  Server mods: 1 new",
		"  You have the newest stable version.",
	}
	for name, lines := range map[string][]string{"after the reload": before, "after a refresh": after} {
		i := indexOf(lines, "Update")
		if i < 0 || i+len(want) > len(lines) || !eq(lines[i:i+len(want)], want) {
			t.Errorf("%s: page\n%s\nwant the Update section to start with\n%s", name, strings.Join(lines, "\n"), strings.Join(want, "\n"))
		}
	}
}

// C8: notices are per instance.
func TestC8NoticeBelongsToItsInstance(t *testing.T) {
	root := t.TempDir()
	home := makeInst(t, root, fullSpec("Home"))
	markAsked(t, home.Dir, modsURL)
	second := makeInst(t, root, instSpec{name: "Second", gtnh: true, version: "2.8.4"})
	insts := []prism.Instance{home, second}
	m, _ := loadedModel(root, 80, 24, insts...)
	applyUpdate(t, m, home, planOf(2, 1), &update.Result{To: "2.8.4"}, insts)

	m.sel = 1
	onSecond := pageLines(m, 60)
	m.sel = 0
	onHome := pageLines(m, 60)

	if containsLine(onSecond, "Updated to GTNH") {
		t.Errorf("Second shows Home's notice:\n%s", strings.Join(onSecond, "\n"))
	}
	if !containsLine(onHome, "Updated to GTNH 2.8.4 just now") {
		t.Errorf("Home lacks its notice:\n%s", strings.Join(onHome, "\n"))
	}
}

// ---- C9 version picker ----

// C9
func TestC9VersionListTagsTheReleases(t *testing.T) {
	m, _ := oneFull(t)

	press(m, "o")

	if dialogTitle(m) != "Which GTNH version do you want?" {
		t.Fatalf("dialog %q", dialogTitle(m))
	}
	l := listOf(t, m)
	want := []ditem{
		{title: "2.8.4", desc: "stable release · 5 days ago · recommended", key: "2.8.4"},
		{title: "2.8.1", desc: "stable release · 5 weeks ago · you have this one", key: "2.8.1"},
		{title: "2.8.0", desc: "stable release · 2 months ago", key: "2.8.0"},
	}
	if !reflect.DeepEqual(l.items, want) || l.cursor != 0 {
		t.Errorf("items %+v cursor %d, want %+v at 0", l.items, l.cursor, want)
	}
}

// java8Manifest: a beta newer than the stable 2.8.4, and 2.8.0 (undated) without a
// Java 8 pack.
func java8Manifest() *manifest.Manifest {
	now := time.Now()
	return &manifest.Manifest{Releases: []manifest.Release{
		{Version: "2.9.0-beta-1", Title: "Beta release", ReleaseDate: now.Add(-2*day - time.Hour),
			Java17URL: "https://downloads.gtnewhorizons.com/b17.zip", Java8URL: "https://downloads.gtnewhorizons.com/b8.zip"},
		{Version: "2.8.4", Title: "Stable release", ReleaseDate: now.Add(-5*day - time.Hour),
			Java17URL: "https://downloads.gtnewhorizons.com/a17.zip", Java8URL: "https://downloads.gtnewhorizons.com/a8.zip"},
		{Version: "2.8.1", Title: "Stable release", ReleaseDate: now.Add(-40 * day),
			Java17URL: "https://downloads.gtnewhorizons.com/c17.zip", Java8URL: "https://downloads.gtnewhorizons.com/c8.zip"},
		{Version: "2.8.0", Title: "Stable release",
			Java17URL: "https://downloads.gtnewhorizons.com/d17.zip"},
	}}
}

func java8Home(t *testing.T) (*model, prism.Instance) {
	t.Helper()
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.1"})
	markAsked(t, in.Dir, "")
	m, _ := newTestModel(Config{PrismDirs: []string{root}}, 80, 24)
	m.Update(loadedMsg{m: java8Manifest(), insts: []prism.Instance{in}})
	m.focus = focusPage
	return m, in
}

// C9: cursor on the recommended one; unavailable packs say so; no date, no "ago".
func TestC9VersionListMarksPacksMissingForTheInstancesJava(t *testing.T) {
	m, _ := java8Home(t)

	cmd := m.chooseVersion()

	if cmd != nil {
		t.Errorf("chooseVersion returned a command")
	}
	l := listOf(t, m)
	want := []ditem{
		{title: "2.9.0-beta-1", desc: "beta · 2 days ago", key: "2.9.0-beta-1"},
		{title: "2.8.4", desc: "stable release · 5 days ago · recommended", key: "2.8.4"},
		{title: "2.8.1", desc: "stable release · 5 weeks ago · you have this one", key: "2.8.1"},
		{title: "2.8.0", desc: "stable release · not available for your Java", key: "2.8.0"},
	}
	if !reflect.DeepEqual(l.items, want) || l.cursor != 1 {
		t.Errorf("items %+v cursor %d, want %+v at 1", l.items, l.cursor, want)
	}
}

// C9
func TestC9PickingAMissingPackExplainsWhy(t *testing.T) {
	cases := []struct {
		name   string
		java17 bool
		drop   func(r *manifest.Release)
		want   string
	}{
		{"java 8", false, func(r *manifest.Release) { r.Java8URL = "" }, "GTNH 2.8.0 has no Java 8 pack, which is what this instance uses."},
		{"java 17", true, func(r *manifest.Release) { r.Java17URL = "" }, "GTNH 2.8.0 has no Java 17+ pack, which is what this instance uses."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.1", java17: c.java17})
			markAsked(t, in.Dir, "")
			man := testManifest()
			c.drop(&man.Releases[2])
			m, _ := newTestModel(Config{PrismDirs: []string{root}}, 80, 24)
			m.Update(loadedMsg{m: man, insts: []prism.Instance{in}})
			m.chooseVersion()

			cmd := press(m, "down", "down", "enter")

			if cmd != nil || m.jobShown(in.Dir) {
				t.Errorf("cmd %v job %+v, want nothing started", cmd != nil, m.job)
			}
			if dialogTitle(m) != "Not available" {
				t.Fatalf("dialog %q, want Not available", dialogTitle(m))
			}
			if got := flat(m.dialog.body(200)); got != c.want {
				t.Errorf("body %q, want %q", got, c.want)
			}
		})
	}
}

// C9
func TestC9PickingAVersionStartsTheUpdate(t *testing.T) {
	m, _, in := askedHome(t, Config{})
	press(m, "o")

	cmd := press(m, "down", "down", "enter")

	if cmd == nil || m.dialog != nil {
		t.Errorf("cmd %v dialog %q, want the prepare command and no dialog", cmd != nil, dialogTitle(m))
	}
	if !m.jobShown(in.Dir) || m.job.title != "Checking what GTNH 2.8.0 changes" || m.target != "2.8.0" {
		t.Errorf("job %+v target %q, want 2.8.0", m.job, m.target)
	}
}

// C9/C3a
func TestC9VersionListRefusesWhileAJobRuns(t *testing.T) {
	m, _, _ := askedHome(t, Config{})
	m.job = jobOn(m.insts[0].Dir, checking284, "prepare")

	cmd := m.chooseVersion()

	if cmd != nil || dialogTitle(m) != "One thing at a time" {
		t.Fatalf("cmd %v dialog %q, want One thing at a time", cmd != nil, dialogTitle(m))
	}
	if got := flat(m.dialog.body(200)); got != "I'm still busy with Home. Let it finish first." {
		t.Errorf("body %q", got)
	}
}

// C9: u on an outdated instance updates to the recommended version.
func TestC9UpdateKeyStartsTheRecommendedUpdate(t *testing.T) {
	m, _, in := askedHome(t, Config{})

	cmd := press(m, "u")

	if cmd == nil || !m.jobShown(in.Dir) || m.job.title != checking284 || m.target != "2.8.4" {
		t.Errorf("cmd %v job %+v target %q, want an update to 2.8.4", cmd != nil, m.job, m.target)
	}
}

// C9: u on an instance of unknown version first asks which version it is.
func TestC9UpdateKeyOnAnUnknownVersionAsksFirst(t *testing.T) {
	m, in := unknownHome(t, true)

	cmd := press(m, "u")

	if cmd != nil || m.jobShown(in.Dir) || dialogTitle(m) != "Which GTNH version is Home on right now?" {
		t.Errorf("cmd %v job %+v dialog %q, want the which-version question", cmd != nil, m.job, dialogTitle(m))
	}
}
