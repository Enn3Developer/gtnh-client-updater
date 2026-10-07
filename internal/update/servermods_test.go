package update

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

const packMod = ".minecraft/mods/gtnh-core.jar"

// syncInstance is an instance GTNH installed (a saved state whose baseline has the
// pack's jar, which is on disk) with the extra files in mods/ and the given managed jars.
func syncInstance(t *testing.T, mods map[string]string, managed map[string]string) prism.Instance {
	t.Helper()
	files := map[string]string{packMod: "core"}
	for n, body := range mods {
		files[".minecraft/mods/"+n] = body
	}
	inst := newInstance(t, "GTNH", files)
	st := &State{Version: "2.8.4", Baseline: fps(map[string]string{packMod: "core"}), CustomModsURL: modsLink, CustomModsAsked: true}
	for _, n := range sortedKeys(managed) {
		st.CustomMods = append(st.CustomMods, n)
		if st.CustomModsFP == nil {
			st.CustomModsFP = map[string]pack.Fingerprint{}
		}
		st.CustomModsFP[n] = fpOf(managed[n])
	}
	if err := SaveState(inst.Dir, st); err != nil {
		t.Fatal(err)
	}
	return inst
}

// manifest284to290 has 2.8.4 (/old.zip) and 2.9.0 (/new.zip).
func manifest284to290(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`{
	  "2.8.4": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/old.zip"}},
	  "2.9.0": {"title":"Stable release","releaseDate":"2026/10/04","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/new.zip"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func syncMods(t *testing.T, h *modsHost, inst prism.Instance, url string) (*ModsSync, *ModsPlan, error) {
	t.Helper()
	s, err := PrepareModsSync(ModsSyncOptions{Client: h.client(), Instance: inst, URL: url}, &nopReporter{})
	if err != nil {
		return nil, nil, err
	}
	p, err := s.Apply(&nopReporter{})
	return s, p, err
}

// modsOnDisk is mods/ as name=content, sorted.
func modsOnDisk(t *testing.T, inst prism.Instance) string {
	t.Helper()
	return dirContents(t, filepath.Join(inst.GameDir, "mods"))
}

func dirContents(t *testing.T, dir string) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		out = append(out, e.Name()+"="+string(b))
	}
	return fmt.Sprint(out)
}

func TestModsSyncInstallsAndAsksTheServerNextTime(t *testing.T) {
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{"extra.jar": "e1"})})
	inst := syncInstance(t, nil, nil)
	s, p, err := syncMods(t, h, inst, "")
	if err != nil || s.FetchErr != nil {
		t.Fatalf("sync = %v, fetch %v", err, s.FetchErr)
	}
	if got := fmt.Sprint(p.Names(ModAdd)); got != "[extra.jar]" || modsOnDisk(t, inst) != "[extra.jar=e1 gtnh-core.jar=core]" {
		t.Errorf("added %s, mods/ %s", got, modsOnDisk(t, inst))
	}
	st, _ := LoadState(inst.Dir)
	if fmt.Sprint(st.CustomMods) != "[extra.jar]" || st.CustomModsFP["extra.jar"] != fpOf("e1") || st.CustomModsSynced.IsZero() {
		t.Errorf("state = %+v", st)
	}

	s, p, err = syncMods(t, h, inst, "")
	if err != nil || s.FetchErr != nil || p.writes() || h.fullAnswers("/custom_mods.zip") != 1 {
		t.Errorf("an unchanged server: %v, fetch %v, plan %v, %d full downloads", err, s.FetchErr, describeAll(p), h.fullAnswers("/custom_mods.zip"))
	}
}

// P1: a server mod the player disabled in Prism stays disabled when it's updated.
func TestModsSyncKeepsADisabledModDisabled(t *testing.T) {
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{"extra.jar": "e2"})})
	inst := syncInstance(t, map[string]string{"extra.jar.disabled": "e1"}, map[string]string{"extra.jar": "e1"})
	if _, _, err := syncMods(t, h, inst, ""); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != "[extra.jar.disabled=e2 gtnh-core.jar=core]" {
		t.Errorf("mods/ = %s, want the update in the disabled name", got)
	}
	if _, p, _ := syncMods(t, h, inst, ""); p.writes() {
		t.Errorf("a second sync still changes things: %v", describeAll(p))
	}
}

// corruptJarZip is a server archive whose jar name fails its CRC check when read.
func corruptJarZip(t *testing.T, good map[string]string, bad string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range sortedKeys(good) {
		w, _ := zw.Create(n)
		w.Write([]byte(good[n]))
	}
	body := []byte("not what the header says")
	w, err := zw.CreateRaw(&zip.FileHeader{Name: bad, Method: zip.Store, CRC32: 0xdeadbeef,
		CompressedSize64: uint64(len(body)), UncompressedSize64: uint64(len(body))})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(body)
	zw.Close()
	return buf.Bytes()
}

// P2: a sync that fails half way puts every jar back as it was.
func TestModsSyncFailureLeavesTheModsAsTheyWere(t *testing.T) {
	data := corruptJarZip(t, map[string]string{"a.jar": "a2"}, "b.jar")
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": data})
	inst := syncInstance(t, map[string]string{"a.jar": "a1", "c.jar": "c1"}, map[string]string{"a.jar": "a1", "c.jar": "c1"})
	before := modsOnDisk(t, inst)
	_, _, err := syncMods(t, h, inst, "")
	if !errors.Is(err, ErrModsRolledBack) || !errors.Is(err, zip.ErrChecksum) {
		t.Fatalf("sync of a damaged archive = %v, want ErrModsRolledBack", err)
	}
	if got := modsOnDisk(t, inst); got != before {
		t.Errorf("mods/ after the failure = %s, want %s", got, before)
	}
	if st, _ := LoadState(inst.Dir); fmt.Sprint(st.CustomMods) != "[a.jar c.jar]" || !st.CustomModsSynced.IsZero() {
		t.Errorf("state after the failure = %+v, want the old notes", st)
	}
	if fileExists(filepath.Join(inst.Dir, StateDir, modsStaging)) {
		t.Error("the staging folder is left after a rollback")
	}
}

// The player's own jar of a server mod's name makes way for the server's, and is kept.
func TestModsSyncKeepsThePlayersReplacedJars(t *testing.T) {
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{"shared.jar": "server"})})
	inst := syncInstance(t, map[string]string{"shared.jar": "mine"}, nil)
	kept := filepath.Join(inst.Dir, StateDir, ReplacedMods)
	writeFile(t, filepath.Join(kept, "shared.jar"), "an older one")
	_, p, err := syncMods(t, h, inst, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(p.Names(ModReplace)); got != "[shared.jar]" {
		t.Errorf("replaced = %s", got)
	}
	if got := modsOnDisk(t, inst); got != "[gtnh-core.jar=core shared.jar=server]" {
		t.Errorf("mods/ = %s", got)
	}
	if got := dirContents(t, kept); got != "[shared (2).jar=mine shared.jar=an older one]" {
		t.Errorf("replaced-mods = %s, want the player's jar next to the older one", got)
	}
}

// A sync killed half way leaves its staging folder and temp files; the next one moves
// the staged jars to the replaced-mods folder and deletes the temp files.
func TestModsSyncCleansUpAfterAKilledSync(t *testing.T) {
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{"extra.jar": "e1"})})
	inst := syncInstance(t, map[string]string{"extra.jar": "e1", "half.jar.gtnh-tmp": "partial"}, map[string]string{"extra.jar": "e1"})
	staging := filepath.Join(inst.Dir, StateDir, modsStaging)
	writeFile(t, filepath.Join(staging, "moved.jar"), "moved aside")
	if _, _, err := syncMods(t, h, inst, ""); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != "[extra.jar=e1 gtnh-core.jar=core]" {
		t.Errorf("mods/ = %s, want the temp file gone", got)
	}
	if got := dirContents(t, filepath.Join(inst.Dir, StateDir, ReplacedMods)); got != "[moved.jar=moved aside]" || fileExists(staging) {
		t.Errorf("replaced-mods = %s (staging left: %v)", got, fileExists(staging))
	}
}

func TestModsSyncOverrides(t *testing.T) {
	h := newModsHost(t, map[string][]byte{
		"/custom_mods.zip": modsZipOf(t, map[string]string{"extra.jar": "e1"}),
		"/other.zip":       modsZipOf(t, map[string]string{"other.jar": "o1"}),
	})
	inst := syncInstance(t, nil, nil)
	if _, _, err := syncMods(t, h, inst, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := syncMods(t, h, inst, "https://mods.example/other.zip"); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState(inst.Dir)
	if got := modsOnDisk(t, inst); got != "[gtnh-core.jar=core other.jar=o1]" || st.CustomModsURL != "https://mods.example/other.zip" {
		t.Errorf("after a new link: mods/ %s, link %q", got, st.CustomModsURL)
	}

	_, p, err := syncMods(t, h, inst, "none")
	if err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState(inst.Dir)
	if got := modsOnDisk(t, inst); got != "[gtnh-core.jar=core]" || fmt.Sprint(p.Names(ModRemove)) != "[other.jar]" {
		t.Errorf("after none: mods/ %s, removed %v", got, p.Names(ModRemove))
	}
	if st.CustomModsURL != "" || !st.CustomModsAsked || len(st.CustomMods) != 0 || st.CustomModsFP != nil || !st.CustomModsSynced.IsZero() {
		t.Errorf("state after none = %+v", st)
	}
	stateDir := filepath.Join(inst.Dir, StateDir)
	if fileExists(filepath.Join(stateDir, modsZip)) || fileExists(filepath.Join(stateDir, modsInfo)) {
		t.Error("the copy of the archive is kept without a link")
	}

	if _, err := PrepareModsSync(ModsSyncOptions{Client: h.client(), Instance: inst, URL: "http://mods.example/x.zip"}, &nopReporter{}); err == nil {
		t.Error("a plain http link was taken")
	}
}

// The server can't be reached: the copy of the last archive stands in; with no copy
// nothing changes.
func TestModsSyncWithoutTheServer(t *testing.T) {
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{"extra.jar": "e1"})})
	inst := syncInstance(t, nil, nil)
	if _, _, err := syncMods(t, h, inst, ""); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState(inst.Dir)
	synced := st.CustomModsSynced
	h.set("/custom_mods.zip", nil)
	os.Remove(filepath.Join(inst.GameDir, "mods", "extra.jar"))

	s, p, err := syncMods(t, h, inst, "")
	if err != nil || !errors.Is(s.FetchErr, ErrModsNotFound) {
		t.Fatalf("sync = %v, fetch %v; want ErrModsNotFound", err, s.FetchErr)
	}
	if got := modsOnDisk(t, inst); got != "[extra.jar=e1 gtnh-core.jar=core]" || fmt.Sprint(p.Names(ModAdd)) != "[extra.jar]" {
		t.Errorf("from the copy: mods/ %s, added %v", got, p.Names(ModAdd))
	}
	if st, _ := LoadState(inst.Dir); !st.CustomModsSynced.Equal(synced) {
		t.Error("a sync from the copy counts as synced with the server")
	}

	dropModsCache(filepath.Join(inst.Dir, StateDir))
	s, p, err = syncMods(t, h, inst, "")
	if err != nil || s.FetchErr == nil || p.writes() {
		t.Fatalf("sync without a copy = %v, fetch %v, plan %v; want nothing changed", err, s.FetchErr, describeAll(p))
	}
	if st, _ := LoadState(inst.Dir); fmt.Sprint(st.CustomMods) != "[extra.jar]" || modsOnDisk(t, inst) != "[extra.jar=e1 gtnh-core.jar=core]" {
		t.Errorf("without a copy: managed %v, mods/ %s", st.CustomMods, modsOnDisk(t, inst))
	}
}

func TestModsSyncCancelled(t *testing.T) {
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{"extra.jar": "e1"})})
	inst := syncInstance(t, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := PrepareModsSync(ModsSyncOptions{Context: ctx, Client: h.client(), Instance: inst}, &nopReporter{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled sync = %v, want context.Canceled", err)
	}
	if got := modsOnDisk(t, inst); got != "[gtnh-core.jar=core]" {
		t.Errorf("mods/ after a cancelled sync = %s", got)
	}
}

// An instance this launcher never installed: a jar of a server mod's name might be
// GTNH's, so it's left alone, and so is the player's same mod under another name.
func TestModsSyncWithoutAKnownPackTouchesNothingOfThePlayers(t *testing.T) {
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{"shared.jar": "server", "new.jar": "n"})})
	inst := newInstance(t, "GTNH", map[string]string{".minecraft/mods/shared.jar": "theirs"})
	if err := UpdateState(inst.Dir, func(st *State) { st.CustomModsURL = modsLink }); err != nil {
		t.Fatal(err)
	}
	_, p, err := syncMods(t, h, inst, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != "[new.jar=n shared.jar=theirs]" || fmt.Sprint(p.Names(ModSkip)) != "[shared.jar]" {
		t.Errorf("mods/ %s, skipped %v", got, p.Names(ModSkip))
	}
}

// P6 and the update: the server's mods aren't part of a GTNH update's backup. A sync
// on its own makes no backup and keeps the GTNH undo; undoing the update leaves the
// server's mods and the notes about them alone, so a later sync still removes a jar the
// server dropped. Apply never goes to the network.
func TestServerModsFollowTheServerAcrossUpdateAndUndo(t *testing.T) {
	oldPack := withRequired(map[string]string{cfgA: "v1", modX: "x1"})
	newPack := withRequired(map[string]string{cfgA: "v2", ".minecraft/mods/x-2.jar": "x2"})
	h := newModsHost(t, map[string][]byte{
		"/old.zip":         zipBytes(t, "GT New Horizons 2.8.4/", oldPack),
		"/new.zip":         zipBytes(t, "GT New Horizons 2.9.0/", newPack),
		"/custom_mods.zip": modsZipOf(t, map[string]string{"extra.jar": "e1"}),
	})
	man := manifest284to290(t)
	inst := newInstance(t, "GTNH 2.8.4", map[string]string{cfgA: "v1", modX: "x1"})
	os.WriteFile(filepath.Join(inst.Dir, "mmc-pack.json"), []byte(oldPack["mmc-pack.json"]), 0o644)

	opts := Options{Client: h.client(), Manifest: man, Instance: inst, Installed: "2.8.4", Target: "2.9.0", CustomModsURL: modsLink}
	s, err := Prepare(opts, &nopReporter{})
	if err != nil {
		t.Fatal(err)
	}
	asked := h.fullAnswers("/custom_mods.zip")
	res, err := s.Apply(&nopReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if h.fullAnswers("/custom_mods.zip") != asked || asked != 1 {
		t.Errorf("the server's archive was downloaded %d times (%d before Apply), want once in Prepare", h.fullAnswers("/custom_mods.zip"), asked)
	}
	if res.BackupDir == "" || fmt.Sprint(res.Mods.Names(ModAdd)) != "[extra.jar]" {
		t.Fatalf("update: backup %q, mods %v", res.BackupDir, describeAll(res.Mods))
	}
	backups, _ := ListBackups(inst.Dir)
	if len(backups) != 1 || len(backups[0].Info.AddedMods) != 0 {
		t.Fatalf("backups after the update = %+v, want one without server mods", backups)
	}

	h.set("/custom_mods.zip", modsZipOf(t, map[string]string{"extra.jar": "e1", "more.jar": "m1"}))
	if _, _, err := syncMods(t, h, inst, ""); err != nil {
		t.Fatal(err)
	}
	after, _ := ListBackups(inst.Dir)
	if len(after) != 1 || after[0].Dir != backups[0].Dir {
		t.Fatalf("backups after a sync = %+v, want the update's", after)
	}

	if _, err := Restore(inst, after[0], &nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if got := modsOnDisk(t, inst); got != "[extra.jar=e1 more.jar=m1 x-1.jar=x1]" {
		t.Errorf("mods/ after undo = %s, want the server's mods untouched", got)
	}
	st, _ := LoadState(inst.Dir)
	if st.Version != "" || fmt.Sprint(st.CustomMods) != "[extra.jar more.jar]" || st.CustomModsURL != modsLink {
		t.Errorf("state after undo = %+v", st)
	}

	h.set("/custom_mods.zip", modsZipOf(t, map[string]string{"extra.jar": "e1"}))
	if _, p, err := syncMods(t, h, inst, ""); err != nil || fmt.Sprint(p.Names(ModRemove)) != "[more.jar]" {
		t.Fatalf("sync after undo = %v, removed %v", err, p.Names(ModRemove))
	}
	if got := modsOnDisk(t, inst); got != "[extra.jar=e1 x-1.jar=x1]" {
		t.Errorf("mods/ = %s, want the dropped jar gone", got)
	}
}

// An update whose server can't be reached and that has no copy of the archive keeps the
// server's mods as they are.
func TestUpdateKeepsServerModsWhenTheServerIsDown(t *testing.T) {
	oldPack := withRequired(map[string]string{cfgA: "v1", modX: "x1"})
	newPack := withRequired(map[string]string{cfgA: "v2", ".minecraft/mods/x-2.jar": "x2"})
	h := newModsHost(t, map[string][]byte{
		"/old.zip": zipBytes(t, "GT New Horizons 2.8.4/", oldPack),
		"/new.zip": zipBytes(t, "GT New Horizons 2.9.0/", newPack),
	})
	inst := newInstance(t, "GTNH 2.8.4", map[string]string{cfgA: "v1", modX: "x1", ".minecraft/mods/extra.jar": "e1"})
	os.WriteFile(filepath.Join(inst.Dir, "mmc-pack.json"), []byte(oldPack["mmc-pack.json"]), 0o644)
	if err := UpdateState(inst.Dir, func(st *State) {
		st.CustomModsURL, st.CustomMods = modsLink, []string{"extra.jar"}
	}); err != nil {
		t.Fatal(err)
	}
	opts := Options{Client: h.client(), Manifest: manifest284to290(t), Instance: inst, Installed: "2.8.4", Target: "2.9.0", CustomModsURL: modsLink}
	rep := &nopReporter{}
	s, err := Prepare(opts, rep)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.ModsErr, ErrModsNotFound) || !slices.Contains(rep.warns, "I couldn't check your server's mods: the link doesn't lead to a file anymore.") {
		t.Errorf("Prepare: ModsErr %v, warnings %q", s.ModsErr, rep.warns)
	}
	res, err := s.Apply(rep)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := read(t, inst, ".minecraft/mods/extra.jar"); got != "e1" || !errors.Is(res.ModsErr, ErrModsNotFound) {
		t.Errorf("extra.jar = %q, ModsErr %v", got, res.ModsErr)
	}
	if st, _ := LoadState(inst.Dir); fmt.Sprint(st.CustomMods) != "[extra.jar]" || st.CustomModsFP["extra.jar"] != fpOf("e1") {
		t.Errorf("state = %+v, want extra.jar still managed and now recorded", st)
	}
}
