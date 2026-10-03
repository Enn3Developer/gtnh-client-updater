package tui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/appcfg"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Tests for the settings screen (settings spec C1-C9). Clause numbers in the comments
// refer to the settings spec.

// ---- fixtures ----

const plainCfg = "[General]\nname=GTNH Pack\nMyCustomKey=hello\n"

const (
	badServer = "That doesn't look like a server address — try play.example.com or play.example.com:25565."
	badMods   = "That doesn't look like a download link — it should start with https://"
	badMemory = "Give me a whole number of MB between 1024 and 65536."
	badFile   = "I can't find a file there."
	badWindow = "Give me width and height like 1920x1080."
)

// fakeApp stands in for appcfg.Save: it records every saved config.
type fakeApp struct {
	saved []appcfg.Config
	err   error
}

type setFix struct {
	m       *model
	dataDir string
	dir     string
	app     *fakeApp
}

// settingsFixture is a model on one instance <dataDir>/instances/pack. cfg is the
// content of its instance.cfg ("" = no instance.cfg); gtnh decides its mmc-pack.json.
func settingsFixture(t *testing.T, cfg string, gtnh bool) *setFix {
	t.Helper()
	f := &setFix{m: sizedModel(t), dataDir: t.TempDir(), app: &fakeApp{}}
	f.dir = filepath.Join(f.dataDir, "instances", "pack")
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if cfg != "" {
		if err := os.WriteFile(filepath.Join(f.dir, "instance.cfg"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mc := "1.12.2"
	if gtnh {
		mc = "1.7.10"
	}
	pack := `{"components":[{"uid":"net.minecraft","version":"` + mc + `"}]}`
	if err := os.WriteFile(filepath.Join(f.dir, "mmc-pack.json"), []byte(pack), 0o644); err != nil {
		t.Fatal(err)
	}
	f.m.cfg.PrismDirs = []string{f.dataDir}
	f.m.manifest = routeManifest(t)
	inst := prism.Instance{Dir: f.dir, Name: "GTNH Pack", GTNH: gtnh}
	f.m.insts, f.m.inst = []prism.Instance{inst}, inst
	f.m.saveApp = func(c appcfg.Config) error {
		f.app.saved = append(f.app.saved, c)
		return f.app.err
	}
	return f
}

// open shows the settings list with the row key selected.
func (f *setFix) open(key string) {
	f.m.setting = key
	f.m.showSettings()
}

// edit opens the edit screen of the row key.
func (f *setFix) edit(t *testing.T, key string) {
	t.Helper()
	f.open(key)
	press(f.m, keyEnter)
	if f.m.screen != scSettingEdit || f.m.setting != key {
		t.Fatalf("setup: enter on %q: screen %d, setting %q; want scSettingEdit (%d)", key, f.m.screen, f.m.setting, scSettingEdit)
	}
}

// submit replaces the typed value with v and presses enter.
func (f *setFix) submit(v string) tea.Cmd {
	f.m.setIn.SetValue(v)
	return press(f.m, keyEnter)
}

func (f *setFix) settings(t *testing.T) prism.Settings {
	t.Helper()
	s, err := prism.ReadSettings(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *setFix) state(t *testing.T) *update.State {
	t.Helper()
	st, err := update.LoadState(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func (f *setFix) cfgText(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dir, "instance.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var overrideKeys = []string{"OverrideMemory", "MinMemAlloc", "MaxMemAlloc", "OverrideJavaArgs", "JvmArgs",
	"OverrideJavaLocation", "JavaPath", "OverrideWindow", "MinecraftWinWidth", "MinecraftWinHeight"}

// otherLines is cfg without the lines of the ten override keys.
func otherLines(cfg string) []string {
	var out []string
	for _, l := range strings.SplitAfter(cfg, "\n") {
		k, _, _ := strings.Cut(l, "=")
		if !slices.Contains(overrideKeys, k) {
			out = append(out, l)
		}
	}
	return out
}

func itemKeys(m *model) []string {
	var out []string
	for _, it := range m.list.Items() {
		out = append(out, it.(item).key)
	}
	return out
}

func itemTitles(m *model) []string {
	var out []string
	for _, it := range m.list.Items() {
		out = append(out, it.(item).title)
	}
	return out
}

func itemDescList(m *model) []string {
	var out []string
	for _, it := range m.list.Items() {
		out = append(out, it.(item).desc)
	}
	return out
}

func helpPairs(m *model) []string {
	var out []string
	for _, b := range m.list.AdditionalShortHelpKeys() {
		out = append(out, b.Help().Key+" "+b.Help().Desc)
	}
	return out
}

// ---- C1 rows ----

func TestSettingsRowsForGTNHInstanceWithoutOverrides(t *testing.T) { // C1, rows 1-8
	f := settingsFixture(t, plainCfg, true)
	f.open("")
	wantKeys := []string{"server", "mods", "memory", "jvm", "java", "window", "after", "prism"}
	wantTitles := []string{"Server to join", "Server extra mods link", "Memory for the game", "Java arguments",
		"Java to run it with", "Window size", "After I start the game", "Prism Launcher location"}
	wantDescs := []string{"none — set one and j on the home screen joins it", "none", "Prism's default", "Prism's default",
		"Prism's default", "Prism's default", "stay open and show whether it's running", "found automatically"}
	if f.m.screen != scSettings {
		t.Fatalf("screen %d, want scSettings (%d)", f.m.screen, scSettings)
	}
	if got := itemKeys(f.m); !slices.Equal(got, wantKeys) {
		t.Errorf("keys = %q, want %q", got, wantKeys)
	}
	if got := itemTitles(f.m); !slices.Equal(got, wantTitles) {
		t.Errorf("titles = %q, want %q", got, wantTitles)
	}
	if got := itemDescList(f.m); !slices.Equal(got, wantDescs) {
		t.Errorf("descs = %q, want %q", got, wantDescs)
	}
}

func TestSettingsRowsShowSavedStateOverridesAndAppConfig(t *testing.T) { // C1, rows 1-8
	cfg := "[General]\nname=GTNH Pack\nOverrideMemory=true\nMinMemAlloc=2048\nMaxMemAlloc=6144\n" +
		"OverrideJavaArgs=true\nJvmArgs=-XX:+UseG1GC\nOverrideJavaLocation=true\nJavaPath=/usr/bin/java\n" +
		"OverrideWindow=true\nMinecraftWinWidth=1920\nMinecraftWinHeight=1080\n"
	f := settingsFixture(t, cfg, true)
	writeState(t, f.dir, update.State{Version: "2.8.4", ServerAddress: "mc.x:1", CustomModsURL: "https://mods.example.com/a/custom.zip", CustomModsAsked: true})
	f.m.app = appcfg.Config{PrismExe: "/opt/prism/prismlauncher", AfterPlay: appcfg.AfterPlayQuit}
	f.open("")
	want := []string{"mc.x:1", "mods.example.com", "6144 MB (at least 2048 MB)", "-XX:+UseG1GC",
		"/usr/bin/java", "1920×1080", "quit", "/opt/prism/prismlauncher"}
	if got := itemDescList(f.m); !slices.Equal(got, want) {
		t.Errorf("descs = %q, want %q", got, want)
	}
}

func TestSettingsOverrideValuesIgnoredWhenOverrideIsOff(t *testing.T) { // C1, rows 3-6
	cfg := "[General]\nname=GTNH Pack\nOverrideMemory=false\nMinMemAlloc=2048\nMaxMemAlloc=6144\n" +
		"OverrideJavaArgs=false\nJvmArgs=-XX:+UseG1GC\nOverrideJavaLocation=false\nJavaPath=/usr/bin/java\n" +
		"OverrideWindow=false\nMinecraftWinWidth=1920\nMinecraftWinHeight=1080\n"
	f := settingsFixture(t, cfg, true)
	f.open("")
	d := itemDescs(f.m)
	got := []string{d["memory"], d["jvm"], d["java"], d["window"]}
	want := []string{"Prism's default", "Prism's default", "Prism's default", "Prism's default"}
	if !slices.Equal(got, want) {
		t.Errorf("memory/jvm/java/window descs = %q, want %q", got, want)
	}
}

func TestSettingsLongJvmArgsAreTruncatedToListWidth(t *testing.T) { // C1, row 4
	long := strings.Repeat("-XX:+UseG1GC ", 20) // 260 columns, wider than any list here
	f := settingsFixture(t, "[General]\nname=GTNH Pack\nOverrideJavaArgs=true\nJvmArgs="+long+"\n", true)
	f.open("")
	d := itemDescs(f.m)["jvm"]
	listW := f.m.listWidthFor(scSettings)
	if w := ansi.StringWidth(d); w > listW || !strings.HasSuffix(d, "…") || !strings.HasPrefix(long, strings.TrimSuffix(d, "…")) {
		t.Errorf("jvm desc %q is %d columns; want a truncated prefix of the args ending in … within the list width %d", d, w, listW)
	}
}

func TestSettingsRowsForNonGTNHInstanceSkipServerRows(t *testing.T) { // C1
	f := settingsFixture(t, plainCfg, false)
	f.open("")
	want := []string{"memory", "jvm", "java", "window", "after", "prism"}
	if got := itemKeys(f.m); !slices.Equal(got, want) {
		t.Errorf("keys for a non-GTNH instance = %q, want %q", got, want)
	}
}

func TestSettingsUnreadableInstanceCfgSaysSoOnRowsThreeToSix(t *testing.T) { // C1
	f := settingsFixture(t, "", true)
	f.open("")
	d := itemDescs(f.m)
	got := []string{d["memory"], d["jvm"], d["java"], d["window"]}
	const cant = "couldn't read instance.cfg"
	if want := []string{cant, cant, cant, cant}; !slices.Equal(got, want) {
		t.Errorf("descs without instance.cfg = %q, want %q", got, want)
	}
	if d["server"] != "none — set one and j on the home screen joins it" || d["after"] != "stay open and show whether it's running" {
		t.Errorf("other rows without instance.cfg = %q; want their normal values", d)
	}
}

func TestSettingsEnterOnUnreadableRowStaysPut(t *testing.T) { // C1
	f := settingsFixture(t, "", true)
	f.open("window")
	press(f.m, keyEnter)
	if f.m.screen != scSettings || selectedKey(f.m) != "window" {
		t.Errorf("enter on window without instance.cfg: screen %d, selected %q; want scSettings (%d), window", f.m.screen, selectedKey(f.m), scSettings)
	}
}

func TestSettingsTitleNamesInstance(t *testing.T) { // C1
	f := settingsFixture(t, plainCfg, true)
	f.open("")
	if f.m.list.Title != "Settings for GTNH Pack" {
		t.Errorf("title = %q, want %q", f.m.list.Title, "Settings for GTNH Pack")
	}
}

func TestSettingsHelpBindings(t *testing.T) { // C1
	f := settingsFixture(t, plainCfg, true)
	f.open("")
	want := []string{"enter change", "esc back", "q quit"}
	if got := helpPairs(f.m); !slices.Equal(got, want) {
		t.Errorf("help = %q, want %q", got, want)
	}
}

func TestSettingsSelectsRememberedRow(t *testing.T) { // C1
	f := settingsFixture(t, plainCfg, true)
	f.open("java")
	if selectedKey(f.m) != "java" {
		t.Errorf("selected %q, want java", selectedKey(f.m))
	}
}

func TestSettingsCountsAsListScreen(t *testing.T) { // C1
	f := settingsFixture(t, plainCfg, true)
	f.open("")
	if !f.m.isListScreen() {
		t.Errorf("scSettings is not a list screen")
	}
}

// ---- C1 keys ----

func TestSettingsQQuits(t *testing.T) { // C1
	f := settingsFixture(t, plainCfg, true)
	f.open("")
	cmd := press(f.m, runes("q"))
	if !f.m.quitting || !isQuit(cmd) {
		t.Errorf("q on settings: quitting %v, quit cmd %v; want true, true", f.m.quitting, isQuit(cmd))
	}
}

func TestSettingsDownMovesSelection(t *testing.T) { // C1: other keys go to the list
	f := settingsFixture(t, plainCfg, true)
	f.open("")
	press(f.m, keyDown)
	if selectedKey(f.m) != "mods" {
		t.Errorf("down from server: selected %q, want mods", selectedKey(f.m))
	}
}

func TestSettingsKeysGoToFilterWhileTyping(t *testing.T) { // C1
	f := settingsFixture(t, plainCfg, true)
	f.open("")
	press(f.m, runes("/"), runes("q"))
	if f.m.quitting || f.m.screen != scSettings || f.m.list.FilterValue() != "q" {
		t.Errorf("typing q into the filter: quitting %v, screen %d, filter %q; want false, scSettings, \"q\"",
			f.m.quitting, f.m.screen, f.m.list.FilterValue())
	}
}

func TestSettingsEscWithAppliedFilterClearsIt(t *testing.T) { // C1
	f := settingsFixture(t, plainCfg, true)
	f.open("")
	press(f.m, runes("/"), runes("m"), runes("e"), runes("m"), keyEnter)
	if f.m.list.FilterState() != list.FilterApplied {
		t.Fatalf("setup: filter state %v, want applied", f.m.list.FilterState())
	}
	press(f.m, keyEsc)
	if f.m.screen != scSettings || f.m.list.FilterState() != list.Unfiltered {
		t.Errorf("esc with a filter: screen %d, filter state %v; want scSettings (%d), unfiltered", f.m.screen, f.m.list.FilterState(), scSettings)
	}
}

func TestSettingsEscReloadsHomeWithNewServer(t *testing.T) { // C1, C6
	f := settingsFixture(t, plainCfg, true)
	writeState(t, f.dir, update.State{Version: "2.8.4"})
	f.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	f.edit(t, "server")
	f.submit("mc.x:1") // short enough that the card line doesn't wrap
	cmd := press(f.m, keyEsc)
	if cmd == nil {
		t.Fatalf("esc on settings: no command, want the reload")
	}
	press(f.m, cmd())
	if v := view(f.m); f.m.screen != scHome || !strings.Contains(v, "Server mc.x:1 — j joins it") {
		t.Errorf("after esc and reload: screen %d, view\n%s\nwant scHome (%d) with the new server on the card", f.m.screen, ansi.Strip(f.m.View()), scHome)
	}
}

// ---- C2 home ----

func TestHomeSOpensSettingsForSelectedInstance(t *testing.T) { // C2
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	press(h.m, keyDown, runes("s"))
	if h.m.screen != scSettings || h.m.inst.Dir != h.newest.Dir || h.m.list.Title != "Settings for Newest" {
		t.Errorf("s on Newest: screen %d, inst %q, title %q; want scSettings (%d), Newest, \"Settings for Newest\"",
			h.m.screen, h.m.inst.Name, h.m.list.Title, scSettings)
	}
}

// ---- C3 after play ----

func TestSettingsAfterPlayTogglesAndSaves(t *testing.T) { // C3
	f := settingsFixture(t, plainCfg, true)
	f.m.app = appcfg.Config{PrismExe: "/opt/p"}
	f.open("after")
	press(f.m, keyEnter)
	firstDesc, firstScreen, firstSel := itemDescs(f.m)["after"], f.m.screen, selectedKey(f.m)
	press(f.m, keyEnter)
	want := []appcfg.Config{{PrismExe: "/opt/p", AfterPlay: appcfg.AfterPlayQuit}, {PrismExe: "/opt/p", AfterPlay: appcfg.AfterPlayStay}}
	if !slices.Equal(f.app.saved, want) {
		t.Errorf("saved configs = %+v, want %+v", f.app.saved, want)
	}
	if firstDesc != "quit" || firstScreen != scSettings || firstSel != "after" {
		t.Errorf("after the first toggle: desc %q, screen %d, selected %q; want quit, scSettings, after", firstDesc, firstScreen, firstSel)
	}
	if d := itemDescs(f.m)["after"]; d != "stay open and show whether it's running" || f.m.screen != scSettings || selectedKey(f.m) != "after" {
		t.Errorf("after the second toggle: desc %q, screen %d, selected %q; want stay open…, scSettings, after", d, f.m.screen, selectedKey(f.m))
	}
}

func TestSettingsAfterPlaySaveErrorRevertsAndShowsError(t *testing.T) { // C3
	f := settingsFixture(t, plainCfg, true)
	f.m.app = appcfg.Config{PrismExe: "/opt/p"}
	diskFull := errors.New("disk full")
	f.app.err = diskFull
	f.open("after")
	press(f.m, keyEnter)
	if f.m.screen != scError || f.m.errPhase != scSettings || f.m.err == nil ||
		f.m.err.Error() != "I couldn't save that setting: disk full" || !errors.Is(f.m.err, diskFull) {
		t.Errorf("save error: screen %d, phase %d, err %v; want scError, scSettings, \"I couldn't save that setting: disk full\"", f.m.screen, f.m.errPhase, f.m.err)
	}
	if f.m.app != (appcfg.Config{PrismExe: "/opt/p"}) {
		t.Errorf("m.app after a failed save = %+v, want it reverted", f.m.app)
	}
	press(f.m, keyEsc)
	if f.m.screen != scSettings {
		t.Errorf("esc on the error: screen %d, want scSettings (%d)", f.m.screen, scSettings)
	}
}

// ---- C4 edit screen ----

func TestSettingsEditScreensShowTitleIntroValueAndFootnote(t *testing.T) { // C4
	const instFoot = "Leave it empty to go back to Prism's default. If Prism is open right now, restart it so it notices."
	cfg := "[General]\nname=GTNH Pack\nOverrideMemory=true\nMinMemAlloc=2048\nMaxMemAlloc=6144\n" +
		"OverrideJavaArgs=true\nJvmArgs=-Xss4m\nOverrideJavaLocation=true\nJavaPath=/usr/bin/java\n" +
		"OverrideWindow=true\nMinecraftWinWidth=1920\nMinecraftWinHeight=1080\n"
	cases := []struct{ key, title, intro, value, foot string }{
		{"server", "Server to join", "Which server do you usually play on? host or host:port, like play.example.com:25565.", "mc.x:1", "Leave it empty for none."},
		{"mods", "Server extra mods link", "Some servers add a few mods on top of GTNH. If the server owner gave you a link for them, paste it here — I'll install them now and keep them in sync every time you update.", "https://m.example/a.zip", "Leave it empty for none."},
		{"memory", "Memory for the game", "How much memory may the game use, in MB? GTNH runs well with 6144 to 8192.", "6144", instFoot},
		{"jvm", "Java arguments", "Extra arguments for Java. Only change this if someone told you what to put here.", "-Xss4m", instFoot},
		{"java", "Java to run it with", "Full path of the java executable Prism should use for this instance.", "/usr/bin/java", instFoot},
		{"window", "Window size", "Width and height of the game window, like 1920x1080.", "1920x1080", instFoot},
		{"prism", "Prism Launcher location", "Full path of the Prism Launcher executable (or the flatpak command).", "/opt/p", "Leave it empty to let me find Prism myself."},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			f := settingsFixture(t, cfg, true)
			writeState(t, f.dir, update.State{ServerAddress: "mc.x:1", CustomModsURL: "https://m.example/a.zip", CustomModsAsked: true})
			f.m.app = appcfg.Config{PrismExe: "/opt/p"}
			f.edit(t, c.key)
			_, body, footer, _ := f.m.page()
			want := words(c.title + " " + c.intro)
			if b := words(body); !strings.HasPrefix(b, want) || !strings.Contains(b, c.value) || !strings.HasSuffix(b, words(c.foot)) {
				t.Errorf("body = %q, want title+intro %q, value %q and footnote %q", b, want, c.value, c.foot)
			}
			if f.m.setIn.Value() != c.value {
				t.Errorf("prefilled %q, want %q", f.m.setIn.Value(), c.value)
			}
			if got := words(footer); got != "enter save esc back" {
				t.Errorf("footer = %q, want %q", got, "enter save esc back")
			}
		})
	}
}

func TestSettingsEditPrefillIsEmptyWithoutOverride(t *testing.T) { // C4
	cfg := "[General]\nname=GTNH Pack\nOverrideWindow=false\nMinecraftWinWidth=1920\nMinecraftWinHeight=1080\n"
	f := settingsFixture(t, cfg, true)
	f.edit(t, "window")
	if f.m.setIn.Value() != "" {
		t.Errorf("prefilled %q, want empty (window override is off)", f.m.setIn.Value())
	}
}

func TestSettingsEditClearsOldError(t *testing.T) { // C4
	f := settingsFixture(t, plainCfg, true)
	f.m.setEr = "stale"
	f.edit(t, "jvm")
	if f.m.setEr != "" {
		t.Errorf("setEr on a fresh edit = %q, want empty", f.m.setEr)
	}
}

// ---- C5/C6 server ----

func TestSettingsServerAddressSaved(t *testing.T) { // C5, C6
	f := settingsFixture(t, plainCfg, true)
	f.edit(t, "server")
	f.submit("h:0")
	f.submit("  play.example:25565  ")
	st := f.state(t)
	if st == nil || st.ServerAddress != "play.example:25565" {
		t.Fatalf("state after saving = %+v, want ServerAddress play.example:25565", st)
	}
	if f.m.screen != scSettings || f.m.setEr != "" || selectedKey(f.m) != "server" || itemDescs(f.m)["server"] != "play.example:25565" {
		t.Errorf("after save: screen %d, setEr %q, selected %q, desc %q; want scSettings, \"\", server, play.example:25565",
			f.m.screen, f.m.setEr, selectedKey(f.m), itemDescs(f.m)["server"])
	}
}

func TestSettingsServerAddressAcceptsValidForms(t *testing.T) { // C5
	for _, v := range []string{"play.example.com", "h:1", "h:65535", "10.0.0.1:25565"} {
		t.Run(v, func(t *testing.T) {
			f := settingsFixture(t, plainCfg, true)
			f.edit(t, "server")
			f.submit(v)
			if st := f.state(t); f.m.screen != scSettings || st == nil || st.ServerAddress != v {
				t.Errorf("server %q: screen %d, state %+v; want scSettings and saved", v, f.m.screen, st)
			}
		})
	}
}

func TestSettingsServerAddressRejectsBadForms(t *testing.T) { // C5, K: port 65536
	for _, v := range []string{"http://x", "a b", "h:99999", "h:0", "h:65536", "a:b:c", ":25565", "h:abc", "h:"} {
		t.Run(v, func(t *testing.T) {
			f := settingsFixture(t, plainCfg, true)
			writeState(t, f.dir, update.State{Version: "2.8.4", ServerAddress: "old:1"})
			f.edit(t, "server")
			f.submit(v)
			if st := f.state(t); f.m.screen != scSettingEdit || f.m.setEr != badServer || st.ServerAddress != "old:1" {
				t.Errorf("server %q: screen %d, setEr %q, saved %q; want scSettingEdit, the message, old:1 kept", v, f.m.screen, f.m.setEr, st.ServerAddress)
			}
		})
	}
}

func TestSettingsServerAddressEmptyClearsIt(t *testing.T) { // C5, C6
	f := settingsFixture(t, plainCfg, true)
	writeState(t, f.dir, update.State{Version: "2.8.4", ServerAddress: "old:1"})
	f.edit(t, "server")
	f.submit("")
	if st := f.state(t); st.ServerAddress != "" || st.Version != "2.8.4" || itemDescs(f.m)["server"] != "none — set one and j on the home screen joins it" {
		t.Errorf("empty server: state %+v, desc %q; want no server, version kept, the none desc", st, itemDescs(f.m)["server"])
	}
}

// ---- C5/C6 mods ----

func TestSettingsModsLinkSavedAndMarkedAsked(t *testing.T) { // C5, C6
	f := settingsFixture(t, plainCfg, true)
	f.edit(t, "mods")
	f.submit("https://mods.example.com/a/custom.zip")
	st := f.state(t)
	if st == nil || st.CustomModsURL != "https://mods.example.com/a/custom.zip" || !st.CustomModsAsked {
		t.Fatalf("state = %+v, want the link and CustomModsAsked", st)
	}
	if f.m.screen != scSettings || itemDescs(f.m)["mods"] != "mods.example.com" {
		t.Errorf("after save: screen %d, desc %q; want scSettings, mods.example.com", f.m.screen, itemDescs(f.m)["mods"])
	}
}

func TestSettingsModsEmptyMeansNoneButAsked(t *testing.T) { // C5, C6
	f := settingsFixture(t, plainCfg, true)
	writeState(t, f.dir, update.State{CustomModsURL: "https://old.example/x.zip"})
	f.edit(t, "mods")
	f.submit("")
	if st := f.state(t); st.CustomModsURL != "" || !st.CustomModsAsked || itemDescs(f.m)["mods"] != "none" {
		t.Errorf("empty mods link: state %+v, desc %q; want no link, asked, none", st, itemDescs(f.m)["mods"])
	}
}

func TestSettingsModsLinkRejectsHTTP(t *testing.T) { // C5
	f := settingsFixture(t, plainCfg, true)
	f.edit(t, "mods")
	f.submit("http://x")
	if st := f.state(t); f.m.screen != scSettingEdit || f.m.setEr != badMods || st != nil {
		t.Errorf("http link: screen %d, setEr %q, state %+v; want scSettingEdit, %q, nothing saved", f.m.screen, f.m.setEr, st, badMods)
	}
}

// ---- C5/C6 memory ----

func TestSettingsMemorySavedKeepsOtherLines(t *testing.T) { // C5, C6
	f := settingsFixture(t, plainCfg, true)
	f.edit(t, "memory")
	f.submit("6144")
	s := f.settings(t)
	if !s.OverrideMemory || s.MaxMemMB != 6144 || s.MinMemMB != 1024 {
		t.Errorf("settings = %+v, want OverrideMemory, MaxMemAlloc 6144, MinMemAlloc 1024", s)
	}
	want := []string{"[General]\n", "name=GTNH Pack\n", "MyCustomKey=hello\n", ""}
	if got := otherLines(f.cfgText(t)); !slices.Equal(got, want) {
		t.Errorf("unrelated instance.cfg lines = %q, want %q", got, want)
	}
	if f.m.screen != scSettings || selectedKey(f.m) != "memory" || itemDescs(f.m)["memory"] != "6144 MB (at least 1024 MB)" {
		t.Errorf("after save: screen %d, selected %q, desc %q; want scSettings, memory, \"6144 MB (at least 1024 MB)\"",
			f.m.screen, selectedKey(f.m), itemDescs(f.m)["memory"])
	}
}

func TestSettingsMemoryBelowSavedMinimumLowersIt(t *testing.T) { // C6, K: min -> max
	f := settingsFixture(t, "[General]\nname=GTNH Pack\nMinMemAlloc=2048\n", true)
	f.edit(t, "memory")
	f.submit("1536")
	if s := f.settings(t); !s.OverrideMemory || s.MaxMemMB != 1536 || s.MinMemMB != 1536 {
		t.Errorf("settings = %+v, want MaxMemAlloc 1536, MinMemAlloc 1536", s)
	}
}

func TestSettingsMemoryAboveSavedMinimumKeepsIt(t *testing.T) { // C6
	f := settingsFixture(t, "[General]\nname=GTNH Pack\nMinMemAlloc=2048\n", true)
	f.edit(t, "memory")
	f.submit("6144")
	if s := f.settings(t); !s.OverrideMemory || s.MaxMemMB != 6144 || s.MinMemMB != 2048 {
		t.Errorf("settings = %+v, want MaxMemAlloc 6144, MinMemAlloc 2048", s)
	}
}

func TestSettingsMemoryEmptyTurnsOverrideOff(t *testing.T) { // C6
	f := settingsFixture(t, "[General]\nname=GTNH Pack\nOverrideMemory=true\nMinMemAlloc=2048\nMaxMemAlloc=6144\n", true)
	f.edit(t, "memory")
	f.submit("")
	if s := f.settings(t); s.OverrideMemory || s.MinMemMB != 2048 || s.MaxMemMB != 6144 {
		t.Errorf("settings = %+v, want OverrideMemory false, Min/Max untouched (2048/6144)", s)
	}
	if d := itemDescs(f.m)["memory"]; d != "Prism's default" {
		t.Errorf("memory desc = %q, want Prism's default", d)
	}
}

func TestSettingsMemoryAcceptsBounds(t *testing.T) { // C5
	for _, c := range []struct {
		v string
		n int
	}{{"1024", 1024}, {"65536", 65536}} {
		t.Run(c.v, func(t *testing.T) {
			f := settingsFixture(t, plainCfg, true)
			f.edit(t, "memory")
			f.submit(c.v)
			if s := f.settings(t); f.m.screen != scSettings || s.MaxMemMB != c.n || !s.OverrideMemory {
				t.Errorf("memory %q: screen %d, settings %+v; want saved as %d", c.v, f.m.screen, s, c.n)
			}
		})
	}
}

func TestSettingsMemoryRejectsOutOfRangeAndNonNumbers(t *testing.T) { // C5
	for _, v := range []string{"12", "1023", "65537", "abc", "6144.5", "6 GB"} {
		t.Run(v, func(t *testing.T) {
			f := settingsFixture(t, plainCfg, true)
			f.edit(t, "memory")
			f.submit(v)
			if got := f.cfgText(t); f.m.screen != scSettingEdit || f.m.setEr != badMemory || got != plainCfg {
				t.Errorf("memory %q: screen %d, setEr %q, cfg %q; want scSettingEdit, %q, untouched", v, f.m.screen, f.m.setEr, got, badMemory)
			}
		})
	}
}

func TestSettingsMemoryWriteErrorShowsError(t *testing.T) { // C6
	f := settingsFixture(t, plainCfg, true)
	f.edit(t, "memory")
	if err := os.Remove(filepath.Join(f.dir, "instance.cfg")); err != nil {
		t.Fatal(err)
	}
	f.submit("6144")
	if f.m.screen != scError || f.m.errPhase != scSettingEdit || f.m.err == nil ||
		!strings.HasPrefix(f.m.err.Error(), "I couldn't save that setting: ") || !errors.Is(f.m.err, os.ErrNotExist) {
		t.Errorf("save without instance.cfg: screen %d, phase %d, err %v; want scError, scSettingEdit, wrapped not-exist", f.m.screen, f.m.errPhase, f.m.err)
	}
}

// ---- C5/C6 jvm ----

func TestSettingsJvmArgsSaved(t *testing.T) { // C6
	f := settingsFixture(t, plainCfg, true)
	f.edit(t, "jvm")
	f.submit("-Xss4m -XX:+UseZGC")
	if s := f.settings(t); !s.OverrideJavaArgs || s.JvmArgs != "-Xss4m -XX:+UseZGC" || itemDescs(f.m)["jvm"] != "-Xss4m -XX:+UseZGC" {
		t.Errorf("settings %+v, desc %q; want OverrideJavaArgs with the args", s, itemDescs(f.m)["jvm"])
	}
}

func TestSettingsJvmArgsEmptyTurnsOverrideOff(t *testing.T) { // C6
	f := settingsFixture(t, "[General]\nname=GTNH Pack\nOverrideJavaArgs=true\nJvmArgs=-Xss4m\n", true)
	f.edit(t, "jvm")
	f.submit("")
	if s := f.settings(t); s.OverrideJavaArgs || s.JvmArgs != "-Xss4m" {
		t.Errorf("settings %+v, want OverrideJavaArgs false, JvmArgs untouched", s)
	}
}

// ---- C5/C6 java ----

func TestSettingsJavaPathSaved(t *testing.T) { // C5, C6
	f := settingsFixture(t, plainCfg, true)
	java := filepath.Join(t.TempDir(), "java")
	if err := os.WriteFile(java, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.edit(t, "java")
	f.submit(java)
	if s := f.settings(t); !s.OverrideJavaLocation || s.JavaPath != java || itemDescs(f.m)["java"] != java {
		t.Errorf("settings %+v, desc %q; want OverrideJavaLocation with %q", s, itemDescs(f.m)["java"], java)
	}
}

func TestSettingsJavaPathEmptyTurnsOverrideOff(t *testing.T) { // C6
	f := settingsFixture(t, "[General]\nname=GTNH Pack\nOverrideJavaLocation=true\nJavaPath=/usr/bin/java\n", true)
	f.edit(t, "java")
	f.submit("")
	if s := f.settings(t); s.OverrideJavaLocation || s.JavaPath != "/usr/bin/java" {
		t.Errorf("settings %+v, want OverrideJavaLocation false, JavaPath untouched", s)
	}
}

func TestSettingsJavaPathRejectsDirectoryAndMissingFile(t *testing.T) { // C5
	for name, v := range map[string]string{"directory": t.TempDir(), "missing": filepath.Join(t.TempDir(), "nope")} {
		t.Run(name, func(t *testing.T) {
			f := settingsFixture(t, plainCfg, true)
			f.edit(t, "java")
			f.submit(v)
			if got := f.cfgText(t); f.m.screen != scSettingEdit || f.m.setEr != badFile || got != plainCfg {
				t.Errorf("java %q: screen %d, setEr %q, cfg %q; want scSettingEdit, %q, untouched", v, f.m.screen, f.m.setEr, got, badFile)
			}
		})
	}
}

// ---- C5/C6 window ----

func TestSettingsWindowSaved(t *testing.T) { // C5, C6
	for _, c := range []struct {
		v    string
		w, h int
	}{{"1920x1080", 1920, 1080}, {"1280×720", 1280, 720}, {"320x16384", 320, 16384}, {"16384x320", 16384, 320}} {
		t.Run(c.v, func(t *testing.T) {
			f := settingsFixture(t, plainCfg, true)
			f.edit(t, "window")
			f.submit(c.v)
			if s := f.settings(t); f.m.screen != scSettings || !s.OverrideWindow || s.WinWidth != c.w || s.WinHeight != c.h {
				t.Errorf("window %q: screen %d, settings %+v; want OverrideWindow %dx%d", c.v, f.m.screen, s, c.w, c.h)
			}
		})
	}
}

func TestSettingsWindowDescUsesTimesSign(t *testing.T) { // C1, C6
	f := settingsFixture(t, plainCfg, true)
	f.edit(t, "window")
	f.submit("1920x1080")
	if d := itemDescs(f.m)["window"]; d != "1920×1080" {
		t.Errorf("window desc = %q, want %q", d, "1920×1080")
	}
}

func TestSettingsWindowEmptyTurnsOverrideOff(t *testing.T) { // C6
	f := settingsFixture(t, "[General]\nname=GTNH Pack\nOverrideWindow=true\nMinecraftWinWidth=1920\nMinecraftWinHeight=1080\n", true)
	f.edit(t, "window")
	f.submit("")
	if s := f.settings(t); s.OverrideWindow || s.WinWidth != 1920 || s.WinHeight != 1080 {
		t.Errorf("settings %+v, want OverrideWindow false, size untouched", s)
	}
}

func TestSettingsWindowRejectsBadSizes(t *testing.T) { // C5
	for _, v := range []string{"abc", "1920", "319x600", "600x319", "16385x600", "600x16385", "1920*1080", "axb"} {
		t.Run(v, func(t *testing.T) {
			f := settingsFixture(t, plainCfg, true)
			f.edit(t, "window")
			f.submit(v)
			if got := f.cfgText(t); f.m.screen != scSettingEdit || f.m.setEr != badWindow || got != plainCfg {
				t.Errorf("window %q: screen %d, setEr %q, cfg %q; want scSettingEdit, %q, untouched", v, f.m.screen, f.m.setEr, got, badWindow)
			}
		})
	}
}

// ---- C5/C6 prism ----

func TestSettingsPrismLocationSaved(t *testing.T) { // C5, C6
	f := settingsFixture(t, plainCfg, true)
	exe := filepath.Join(t.TempDir(), "prismlauncher")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.m.app = appcfg.Config{AfterPlay: appcfg.AfterPlayQuit}
	f.edit(t, "prism")
	f.submit(exe)
	want := []appcfg.Config{{PrismExe: exe, AfterPlay: appcfg.AfterPlayQuit}}
	if !slices.Equal(f.app.saved, want) || f.m.screen != scSettings || itemDescs(f.m)["prism"] != exe {
		t.Errorf("saved %+v, screen %d, desc %q; want %+v, scSettings, the path", f.app.saved, f.m.screen, itemDescs(f.m)["prism"], want)
	}
}

func TestSettingsPrismLocationEmptyMeansAutomatic(t *testing.T) { // C6
	f := settingsFixture(t, plainCfg, true)
	f.m.app = appcfg.Config{PrismExe: "/opt/p"}
	f.edit(t, "prism")
	f.submit("")
	want := []appcfg.Config{{}}
	if !slices.Equal(f.app.saved, want) || itemDescs(f.m)["prism"] != "found automatically" {
		t.Errorf("saved %+v, desc %q; want %+v, found automatically", f.app.saved, itemDescs(f.m)["prism"], want)
	}
}

func TestSettingsPrismLocationRejectsMissingFile(t *testing.T) { // C5
	f := settingsFixture(t, plainCfg, true)
	f.edit(t, "prism")
	f.submit(filepath.Join(t.TempDir(), "nope"))
	if f.m.screen != scSettingEdit || f.m.setEr != badFile || len(f.app.saved) != 0 {
		t.Errorf("missing prism: screen %d, setEr %q, saved %+v; want scSettingEdit, %q, nothing", f.m.screen, f.m.setEr, f.app.saved, badFile)
	}
}

func TestSettingsPrismLocationSaveErrorRevertsAndShowsError(t *testing.T) { // C6
	f := settingsFixture(t, plainCfg, true)
	exe := filepath.Join(t.TempDir(), "prismlauncher")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.m.app = appcfg.Config{PrismExe: "/opt/p"}
	f.app.err = errors.New("disk full")
	f.edit(t, "prism")
	f.submit(exe)
	if f.m.screen != scError || f.m.errPhase != scSettingEdit || f.m.err == nil || f.m.err.Error() != "I couldn't save that setting: disk full" {
		t.Errorf("save error: screen %d, phase %d, err %v; want scError, scSettingEdit, the message", f.m.screen, f.m.errPhase, f.m.err)
	}
	if f.m.app.PrismExe != "/opt/p" {
		t.Errorf("m.app.PrismExe after a failed save = %q, want /opt/p", f.m.app.PrismExe)
	}
}

// ---- C7 esc on the edit screen ----

func TestSettingsEditEscSavesNothingAndClearsError(t *testing.T) { // C7
	f := settingsFixture(t, plainCfg, true)
	f.edit(t, "memory")
	f.submit("12")
	f.m.setIn.SetValue("6144")
	press(f.m, keyEsc)
	if got := f.cfgText(t); f.m.screen != scSettings || f.m.setEr != "" || got != plainCfg || selectedKey(f.m) != "memory" {
		t.Errorf("esc on edit: screen %d, setEr %q, cfg %q, selected %q; want scSettings, \"\", untouched, memory",
			f.m.screen, f.m.setEr, got, selectedKey(f.m))
	}
}

func TestSettingsEditTypingGoesToInput(t *testing.T) { // C7
	f := settingsFixture(t, plainCfg, true)
	f.edit(t, "jvm")
	press(f.m, runes("-"), runes("q"))
	if f.m.setIn.Value() != "-q" || f.m.quitting || f.m.screen != scSettingEdit {
		t.Errorf("typing -q: value %q, quitting %v, screen %d; want \"-q\", false, scSettingEdit", f.m.setIn.Value(), f.m.quitting, f.m.screen)
	}
}

// ---- C8 error routing ----

func TestErrorEscReturnsToSettingsForBothSettingsPhases(t *testing.T) { // C8
	for name, phase := range map[string]screen{"settings": scSettings, "edit": scSettingEdit} {
		t.Run(name, func(t *testing.T) {
			f := settingsFixture(t, plainCfg, true)
			f.m.err, f.m.errPhase, f.m.screen = errors.New("boom"), phase, scError
			press(f.m, keyEsc)
			if f.m.screen != scSettings || f.m.list.Title != "Settings for GTNH Pack" {
				t.Errorf("esc on an error from phase %d: screen %d, title %q; want scSettings (%d)", phase, f.m.screen, f.m.list.Title, scSettings)
			}
		})
	}
}
