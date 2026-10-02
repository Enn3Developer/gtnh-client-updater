package update

import (
	"archive/zip"
	"bytes"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

func fpOf(s string) pack.Fingerprint {
	return pack.Fingerprint{Size: uint64(len(s)), CRC: crc32.ChecksumIEEE([]byte(s))}
}

func fps(files map[string]string) map[string]pack.Fingerprint {
	out := map[string]pack.Fingerprint{}
	for k, v := range files {
		out[k] = fpOf(v)
	}
	return out
}

// newInstance creates a Prism instance dir with the given canonical-path files.
func newInstance(t *testing.T, name string, files map[string]string) prism.Instance {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	inst := prism.Instance{Dir: dir, Name: name, GameDir: filepath.Join(dir, ".minecraft")}
	os.MkdirAll(inst.GameDir, 0o755)
	os.WriteFile(filepath.Join(dir, "instance.cfg"), []byte("[General]\nJavaPath=/opt/java\nname="+name+"\n"), 0o644)
	for p, body := range files {
		d := DiskPath(inst, p)
		os.MkdirAll(filepath.Dir(d), 0o755)
		if err := os.WriteFile(d, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return inst
}

func read(t *testing.T, inst prism.Instance, p string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(DiskPath(inst, p))
	if err != nil {
		return "", false
	}
	return string(b), true
}

func planKinds(pl *Plan) map[string]Kind {
	out := map[string]Kind{}
	for _, a := range pl.Actions {
		out[a.Path] = a.Kind
	}
	return out
}

const (
	cfgA = ".minecraft/config/a.cfg"
	modX = ".minecraft/mods/x-1.jar"
)

func TestMakePlanConffileRules(t *testing.T) {
	base := map[string]string{
		".minecraft/config/untouched.cfg": "v1",
		".minecraft/config/edited.cfg":    "v1",
		".minecraft/config/both.cfg":      "v1",
		".minecraft/config/same.cfg":      "v1",
		".minecraft/config/dropped.cfg":   "v1",
		".minecraft/config/dropedit.cfg":  "v1",
		".minecraft/config/deleted.cfg":   "v1",
	}
	disk := map[string]string{
		".minecraft/config/untouched.cfg": "v1",
		".minecraft/config/edited.cfg":    "mine",
		".minecraft/config/both.cfg":      "mine",
		".minecraft/config/same.cfg":      "v2",
		".minecraft/config/dropped.cfg":   "v1",
		".minecraft/config/dropedit.cfg":  "mine",
		".minecraft/config/usercreated":   "mine",
	}
	next := map[string]string{
		".minecraft/config/untouched.cfg": "v2",
		".minecraft/config/edited.cfg":    "v1",
		".minecraft/config/both.cfg":      "v2",
		".minecraft/config/same.cfg":      "v2",
		".minecraft/config/deleted.cfg":   "v1",
		".minecraft/config/new.cfg":       "v2",
		".minecraft/config/usercreated":   "pack",
	}
	inst := newInstance(t, "i", disk)
	B, N := fps(base), fps(next)
	pl := MakePlan(inst, B, N, Scan(inst, B, N, nil), nil)
	got := planKinds(pl)
	want := map[string]Kind{
		".minecraft/config/untouched.cfg": Install,
		".minecraft/config/both.cfg":      Conflict,
		".minecraft/config/dropped.cfg":   Remove,
		".minecraft/config/new.cfg":       Install,
		".minecraft/config/usercreated":   Conflict,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("plan\n got %v\nwant %v", got, want)
	}
	sort.Strings(pl.Kept)
	if k := fmt.Sprint(pl.Kept); k != "[.minecraft/config/deleted.cfg .minecraft/config/dropedit.cfg .minecraft/config/edited.cfg]" {
		t.Errorf("kept %s", k)
	}
}

func TestMakePlanPackOwnedRules(t *testing.T) {
	base := map[string]string{
		modX:                           "x1",
		".minecraft/mods/gone.jar":     "g",
		".minecraft/mods/deleted.jar":  "d",
		".minecraft/mods/disabled.jar": "old",
		".minecraft/mods/patched.jar":  "p",
		"patches/net.minecraft.json":   "p1",
		"libraries/lwjgl3ify-1.0.jar":  "l1",
	}
	disk := map[string]string{
		modX:                                    "x1",
		".minecraft/mods/gone.jar":              "g",
		".minecraft/mods/disabled.jar.disabled": "old",
		".minecraft/mods/patched.jar":           "hacked",
		".minecraft/mods/mine.jar":              "player's own",
		"patches/net.minecraft.json":            "p1",
		"libraries/lwjgl3ify-1.0.jar":           "l1",
	}
	next := map[string]string{
		".minecraft/mods/x-2.jar":      "x2",
		".minecraft/mods/deleted.jar":  "d",
		".minecraft/mods/disabled.jar": "new",
		".minecraft/mods/patched.jar":  "p",
		"patches/net.minecraft.json":   "p2",
		"libraries/lwjgl3ify-2.0.jar":  "l2",
		"instance.cfg":                 "name=pack",
	}
	inst := newInstance(t, "i", disk)
	B, N := fps(base), fps(next)
	pl := MakePlan(inst, B, N, Scan(inst, B, N, nil), nil)
	want := map[string]Kind{
		modX:                                    Remove,
		".minecraft/mods/x-2.jar":               Install,
		".minecraft/mods/gone.jar":              Remove,
		".minecraft/mods/disabled.jar.disabled": Install, // updated, stays disabled
		".minecraft/mods/patched.jar":           Install, // pack wins on mods
		".minecraft/mods/deleted.jar":           Install, // restored
		"patches/net.minecraft.json":            Install,
		"libraries/lwjgl3ify-1.0.jar":           Remove,
		"libraries/lwjgl3ify-2.0.jar":           Install,
	}
	if got := planKinds(pl); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("plan\n got %v\nwant %v", got, want)
	}
	if fmt.Sprint(pl.ExtraMods) != "[mine.jar]" {
		t.Errorf("extra mods %v", pl.ExtraMods)
	}
	if pl.Count(Conflict) != 0 {
		t.Error("pack-owned paths must never conflict")
	}
}

func TestApplyAndRollback(t *testing.T) {
	disk := map[string]string{cfgA: "v1", modX: "x1", ".minecraft/saves/w/level.dat": "world"}
	inst := newInstance(t, "i", disk)
	nextFiles := map[string]string{cfgA: "v2", ".minecraft/mods/x-2.jar": "x2", "mmc-pack.json": "{}"}
	next := openPack(t, nextFiles)
	B, N := fps(map[string]string{cfgA: "v1", modX: "x1"}), next.Fingerprints()
	pl := MakePlan(inst, B, N, Scan(inst, B, N, nil), nil)

	// Sabotage: make the last action's parent a file so the write fails.
	pl.Actions = append(pl.Actions, Action{Kind: Install, Path: "mmc-pack.json",
		Disk: filepath.Join(inst.GameDir, "saves", "w", "level.dat", "x")})
	backup := filepath.Join(inst.Dir, StateDir, "backup-t")
	if err := Apply(pl, next, inst.Dir, backup, nil); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("want rolled-back error, got %v", err)
	}
	for p, want := range disk {
		if got, _ := read(t, inst, p); got != want {
			t.Errorf("after rollback %s = %q, want %q", p, got, want)
		}
	}
	if _, ok := read(t, inst, ".minecraft/mods/x-2.jar"); ok {
		t.Error("rollback left a new file behind")
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Error("backup dir left after a clean rollback")
	}

	// Now for real.
	pl.Actions = pl.Actions[:len(pl.Actions)-1]
	if err := Apply(pl, next, inst.Dir, backup, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := read(t, inst, cfgA); got != "v2" {
		t.Errorf("config not updated: %q", got)
	}
	if _, ok := read(t, inst, modX); ok {
		t.Error("old mod not removed")
	}
	if b, _ := os.ReadFile(filepath.Join(backup, ".minecraft", "mods", "x-1.jar")); string(b) != "x1" {
		t.Error("old mod not in backup")
	}
	if got, _ := read(t, inst, ".minecraft/saves/w/level.dat"); got != "world" {
		t.Error("world touched")
	}
}

func openPack(t *testing.T, files map[string]string) *pack.Pack {
	t.Helper()
	name := filepath.Join(t.TempDir(), "p.zip")
	os.WriteFile(name, zipBytes(t, "GT New Horizons X/", withRequired(files)), 0o644)
	p, err := pack.OpenFile(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

func withRequired(files map[string]string) map[string]string {
	out := map[string]string{"mmc-pack.json": `{"components":[{"uid":"org.lwjgl3"},{"uid":"net.minecraft","version":"1.7.10"}]}`}
	for k, v := range files {
		out[k] = v
	}
	return out
}

func zipBytes(t *testing.T, prefix string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		w, _ := zw.Create(prefix + k)
		w.Write([]byte(files[k]))
	}
	zw.Close()
	return buf.Bytes()
}

func TestDetectVersion(t *testing.T) {
	m, _ := manifest.Parse([]byte(`{
	  "2.8.4": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/a.zip"}},
	  "2.8.1": {"title":"Stable release","releaseDate":"2025/10/19","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/b.zip"}},
	  "2.9.0-beta-3": {"title":"Beta release","releaseDate":"2026/09/06","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/c.zip"}}}`))

	inst := newInstance(t, "GT_New_Horizons_2.8.4_Java_17-25", nil)
	if d := DetectVersion(inst, nil, m); d.Version != "2.8.4" || d.Source != "instance name" {
		t.Errorf("name: %+v", d)
	}
	os.WriteFile(filepath.Join(inst.GameDir, "changelog from 2.8.4 to 2.9.0-beta-3.md"), nil, 0o644)
	if d := DetectVersion(inst, nil, m); d.Version != "2.9.0-beta-3" || d.Source != "pack changelog" {
		t.Errorf("changelog: %+v", d)
	}
	if d := DetectVersion(inst, &State{Version: "2.8.1"}, m); d.Version != "2.8.1" {
		t.Errorf("state: %+v", d)
	}
	if d := DetectVersion(newInstance(t, "Pack 2.8.10", nil), nil, m); d.Version != "" {
		t.Errorf("2.8.1 must not match inside 2.8.10: %+v", d)
	}
}

func TestRenameVersionKeepsSettings(t *testing.T) {
	inst := newInstance(t, "My GTNH 2.8.4 run", nil)
	name, ok, err := prism.RenameVersion(inst.Dir, "2.8.4", "2.9.0-RC-1")
	if err != nil || !ok || name != "My GTNH 2.9.0-RC-1 run" {
		t.Fatalf("rename: %q %v %v", name, ok, err)
	}
	b, _ := os.ReadFile(filepath.Join(inst.Dir, "instance.cfg"))
	if !strings.Contains(string(b), "JavaPath=/opt/java") {
		t.Error("other settings lost")
	}
}

// hostRewrite sends every request to srv, whatever host the URL names, so the pinned
// official download URLs can be served by a test server.
type hostRewrite struct{ srv *httptest.Server }

func (h hostRewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = "http", strings.TrimPrefix(h.srv.URL, "http://")
	return http.DefaultTransport.RoundTrip(r)
}

type nopReporter struct{ warns []string }

func (*nopReporter) Step(string)           {}
func (*nopReporter) Progress(int64, int64) {}
func (n *nopReporter) Warn(msg string)     { n.warns = append(n.warns, msg) }

func TestSessionEndToEnd(t *testing.T) {
	oldPack := withRequired(map[string]string{
		cfgA: "v1", ".minecraft/config/mine.cfg": "v1", modX: "x1", "instance.cfg": "name=pack",
	})
	newPack := withRequired(map[string]string{
		cfgA: "v2", ".minecraft/config/mine.cfg": "v2", ".minecraft/mods/x-2.jar": "x2",
	})
	custom := zipBytes(t, "", map[string]string{"extra-1.jar": "e1"})
	files := map[string][]byte{
		"/old.zip":         zipBytes(t, "GT New Horizons 2.8.4/", oldPack),
		"/new.zip":         zipBytes(t, "GT New Horizons 2.9.0/", newPack),
		"/custom_mods.zip": custom,
	}
	downloads := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.Header.Get("Range") == "" {
			downloads[r.URL.Path]++
		}
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, r.URL.Path, time.Time{}, bytes.NewReader(b))
	}))
	defer srv.Close()
	client := &http.Client{Transport: hostRewrite{srv}}
	m, _ := manifest.Parse([]byte(`{
	  "2.8.4": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/old.zip"}},
	  "2.9.0": {"title":"Stable release","releaseDate":"2026/10/04","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/new.zip"}}}`))

	inst := newInstance(t, "GT_New_Horizons_2.8.4_Java_17-25", map[string]string{
		cfgA: "v1", ".minecraft/config/mine.cfg": "tweaked", modX: "x1",
		".minecraft/saves/w/level.dat": "world",
	})
	os.WriteFile(filepath.Join(inst.Dir, "mmc-pack.json"), []byte(oldPack["mmc-pack.json"]), 0o644)

	opts := Options{Client: client, Manifest: m, Instance: inst, Installed: "2.8.4", Target: "2.9.0",
		CustomModsURL: "https://files.example/custom_mods.zip"}
	rep := &nopReporter{}
	s, err := Prepare(opts, rep)
	if err != nil {
		t.Fatal(err)
	}
	if s.BaselineSource != "reconstructed from 2.8.4" || s.Plan.BaselineMatch != 1 {
		t.Errorf("baseline %q match %v", s.BaselineSource, s.Plan.BaselineMatch)
	}
	res, err := s.Apply(rep)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{
		cfgA:                               "v2",
		".minecraft/config/mine.cfg":       "tweaked",
		".minecraft/config/mine.cfg.mcnew": "v2",
		".minecraft/mods/x-2.jar":          "x2",
		".minecraft/mods/extra-1.jar":      "e1",
		".minecraft/saves/w/level.dat":     "world",
	}
	for p, want := range checks {
		if got, ok := read(t, inst, p); !ok || got != want {
			t.Errorf("%s = %q (exists %v), want %q", p, got, ok, want)
		}
	}
	if _, ok := read(t, inst, modX); ok {
		t.Error("old mod still present")
	}
	if res.Renamed != "GT_New_Horizons_2.9.0_Java_17-25" {
		t.Errorf("renamed to %q", res.Renamed)
	}
	if _, err := os.Stat(filepath.Join(inst.Dir, StateDir, "download.zip")); !os.IsNotExist(err) {
		t.Error("download not cleaned up")
	}
	st, _ := LoadState(inst.Dir)
	if st.Version != "2.9.0" || fmt.Sprint(st.CustomMods) != "[extra-1.jar]" {
		t.Errorf("state %+v", st)
	}

	// Server drops the custom mod: a re-run at the same version removes it again, and
	// uses the saved baseline.
	delete(files, "/custom_mods.zip")
	opts.Installed = "2.9.0"
	s, err = Prepare(opts, rep)
	if err != nil {
		t.Fatal(err)
	}
	if s.BaselineSource != "saved" || len(s.Plan.Actions) != 0 {
		t.Errorf("rerun: baseline %q, actions %v", s.BaselineSource, s.Plan.Actions)
	}
	if s.next != nil || downloads["/new.zip"] != 1 {
		t.Errorf("same-version rerun downloaded the pack again (%d downloads)", downloads["/new.zip"])
	}
	if _, err := s.Apply(rep); err != nil {
		t.Fatal(err)
	}
	if _, ok := read(t, inst, ".minecraft/mods/extra-1.jar"); ok {
		t.Error("custom mod removed on the server is still installed")
	}

	// The link is remembered; switching it off takes the synced jars out.
	files["/custom_mods.zip"] = custom
	if s, err = Prepare(opts, rep); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(rep); err != nil {
		t.Fatal(err)
	}
	if st, _ := LoadState(inst.Dir); st.CustomModsURL != opts.CustomModsURL || !st.CustomModsAsked {
		t.Errorf("link not remembered: %+v", st)
	}

	// A run with nothing changed on disk or on the server makes no backup and must
	// not prune the newest existing one.
	stateDir := filepath.Join(inst.Dir, StateDir)
	backupsBefore := backupDirs(t, stateDir)
	if len(backupsBefore) == 0 {
		t.Fatal("no backup-* dir exists before the no-op run")
	}
	newestBackup := backupsBefore[len(backupsBefore)-1]
	if s, err = Prepare(opts, rep); err != nil {
		t.Fatal(err)
	}
	if len(s.Plan.Actions) != 0 {
		t.Errorf("no-op run: actions %v, want none", s.Plan.Actions)
	}
	noop, err := s.Apply(rep)
	if err != nil {
		t.Fatal(err)
	}
	// CHARACTERIZATION: suspected bug: Apply decides "made a backup" by stat-ing
	// backup-<second>, so a no-op run in the same second as the previous backing-up run
	// reports that run's dir as its BackupDir (and two backing-up runs in one second
	// would share a dir). Accept "" or the pre-existing newest dir; nothing new may appear.
	if want := filepath.Join(stateDir, newestBackup); noop.BackupDir != "" && noop.BackupDir != want {
		t.Errorf("no-op run: BackupDir = %q, want \"\" (or %q within the same second)", noop.BackupDir, want)
	}
	if got := backupDirs(t, stateDir); fmt.Sprint(got) != fmt.Sprint(backupsBefore) {
		t.Errorf("no-op run changed the backups: %v, want %v", got, backupsBefore)
	}
	opts.CustomModsURL = ""
	if s, err = Prepare(opts, rep); err != nil {
		t.Fatal(err)
	}
	res, err = s.Apply(rep)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := read(t, inst, ".minecraft/mods/extra-1.jar"); ok || res.CustomMods == nil || len(res.CustomMods.Removed) != 1 {
		t.Errorf("switching the sync off left the jar: %+v", res.CustomMods)
	}
	if st, _ := LoadState(inst.Dir); st.CustomModsURL != "" || !st.CustomModsAsked || len(st.CustomMods) != 0 {
		t.Errorf("state after switching off: %+v", st)
	}

	// A damaged install (a pack mod deleted) makes the same-version run repair it.
	os.Remove(DiskPath(inst, ".minecraft/mods/x-2.jar"))
	before := downloads["/new.zip"]
	if s, err = Prepare(opts, rep); err != nil {
		t.Fatal(err)
	}
	if downloads["/new.zip"] != before+1 {
		t.Error("repair did not download the pack")
	}
	if _, err := s.Apply(rep); err != nil {
		t.Fatal(err)
	}
	if got, _ := read(t, inst, ".minecraft/mods/x-2.jar"); got != "x2" {
		t.Errorf("deleted pack mod not restored: %q", got)
	}
}

// C2, K: Session.Apply carries the saved ServerAddress into the new state; a first-time
// Apply (no saved state) leaves it empty.
func TestSessionApplyKeepsServerAddress(t *testing.T) {
	oldPack := withRequired(map[string]string{cfgA: "v1", modX: "x1"})
	newPack := withRequired(map[string]string{cfgA: "v2", ".minecraft/mods/x-2.jar": "x2"})
	files := map[string][]byte{
		"/old.zip": zipBytes(t, "GT New Horizons 2.8.4/", oldPack),
		"/new.zip": zipBytes(t, "GT New Horizons 2.9.0/", newPack),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, r.URL.Path, time.Time{}, bytes.NewReader(b))
	}))
	defer srv.Close()
	m, err := manifest.Parse([]byte(`{
	  "2.8.4": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/old.zip"}},
	  "2.9.0": {"title":"Stable release","releaseDate":"2026/10/04","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/new.zip"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	inst := newInstance(t, "GT_New_Horizons_2.8.4_Java_17-25", map[string]string{cfgA: "v1", modX: "x1"})
	os.WriteFile(filepath.Join(inst.Dir, "mmc-pack.json"), []byte(oldPack["mmc-pack.json"]), 0o644)
	opts := Options{Client: &http.Client{Transport: hostRewrite{srv}}, Manifest: m, Instance: inst,
		Installed: "2.8.4", Target: "2.9.0"}
	rep := &nopReporter{}

	s, err := Prepare(opts, rep)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(rep); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(inst.Dir)
	if err != nil || st == nil {
		t.Fatalf("LoadState after first Apply = %v, %v", st, err)
	}
	if st.ServerAddress != "" {
		t.Errorf("first-time Apply: ServerAddress = %q, want \"\"", st.ServerAddress)
	}

	if err := UpdateState(inst.Dir, func(st *State) { st.ServerAddress = "play.example:25565" }); err != nil {
		t.Fatal(err)
	}
	opts.Installed = "2.9.0"
	if s, err = Prepare(opts, rep); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(rep); err != nil {
		t.Fatal(err)
	}
	st, err = LoadState(inst.Dir)
	if err != nil || st == nil {
		t.Fatalf("LoadState after rerun = %v, %v", st, err)
	}
	if st.ServerAddress != "play.example:25565" || st.Version != "2.9.0" {
		t.Errorf("after rerun Apply: ServerAddress %q version %q, want %q %q", st.ServerAddress, st.Version, "play.example:25565", "2.9.0")
	}
}

// backupDirs returns the names of the backup-* dirs in stateDir, sorted; the names carry
// a sortable timestamp, so the last one is the newest.
func backupDirs(t *testing.T, stateDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(stateDir) // ReadDir sorts by name
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "backup-") {
			names = append(names, e.Name())
		}
	}
	return names
}
