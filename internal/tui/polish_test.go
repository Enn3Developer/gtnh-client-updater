package tui

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// ---- C1: one shared release description ----

// C1: kind lowercased, " · ago" only when dated, then each tag in order.
func TestC1ReleaseDescIsTheLowercaseKindThenAgoThenTags(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	fiveDaysAgo := now.Add(-5*day - time.Hour)
	cases := []struct {
		name string
		r    manifest.Release
		tags []string
		want string
	}{
		{"stable with two tags",
			manifest.Release{Version: "2.8.4", Title: "Stable release", ReleaseDate: fiveDaysAgo},
			[]string{"recommended", "you have this one"},
			"stable release · 5 days ago · recommended · you have this one"},
		{"beta without a date",
			manifest.Release{Version: "2.9.0-beta-1", Title: "Beta release"},
			nil, "beta"},
		{"release candidate dated",
			manifest.Release{Version: "2.9.0-RC-1", Title: "Release candidate", ReleaseDate: now.Add(-40 * day)},
			nil, "release candidate · 5 weeks ago"},
		{"pre-release dated with a tag",
			manifest.Release{Version: "2.9.0-pre-2", Title: "Pre", ReleaseDate: now.Add(-80 * day)},
			[]string{"recommended"}, "pre-release · 2 months ago · recommended"},
		{"other release gives its title lowercased",
			manifest.Release{Version: "2.9.0-dev", Title: "Nightly Build", ReleaseDate: fiveDaysAgo},
			nil, "nightly build · 5 days ago"},
		{"undated stable with a tag",
			manifest.Release{Version: "2.8.0", Title: "Stable release"},
			[]string{"Java 8 only"}, "stable release · Java 8 only"},
		{"tags keep their order",
			manifest.Release{Version: "2.8.0", Title: "Stable release"},
			[]string{"you have this one", "not available for your Java"},
			"stable release · you have this one · not available for your Java"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := releaseDesc(c.r, now, c.tags...); got != c.want {
				t.Errorf("releaseDesc = %q, want %q", got, c.want)
			}
		})
	}
}

// C1b: an instance on the recommended version gets both tags, recommended first.
func TestC1bVersionPickerTagsRecommendedBeforeYouHaveThisOne(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4", java17: true})
	m, _ := loadedModel(root, 80, 24, in)
	m.focus = focusPage

	m.chooseVersion()

	if dialogTitle(m) != "Which GTNH version do you want?" {
		t.Fatalf("dialog %q", dialogTitle(m))
	}
	l := listOf(t, m)
	want := []ditem{
		{title: "2.8.4", desc: "stable release · 5 days ago · recommended · you have this one", key: "2.8.4"},
		{title: "2.8.1", desc: "stable release · 5 weeks ago", key: "2.8.1"},
		{title: "2.8.0", desc: "stable release · 2 months ago", key: "2.8.0"},
	}
	if !reflect.DeepEqual(l.items, want) {
		t.Errorf("items %+v, want %+v", l.items, want)
	}
}

// C1b: "you have this one" comes before "not available for your Java".
func TestC1bVersionPickerTagsYouHaveThisOneBeforeNotAvailable(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.0"})
	m, _ := newTestModel(Config{PrismDirs: []string{root}}, 80, 24)
	m.Update(loadedMsg{m: java8Manifest(), insts: []prism.Instance{in}})
	m.focus = focusPage

	m.chooseVersion()

	l := listOf(t, m)
	want := []ditem{
		{title: "2.9.0-beta-1", desc: "beta · 2 days ago", key: "2.9.0-beta-1"},
		{title: "2.8.4", desc: "stable release · 5 days ago · recommended", key: "2.8.4"},
		{title: "2.8.1", desc: "stable release · 5 weeks ago", key: "2.8.1"},
		{title: "2.8.0", desc: "stable release · you have this one · not available for your Java", key: "2.8.0"},
	}
	if !reflect.DeepEqual(l.items, want) {
		t.Errorf("items %+v, want %+v", l.items, want)
	}
}

// C1: the pickers use the injected clock for "ago".
func TestC1CreatePickerUsesTheInjectedClock(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	man := &manifest.Manifest{Releases: []manifest.Release{
		{Version: "2.8.4", Title: "Stable release", ReleaseDate: now.Add(-3*day - time.Hour),
			Java17URL: "https://downloads.gtnewhorizons.com/a17.zip", Java8URL: "https://downloads.gtnewhorizons.com/a8.zip"},
	}}
	m, _ := newTestModel(Config{PrismDirs: []string{t.TempDir()}}, 80, 24)
	m.now = func() time.Time { return now }
	m.Update(loadedMsg{m: man})

	m.pickCreateVersion()

	l := listOf(t, m)
	want := [][2]string{{"2.8.4", "stable release · 3 days ago · recommended"}}
	if got := itemTexts(l); !equalPairs(got, want) {
		t.Errorf("items %q, want %q", got, want)
	}
}

// C1d: the Update row keeps its text and nothing on the page is capitalized.
func TestC1dUpdateRowSaysTheKindLowercase(t *testing.T) {
	m, _ := oneFull(t)
	const w = 78

	got := pageLines(m, w)

	if l := lineWith(got, "is out"); l != hinted("  ", "GTNH 2.8.4 is out · stable release · 5 days ago", "u", w) {
		t.Errorf("update row = %q", l)
	}
	page := strings.Join(got, "\n")
	for _, capital := range []string{"Stable release", "Beta", "Release candidate", "Pre-release"} {
		if strings.Contains(page, capital) {
			t.Errorf("page contains %q:\n%s", capital, page)
		}
	}
}

// ---- C2: q and ctrl+c by the running job, editing or not ----

type quitCell struct {
	name     string
	kind     jobKind
	phase    string // "" = no job
	canceler bool   // set m.cancelCreate
	want     string // "quit" | "ignore" | "cancel"
}

var quitCells = []quitCell{
	{"no job", 0, "", false, "quit"},
	{"update preparing", jobUpdate, "prepare", false, "quit"},
	{"update applying", jobUpdate, "apply", false, "ignore"},
	{"create applying", jobCreate, "apply", false, "ignore"},
	{"create downloading", jobCreate, "prepare", true, "cancel"},
}

// C2a-C2c: ctrl+c while editing a setting, and q/ctrl+c outside a field (C2d).
func TestC2QuitKeysFollowTheRunningJob(t *testing.T) {
	for _, editing := range []bool{true, false} {
		keys := []string{"q", "ctrl+c"}
		if editing {
			keys = []string{"ctrl+c"} // q is typed into the field (C2d)
		}
		for _, k := range keys {
			for _, c := range quitCells {
				name := k + "/not editing/" + c.name
				if editing {
					name = k + "/editing/" + c.name
				}
				t.Run(name, func(t *testing.T) {
					checkQuitCell(t, editing, k, c)
				})
			}
		}
	}
}

func checkQuitCell(t *testing.T, editing bool, key string, c quitCell) {
	t.Helper()
	root := t.TempDir()
	in := makeInst(t, root, fullSpec("Home"))
	m, _ := loadedModel(root, 80, 24, in)
	m.focus = focusPage
	cfgBefore := seCfg(t, in.Dir)
	if editing {
		m.editSetting("memory")
		if m.edit == nil {
			t.Fatal("editSetting(memory) didn't start editing")
		}
		m.edit.input.SetValue("2048")
	}
	if c.phase != "" {
		m.job = &job{kind: c.kind, dir: filepath.Join(root, "instances", "Other"), title: "Working on Other", phase: c.phase}
	}
	cancels := 0
	if c.canceler {
		m.cancelCreate = func() { cancels++ }
	}

	cmd := press(m, key)

	switch c.want {
	case "quit": // C2a
		if !m.quitting || !hasQuit(runCmd(cmd)) {
			t.Errorf("quitting %v, want the program to quit", m.quitting)
		}
		if editing && m.edit != nil {
			t.Errorf("edit %+v, want the edit cancelled", m.edit)
		}
	case "ignore": // C2b
		if m.quitting || cmd != nil {
			t.Errorf("quitting %v cmd %v, want nothing mid-apply", m.quitting, cmd != nil)
		}
		if editing && (m.edit == nil || m.edit.input.Value() != "2048") {
			t.Errorf("edit %+v, want the field still open with 2048", m.edit)
		}
	case "cancel": // C2c
		if cancels != 1 || !m.quitAfterCancel {
			t.Errorf("cancels %d quitAfterCancel %v, want the download cancelled once", cancels, m.quitAfterCancel)
		}
		if m.quitting || hasQuit(runCmd(cmd)) {
			t.Errorf("quitting %v, want no quit before the cleanup", m.quitting)
		}
		if editing && m.edit != nil {
			t.Errorf("edit %+v, want the edit cancelled", m.edit)
		}
	}
	if got := seCfg(t, in.Dir); got != cfgBefore {
		t.Errorf("instance.cfg changed:\n%s\nwant\n%s", got, cfgBefore)
	}
}
