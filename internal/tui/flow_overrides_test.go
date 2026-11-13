package tui

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// idJar is a jar whose mcmod.info names the mod id, with body to tell jars apart.
func idJar(t *testing.T, id, body string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("mcmod.info")
	w.Write([]byte(`[{"modid":"` + id + `"}]`))
	w, _ = zw.Create("body.txt")
	w.Write([]byte(body))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// overridesHome is askedHome with GTNH's foo-1.0.jar and bar-1.jar in mods/, the
// selection on the Mod overrides row.
func overridesHome(t *testing.T) (*model, *fakes, prism.Instance) {
	t.Helper()
	m, f, in := askedHome(t, Config{})
	writeFile(t, filepath.Join(in.GameDir, "mods", "foo-1.0.jar"), idJar(t, "foo", "gtnh"))
	writeFile(t, filepath.Join(in.GameDir, "mods", "bar-1.jar"), idJar(t, "bar", "bar"))
	m.row = indexOf(rowIDs(m), "overrides")
	return m, f, in
}

// typeInto sets the open input dialog's field to v.
func typeInto(t *testing.T, m *model, v string) {
	t.Helper()
	if m.dialog == nil || m.dialog.input == nil {
		t.Fatalf("no input dialog open: %q", dialogTitle(m))
	}
	m.dialog.input.SetValue(v)
}

func TestOverridesRowValue(t *testing.T) {
	one := []update.Override{{Jar: "foo-custom.jar", Replaces: []string{"foo-1.0.jar"}}}
	for _, c := range []struct {
		ovs  []update.Override
		want string
	}{
		{nil, "none"},
		{one, "foo-custom.jar instead of GTNH's"},
		{append(one, update.Override{Jar: "b.jar"}), "2 jars instead of GTNH's"},
	} {
		if got := overridesValue(homeInfo{overrides: c.ovs}); got != c.want {
			t.Errorf("value = %q, want %q", got, c.want)
		}
	}
}

func TestOverridesAddAJarOfTheSameMod(t *testing.T) {
	m, _, in := overridesHome(t)
	mine := idJar(t, "foo", "mine")
	src := filepath.Join(t.TempDir(), "foo-custom.jar")
	writeFile(t, src, mine)

	press(m, "enter")
	if dialogTitle(m) != "Which jar do you want to use?" {
		t.Fatalf("dialog %q, want the field for a jar", dialogTitle(m))
	}
	typeInto(t, m, `"`+src+`"`)
	press(m, "enter")
	if dialogTitle(m) != "Use foo-custom.jar instead of GTNH's foo-1.0.jar?" {
		t.Fatalf("dialog %q, want the confirmation", dialogTitle(m))
	}
	if !strings.Contains(strings.Join(bodyLines(t, m), " "), "I'll switch off GTNH's foo-1.0.jar (it stays in the mods folder)") {
		t.Errorf("body = %q", bodyLines(t, m))
	}
	press(m, "enter")
	if m.dialog != nil {
		t.Fatalf("dialog %q still open: %q", dialogTitle(m), bodyLines(t, m))
	}
	if got, _ := os.ReadFile(filepath.Join(in.GameDir, "mods", "foo-custom.jar")); string(got) != mine {
		t.Errorf("the jar wasn't copied in")
	}
	if _, err := os.Stat(filepath.Join(in.GameDir, "mods", "foo-1.0.jar.disabled")); err != nil {
		t.Errorf("GTNH's jar wasn't switched off: %v", err)
	}
	if line := lineWith(screen(m), "Mod overrides"); !strings.Contains(line, "foo-custom.jar instead of GTNH's ✓ saved") {
		t.Errorf("row = %q", line)
	}
}

func TestOverridesAskWhichModWhenTheJarDoesntSay(t *testing.T) {
	m, _, _ := overridesHome(t)
	src := filepath.Join(t.TempDir(), "something.jar")
	writeFile(t, src, idJar(t, "something", "x"))

	press(m, "enter")
	typeInto(t, m, src)
	press(m, "enter")
	if dialogTitle(m) != "Which of GTNH's mods does something.jar replace?" {
		t.Fatalf("dialog %q, want the picker", dialogTitle(m))
	}
	if l := listOf(t, m); len(l.items) != 2 || l.items[0].key != "bar-1.jar" {
		t.Errorf("items = %+v", l.items)
	}
	press(m, "esc")
	if dialogTitle(m) != "Which jar do you want to use?" || m.dialog.input.Value() != src {
		t.Fatalf("esc: dialog %q, want the field back with the path", dialogTitle(m))
	}
	press(m, "enter", "down", "enter")
	if dialogTitle(m) != "Use something.jar instead of GTNH's foo-1.0.jar?" {
		t.Fatalf("dialog %q", dialogTitle(m))
	}
}

func TestOverridesBadJarStaysInTheField(t *testing.T) {
	m, _, _ := overridesHome(t)
	press(m, "enter")
	typeInto(t, m, filepath.Join(t.TempDir(), "nope.jar"))
	press(m, "enter")
	if dialogTitle(m) != "Which jar do you want to use?" || m.dialog.inputErr != "I can't find that file." {
		t.Errorf("dialog %q, error %q", dialogTitle(m), m.dialog.inputErr)
	}
}

func TestOverridesRemove(t *testing.T) {
	m, _, in := overridesHome(t)
	src := filepath.Join(t.TempDir(), "foo-custom.jar")
	writeFile(t, src, idJar(t, "foo", "mine"))
	if err := update.AddOverride(in, src, []string{"foo-1.0.jar"}); err != nil {
		t.Fatal(err)
	}
	m.refresh()

	press(m, "enter")
	l := listOf(t, m)
	if dialogTitle(m) != overridesTitle || len(l.items) != 2 || l.items[0].desc != "instead of GTNH's foo-1.0.jar" || l.items[1].title != "Add another…" {
		t.Fatalf("dialog %q items %+v", dialogTitle(m), l.items)
	}
	press(m, "enter")
	if dialogTitle(m) != "Stop using foo-custom.jar?" {
		t.Fatalf("dialog %q", dialogTitle(m))
	}
	press(m, "enter")
	if m.dialog != nil {
		t.Fatalf("dialog %q still open: %q", dialogTitle(m), bodyLines(t, m))
	}
	if _, err := os.Stat(filepath.Join(in.GameDir, "mods", "foo-1.0.jar")); err != nil {
		t.Errorf("GTNH's jar isn't back on: %v", err)
	}
	if line := lineWith(screen(m), "Mod overrides"); !strings.Contains(line, "none ✓ saved") {
		t.Errorf("row = %q", line)
	}
}

func TestOverridesRefusedWhileTheGameRuns(t *testing.T) {
	m, f, in := overridesHome(t)
	src := filepath.Join(t.TempDir(), "foo-custom.jar")
	writeFile(t, src, idJar(t, "foo", "mine"))
	f.running = true
	press(m, "enter")
	typeInto(t, m, src)
	press(m, "enter", "enter")
	if dialogTitle(m) != "The game is running" {
		t.Errorf("dialog %q", dialogTitle(m))
	}
	if _, err := os.Stat(filepath.Join(in.GameDir, "mods", "foo-custom.jar")); err == nil {
		t.Errorf("the jar was copied in while the game runs")
	}
}

func TestOverridesRefusedWhileAJobRuns(t *testing.T) {
	m, _, in := overridesHome(t)
	m.job = &job{kind: jobUpdate, dir: in.Dir, phase: "prepare"}
	m.manageOverrides()
	if dialogTitle(m) != "One thing at a time" {
		t.Errorf("dialog %q", dialogTitle(m))
	}
}

func TestCleanJarPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "My Mod.jar")
	writeFile(t, p, "x")
	if got := cleanJarPath(`'` + p + `'`); got != p {
		t.Errorf("quoted = %q", got)
	}
	if runtime.GOOS != "windows" {
		if got := cleanJarPath(strings.ReplaceAll(p, " ", `\ `)); got != p {
			t.Errorf("escaped = %q", got)
		}
	}
}

// ---- the update's question ----

// overrideSession is a session whose update changes the mod of foo-custom.jar, with conflicts.
func overrideSession(conflicts ...string) *update.Session {
	s := sessionOf(planOf(1, 0, conflicts...))
	s.Overrides = []update.OverrideChange{{Jar: "foo-custom.jar", Old: []string{"foo-1.0.jar"}, New: []string{"foo-1.1.jar"}}}
	return s
}

func TestUpdateAsksAboutOverridesBeforeTheConfigs(t *testing.T) {
	m, _, _ := startedHome(t, Config{}, "2.8.4")
	s := overrideSession(conflictAlpha)
	m.Update(preparedMsg{s})

	if dialogTitle(m) != "Which version of this mod do you want?" {
		t.Fatalf("dialog %q", dialogTitle(m))
	}
	l := listOf(t, m)
	if l.items[0].desc != "keep mine · GTNH has foo-1.1.jar" {
		t.Errorf("desc = %q", l.items[0].desc)
	}
	press(m, " ")
	if s.OverrideChoice("foo-custom.jar") != update.TakeNew || l.items[0].desc != "use GTNH's foo-1.1.jar" {
		t.Errorf("space: choice %v desc %q", s.OverrideChoice("foo-custom.jar"), l.items[0].desc)
	}
	press(m, "enter")
	if dialogTitle(m) != "1 config file changed both on your side and in the new version" {
		t.Fatalf("after enter: dialog %q, want the configs", dialogTitle(m))
	}
	press(m, "enter")
	if !strings.Contains(strings.Join(bodyLines(t, m), "\n"), "GTNH's foo-1.1.jar replaces your foo-custom.jar — yours goes to the backup") {
		t.Errorf("confirmation = %q", bodyLines(t, m))
	}
}

func TestUpdateOverridesPreselectedFromTheCommandLine(t *testing.T) {
	m, _, _ := startedHome(t, Config{Overrides: "gtnh"}, "2.8.4")
	s := overrideSession()
	m.Update(preparedMsg{s})
	if s.OverrideChoice("foo-custom.jar") != update.TakeNew {
		t.Errorf("choice = %v, want GTNH's", s.OverrideChoice("foo-custom.jar"))
	}
	press(m, "enter")
	if dialogTitle(m) != confirmTitle {
		t.Fatalf("dialog %q, want the confirmation", dialogTitle(m))
	}
}

func TestUpdateOverridesEscCancelsTheUpdate(t *testing.T) {
	m, _, _ := startedHome(t, Config{}, "2.8.4")
	m.Update(preparedMsg{overrideSession()})
	press(m, "esc")
	if m.dialog != nil || m.session != nil || m.job != nil {
		t.Errorf("esc: dialog %q, session %v, job %v", dialogTitle(m), m.session, m.job)
	}
}

func TestUpdateWithoutAQuestionStillSaysWhatHappensToOverrides(t *testing.T) {
	m, _, _ := startedHome(t, Config{}, "2.8.4")
	s := sessionOf(planOf(1, 0))
	s.Overrides = []update.OverrideChange{{Jar: "gone.jar", Old: []string{"g-1.jar"}, New: []string{"g-2.jar"}, Gone: true}}
	m.Update(preparedMsg{s})
	if dialogTitle(m) != confirmTitle {
		t.Fatalf("dialog %q, want the confirmation", dialogTitle(m))
	}
	if !strings.Contains(strings.Join(bodyLines(t, m), "\n"), "GTNH's g-2.jar comes back on: your gone.jar isn't in the mods folder any more") {
		t.Errorf("confirmation = %q", bodyLines(t, m))
	}
}
