package update

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// instanceFiles is a test instance with the contents of GTNH's foo and bar jars.
type instanceFiles struct {
	prism.Instance
	foo, bar string
}

func TestFetchModsReadsTheOverridesFolder(t *testing.T) {
	data := zipBytes(t, "custom_mods/", map[string]string{
		"a.jar": "a", "overrides/foo-server.jar": "f", "overrides/deep/x.jar": "x", "other/y.jar": "y",
	})
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": data})
	a, err := fetchFrom(t, h)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(a.Names(), a.over, a.ignored); got != "[a.jar foo-server.jar] map[foo-server.jar:true] [other/y.jar overrides/deep/x.jar]" {
		t.Errorf("archive = %s", got)
	}

	// Only overrides: the folder isn't taken for the zipped folder, and it isn't "inside folders".
	h.set("/custom_mods.zip", modsZipOf(t, map[string]string{"overrides/foo-server.jar": "f"}))
	if a, err = fetchFrom(t, h); err != nil || fmt.Sprint(a.Names(), a.over) != "[foo-server.jar] map[foo-server.jar:true]" {
		t.Errorf("only overrides: %v, %v", err, a)
	}
}

// serverOverrideInstance is an instance GTNH 2.8.4 installed with GTNH's foo and bar, that
// syncs from modsLink.
func serverOverrideInstance(t *testing.T) (inst instanceFiles) {
	t.Helper()
	inst.foo, inst.bar = modJar(t, "foo", "gtnh foo"), modJar(t, "bar", "bar")
	inst.Instance = overrideInstance(t, map[string]string{fooOld: inst.foo, barJar: inst.bar})
	if err := UpdateState(inst.Dir, func(st *State) { st.CustomModsURL, st.CustomModsAsked = modsLink, true }); err != nil {
		t.Fatal(err)
	}
	return inst
}

func TestServerOverrideSwitchesGTNHsJarOffAndBackOn(t *testing.T) {
	inst := serverOverrideInstance(t)
	server := modJar(t, "foo", "server foo")
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{
		"overrides/foo-server.jar": server, "extra.jar": "e",
	})})
	_, p, err := syncMods(t, h, inst.Instance, "")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(p.Names(ModAdd), p.Names(ModDisable), p.Overrides) != "[extra.jar foo-server.jar] [foo-1.0.jar] [{foo-server.jar [foo-1.0.jar]}]" {
		t.Errorf("plan = %v, overrides %v", describeAll(p), p.Overrides)
	}
	want := fmt.Sprintf("[bar-1.jar=%s extra.jar=e foo-1.0.jar.disabled=%s foo-server.jar=%s]", inst.bar, inst.foo, server)
	if got := modsOnDisk(t, inst.Instance); got != want {
		t.Errorf("mods/ = %s, want %s", got, want)
	}
	st, _ := LoadState(inst.Dir)
	if fmt.Sprint(st.CustomMods, st.ServerOverrides) != "[extra.jar foo-server.jar] [{foo-server.jar [foo-1.0.jar]}]" {
		t.Errorf("state = %v %v", st.CustomMods, st.ServerOverrides)
	}
	if _, p, _ := syncMods(t, h, inst.Instance, ""); p.writes() || fmt.Sprint(p.Overrides) != "[{foo-server.jar [foo-1.0.jar]}]" {
		t.Errorf("a second sync = %v, overrides %v", describeAll(p), p.Overrides)
	}

	// A server jar of the same mod outside the overrides folder doesn't override.
	h.set("/custom_mods.zip", modsZipOf(t, map[string]string{"foo-server.jar": server, "extra.jar": "e"}))
	if _, p, err = syncMods(t, h, inst.Instance, ""); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(describeAll(p)) != "[on foo-1.0.jar @foo-1.0.jar.disabled skip foo-server.jar ~foo-1.0.jar -foo-server.jar]" {
		t.Errorf("plan = %v", describeAll(p))
	}
	if got := modsOnDisk(t, inst.Instance); got != fmt.Sprintf("[bar-1.jar=%s extra.jar=e foo-1.0.jar=%s]", inst.bar, inst.foo) {
		t.Errorf("mods/ = %s, want GTNH's foo back on", got)
	}
	if st, _ := LoadState(inst.Dir); len(st.ServerOverrides) != 0 {
		t.Errorf("server overrides = %v", st.ServerOverrides)
	}
}

func TestStoppingServerModsSwitchesGTNHsJarBackOn(t *testing.T) {
	inst := serverOverrideInstance(t)
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{
		"overrides/foo-server.jar": modJar(t, "foo", "server foo"),
	})})
	if _, _, err := syncMods(t, h, inst.Instance, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := syncMods(t, h, inst.Instance, "none"); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst.Instance); got != fmt.Sprintf("[bar-1.jar=%s foo-1.0.jar=%s]", inst.bar, inst.foo) {
		t.Errorf("mods/ = %s", got)
	}
}

// The server's override wins over the player's own override of that mod.
func TestServerOverrideTakesOverThePlayers(t *testing.T) {
	inst := serverOverrideInstance(t)
	mine := modJar(t, "foo", "mine")
	if err := AddOverride(inst.Instance, srcJar(t, "foo-custom.jar", mine), []string{"foo-1.0.jar"}); err != nil {
		t.Fatal(err)
	}
	server := modJar(t, "foo", "server foo")
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{"overrides/foo-server.jar": server})})
	_, p, err := syncMods(t, h, inst.Instance, "")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(p.Names(ModSetAside), p.Dropped, p.Count(ModDisable)) != "[foo-custom.jar] [foo-custom.jar] 0" {
		t.Errorf("plan = %v, dropped %v", describeAll(p), p.Dropped)
	}
	want := fmt.Sprintf("[bar-1.jar=%s foo-1.0.jar.disabled=%s foo-server.jar=%s]", inst.bar, inst.foo, server)
	if got := modsOnDisk(t, inst.Instance); got != want {
		t.Errorf("mods/ = %s, want %s", got, want)
	}
	if got := dirContents(t, filepath.Join(inst.Dir, StateDir, ReplacedMods)); got != "[foo-custom.jar="+mine+"]" {
		t.Errorf("replaced-mods = %s", got)
	}
	st, _ := LoadState(inst.Dir)
	if len(st.Overrides) != 0 || fmt.Sprint(st.ServerOverrides) != "[{foo-server.jar [foo-1.0.jar]}]" {
		t.Errorf("overrides = %v, server's %v", st.Overrides, st.ServerOverrides)
	}

	if _, err := CheckOverride(inst.Instance, srcJar(t, "foo-again.jar", modJar(t, "foo", "again"))); err == nil ||
		!strings.Contains(err.Error(), "foo-server.jar") {
		t.Errorf("CheckOverride of the server's mod = %v", err)
	}
}

// A server override with the name of GTNH's jar can't stand beside it: it's left out.
func TestServerOverrideNamedLikeGTNHsJarIsLeftOut(t *testing.T) {
	inst := serverOverrideInstance(t)
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{
		"overrides/foo-1.0.jar": modJar(t, "foo", "server foo"),
	})})
	_, p, err := syncMods(t, h, inst.Instance, "")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(p.Names(ModSkip)) != "[foo-1.0.jar]" || p.writes() || len(p.Overrides) != 0 {
		t.Errorf("plan = %v", describeAll(p))
	}
}

// An update installs GTNH's new jar of the overridden mod switched off, and undo and the
// next sync keep it that way.
func TestServerOverrideAcrossUpdateAndUndo(t *testing.T) {
	foo2 := modJar(t, "foo", "gtnh foo 1.1")
	inst := serverOverrideInstance(t)
	server := modJar(t, "foo", "server foo")
	oldPk := map[string]string{fooOld: inst.foo, barJar: inst.bar}
	h := newModsHost(t, map[string][]byte{
		"/old.zip":         zipBytes(t, "GT New Horizons 2.8.4/", withRequired(oldPk)),
		"/new.zip":         zipBytes(t, "GT New Horizons 2.9.0/", withRequired(map[string]string{fooNew: foo2, barJar: inst.bar})),
		"/custom_mods.zip": modsZipOf(t, map[string]string{"overrides/foo-server.jar": server}),
	})
	if _, _, err := syncMods(t, h, inst.Instance, ""); err != nil {
		t.Fatal(err)
	}
	s, err := Prepare(Options{Client: h.client(), Manifest: manifest284to290(t), Instance: inst.Instance,
		Installed: "2.8.4", Target: "2.9.0", CustomModsURL: modsLink}, &nopReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Overrides) != 0 {
		t.Errorf("the update asks about %v, want nothing: the server decides", s.Overrides)
	}
	if fmt.Sprint(s.ModsPlan.Names(ModDisable)) != "[foo-1.1.jar]" {
		t.Errorf("preview = %v", describeAll(s.ModsPlan))
	}
	res, err := s.Apply(&nopReporter{})
	if err != nil || res.ModsErr != nil {
		t.Fatal(err, res.ModsErr)
	}
	want := fmt.Sprintf("[bar-1.jar=%s foo-1.1.jar.disabled=%s foo-server.jar=%s]", inst.bar, foo2, server)
	if got := modsOnDisk(t, inst.Instance); got != want {
		t.Errorf("mods/ after the update = %s, want %s", got, want)
	}
	if st, _ := LoadState(inst.Dir); fmt.Sprint(st.ServerOverrides) != "[{foo-server.jar [foo-1.1.jar]}]" {
		t.Errorf("server overrides after the update = %v", st.ServerOverrides)
	}

	bs, _ := ListBackups(inst.Dir)
	if _, err := Restore(inst.Instance, bs[0], &nopReporter{}); err != nil {
		t.Fatal(err)
	}
	want = fmt.Sprintf("[bar-1.jar=%s foo-1.0.jar.disabled=%s foo-server.jar=%s]", inst.bar, inst.foo, server)
	if got := modsOnDisk(t, inst.Instance); got != want {
		t.Errorf("mods/ after undo = %s, want %s", got, want)
	}
	if _, p, err := syncMods(t, h, inst.Instance, ""); err != nil || p.writes() || fmt.Sprint(p.Overrides) != "[{foo-server.jar [foo-1.0.jar]}]" {
		t.Errorf("sync after undo = %v, %v, overrides %v", err, describeAll(p), p.Overrides)
	}
}

// Removing the player's override of another mod never switches on a jar the server
// overrides.
func TestRemovingAnOverrideLeavesTheServersAlone(t *testing.T) {
	inst := serverOverrideInstance(t)
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{
		"overrides/foo-server.jar": modJar(t, "foo", "server foo"),
	})})
	if _, _, err := syncMods(t, h, inst.Instance, ""); err != nil {
		t.Fatal(err)
	}
	if err := UpdateState(inst.Dir, func(st *State) {
		st.Overrides = []Override{{Jar: "odd.jar", Replaces: []string{"foo-1.0.jar"}}}
	}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(inst.GameDir, "mods", "odd.jar"), "odd")
	if err := RemoveOverride(inst.Instance, "odd.jar"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(inst.GameDir, "mods", "foo-1.0.jar")); err == nil {
		t.Errorf("GTNH's foo was switched back on under the server's override")
	}
}
