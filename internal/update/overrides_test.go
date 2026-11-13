package update

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

const (
	fooOld = ".minecraft/mods/foo-1.0.jar"
	fooNew = ".minecraft/mods/foo-1.1.jar"
	barJar = ".minecraft/mods/bar-1.jar"
)

// modJar is a jar whose mcmod.info names the mod id ("" = none), with body to tell
// jars apart.
func modJar(t *testing.T, id, body string) string {
	t.Helper()
	files := map[string]string{"body.txt": body}
	if id != "" {
		files["mcmod.info"] = `[{"modid":"` + id + `"}]`
	}
	return string(zipBytes(t, "", files))
}

// srcJar writes the player's jar name, outside the instance, and returns its path.
func srcJar(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	writeFile(t, p, content)
	return p
}

// overrideInstance is an instance on 2.8.4 with GTNH's foo and bar jars, installed by
// this launcher (its baseline saved).
func overrideInstance(t *testing.T, pk map[string]string) prism.Instance {
	t.Helper()
	inst := newInstance(t, "GTNH 2.8.4", pk)
	os.WriteFile(filepath.Join(inst.Dir, "mmc-pack.json"), []byte(withRequired(nil)["mmc-pack.json"]), 0o644)
	if err := SaveState(inst.Dir, &State{Version: "2.8.4", Baseline: fps(withRequired(pk))}); err != nil {
		t.Fatal(err)
	}
	return inst
}

func TestOverrideAddThenRemove(t *testing.T) {
	foo, bar := modJar(t, "foo", "gtnh foo"), modJar(t, "bar", "bar")
	inst := overrideInstance(t, map[string]string{fooOld: foo, barJar: bar})
	mine := modJar(t, "foo", "my foo")
	src := srcJar(t, "foo-custom.jar", mine)

	c, err := CheckOverride(inst, src)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(c.Same) != "[foo-1.0.jar]" || fmt.Sprint(c.All) != "[bar-1.jar foo-1.0.jar]" || c.InMods {
		t.Fatalf("check = %+v", c)
	}
	if err := AddOverride(inst, src, c.Same); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != fmt.Sprintf("[bar-1.jar=%s foo-1.0.jar.disabled=%s foo-custom.jar=%s]", bar, foo, mine) {
		t.Errorf("mods/ after add = %s", got)
	}
	st, _ := LoadState(inst.Dir)
	if fmt.Sprint(st.Overrides) != "[{foo-custom.jar [foo-1.0.jar]}]" || st.Version != "2.8.4" {
		t.Errorf("state after add = %+v", st)
	}
	if _, err := CheckOverride(inst, src); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("checking it again = %v, want already in use", err)
	}

	if err := RemoveOverride(inst, "foo-custom.jar"); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != fmt.Sprintf("[bar-1.jar=%s foo-1.0.jar=%s]", bar, foo) {
		t.Errorf("mods/ after remove = %s", got)
	}
	if got, _ := os.ReadFile(filepath.Join(inst.Dir, StateDir, ReplacedMods, "foo-custom.jar")); string(got) != mine {
		t.Errorf("the player's jar isn't in replaced-mods: %q", got)
	}
	if st, _ := LoadState(inst.Dir); len(st.Overrides) != 0 {
		t.Errorf("overrides after remove = %v", st.Overrides)
	}
}

func TestCheckOverride(t *testing.T) {
	foo, bar := modJar(t, "foo", "gtnh foo"), modJar(t, "bar", "bar")
	inst := overrideInstance(t, map[string]string{fooOld: foo, barJar: bar})
	modsDir := filepath.Join(inst.GameDir, "mods")
	writeFile(t, filepath.Join(modsDir, "dropped-in.jar"), modJar(t, "foo", "dropped"))
	writeFile(t, filepath.Join(modsDir, "clash.jar"), modJar(t, "clash", "player's"))
	for _, tc := range []struct {
		name, src, want string
	}{
		{"a missing file", filepath.Join(t.TempDir(), "nope.jar"), "I can't find that file."},
		{"a folder", t.TempDir(), "That's a folder"},
		{"not a jar", srcJar(t, "notes.txt", "hi"), "That isn't a .jar file."},
		{"a broken jar", srcJar(t, "broken.jar", "not a zip"), "doesn't open as a .jar"},
		{"GTNH's own jar", filepath.Join(modsDir, "foo-1.0.jar"), "foo-1.0.jar is one of GTNH's own mods."},
		{"a name taken by another jar", srcJar(t, "clash.jar", modJar(t, "baz", "x")), "There's a different clash.jar"},
	} {
		if _, err := CheckOverride(inst, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	// A jar the player dropped into mods/ is used where it is; one with no mod id is
	// matched by its name without the version.
	c, err := CheckOverride(inst, filepath.Join(modsDir, "dropped-in.jar"))
	if err != nil || !c.InMods || fmt.Sprint(c.Same) != "[foo-1.0.jar]" {
		t.Errorf("dropped in: %+v, %v", c, err)
	}
	c, err = CheckOverride(inst, srcJar(t, "bar-2-patched.jar", modJar(t, "", "b2")))
	if err != nil || fmt.Sprint(c.Same) != "[bar-1.jar]" {
		t.Errorf("by name: %+v, %v", c, err)
	}
	c, err = CheckOverride(inst, srcJar(t, "other.jar", modJar(t, "other", "o")))
	if err != nil || len(c.Same) != 0 || fmt.Sprint(c.All) != "[bar-1.jar foo-1.0.jar]" {
		t.Errorf("no match: %+v, %v, want All to be GTNH's jars only", c, err)
	}
}

// overrideUpdate prepares 2.8.4 -> 2.9.0 of an instance using foo-custom.jar instead of
// GTNH's foo, where 2.9.0 ships newPk.
func overrideUpdate(t *testing.T, oldPk, newPk map[string]string) (prism.Instance, *Session, string) {
	t.Helper()
	inst := overrideInstance(t, oldPk)
	mine := modJar(t, "foo", "my foo")
	src := srcJar(t, "foo-custom.jar", mine)
	c, err := CheckOverride(inst, src)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddOverride(inst, src, c.Same); err != nil {
		t.Fatal(err)
	}
	h := newModsHost(t, map[string][]byte{
		"/old.zip": zipBytes(t, "GT New Horizons 2.8.4/", withRequired(oldPk)),
		"/new.zip": zipBytes(t, "GT New Horizons 2.9.0/", withRequired(newPk)),
	})
	s, err := Prepare(Options{Client: h.client(), Manifest: manifest284to290(t), Instance: inst,
		Installed: "2.8.4", Target: "2.9.0"}, &nopReporter{})
	if err != nil {
		t.Fatal(err)
	}
	return inst, s, mine
}

func TestUpdateKeepsAnOverrideAndUndoBringsItBack(t *testing.T) {
	foo, foo2, bar := modJar(t, "foo", "gtnh foo"), modJar(t, "foo", "gtnh foo 1.1"), modJar(t, "bar", "bar")
	inst, s, mine := overrideUpdate(t,
		map[string]string{fooOld: foo, barJar: bar, cfgA: "v1"},
		map[string]string{fooNew: foo2, barJar: bar, cfgA: "v2"})
	if fmt.Sprint(s.Overrides) != "[{foo-custom.jar [foo-1.0.jar] [foo-1.1.jar] false false}]" {
		t.Fatalf("overrides = %+v", s.Overrides)
	}
	if !s.Overrides[0].Asks() || s.OverrideChoice("foo-custom.jar") != KeepMine {
		t.Fatalf("the player decides, keeping theirs unless they choose")
	}
	if fmt.Sprint(s.Plan.ExtraMods) != "[]" {
		t.Errorf("extra mods = %v, want the override left out", s.Plan.ExtraMods)
	}
	if _, err := s.Apply(&nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != fmt.Sprintf("[bar-1.jar=%s foo-1.1.jar.disabled=%s foo-custom.jar=%s]", bar, foo2, mine) {
		t.Errorf("mods/ after the update = %s", got)
	}
	st, _ := LoadState(inst.Dir)
	if fmt.Sprint(st.Overrides) != "[{foo-custom.jar [foo-1.1.jar]}]" {
		t.Errorf("overrides after the update = %v", st.Overrides)
	}

	bs, _ := ListBackups(inst.Dir)
	if _, err := Restore(inst, bs[0], &nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != fmt.Sprintf("[bar-1.jar=%s foo-1.0.jar.disabled=%s foo-custom.jar=%s]", bar, foo, mine) {
		t.Errorf("mods/ after undo = %s", got)
	}
	st, _ = LoadState(inst.Dir)
	if fmt.Sprint(st.Overrides) != "[{foo-custom.jar [foo-1.0.jar]}]" || st.Version != "2.8.4" {
		t.Errorf("state after undo = %+v", st)
	}
}

func TestUpdateTakesGTNHsJarAndUndoBringsTheOverrideBack(t *testing.T) {
	foo, foo2, bar := modJar(t, "foo", "gtnh foo"), modJar(t, "foo", "gtnh foo 1.1"), modJar(t, "bar", "bar")
	inst, s, mine := overrideUpdate(t,
		map[string]string{fooOld: foo, barJar: bar},
		map[string]string{fooNew: foo2, barJar: bar})
	s.ChooseOverride("foo-custom.jar", TakeNew)
	if _, err := s.Apply(&nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != fmt.Sprintf("[bar-1.jar=%s foo-1.1.jar=%s]", bar, foo2) {
		t.Errorf("mods/ after the update = %s", got)
	}
	if st, _ := LoadState(inst.Dir); len(st.Overrides) != 0 {
		t.Errorf("overrides after the update = %v", st.Overrides)
	}

	bs, _ := ListBackups(inst.Dir)
	if _, err := Restore(inst, bs[0], &nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != fmt.Sprintf("[bar-1.jar=%s foo-1.0.jar.disabled=%s foo-custom.jar=%s]", bar, foo, mine) {
		t.Errorf("mods/ after undo = %s", got)
	}
	if st, _ := LoadState(inst.Dir); fmt.Sprint(st.Overrides) != "[{foo-custom.jar [foo-1.0.jar]}]" {
		t.Errorf("overrides after undo = %v", st.Overrides)
	}
}

func TestUpdateThatLeavesTheModAloneAsksNothing(t *testing.T) {
	foo, bar := modJar(t, "foo", "gtnh foo"), modJar(t, "bar", "bar")
	inst, s, mine := overrideUpdate(t,
		map[string]string{fooOld: foo, barJar: bar, cfgA: "v1"},
		map[string]string{fooOld: foo, barJar: modJar(t, "bar", "bar 2"), cfgA: "v2"})
	if len(s.Overrides) != 0 {
		t.Fatalf("overrides = %+v, want none to ask about", s.Overrides)
	}
	if _, err := s.Apply(&nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); !strings.Contains(got, "foo-1.0.jar.disabled="+foo+" foo-custom.jar="+mine+"]") {
		t.Errorf("mods/ = %s", got)
	}
	if st, _ := LoadState(inst.Dir); fmt.Sprint(st.Overrides) != "[{foo-custom.jar [foo-1.0.jar]}]" {
		t.Errorf("overrides = %v", st.Overrides)
	}
}

// An override with GTNH's jar's own name: GTNH's sits beside it, disabled, and is
// updated there.
func TestOverrideOfTheSameName(t *testing.T) {
	const fooJar = ".minecraft/mods/foo.jar"
	foo, foo2 := modJar(t, "foo", "gtnh foo"), modJar(t, "foo", "gtnh foo 2")
	inst := overrideInstance(t, map[string]string{fooJar: foo, barJar: "bar"})
	mine := modJar(t, "foo", "my foo")
	src := srcJar(t, "foo.jar", mine)
	c, err := CheckOverride(inst, src)
	if err != nil || fmt.Sprint(c.Same) != "[foo.jar]" {
		t.Fatalf("check = %+v, %v", c, err)
	}
	if err := AddOverride(inst, src, c.Same); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != fmt.Sprintf("[bar-1.jar=bar foo.jar=%s foo.jar.disabled=%s]", mine, foo) {
		t.Fatalf("mods/ after add = %s", got)
	}
	h := newModsHost(t, map[string][]byte{
		"/old.zip": zipBytes(t, "GT New Horizons 2.8.4/", withRequired(map[string]string{fooJar: foo, barJar: "bar"})),
		"/new.zip": zipBytes(t, "GT New Horizons 2.9.0/", withRequired(map[string]string{fooJar: foo2, barJar: "bar"})),
	})
	s, err := Prepare(Options{Client: h.client(), Manifest: manifest284to290(t), Instance: inst,
		Installed: "2.8.4", Target: "2.9.0"}, &nopReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(s.Overrides) != "[{foo.jar [foo.jar] [foo.jar] false false}]" {
		t.Fatalf("overrides = %+v", s.Overrides)
	}
	if _, err := s.Apply(&nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != fmt.Sprintf("[bar-1.jar=bar foo.jar=%s foo.jar.disabled=%s]", mine, foo2) {
		t.Errorf("mods/ after keeping mine = %s", got)
	}
	if err := RemoveOverride(inst, "foo.jar"); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != fmt.Sprintf("[bar-1.jar=bar foo.jar=%s]", foo2) {
		t.Errorf("mods/ after remove = %s", got)
	}
}

// The player deleted their jar by hand: the update switches GTNH's back on.
func TestUpdateDropsAnOverrideWhoseJarIsGone(t *testing.T) {
	foo, foo2 := modJar(t, "foo", "gtnh foo"), modJar(t, "foo", "gtnh foo 1.1")
	inst := overrideInstance(t, map[string]string{fooOld: foo, barJar: "bar"})
	src := srcJar(t, "foo-custom.jar", modJar(t, "foo", "mine"))
	if err := AddOverride(inst, src, []string{"foo-1.0.jar"}); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(inst.GameDir, "mods", "foo-custom.jar"))
	h := newModsHost(t, map[string][]byte{
		"/old.zip": zipBytes(t, "GT New Horizons 2.8.4/", withRequired(map[string]string{fooOld: foo, barJar: "bar"})),
		"/new.zip": zipBytes(t, "GT New Horizons 2.9.0/", withRequired(map[string]string{fooNew: foo2, barJar: "bar"})),
	})
	s, err := Prepare(Options{Client: h.client(), Manifest: manifest284to290(t), Instance: inst,
		Installed: "2.8.4", Target: "2.9.0"}, &nopReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Overrides) != 1 || !s.Overrides[0].Gone || s.Overrides[0].Asks() {
		t.Fatalf("overrides = %+v", s.Overrides)
	}
	if _, err := s.Apply(&nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != fmt.Sprintf("[bar-1.jar=bar foo-1.1.jar=%s]", foo2) {
		t.Errorf("mods/ = %s", got)
	}
	if st, _ := LoadState(inst.Dir); len(st.Overrides) != 0 {
		t.Errorf("overrides = %v", st.Overrides)
	}
}

// An override added after an update fits the old version after undo.
func TestUndoFitsAnOverrideAddedAfterTheUpdate(t *testing.T) {
	foo, foo2 := modJar(t, "foo", "gtnh foo"), modJar(t, "foo", "gtnh foo 1.1")
	oldPk, newPk := map[string]string{fooOld: foo, barJar: "bar"}, map[string]string{fooNew: foo2, barJar: "bar"}
	inst := overrideInstance(t, oldPk)
	h := newModsHost(t, map[string][]byte{
		"/old.zip": zipBytes(t, "GT New Horizons 2.8.4/", withRequired(oldPk)),
		"/new.zip": zipBytes(t, "GT New Horizons 2.9.0/", withRequired(newPk)),
	})
	s, err := Prepare(Options{Client: h.client(), Manifest: manifest284to290(t), Instance: inst,
		Installed: "2.8.4", Target: "2.9.0"}, &nopReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(&nopReporter{}); err != nil {
		t.Fatal(err)
	}
	mine := modJar(t, "foo", "mine")
	if err := AddOverride(inst, srcJar(t, "foo-custom.jar", mine), []string{"foo-1.1.jar"}); err != nil {
		t.Fatal(err)
	}
	bs, _ := ListBackups(inst.Dir)
	if _, err := Restore(inst, bs[0], &nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != fmt.Sprintf("[bar-1.jar=bar foo-1.0.jar.disabled=%s foo-custom.jar=%s]", foo, mine) {
		t.Errorf("mods/ after undo = %s", got)
	}
	if st, _ := LoadState(inst.Dir); fmt.Sprint(st.Overrides) != "[{foo-custom.jar [foo-1.0.jar]}]" {
		t.Errorf("overrides after undo = %v", st.Overrides)
	}
}

// The server's jar of a mod the player overrides is left out, like one GTNH ships.
func TestServerModsLeaveAnOverrideAlone(t *testing.T) {
	foo := modJar(t, "foo", "gtnh foo")
	inst := overrideInstance(t, map[string]string{fooOld: foo, barJar: "bar"})
	mine := modJar(t, "foo", "mine")
	if err := AddOverride(inst, srcJar(t, "foo-custom.jar", mine), []string{"foo-1.0.jar"}); err != nil {
		t.Fatal(err)
	}
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{"foo-server.jar": modJar(t, "foo", "server")})})
	_, p, err := syncMods(t, h, inst, modsLink)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(p.Names(ModSkip)) != "[foo-server.jar]" || p.Count(ModSetAside) != 0 {
		t.Errorf("plan = %v", describeAll(p))
	}
	if got := modsOnDisk(t, inst); !strings.Contains(got, "foo-custom.jar="+mine) || strings.Contains(got, "foo-server") {
		t.Errorf("mods/ = %s", got)
	}
}

func TestAddOverrideRefusesANameTakenByAnotherJar(t *testing.T) {
	inst := overrideInstance(t, map[string]string{fooOld: modJar(t, "foo", "f"), barJar: "bar"})
	writeFile(t, filepath.Join(inst.GameDir, "mods", "foo-custom.jar"), "player's other jar")
	err := AddOverride(inst, srcJar(t, "foo-custom.jar", modJar(t, "foo", "mine")), []string{"foo-1.0.jar"})
	if err == nil {
		t.Fatal("added over a jar of the same name")
	}
	if got := modsOnDisk(t, inst); !strings.Contains(got, "foo-1.0.jar=") || !strings.Contains(got, "foo-custom.jar=player's other jar") {
		t.Errorf("mods/ = %s, want untouched", got)
	}
}

// The new version ships the player's jar byte for byte: it's GTNH's from then on.
func TestUpdateThatShipsThePlayersJar(t *testing.T) {
	foo, bar := modJar(t, "foo", "gtnh foo"), modJar(t, "bar", "bar")
	mine := modJar(t, "foo", "my foo")
	inst, s, _ := overrideUpdate(t,
		map[string]string{fooOld: foo, barJar: bar},
		map[string]string{".minecraft/mods/foo-custom.jar": mine, barJar: bar})
	if len(s.Overrides) != 1 || !s.Overrides[0].Shipped || s.Overrides[0].Asks() {
		t.Fatalf("overrides = %+v", s.Overrides)
	}
	if _, err := s.Apply(&nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != fmt.Sprintf("[bar-1.jar=%s foo-custom.jar=%s]", bar, mine) {
		t.Errorf("mods/ = %s", got)
	}
	if st, _ := LoadState(inst.Dir); len(st.Overrides) != 0 {
		t.Errorf("overrides = %v", st.Overrides)
	}
}

// GTNH drops the mod: keeping the player's jar makes it a mod of their own; taking
// GTNH's side takes it out.
func TestUpdateThatDropsTheMod(t *testing.T) {
	foo, bar := modJar(t, "foo", "gtnh foo"), modJar(t, "bar", "bar")
	for _, c := range []struct {
		choice Choice
		want   string
	}{
		{KeepMine, "[bar-1.jar=%s foo-custom.jar=%s]"},
		{TakeNew, "[bar-1.jar=%s]"},
	} {
		inst, s, mine := overrideUpdate(t, map[string]string{fooOld: foo, barJar: bar}, map[string]string{barJar: bar})
		if fmt.Sprint(s.Overrides) != "[{foo-custom.jar [foo-1.0.jar] [] false false}]" {
			t.Fatalf("overrides = %+v", s.Overrides)
		}
		s.ChooseOverride("foo-custom.jar", c.choice)
		if _, err := s.Apply(&nopReporter{}); err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf(c.want, bar, mine)
		if c.choice == TakeNew {
			want = fmt.Sprintf(c.want, bar)
		}
		if got := modsOnDisk(t, inst); got != want {
			t.Errorf("%v: mods/ = %s, want %s", c.choice, got, want)
		}
		if st, _ := LoadState(inst.Dir); len(st.Overrides) != 0 {
			t.Errorf("%v: overrides = %v", c.choice, st.Overrides)
		}
	}
}
