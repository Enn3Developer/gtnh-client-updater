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

func TestPrintCustomModsNilPrintsNothing(t *testing.T) {
	if out := captureStdout(t, func() { printCustomMods(nil) }); out != "" {
		t.Errorf("printCustomMods(nil) printed %q, want nothing", out)
	}
}

func TestPrintCustomModsWithoutSkippedHasNoSkippedLine(t *testing.T) {
	cm := &update.CustomModsResult{Installed: []string{"a.jar", "b.jar"}, Added: []string{"a.jar"}}
	out := captureStdout(t, func() { printCustomMods(cm) })
	if !strings.Contains(out, "2 installed") || strings.Contains(out, "skipped") {
		t.Errorf("printCustomMods = %q, want the 2 installed mods counted and no skipped line", out)
	}
}

func TestPrintCustomModsListsSkippedJars(t *testing.T) {
	cm := &update.CustomModsResult{Skipped: []string{"x.jar"}}
	out := captureStdout(t, func() { printCustomMods(cm) })
	if !strings.Contains(out, "skipped") || !strings.Contains(out, "x.jar") {
		t.Errorf("printCustomMods = %q, want a skipped line naming x.jar", out)
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
	if !strings.Contains(out, "heads up: your server's extra mods couldn't be installed:") {
		t.Errorf("output = %q, want the server-mods install warning", out)
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
	if !strings.Contains(out, "heads up: your server's extra mods couldn't be synced:") {
		t.Errorf("output = %q, want the server-mods sync warning", out)
	}
}
