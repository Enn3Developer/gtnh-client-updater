package update

import (
	"archive/zip"
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

const (
	packCfgCRLF   = "[General]\r\nInstanceType=OneSix\r\nname=GTNH Pack Name\r\nJavaPath=java\r\n"
	customModsURL = "https://files.example/custom_mods.zip"
	newName       = "GT New Horizons 2.9.0"
)

// createPack is the tiny default pack: 5 entries, instance.cfg included.
func createPack() map[string]string {
	return withRequired(map[string]string{
		"instance.cfg":                 packCfgCRLF,
		".minecraft/mods/pack-mod.jar": "pm",
		".minecraft/config/a.cfg":      "cfg",
		"patches/x.json":               "p",
	})
}

type progressCall struct{ done, total int64 }

// recReporter records steps and the progress reported within each step.
type recReporter struct {
	steps    []string
	progress map[string][]progressCall
}

func (r *recReporter) Step(s string) {
	r.steps = append(r.steps, s)
	if r.progress == nil {
		r.progress = map[string][]progressCall{}
	}
}
func (r *recReporter) Progress(d, t int64) {
	if len(r.steps) == 0 {
		return
	}
	cur := r.steps[len(r.steps)-1]
	r.progress[cur] = append(r.progress[cur], progressCall{d, t})
}
func (*recReporter) Warn(string) {}

type createEnv struct {
	opts    CreateOptions
	zipData []byte // the served 2.9.0 pack
	files   map[string][]byte
}

// newCreateEnv serves packFiles as release 2.9.0 (Java 17 URL) from an httptest server
// reached through the pinned official host. Extra routes: /legacy.zip (2.8.0, Java 8
// only), /broken.zip (2.7.0, HTTP 500), /corrupt.zip (2.6.0, bad CRC), /custom_mods.zip
// when set in files, /custom500 path returns 500.
func newCreateEnv(t *testing.T, packFiles map[string]string) *createEnv {
	t.Helper()
	env := &createEnv{zipData: zipBytes(t, "GT New Horizons 2.9.0/", packFiles)}
	env.files = map[string][]byte{
		"/new.zip":     env.zipData,
		"/legacy.zip":  env.zipData,
		"/corrupt.zip": corruptZip(t),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/broken.zip" || r.URL.Path == "/custom500.zip" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		b, ok := env.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, r.URL.Path, time.Time{}, bytes.NewReader(b))
	}))
	t.Cleanup(srv.Close)
	m, err := manifest.Parse([]byte(`{
	  "2.9.0": {"title":"Stable release","releaseDate":"2026/10/04","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/new.zip","java8Url":"https://downloads.gtnewhorizons.com/legacy.zip"}},
	  "2.8.0": {"title":"Stable release","releaseDate":"2025/10/04","mmc":{"java8Url":"https://downloads.gtnewhorizons.com/legacy.zip"}},
	  "2.7.0": {"title":"Stable release","releaseDate":"2024/10/04","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/broken.zip"}},
	  "2.6.0": {"title":"Stable release","releaseDate":"2023/10/04","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/corrupt.zip"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	instances := filepath.Join(t.TempDir(), "instances")
	if err := os.MkdirAll(instances, 0o755); err != nil {
		t.Fatal(err)
	}
	env.opts = CreateOptions{
		Client:       &http.Client{Transport: hostRewrite{srv}},
		Manifest:     m,
		InstancesDir: instances,
		Name:         newName,
		Target:       "2.9.0",
	}
	return env
}

// corruptZip is a valid-looking pack whose stored mod entry fails its CRC check.
func corruptZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := withRequired(map[string]string{".minecraft/mods/m.jar": strings.Repeat("A", 64)})
	for _, k := range []string{".minecraft/mods/m.jar", "mmc-pack.json"} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "GT New Horizons 2.6.0/" + k, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(files[k]))
	}
	zw.Close()
	b := buf.Bytes()
	i := bytes.Index(b, []byte(strings.Repeat("A", 64)))
	b[i+10] = 'B'
	return b
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// createInstance runs PrepareCreate + Apply and fails the test on error.
func createInstance(t *testing.T, env *createEnv, rep Reporter) (*Creation, *CreateResult) {
	t.Helper()
	c, err := PrepareCreate(env.opts, rep)
	if err != nil {
		t.Fatalf("PrepareCreate: %v", err)
	}
	res, err := c.Apply(rep)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return c, res
}

// C1
func TestDefaultInstanceNameIsGTNewHorizonsPlusVersion(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"2.8.4", "GT New Horizons 2.8.4"},
		{"2.9.0-RC-1", "GT New Horizons 2.9.0-RC-1"},
	} {
		if got := DefaultInstanceName(tc.in); got != tc.want {
			t.Errorf("DefaultInstanceName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// C2
func TestCheckInstanceNameRejectsBadNames(t *testing.T) {
	dir := t.TempDir()
	cases := []struct{ name, in string }{
		{"empty", ""},
		{"only spaces", "   "},
		{"slash", "a/b"},
		{"backslash", `a\b`},
		{"colon", "a:b"},
		{"star", "a*b"},
		{"question mark", "a?b"},
		{"double quote", `a"b`},
		{"less than", "a<b"},
		{"greater than", "a>b"},
		{"pipe", "a|b"},
		{"control char 0x01", "a\x01b"},
		{"tab inside", "a\tb"},
		{"control char 0x1f", "a\x1fb"},
		{"leading dot", ".hidden"},
		{"leading underscore", "_private"},
		{"dot", "."},
		{"dotdot", ".."},
		{"leading dot after trim", "  .hidden"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckInstanceName(dir, tc.in)
			if err == nil {
				t.Fatalf("CheckInstanceName(%q) = nil, want an error", tc.in)
			}
			if errors.Is(err, ErrInstanceExists) {
				t.Errorf("CheckInstanceName(%q) = %v, want an error other than ErrInstanceExists", tc.in, err)
			}
		})
	}
}

// C2
func TestCheckInstanceNameReportsExistingDirOrFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "taken dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "taken file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"taken dir", "taken file", "  taken dir  "} {
		if err := CheckInstanceName(dir, in); !errors.Is(err, ErrInstanceExists) {
			t.Errorf("CheckInstanceName(%q) = %v, want ErrInstanceExists", in, err)
		}
	}
}

// C2
func TestCheckInstanceNameAcceptsValidNames(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	cases := []struct{ name, dir, in string }{
		{"default name", dir, "GT New Horizons 2.8.4"},
		{"surrounding spaces trimmed", dir, "  GT New Horizons 2.8.4  "},
		{"inner dot", dir, "a.b"},
		{"trailing underscore", dir, "x_"},
		{"instances dir missing", missing, "GT New Horizons 2.8.4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckInstanceName(tc.dir, tc.in); err != nil {
				t.Errorf("CheckInstanceName(%q) = %v, want nil", tc.in, err)
			}
		})
	}
}

// C3
func TestPrepareCreateRejectsBadNameAndCreatesNothing(t *testing.T) {
	env := newCreateEnv(t, createPack())
	env.opts.Name = "bad|name"
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err == nil {
		c.Close()
		t.Fatal("PrepareCreate with an invalid name succeeded")
	}
	if got := listDir(t, env.opts.InstancesDir); len(got) != 0 {
		t.Errorf("instances dir = %v, want empty", got)
	}
}

// C3
func TestPrepareCreateRejectsExistingNameAndLeavesItAlone(t *testing.T) {
	env := newCreateEnv(t, createPack())
	taken := filepath.Join(env.opts.InstancesDir, newName)
	if err := os.Mkdir(taken, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(taken, "keep.txt"), []byte("keep"), 0o644)
	_, err := PrepareCreate(env.opts, &recReporter{})
	if !errors.Is(err, ErrInstanceExists) {
		t.Fatalf("PrepareCreate = %v, want ErrInstanceExists", err)
	}
	if got := listDir(t, taken); !reflect.DeepEqual(got, []string{"keep.txt"}) {
		t.Errorf("existing folder now holds %v, want [keep.txt]", got)
	}
}

// C3
func TestPrepareCreateRejectsUnknownTargetAndCreatesNothing(t *testing.T) {
	env := newCreateEnv(t, createPack())
	env.opts.Target = "9.9.9"
	if _, err := PrepareCreate(env.opts, &recReporter{}); err == nil {
		t.Fatal("PrepareCreate with an unknown version succeeded")
	}
	if got := listDir(t, env.opts.InstancesDir); len(got) != 0 {
		t.Errorf("instances dir = %v, want empty", got)
	}
}

// C3
func TestPrepareCreateDownloadsIntoNewFolderWithTrimmedName(t *testing.T) {
	env := newCreateEnv(t, createPack())
	env.opts.Name = "  My Pack  "
	rep := &recReporter{}
	c, err := PrepareCreate(env.opts, rep)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	wantDir := filepath.Join(env.opts.InstancesDir, "My Pack")
	if c.Dir != wantDir {
		t.Errorf("Dir = %q, want %q", c.Dir, wantDir)
	}
	if got := mustRead(t, filepath.Join(wantDir, StateDir, "download.zip")); got != string(env.zipData) {
		t.Errorf("download.zip differs from the served pack (%d vs %d bytes)", len(got), len(env.zipData))
	}
	if c.Files != 5 {
		t.Errorf("Files = %d, want 5", c.Files)
	}
}

// C3
func TestPrepareCreateReportsDownloadAndCheckSteps(t *testing.T) {
	env := newCreateEnv(t, createPack())
	rep := &recReporter{}
	c, err := PrepareCreate(env.opts, rep)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	want := []string{"Downloading GTNH 2.9.0", "Checking the download"}
	if !reflect.DeepEqual(rep.steps, want) {
		t.Errorf("steps = %q, want %q", rep.steps, want)
	}
}

// C3
func TestPrepareCreateFlavor(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		want         manifest.Flavor
	}{
		{"java 17 url present", "2.9.0", manifest.Java17},
		{"only java 8 url", "2.8.0", manifest.Java8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCreateEnv(t, createPack())
			env.opts.Target = tc.target
			c, err := PrepareCreate(env.opts, &recReporter{})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if c.Flavor != tc.want {
				t.Errorf("Flavor = %v, want %v", c.Flavor, tc.want)
			}
		})
	}
}

// C3
func TestPrepareCreateDownloadFailureLeavesNothing(t *testing.T) {
	for _, tc := range []struct{ name, target string }{
		{"server 500", "2.7.0"},
		{"corrupt zip", "2.6.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCreateEnv(t, createPack())
			env.opts.Target = tc.target
			if _, err := PrepareCreate(env.opts, &recReporter{}); err == nil {
				t.Fatal("PrepareCreate succeeded")
			}
			if exists(filepath.Join(env.opts.InstancesDir, newName)) {
				t.Errorf("instance folder left behind")
			}
		})
	}
}

// C4, K1
func TestCloseWithoutApplyRemovesFolderAndIsSafeTwice(t *testing.T) {
	env := newCreateEnv(t, createPack())
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	c.Close()
	if exists(c.Dir) {
		t.Errorf("Close left the unfinished instance folder %s", c.Dir)
	}
}

// C4, K1
func TestCloseKeepsFolderThatHasInstanceCfg(t *testing.T) {
	env := newCreateEnv(t, createPack())
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Dir, "instance.cfg"), []byte("name=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if got := mustRead(t, filepath.Join(c.Dir, "instance.cfg")); got != "name=x\n" {
		t.Errorf("instance.cfg = %q, want %q", got, "name=x\n")
	}
	if exists(filepath.Join(c.Dir, StateDir, "download.zip")) {
		t.Errorf("download.zip not removed by Close")
	}
}

// C4, C10, K1: Apply calls Close; a second Close keeps the finished instance.
func TestCloseAfterApplyKeepsInstance(t *testing.T) {
	env := newCreateEnv(t, createPack())
	c, _ := createInstance(t, env, &recReporter{})
	c.Close()
	if !exists(filepath.Join(c.Dir, "instance.cfg")) {
		t.Errorf("instance.cfg gone after Close on a finished instance")
	}
	if got := mustRead(t, filepath.Join(c.Dir, ".minecraft", "config", "a.cfg")); got != "cfg" {
		t.Errorf("a.cfg = %q, want %q", got, "cfg")
	}
}

// C5
func TestApplyWritesEveryPackEntryByteExact(t *testing.T) {
	env := newCreateEnv(t, createPack())
	c, _ := createInstance(t, env, &recReporter{})
	want := map[string]string{
		filepath.Join(c.Dir, "mmc-pack.json"):                      createPack()["mmc-pack.json"],
		filepath.Join(c.Dir, ".minecraft", "mods", "pack-mod.jar"): "pm",
		filepath.Join(c.Dir, ".minecraft", "config", "a.cfg"):      "cfg",
		filepath.Join(c.Dir, "patches", "x.json"):                  "p",
	}
	for p, w := range want {
		if got, ok := readFile(p); got != w {
			t.Errorf("%s = %q (exists %v), want %q", p, got, ok, w)
		}
	}
}

// C5: an older pack's "minecraft/" lands in .minecraft.
func TestApplyWritesOldMinecraftDirIntoDotMinecraft(t *testing.T) {
	env := newCreateEnv(t, withRequired(map[string]string{
		"minecraft/mods/old.jar":   "o",
		"minecraft/config/old.cfg": "oc",
	}))
	c, _ := createInstance(t, env, &recReporter{})
	if got := mustRead(t, filepath.Join(c.Dir, ".minecraft", "config", "old.cfg")); got != "oc" {
		t.Errorf("old.cfg = %q, want %q", got, "oc")
	}
	if exists(filepath.Join(c.Dir, "minecraft")) {
		t.Errorf("a minecraft/ dir was created")
	}
}

// C5
func TestApplyReportsInstallProgressOverAllEntries(t *testing.T) {
	env := newCreateEnv(t, createPack())
	rep := &recReporter{}
	c, res := createInstance(t, env, rep)
	calls := rep.progress["Installing files"]
	if len(calls) == 0 {
		t.Fatalf("no progress under step \"Installing files\" (steps %q)", rep.steps)
	}
	if last := calls[len(calls)-1]; last != (progressCall{5, 5}) {
		t.Errorf("last progress = %+v, want {5 5}", last)
	}
	for _, pc := range calls {
		if pc.total != 5 {
			t.Errorf("progress total = %d, want 5", pc.total)
		}
	}
	if c.Files != 5 || res.Files != 5 {
		t.Errorf("Files = %d / result %d, want 5", c.Files, res.Files)
	}
}

// C5
func TestApplyLeavesNoTempFiles(t *testing.T) {
	env := newCreateEnv(t, createPack())
	c, _ := createInstance(t, env, &recReporter{})
	var tmp []string
	filepath.Walk(c.Dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, ".gtnh-tmp") {
			tmp = append(tmp, p)
		}
		return nil
	})
	if len(tmp) != 0 {
		t.Errorf("temp files left: %v", tmp)
	}
}

// C6, K2
func TestApplyInstanceCfg(t *testing.T) {
	cases := []struct {
		name string
		cfg  *string // nil: pack ships no instance.cfg
		want string
	}{
		{"name line replaced, CRLF kept", strPtr(packCfgCRLF),
			"[General]\r\nInstanceType=OneSix\r\nname=" + newName + "\r\nJavaPath=java\r\n"},
		{"name line replaced, LF", strPtr("[General]\nname=Old\nnotname=Old\n"),
			"[General]\nname=" + newName + "\nnotname=Old\n"},
		{"no name line, after [General]", strPtr("[General]\nJavaPath=java\n"),
			"[General]\nname=" + newName + "\nJavaPath=java\n"},
		{"no name line, no [General]", strPtr("JavaPath=java\n"),
			"JavaPath=java\nname=" + newName + "\n"},
		{"no name line, no trailing newline", strPtr("JavaPath=java"),
			"JavaPath=java\nname=" + newName + "\n"},
		{"empty instance.cfg", strPtr(""),
			"name=" + newName + "\n"},
		{"no instance.cfg in pack", nil,
			"[General]\nInstanceType=OneSix\nname=" + newName + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := createPack()
			delete(files, "instance.cfg")
			if tc.cfg != nil {
				files["instance.cfg"] = *tc.cfg
			}
			env := newCreateEnv(t, files)
			c, _ := createInstance(t, env, &recReporter{})
			if got := mustRead(t, filepath.Join(c.Dir, "instance.cfg")); got != tc.want {
				t.Errorf("instance.cfg = %q, want %q", got, tc.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

// C7
func TestApplyInstallsServerModsPackModWins(t *testing.T) {
	env := newCreateEnv(t, createPack())
	env.files["/custom_mods.zip"] = zipBytes(t, "", map[string]string{"extra-1.jar": "e1", "pack-mod.jar": "server copy"})
	env.opts.CustomModsURL = customModsURL
	env.opts.CustomModsAsked = true
	rep := &recReporter{}
	c, res := createInstance(t, env, rep)
	if res.CustomErr != nil {
		t.Fatalf("CustomErr = %v", res.CustomErr)
	}
	if res.CustomMods == nil {
		t.Fatal("CustomMods = nil")
	}
	if got := mustRead(t, filepath.Join(c.Dir, ".minecraft", "mods", "extra-1.jar")); got != "e1" {
		t.Errorf("extra-1.jar = %q, want %q", got, "e1")
	}
	if got := mustRead(t, filepath.Join(c.Dir, ".minecraft", "mods", "pack-mod.jar")); got != "pm" {
		t.Errorf("pack-mod.jar = %q, want pack's %q", got, "pm")
	}
	if !reflect.DeepEqual(res.CustomMods.Installed, []string{"extra-1.jar"}) {
		t.Errorf("Installed = %v, want [extra-1.jar]", res.CustomMods.Installed)
	}
	if !reflect.DeepEqual(res.CustomMods.Skipped, []string{"pack-mod.jar"}) {
		t.Errorf("Skipped = %v, want [pack-mod.jar]", res.CustomMods.Skipped)
	}
	st, err := LoadState(c.Dir)
	if err != nil || st == nil {
		t.Fatalf("LoadState = %v, %v", st, err)
	}
	if !reflect.DeepEqual(st.CustomMods, []string{"extra-1.jar"}) {
		t.Errorf("State.CustomMods = %v, want [extra-1.jar]", st.CustomMods)
	}
}

// C7: the server-mods step comes after the files.
func TestApplyServerModsStepFollowsInstall(t *testing.T) {
	env := newCreateEnv(t, createPack())
	env.files["/custom_mods.zip"] = zipBytes(t, "", map[string]string{"extra-1.jar": "e1"})
	env.opts.CustomModsURL = customModsURL
	rep := &recReporter{}
	createInstance(t, env, rep)
	idx := func(s string) int {
		for i, x := range rep.steps {
			if x == s {
				return i
			}
		}
		return -1
	}
	files, mods := idx("Installing files"), idx("Installing your server's extra mods")
	if files < 0 || mods < 0 || mods < files {
		t.Errorf("steps = %q, want \"Installing files\" before \"Installing your server's extra mods\"", rep.steps)
	}
}

// C7
func TestApplyServerModsFailureIsNotFatal(t *testing.T) {
	env := newCreateEnv(t, createPack())
	env.opts.CustomModsURL = "https://files.example/custom500.zip"
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Apply(&recReporter{})
	if err != nil {
		t.Fatalf("Apply = %v, want success when only the server mods fail", err)
	}
	if res.CustomErr == nil {
		t.Errorf("CustomErr = nil, want the fetch error")
	}
	if !exists(filepath.Join(c.Dir, "instance.cfg")) {
		t.Errorf("instance missing after a server-mods failure")
	}
	st, err := LoadState(c.Dir)
	if err != nil || st == nil {
		t.Fatalf("LoadState = %v, %v", st, err)
	}
	if len(st.CustomMods) != 0 {
		t.Errorf("State.CustomMods = %v, want empty", st.CustomMods)
	}
}

// C7
func TestApplyLeavesNoBackupDir(t *testing.T) {
	for _, tc := range []struct{ name, url string }{
		{"mods ok", customModsURL},
		{"mods fail", "https://files.example/custom500.zip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCreateEnv(t, createPack())
			env.files["/custom_mods.zip"] = zipBytes(t, "", map[string]string{"extra-1.jar": "e1"})
			env.opts.CustomModsURL = tc.url
			c, _ := createInstance(t, env, &recReporter{})
			got, _ := filepath.Glob(filepath.Join(c.Dir, StateDir, "backup-*"))
			if len(got) != 0 {
				t.Errorf("backup dirs left: %v", got)
			}
		})
	}
}

// C8, K3
func TestApplySavesStateWithFullPackBaseline(t *testing.T) {
	env := newCreateEnv(t, createPack())
	c, _ := createInstance(t, env, &recReporter{})
	zipPath := filepath.Join(t.TempDir(), "same.zip")
	if err := os.WriteFile(zipPath, env.zipData, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := pack.OpenFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	want := p.Fingerprints()
	if _, ok := want["instance.cfg"]; !ok || len(want) != 5 {
		t.Fatalf("fixture fingerprints = %v, want 5 entries incl. instance.cfg", want)
	}
	st, err := LoadState(c.Dir)
	if err != nil || st == nil {
		t.Fatalf("LoadState = %v, %v", st, err)
	}
	if st.Version != "2.9.0" {
		t.Errorf("State.Version = %q, want 2.9.0", st.Version)
	}
	if !reflect.DeepEqual(st.Baseline, want) {
		t.Errorf("State.Baseline = %v, want %v", st.Baseline, want)
	}
}

// C8
func TestApplySavesCustomModsSettingsVerbatim(t *testing.T) {
	for _, tc := range []struct {
		name  string
		url   string
		asked bool
	}{
		{"url and asked", customModsURL, true},
		{"none, never asked", "", false},
		{"none, asked", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCreateEnv(t, createPack())
			env.files["/custom_mods.zip"] = zipBytes(t, "", map[string]string{"extra-1.jar": "e1"})
			env.opts.CustomModsURL, env.opts.CustomModsAsked = tc.url, tc.asked
			c, _ := createInstance(t, env, &recReporter{})
			st, err := LoadState(c.Dir)
			if err != nil || st == nil {
				t.Fatalf("LoadState = %v, %v", st, err)
			}
			if st.CustomModsURL != tc.url || st.CustomModsAsked != tc.asked {
				t.Errorf("state URL %q asked %v, want %q %v", st.CustomModsURL, st.CustomModsAsked, tc.url, tc.asked)
			}
		})
	}
}

// C9: a file where the mods dir should be makes a write fail.
func TestApplyFailureRemovesInstanceFolder(t *testing.T) {
	env := newCreateEnv(t, createPack())
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err != nil {
		t.Fatal(err)
	}
	game := filepath.Join(c.Dir, ".minecraft")
	if err := os.MkdirAll(game, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(game, "mods"), []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(&recReporter{}); err == nil {
		t.Fatal("Apply succeeded although mods/ is a file")
	}
	if exists(c.Dir) {
		t.Errorf("instance folder %s left after a failed Apply", c.Dir)
	}
}

// C10
func TestApplySuccessLeavesLoadableInstance(t *testing.T) {
	env := newCreateEnv(t, createPack())
	env.opts.Name = "  " + newName + "  "
	c, res := createInstance(t, env, &recReporter{})
	if exists(filepath.Join(c.Dir, StateDir, "download.zip")) {
		t.Errorf("download.zip left behind")
	}
	if !exists(filepath.Join(c.Dir, StateDir, "state.json")) {
		t.Errorf("state.json missing")
	}
	if !reflect.DeepEqual(res.Instance, c.Instance) {
		t.Errorf("result Instance = %+v, want %+v", res.Instance, c.Instance)
	}
	if res.Files != c.Files {
		t.Errorf("result Files = %d, want %d", res.Files, c.Files)
	}
	inst, err := prism.LoadInstance(c.Dir)
	if err != nil {
		t.Fatalf("LoadInstance: %v", err)
	}
	if inst.Name != newName || !inst.GTNH {
		t.Errorf("loaded instance name %q GTNH %v, want %q true", inst.Name, inst.GTNH, newName)
	}
}

// C11
func TestCreateTouchesOnlyItsOwnFolder(t *testing.T) {
	env := newCreateEnv(t, createPack())
	sibling := filepath.Join(env.opts.InstancesDir, "Other")
	os.MkdirAll(filepath.Join(sibling, ".minecraft"), 0o755)
	os.WriteFile(filepath.Join(sibling, "instance.cfg"), []byte("name=Other\n"), 0o644)
	os.WriteFile(filepath.Join(env.opts.InstancesDir, "instgroups.json"), []byte("{}"), 0o644)
	createInstance(t, env, &recReporter{})
	got := listDir(t, env.opts.InstancesDir)
	sort.Strings(got)
	want := []string{newName, "Other", "instgroups.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("instances dir = %v, want %v", got, want)
	}
	if got := listDir(t, sibling); !reflect.DeepEqual(got, []string{".minecraft", "instance.cfg"}) {
		t.Errorf("sibling holds %v, want [.minecraft instance.cfg]", got)
	}
	if got := mustRead(t, filepath.Join(sibling, "instance.cfg")); got != "name=Other\n" {
		t.Errorf("sibling instance.cfg = %q", got)
	}
	if got := mustRead(t, filepath.Join(env.opts.InstancesDir, "instgroups.json")); got != "{}" {
		t.Errorf("instgroups.json = %q", got)
	}
}

// C9: a write that can't land (a non-empty directory where a pack file goes) fails the
// whole creation; nothing is left behind.
func TestApplyFailureWhenTargetIsDirectoryRemovesInstanceFolder(t *testing.T) {
	env := newCreateEnv(t, createPack())
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(c.Dir, ".minecraft", "config", "a.cfg")
	if err := os.MkdirAll(blocker, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocker, "inside"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(&recReporter{}); err == nil {
		t.Fatal("Apply succeeded although a.cfg is a non-empty directory")
	}
	if exists(c.Dir) {
		t.Errorf("instance folder %s left after a failed Apply", c.Dir)
	}
}
