package update

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// restoreReporter records steps and the last progress call.
type restoreReporter struct {
	steps       []string
	warns       []string
	progress    [][2]int64
	lastDone    int64
	lastTotal   int64
	sawProgress bool
}

func (r *restoreReporter) Step(s string) { r.steps = append(r.steps, s) }
func (r *restoreReporter) Progress(done, total int64) {
	r.progress = append(r.progress, [2]int64{done, total})
	r.lastDone, r.lastTotal, r.sawProgress = done, total, true
}
func (r *restoreReporter) Warn(s string) { r.warns = append(r.warns, s) }

// writeFile creates path (and its parents) with body.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// packServer serves the given URL paths (on any host, via hostRewrite) and returns a
// client plus a manifest with 2.8.1 (/old.zip) and 2.8.4 (/new.zip).
func packServer(t *testing.T, files map[string][]byte) (*http.Client, *manifest.Manifest) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, r.URL.Path, time.Time{}, bytes.NewReader(b))
	}))
	t.Cleanup(srv.Close)
	m, err := manifest.Parse([]byte(`{
	  "2.8.1": {"title":"Stable release","releaseDate":"2025/10/19","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/old.zip"}},
	  "2.8.4": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/new.zip"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: hostRewrite{srv}}, m
}

const (
	takeCfg = ".minecraft/config/take.cfg"
	keepCfg = ".minecraft/config/keep.cfg"
	delCfg  = ".minecraft/config/del.cfg"
	sameCfg = ".minecraft/config/same.cfg"
	newMod  = ".minecraft/mods/new-1.jar"
	mineJar = ".minecraft/mods/mine.jar"
	world   = ".minecraft/saves/w/level.dat"
	name281 = "GT_New_Horizons_2.8.1_Java_17-25"
)

// C1, C2, C3, C4, C6-C10, K1, K2: a full update 2.8.1 -> 2.8.4 is recorded in the
// backup manifest and Restore undoes it.
func TestUpdateThenRestoreRoundTrip(t *testing.T) {
	oldPack := withRequired(map[string]string{
		takeCfg: "v1", keepCfg: "v1", delCfg: "v1", sameCfg: "s", modX: "x1", "instance.cfg": "name=pack",
	})
	newPack := withRequired(map[string]string{
		takeCfg: "v2", keepCfg: "v2", delCfg: "v2", sameCfg: "s", newMod: "n1",
	})
	client, m := packServer(t, map[string][]byte{
		"/old.zip":         zipBytes(t, "GT New Horizons 2.8.1/", oldPack),
		"/new.zip":         zipBytes(t, "GT New Horizons 2.8.4/", newPack),
		"/custom_mods.zip": zipBytes(t, "", map[string]string{"extra-1.jar": "e1"}),
	})
	inst := newInstance(t, name281, map[string]string{
		takeCfg: "mine-take", keepCfg: "mine-keep", sameCfg: "s", modX: "x1",
		mineJar: "player", world: "world",
	})
	writeFile(t, filepath.Join(inst.Dir, "mmc-pack.json"), oldPack["mmc-pack.json"])
	const customURL = "https://files.example/custom_mods.zip"
	opts := Options{Client: client, Manifest: m, Instance: inst, Installed: "2.8.1", Target: "2.8.4",
		CustomModsURL: customURL}
	rep := &nopReporter{}

	s, err := Prepare(opts, rep)
	if err != nil {
		t.Fatal(err)
	}
	s.Plan.Choose(takeCfg, TakeNew)
	s.Plan.Choose(keepCfg, KeepMine)
	s.Plan.Choose(delCfg, TakeNew)
	start := time.Now()
	ures, err := s.Apply(rep)
	if err != nil {
		t.Fatal(err)
	}
	end := time.Now()
	if ures.BackupDir == "" {
		t.Fatal("update made no backup dir")
	}

	// C4: exactly one backup with the right info.
	list, err := ListBackups(inst.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("ListBackups = %d entries, want 1: %+v", len(list), list)
	}
	b := list[0]

	// C3: manifest file is valid indented JSON.
	data, err := os.ReadFile(filepath.Join(ures.BackupDir, BackupManifest))
	if err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	if !json.Valid(data) || !strings.Contains(string(data), "\n") || !strings.Contains(string(data), `"from": "2.8.1"`) {
		t.Errorf("manifest is not indented JSON with from=2.8.1:\n%s", data)
	}
	if b.Dir != ures.BackupDir {
		t.Errorf("Backup.Dir = %q, want %q", b.Dir, ures.BackupDir)
	}
	if b.Info.From != "2.8.1" || b.Info.To != "2.8.4" {
		t.Errorf("From/To = %q/%q, want 2.8.1/2.8.4", b.Info.From, b.Info.To)
	}
	if b.Info.PrevName != name281 {
		t.Errorf("PrevName = %q, want %q", b.Info.PrevName, name281)
	}
	if b.Info.PrevState != nil {
		t.Errorf("PrevState = %+v, want nil (no state.json before)", b.Info.PrevState)
	}
	if b.Info.When.Before(start.Truncate(time.Second)) || b.Info.When.After(end.Add(time.Second)) {
		t.Errorf("When = %v, want between %v and %v", b.Info.When, start, end)
	}
	wantAdded := "[.minecraft/config/del.cfg .minecraft/config/keep.cfg.mcnew .minecraft/mods/new-1.jar]"
	if got := fmt.Sprint(b.Info.Added); got != wantAdded {
		t.Errorf("Added = %s, want %s", got, wantAdded)
	}
	if got := fmt.Sprint(b.Info.AddedMods); got != "[extra-1.jar]" {
		t.Errorf("AddedMods = %s, want [extra-1.jar]", got)
	}

	updState, err := LoadState(inst.Dir)
	if err != nil || updState == nil {
		t.Fatalf("state after update = %v, %v", updState, err)
	}

	// Restore.
	rres, err := Restore(inst, b, rep)
	if err != nil {
		t.Fatal(err)
	}
	if rres.From != "2.8.4" || rres.To != "2.8.1" {
		t.Errorf("restore From/To = %q/%q, want 2.8.4/2.8.1", rres.From, rres.To)
	}
	if rres.Removed != 4 {
		t.Errorf("Removed = %d, want 4", rres.Removed)
	}
	if rres.MovedBack != 2 {
		t.Errorf("MovedBack = %d, want 2 (take.cfg and x-1.jar)", rres.MovedBack)
	}
	if len(rres.Skipped) != 0 {
		t.Errorf("Skipped = %v, want none", rres.Skipped)
	}
	if rres.Renamed != name281 {
		t.Errorf("Renamed = %q, want %q", rres.Renamed, name281)
	}

	want := map[string]string{
		takeCfg: "mine-take", keepCfg: "mine-keep", modX: "x1", sameCfg: "s",
		mineJar: "player", world: "world",
	}
	for p, w := range want {
		if got, ok := read(t, inst, p); !ok || got != w {
			t.Errorf("after restore %s = %q (exists %v), want %q", p, got, ok, w)
		}
	}
	for _, p := range []string{newMod, delCfg, keepCfg + ".mcnew", ".minecraft/mods/extra-1.jar"} {
		if _, ok := read(t, inst, p); ok {
			t.Errorf("after restore %s still exists", p)
		}
	}
	if !exists(filepath.Join(inst.GameDir, "mods")) {
		t.Error("mods/ was removed although it still holds files")
	}

	st, err := LoadState(inst.Dir)
	if err != nil || st == nil {
		t.Fatalf("state after restore = %v, %v", st, err)
	}
	if st.Version != "" || len(st.Baseline) != 0 || st.CustomMods != nil {
		t.Errorf("state after restore = %+v, want empty version/baseline and nil CustomMods", st)
	}
	if st.CustomModsURL != customURL || st.CustomModsAsked != updState.CustomModsAsked {
		t.Errorf("custom-mods settings = %q/%v, want %q/%v", st.CustomModsURL, st.CustomModsAsked, customURL, updState.CustomModsAsked)
	}

	cfg, err := os.ReadFile(filepath.Join(inst.Dir, "instance.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "name="+name281) || strings.Contains(string(cfg), "2.8.4") {
		t.Errorf("instance.cfg name not restored:\n%s", cfg)
	}
	if !strings.Contains(string(cfg), "JavaPath=/opt/java") {
		t.Errorf("instance.cfg lost JavaPath:\n%s", cfg)
	}

	if exists(b.Dir) {
		t.Error("backup dir still exists after a restore with nothing skipped")
	}
	after, err := ListBackups(inst.Dir)
	if err != nil || after != nil {
		t.Errorf("ListBackups after restore = %v, %v; want nil, nil", after, err)
	}
}

// C3, C8: with a saved state before the update, PrevState is recorded and restored;
// the player's later settings (server address, custom-mods link) are kept.
func TestRestoreBringsBackPreviousStateKeepingCurrentSettings(t *testing.T) {
	oldPack := withRequired(map[string]string{cfgA: "v1", modX: "x1"})
	newPack := withRequired(map[string]string{cfgA: "v2", ".minecraft/mods/x-2.jar": "x2"})
	client, m := packServer(t, map[string][]byte{
		"/old.zip": zipBytes(t, "GT New Horizons 2.8.1/", oldPack),
		"/new.zip": zipBytes(t, "GT New Horizons 2.8.4/", newPack),
	})
	inst := newInstance(t, name281, map[string]string{cfgA: "v1", modX: "x1"})
	writeFile(t, filepath.Join(inst.Dir, "mmc-pack.json"), oldPack["mmc-pack.json"])
	baseline := fps(oldPack)
	if err := SaveState(inst.Dir, &State{Version: "2.8.1", Baseline: baseline, ServerAddress: "play.example:25565"}); err != nil {
		t.Fatal(err)
	}
	rep := &nopReporter{}
	s, err := Prepare(Options{Client: client, Manifest: m, Instance: inst, Installed: "2.8.1", Target: "2.8.4"}, rep)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(rep); err != nil {
		t.Fatal(err)
	}
	list, err := ListBackups(inst.Dir)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListBackups = %+v, %v; want one entry", list, err)
	}
	prev := list[0].Info.PrevState
	if prev == nil || prev.Version != "2.8.1" || prev.ServerAddress != "play.example:25565" ||
		fmt.Sprint(prev.Baseline) != fmt.Sprint(baseline) {
		t.Errorf("PrevState = %+v, want the saved 2.8.1 state", prev)
	}

	if err := UpdateState(inst.Dir, func(st *State) {
		st.ServerAddress = "other.example"
		st.CustomModsURL = "https://changed.example/c.zip"
		st.CustomModsAsked = true
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(inst, list[0], rep); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(inst.Dir)
	if err != nil || st == nil {
		t.Fatalf("state after restore = %v, %v", st, err)
	}
	if st.Version != "2.8.1" || fmt.Sprint(st.Baseline) != fmt.Sprint(baseline) {
		t.Errorf("state after restore: version %q baseline %v, want 2.8.1 and the old baseline", st.Version, st.Baseline)
	}
	if st.ServerAddress != "other.example" || st.CustomModsURL != "https://changed.example/c.zip" || !st.CustomModsAsked {
		t.Errorf("settings after restore = %q/%q/%v, want other.example/https://changed.example/c.zip/true",
			st.ServerAddress, st.CustomModsURL, st.CustomModsAsked)
	}
	if got, _ := read(t, inst, cfgA); got != "v1" {
		t.Errorf("a.cfg after restore = %q, want v1", got)
	}
}

// writeManifest writes raw JSON as the manifest of backup dir name.
func writeManifest(t *testing.T, inst prism.Instance, name, raw string) string {
	t.Helper()
	dir := filepath.Join(inst.Dir, StateDir, name)
	writeFile(t, filepath.Join(dir, BackupManifest), raw)
	return dir
}

// C4
func TestListBackupsSkipsDirsWithoutOrWithBrokenManifestAndSortsByWhen(t *testing.T) {
	inst := newInstance(t, "i", nil)
	older := writeManifest(t, inst, "backup-z", `{"from":"2.8.0","to":"2.8.1","when":"2025-01-01T00:00:00Z"}`)
	newer := writeManifest(t, inst, "backup-a", `{"from":"2.8.1","to":"2.8.4","when":"2026-01-01T00:00:00Z"}`)
	writeFile(t, filepath.Join(inst.Dir, StateDir, "backup-nomanifest", ".minecraft", "mods", "x.jar"), "x")
	writeManifest(t, inst, "backup-broken", `{not json`)
	writeManifest(t, inst, "other", `{"from":"1","to":"2","when":"2027-01-01T00:00:00Z"}`)

	list, err := ListBackups(inst.Dir)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, b := range list {
		dirs = append(dirs, b.Dir)
	}
	if fmt.Sprint(dirs) != fmt.Sprint([]string{newer, older}) {
		t.Errorf("ListBackups dirs = %v, want %v", dirs, []string{newer, older})
	}
	if len(list) == 2 && (list[0].Info.To != "2.8.4" || list[1].Info.From != "2.8.0") {
		t.Errorf("infos = %+v", list)
	}
}

// C4: equal When -> dir name descending.
func TestListBackupsBreaksTiesByDirNameDescending(t *testing.T) {
	inst := newInstance(t, "i", nil)
	a := writeManifest(t, inst, "backup-a", `{"from":"1","to":"2","when":"2026-01-01T00:00:00Z"}`)
	b := writeManifest(t, inst, "backup-b", `{"from":"1","to":"2","when":"2026-01-01T00:00:00Z"}`)
	list, err := ListBackups(inst.Dir)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, x := range list {
		dirs = append(dirs, x.Dir)
	}
	if fmt.Sprint(dirs) != fmt.Sprint([]string{b, a}) {
		t.Errorf("ListBackups dirs = %v, want %v", dirs, []string{b, a})
	}
}

// C4
func TestListBackupsWithoutStateDirIsNil(t *testing.T) {
	inst := newInstance(t, "i", nil)
	list, err := ListBackups(inst.Dir)
	if err != nil || list != nil {
		t.Errorf("ListBackups = %v, %v; want nil, nil", list, err)
	}
}

// C4
func TestListBackupsWithNoQualifyingDirsIsNil(t *testing.T) {
	inst := newInstance(t, "i", nil)
	writeFile(t, filepath.Join(inst.Dir, StateDir, "backup-old", ".minecraft", "config", "a.cfg"), "a")
	writeFile(t, filepath.Join(inst.Dir, StateDir, "state.json"), `{"version":"2.8.1"}`)
	list, err := ListBackups(inst.Dir)
	if err != nil || list != nil {
		t.Errorf("ListBackups = %v, %v; want nil, nil", list, err)
	}
}

// C6, C7, C10, K1: external paths are skipped, never removed; a path already gone is
// not counted; the backup dir stays when something was skipped.
func TestRestoreSkipsExternalPathsAndKeepsBackupDir(t *testing.T) {
	inst := newInstance(t, "i", nil)
	dir := writeManifest(t, inst, "backup-x",
		`{"from":"2.8.1","to":"2.8.4","when":"2026-01-01T00:00:00Z","added":["_external/tmp/x.cfg",".minecraft/config/gone.cfg"]}`)
	extStash := filepath.Join(dir, "_external", "tmp", "y.cfg")
	writeFile(t, extStash, "ext")
	b := Backup{Dir: dir, Info: BackupInfo{From: "2.8.1", To: "2.8.4",
		Added: []string{"_external/tmp/x.cfg", ".minecraft/config/gone.cfg"}}}

	res, err := Restore(inst, b, &nopReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(res.Skipped); got != "[_external/tmp/x.cfg _external/tmp/y.cfg]" {
		t.Errorf("Skipped = %s, want [_external/tmp/x.cfg _external/tmp/y.cfg]", got)
	}
	if res.Removed != 0 || res.MovedBack != 0 {
		t.Errorf("Removed/MovedBack = %d/%d, want 0/0", res.Removed, res.MovedBack)
	}
	if res.Renamed != "" {
		t.Errorf("Renamed = %q, want \"\" (name has no version)", res.Renamed)
	}
	if got, err := os.ReadFile(extStash); err != nil || string(got) != "ext" {
		t.Errorf("external stash = %q, %v; want left in place", got, err)
	}
	list, err := ListBackups(inst.Dir)
	if err != nil || len(list) != 1 || list[0].Dir != dir {
		t.Errorf("ListBackups after skipped restore = %+v, %v; want the backup still listed", list, err)
	}
}

// C6: added files and server jars are removed, emptied parents pruned below inst.Dir.
func TestRestoreRemovesAddedFilesAndPrunesEmptyParents(t *testing.T) {
	inst := newInstance(t, "i", map[string]string{
		".minecraft/newdir/sub/f.txt": "f",
		".minecraft/mods/srv-1.jar":   "s",
		".minecraft/mods/keep.jar":    "k",
	})
	dir := writeManifest(t, inst, "backup-x",
		`{"from":"2.8.1","to":"2.8.4","when":"2026-01-01T00:00:00Z","added":[".minecraft/newdir/sub/f.txt"],"addedMods":["srv-1.jar"]}`)
	b := Backup{Dir: dir, Info: BackupInfo{From: "2.8.1", To: "2.8.4",
		Added: []string{".minecraft/newdir/sub/f.txt"}, AddedMods: []string{"srv-1.jar"}}}

	res, err := Restore(inst, b, &nopReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 {
		t.Errorf("Removed = %d, want 2", res.Removed)
	}
	if exists(filepath.Join(inst.GameDir, "newdir")) {
		t.Error("empty parent dirs of a removed file were not pruned")
	}
	if !exists(inst.GameDir) {
		t.Error(".minecraft was pruned")
	}
	if exists(filepath.Join(inst.GameDir, "mods", "srv-1.jar")) {
		t.Error("server jar not removed")
	}
	if got, _ := read(t, inst, ".minecraft/mods/keep.jar"); got != "k" {
		t.Errorf("keep.jar = %q, want k", got)
	}
}

// C7, C10, C9: custom-mods stash goes to mods/, a stashed config overwrites the current
// one, the backup dir is removed, steps and progress are reported.
func TestRestoreMovesCustomModsAndOverwritesExistingFiles(t *testing.T) {
	inst := newInstance(t, "GT_New_Horizons_2.8.4_Java_17-25", map[string]string{cfgA: "new"})
	dir := writeManifest(t, inst, "backup-x", `{"from":"2.8.1","to":"2.8.4","when":"2026-01-01T00:00:00Z"}`)
	writeFile(t, filepath.Join(dir, "custom-mods", "old-1.jar"), "old jar")
	writeFile(t, filepath.Join(dir, ".minecraft", "config", "a.cfg"), "old cfg")
	b := Backup{Dir: dir, Info: BackupInfo{From: "2.8.1", To: "2.8.4"}}
	rep := &restoreReporter{}

	res, err := Restore(inst, b, rep)
	if err != nil {
		t.Fatal(err)
	}
	if res.MovedBack != 2 || res.Removed != 0 || len(res.Skipped) != 0 {
		t.Errorf("MovedBack/Removed/Skipped = %d/%d/%v, want 2/0/[]", res.MovedBack, res.Removed, res.Skipped)
	}
	if res.From != "2.8.4" || res.To != "2.8.1" || res.Renamed != name281 {
		t.Errorf("From/To/Renamed = %q/%q/%q, want 2.8.4/2.8.1/%s", res.From, res.To, res.Renamed, name281)
	}
	if got, err := os.ReadFile(filepath.Join(inst.GameDir, "mods", "old-1.jar")); err != nil || string(got) != "old jar" {
		t.Errorf("mods/old-1.jar = %q, %v; want the stashed jar", got, err)
	}
	if got, _ := read(t, inst, cfgA); got != "old cfg" {
		t.Errorf("a.cfg = %q, want old cfg", got)
	}
	if exists(filepath.Join(inst.Dir, BackupManifest)) {
		t.Error("the manifest was moved into the instance")
	}
	if exists(dir) {
		t.Error("backup dir still exists")
	}
	wantSteps := "[Removing files the update added Putting the old files back Saving my notes]"
	if got := fmt.Sprint(rep.steps); got != wantSteps {
		t.Errorf("steps = %s, want %s", got, wantSteps)
	}
	if !rep.sawProgress || rep.lastDone != 2 || rep.lastTotal != 2 {
		t.Errorf("progress calls = %v, want the last one to be 2/2", rep.progress)
	}
}

// C8: without a current state.json the saved settings are zero; PrevState supplies the
// rest including CustomMods.
func TestRestoreWithoutCurrentStateUsesZeroSettings(t *testing.T) {
	inst := newInstance(t, "i", nil)
	dir := writeManifest(t, inst, "backup-x", `{"from":"2.8.1","to":"2.8.4","when":"2026-01-01T00:00:00Z"}`)
	prev := &State{Version: "2.8.1", Baseline: fps(map[string]string{cfgA: "v1"}),
		CustomMods: []string{"old-1.jar"}, CustomModsURL: "https://prev.example/c.zip",
		CustomModsAsked: true, ServerAddress: "prev.example"}
	b := Backup{Dir: dir, Info: BackupInfo{From: "2.8.1", To: "2.8.4", PrevState: prev}}

	if _, err := Restore(inst, b, &nopReporter{}); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(inst.Dir)
	if err != nil || st == nil {
		t.Fatalf("state after restore = %v, %v", st, err)
	}
	if st.Version != "2.8.1" || fmt.Sprint(st.Baseline) != fmt.Sprint(fps(map[string]string{cfgA: "v1"})) ||
		fmt.Sprint(st.CustomMods) != "[old-1.jar]" {
		t.Errorf("state = %+v, want version, baseline and CustomMods from PrevState", st)
	}
	if st.CustomModsURL != "" || st.CustomModsAsked || st.ServerAddress != "" {
		t.Errorf("settings = %q/%v/%q, want zero values (no current state)", st.CustomModsURL, st.CustomModsAsked, st.ServerAddress)
	}
}
