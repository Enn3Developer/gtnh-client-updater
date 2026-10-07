package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

func TestPackagedBuildsNeverSelfUpdate(t *testing.T) {
	old := packaged
	packaged = "AUR"
	defer func() { packaged = old }()
	// A client that fails every request proves the refusal happens before any network use.
	client := &http.Client{Transport: failTransport{t}}
	err := selfUpdate(client)
	if err == nil || !strings.Contains(err.Error(), "package manager") {
		t.Fatalf("packaged build: want package-manager error, got %v", err)
	}
}

type failTransport struct{ t *testing.T }

func (f failTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Fatalf("unexpected network request to %s", r.URL)
	return nil, nil
}

// ---- -configs (C1) ----

func TestConfigChoiceNewIsTakeNew(t *testing.T) { // C1
	got, err := configChoice("new")
	if err != nil || got != update.TakeNew {
		t.Errorf("configChoice(\"new\") = %v, %v; want TakeNew, nil", got, err)
	}
}

func TestConfigChoiceMineIsKeepMine(t *testing.T) { // C1
	got, err := configChoice("mine")
	if err != nil || got != update.KeepMine {
		t.Errorf("configChoice(\"mine\") = %v, %v; want KeepMine, nil", got, err)
	}
}

func TestConfigChoiceRejectsAnythingElse(t *testing.T) { // C1
	for _, v := range []string{"", "NEW", "both", "mine "} {
		t.Run(v, func(t *testing.T) {
			if _, err := configChoice(v); err == nil {
				t.Errorf("configChoice(%q) error = nil, want an error", v)
			}
		})
	}
}

// ---- -create options (C2) ----

const testManifest = `{
  "2.8.4": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/a.zip"}},
  "2.8.1": {"title":"Stable release","releaseDate":"2025/10/01","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/c.zip"}},
  "2.9.0-RC-1": {"title":"Beta release","releaseDate":"2026/09/24","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/b.zip"}}}`

func testMan(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(testManifest))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCreateOptionsLatestIsNewestRelease(t *testing.T) { // C2
	opts, err := createOptions(testMan(t), t.TempDir(), "x", "latest", "")
	if err != nil || opts.Target != "2.9.0-RC-1" {
		t.Errorf("createOptions(latest) target = %q, %v; want 2.9.0-RC-1, nil", opts.Target, err)
	}
}

func TestCreateOptionsLatestStableIsNewestStable(t *testing.T) { // C2
	opts, err := createOptions(testMan(t), t.TempDir(), "x", "latest-stable", "")
	if err != nil || opts.Target != "2.8.4" {
		t.Errorf("createOptions(latest-stable) target = %q, %v; want 2.8.4, nil", opts.Target, err)
	}
}

func TestCreateOptionsExactKeyIsKept(t *testing.T) { // C2
	opts, err := createOptions(testMan(t), t.TempDir(), "x", "2.8.1", "")
	if err != nil || opts.Target != "2.8.1" {
		t.Errorf("createOptions(2.8.1) target = %q, %v; want 2.8.1, nil", opts.Target, err)
	}
}

func TestCreateOptionsUnknownTargetFails(t *testing.T) { // C2
	if _, err := createOptions(testMan(t), t.TempDir(), "x", "3.0.0", ""); err == nil {
		t.Error("createOptions(3.0.0) error = nil, want an unknown-version error")
	}
}

func TestCreateOptionsEmptyNameDefaultsFromTarget(t *testing.T) { // C2
	opts, err := createOptions(testMan(t), t.TempDir(), "", "2.8.1", "")
	if err != nil || opts.Name != "GT New Horizons 2.8.1" {
		t.Errorf("createOptions(name \"\") name = %q, %v; want %q, nil", opts.Name, err, "GT New Horizons 2.8.1")
	}
}

func TestCreateOptionsTrimsName(t *testing.T) { // C2
	opts, err := createOptions(testMan(t), t.TempDir(), "  My Pack  ", "2.8.1", "")
	if err != nil || opts.Name != "My Pack" {
		t.Errorf("createOptions(name padded) name = %q, %v; want \"My Pack\", nil", opts.Name, err)
	}
}

func TestCreateOptionsExistingFolderIsErrInstanceExists(t *testing.T) { // C2
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := createOptions(testMan(t), dir, "Taken", "2.8.1", "")
	if !errors.Is(err, update.ErrInstanceExists) {
		t.Errorf("createOptions(existing name) error = %v, want ErrInstanceExists", err)
	}
}

func TestCreateOptionsInvalidNameFails(t *testing.T) { // C2
	_, err := createOptions(testMan(t), t.TempDir(), "a/b", "2.8.1", "")
	if err == nil || errors.Is(err, update.ErrInstanceExists) {
		t.Errorf("createOptions(\"a/b\") error = %v, want an invalid-name error", err)
	}
}

func TestCreateOptionsNoServerModsFlagLeavesItUnasked(t *testing.T) { // C2
	opts, err := createOptions(testMan(t), t.TempDir(), "x", "2.8.1", "")
	if err != nil || opts.CustomModsURL != "" || opts.CustomModsAsked {
		t.Errorf("createOptions(serverMods \"\") = url %q asked %v err %v; want \"\", false, nil", opts.CustomModsURL, opts.CustomModsAsked, err)
	}
}

func TestCreateOptionsServerModsNoneIsAskedAndEmpty(t *testing.T) { // C2
	opts, err := createOptions(testMan(t), t.TempDir(), "x", "2.8.1", "none")
	if err != nil || opts.CustomModsURL != "" || !opts.CustomModsAsked {
		t.Errorf("createOptions(serverMods none) = url %q asked %v err %v; want \"\", true, nil", opts.CustomModsURL, opts.CustomModsAsked, err)
	}
}

func TestCreateOptionsServerModsURLIsKeptVerbatim(t *testing.T) { // C2
	opts, err := createOptions(testMan(t), t.TempDir(), "x", "2.8.1", "https://x/y.zip")
	if err != nil || opts.CustomModsURL != "https://x/y.zip" || !opts.CustomModsAsked {
		t.Errorf("createOptions(serverMods url) = url %q asked %v err %v; want https://x/y.zip, true, nil", opts.CustomModsURL, opts.CustomModsAsked, err)
	}
}

func TestCreateOptionsFillsManifestDirAndLeavesClientNil(t *testing.T) { // C2
	man, dir := testMan(t), t.TempDir()
	opts, err := createOptions(man, dir, "x", "2.8.1", "")
	if err != nil || opts.Manifest != man || opts.InstancesDir != dir || opts.Client != nil {
		t.Errorf("createOptions = manifest same %v, dir %q, client %v, err %v; want same, %q, nil, nil",
			opts.Manifest == man, opts.InstancesDir, opts.Client, err, dir)
	}
}

// ---- createHeadless: everything that fails before the download ----

// manifestTransport serves testManifest for the manifest URL and fails the test on anything else.
type manifestTransport struct{ t *testing.T }

func (f manifestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() != manifest.URL {
		f.t.Fatalf("unexpected network request to %s", r.URL)
	}
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(testManifest)), Request: r}, nil
}

func TestCreateHeadlessWithoutPrismFailsBeforeNetwork(t *testing.T) {
	client := &http.Client{Transport: failTransport{t}}
	err := createHeadless(client, nil, "x", "2.8.1", "")
	if err == nil || !strings.Contains(err.Error(), "Prism Launcher not found") {
		t.Errorf("createHeadless(no dirs) error = %v, want Prism-not-found", err)
	}
}

func TestCreateHeadlessManifestFailureIsReturned(t *testing.T) {
	client := &http.Client{Transport: errTransport{}}
	err := createHeadless(client, []string{t.TempDir()}, "x", "2.8.1", "")
	if !errors.Is(err, errOffline) {
		t.Errorf("createHeadless(offline) error = %v, want it to wrap the transport error", err)
	}
}

func TestCreateHeadlessRefusesExistingInstanceFolder(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, "instances", "Taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: manifestTransport{t}}
	err := createHeadless(client, []string{data}, "Taken", "2.8.1", "")
	if !errors.Is(err, update.ErrInstanceExists) {
		t.Errorf("createHeadless(existing name) error = %v, want ErrInstanceExists", err)
	}
}

var errOffline = errors.New("offline")

type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, errOffline }

// ---- printCustomMods ----

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	f()
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestPrintModsWithoutASyncPrintsNothing(t *testing.T) {
	if out := captureStdout(t, func() { printMods("https://mods.example/m.zip", nil, nil) }); out != "" {
		t.Errorf("printMods(nil) printed %q, want nothing", out)
	}
}

func TestPrintModsListsWhatChanged(t *testing.T) {
	p := &update.ModsPlan{
		Changes: []update.ModChange{
			{Kind: update.ModAdd, Name: "a.jar", Disk: "a.jar"},
			{Kind: update.ModReplace, Name: "b.jar", Disk: "b.jar", Preserve: true},
			{Kind: update.ModRemove, Name: "c.jar", Disk: "c.jar.disabled"},
			{Kind: update.ModSkip, Name: "core.jar", With: "core.jar"},
			{Kind: update.ModKeep, Name: "d.jar", Disk: "d.jar"},
		},
		Managed: map[string]pack.Fingerprint{"a.jar": {}, "b.jar": {}},
		Ignored: []string{"sub/x.jar"},
	}
	out := captureStdout(t, func() { printMods("https://mods.example/m.zip", p, nil) })
	want := `  server mods from https://mods.example/m.zip: 2
  server mods added: a.jar
  server mods updated: b.jar
  server mods removed: c.jar.disabled
  your own jars moved to .gtnh-updater/replaced-mods to make way for the server's: b.jar
  server mods left out, the instance has them already: core.jar
  dropped by the server but kept, you changed them: d.jar
  left out of the server's zip (in a folder, or a name Windows doesn't allow): sub/x.jar
`
	if out != want {
		t.Errorf("printMods =\n%s\nwant\n%s", out, want)
	}
}

func TestPrintModsSaysWhyTheSyncFailed(t *testing.T) {
	out := captureStdout(t, func() { printMods("", nil, errors.New("disk full")) })
	if out != "  heads up: your server's mods couldn't be synced: disk full\n" {
		t.Errorf("printMods = %q", out)
	}
}

// modsTransport answers every request with zipData and an ETag.
type modsTransport struct{ zipData []byte }

func (f modsTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", ContentLength: int64(len(f.zipData)),
		Header: http.Header{"Etag": {`"1"`}}, Body: io.NopCloser(bytes.NewReader(f.zipData)), Request: r}, nil
}

// -play syncs the server's mods before the game starts, from the -server-mods link.
func TestSyncModsHeadlessInstallsTheServersMods(t *testing.T) {
	data := t.TempDir()
	var err error
	captureStdout(t, func() {
		err = createHeadless(&http.Client{Transport: packTransport{t, tinyPack(t)}}, []string{data}, "Mine", "2.8.1", "none")
	})
	if err != nil {
		t.Fatalf("setup createHeadless = %v", err)
	}
	inst, err := prism.LoadInstance(filepath.Join(data, "instances", "Mine"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("extra.jar")
	w.Write([]byte("extra"))
	zw.Close()
	client := &http.Client{Transport: modsTransport{buf.Bytes()}}

	out := captureStdout(t, func() { syncModsHeadless(client, inst, "https://mods.example.org/m.zip") })

	if got, err := os.ReadFile(filepath.Join(inst.GameDir, "mods", "extra.jar")); err != nil || string(got) != "extra" {
		t.Errorf("extra.jar = %q, %v; want the server's jar", got, err)
	}
	if !strings.Contains(out, "server mods added: extra.jar") {
		t.Errorf("output = %q, want the added jar", out)
	}
	if st, _ := update.LoadState(inst.Dir); st == nil || st.CustomModsURL != "https://mods.example.org/m.zip" || !st.CustomModsAsked {
		t.Errorf("state = %+v, want the link saved", st)
	}
}

// A server that can't be reached is only a heads up: the game starts anyway.
func TestSyncModsHeadlessFailureIsAHeadsUp(t *testing.T) {
	inst := prism.Instance{Dir: t.TempDir(), Name: "Mine", GTNH: true}
	inst.GameDir = filepath.Join(inst.Dir, ".minecraft")
	client := &http.Client{Transport: packTransport{t, nil}}

	out := captureStdout(t, func() { syncModsHeadless(client, inst, brokenModsURL) })

	if !strings.Contains(out, "heads up: I couldn't check your server's mods: the server behind the link answered with error 500.") {
		t.Errorf("output = %q, want the heads up", out)
	}
}

// ---- -configs default (C15) ----

// withCommandLine swaps in a fresh flag set holding a -configs flag parsed from args.
func withCommandLine(t *testing.T, args ...string) *string {
	t.Helper()
	old := flag.CommandLine
	fs := flag.NewFlagSet("gtnh-update", flag.ContinueOnError)
	v := fs.String("configs", "new", "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	flag.CommandLine = fs
	t.Cleanup(func() { flag.CommandLine = old })
	return v
}

func TestTUIConfigsIsEmptyWhenFlagNotGiven(t *testing.T) { // C15
	v := withCommandLine(t)
	if got := tuiConfigs(*v); got != "" {
		t.Errorf("tuiConfigs(%q) without -configs = %q, want \"\"", *v, got)
	}
}

func TestTUIConfigsIsTheValueWhenFlagGiven(t *testing.T) { // C15
	v := withCommandLine(t, "-configs", "mine")
	if got := tuiConfigs(*v); got != "mine" {
		t.Errorf("tuiConfigs with -configs mine = %q, want \"mine\"", got)
	}
}

func TestTUIConfigsKeepsExplicitDefaultValue(t *testing.T) { // C15
	v := withCommandLine(t, "-configs", "new")
	if got := tuiConfigs(*v); got != "new" {
		t.Errorf("tuiConfigs with -configs new = %q, want \"new\"", got)
	}
}

// ---- createHeadless: full run ----

// packTransport serves testManifest and, for the pinned download host, zipData.
type packTransport struct {
	t       *testing.T
	zipData []byte
}

func (f packTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body := testManifest
	switch {
	case r.URL.String() == manifest.URL:
	case r.URL.Host == manifest.DownloadHost:
		body = string(f.zipData)
	case r.URL.Host == brokenModsHost:
		return &http.Response{StatusCode: http.StatusInternalServerError, Status: "500 Internal Server Error",
			Body: io.NopCloser(strings.NewReader("boom")), Request: r}, nil
	default:
		f.t.Fatalf("unexpected network request to %s", r.URL)
	}
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", ContentLength: int64(len(body)),
		Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func tinyPack(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"GT New Horizons 2.8.1/mmc-pack.json":           `{"components":[{"uid":"org.lwjgl3"},{"uid":"net.minecraft","version":"1.7.10"}]}`,
		"GT New Horizons 2.8.1/instance.cfg":            "[General]\nname=Pack\n",
		"GT New Horizons 2.8.1/.minecraft/mods/a.jar":   "a",
		"GT New Horizons 2.8.1/.minecraft/config/b.cfg": "b",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCreateHeadlessCreatesFinishedInstance(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, "instances"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: packTransport{t, tinyPack(t)}}
	var err error
	out := captureStdout(t, func() { err = createHeadless(client, []string{data}, "Mine", "2.8.1", "none") })
	if err != nil {
		t.Fatalf("createHeadless = %v, want nil", err)
	}
	cfg, rerr := os.ReadFile(filepath.Join(data, "instances", "Mine", "instance.cfg"))
	if rerr != nil || string(cfg) != "[General]\nname=Mine\n" {
		t.Errorf("instance.cfg = %q, %v; want the pack's cfg named Mine", cfg, rerr)
	}
	if !strings.Contains(out, "Done: Mine is ready in Prism.") {
		t.Errorf("output = %q, want the Done line", out)
	}
}

// ---- headless: -version resolution ----

func TestHeadlessUnknownVersionFailsNamingIt(t *testing.T) {
	inst := filepath.Join(t.TempDir(), "Pack")
	if err := os.MkdirAll(filepath.Join(inst, ".minecraft"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inst, "instance.cfg"), []byte("name=Pack\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inst, "mmc-pack.json"), []byte(`{"components":[{"uid":"net.minecraft","version":"1.7.10"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: manifestTransport{t}}
	err := headless(client, nil, inst, "2.8.1", "9.9.9", "none", update.TakeNew)
	if err == nil || !strings.Contains(err.Error(), `"9.9.9"`) {
		t.Errorf("headless(-version 9.9.9) error = %v, want one naming \"9.9.9\"", err)
	}
}

// brokenModsHost answers every server-mods request with HTTP 500.
const brokenModsHost = "mods.example"

const brokenModsURL = "https://" + brokenModsHost + "/custom_mods.zip"

func TestCreateHeadlessReportsServerModsFailure(t *testing.T) { // C1
	data := t.TempDir()
	client := &http.Client{Transport: packTransport{t, tinyPack(t)}}
	var err error
	out := captureStdout(t, func() { err = createHeadless(client, []string{data}, "Mine", "2.8.1", brokenModsURL) })
	if err != nil {
		t.Fatalf("createHeadless = %v, want nil (server mods failing isn't fatal)", err)
	}
	if !strings.Contains(out, "heads up: I couldn't get your server's mods: the server behind the link answered with error 500.") {
		t.Errorf("output = %q, want the server-mods warning", out)
	}
}

func TestCreateHeadlessWithoutServerModsHasNoWarning(t *testing.T) { // C1
	data := t.TempDir()
	client := &http.Client{Transport: packTransport{t, tinyPack(t)}}
	var err error
	out := captureStdout(t, func() { err = createHeadless(client, []string{data}, "Mine", "2.8.1", "none") })
	if err != nil || strings.Contains(out, "heads up") {
		t.Errorf("createHeadless = %v, output %q; want nil and no heads up", err, out)
	}
}

func TestHeadlessReportsServerModsSyncFailure(t *testing.T) { // C1
	data := t.TempDir()
	client := &http.Client{Transport: packTransport{t, tinyPack(t)}}
	var err error
	captureStdout(t, func() { err = createHeadless(client, []string{data}, "Mine", "2.8.1", "none") })
	if err != nil {
		t.Fatalf("setup createHeadless = %v", err)
	}
	inst := filepath.Join(data, "instances", "Mine")
	out := captureStdout(t, func() { err = headless(client, nil, inst, "", "2.8.1", brokenModsURL, update.TakeNew) })
	if err != nil {
		t.Fatalf("headless = %v, want nil (server mods failing isn't fatal)", err)
	}
	if !strings.Contains(out, "heads up: I couldn't check your server's mods: the server behind the link answered with error 500.") {
		t.Errorf("output = %q, want the server-mods warning", out)
	}
	if strings.Count(out, "heads up") != 1 {
		t.Errorf("output = %q, want the warning once", out)
	}
}

// ---- -play (home spec C13) ----

func TestCheckPlayFlagsRejectsPlayWithCreate(t *testing.T) { // C13
	err := checkPlayFlags(true, true, false, "")
	if err == nil || err.Error() != "-play can't be used with -create" {
		t.Errorf("checkPlayFlags(-play -create) = %v, want %q", err, "-play can't be used with -create")
	}
}

func TestCheckPlayFlagsPlayYesNeedsInstance(t *testing.T) { // C13
	err := checkPlayFlags(true, false, true, "")
	if err == nil || err.Error() != "-play -yes needs -instance" {
		t.Errorf("checkPlayFlags(-play -yes) = %v, want %q", err, "-play -yes needs -instance")
	}
}

func TestCheckPlayFlagsAcceptsValidCombinations(t *testing.T) { // C13
	cases := []struct {
		name              string
		play, create, yes bool
		instance          string
	}{
		{"-play alone", true, false, false, ""},
		{"-play -yes -instance", true, false, true, "X"},
		{"-create -yes without -play", false, true, true, ""},
		{"-yes without -play or -instance", false, false, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := checkPlayFlags(c.play, c.create, c.yes, c.instance); err != nil {
				t.Errorf("checkPlayFlags = %v, want nil", err)
			}
		})
	}
}

func TestPlayHeadlessUnknownInstanceFailsBeforeNetwork(t *testing.T) { // C13
	client := &http.Client{Transport: failTransport{t}}
	err := playHeadless(client, []string{t.TempDir()}, "nope", "", "", "", update.TakeNew)
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("playHeadless(nope) = %v, want an error naming \"nope\"", err)
	}
}

func TestPlayHeadlessWithVersionUpdatesFirstAndStopsOnItsError(t *testing.T) { // C13
	client := &http.Client{Transport: errTransport{}}
	err := playHeadless(client, []string{t.TempDir()}, "nope", "", "2.8.4", "none", update.TakeNew)
	if !errors.Is(err, errOffline) {
		t.Errorf("playHeadless(-version, offline) = %v, want the update's network error", err)
	}
}

func TestPlayHeadlessWithoutPrismFails(t *testing.T) { // C13
	client := &http.Client{Transport: failTransport{t}}
	err := playHeadless(client, nil, "nope", "", "", "", update.TakeNew)
	if err == nil || err.Error() != "Prism Launcher not found -- pass -prism-dir" {
		t.Errorf("playHeadless(no dirs) = %v, want %q", err, "Prism Launcher not found -- pass -prism-dir")
	}
}

// ---- -configs maps through update.ParseChoice (C6) ----

func TestC6ConfigChoiceAgreesWithParseChoice(t *testing.T) {
	for _, v := range []string{"new", "mine"} {
		want, _ := update.ParseChoice(v)
		got, err := configChoice(v)
		if err != nil || got != want {
			t.Errorf("configChoice(%q) = %v, %v; want %v, nil", v, got, err, want)
		}
	}
}

func TestC6ConfigChoiceKeepsItsOwnErrorText(t *testing.T) {
	_, err := configChoice("both")
	want := `-configs must be "new" or "mine", not "both"`
	if err == nil || err.Error() != want {
		t.Errorf("configChoice(\"both\") error = %v, want %q", err, want)
	}
}
