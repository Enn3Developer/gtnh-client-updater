package tui

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// ---- helpers ----

var (
	keyEnter = tea.KeyMsg{Type: tea.KeyEnter}
	keyEsc   = tea.KeyMsg{Type: tea.KeyEsc}
	keyDown  = tea.KeyMsg{Type: tea.KeyDown}
	keyUp    = tea.KeyMsg{Type: tea.KeyUp}
	keyPgDn  = tea.KeyMsg{Type: tea.KeyPgDown}
	keyPgUp  = tea.KeyMsg{Type: tea.KeyPgUp}
	keyHome  = tea.KeyMsg{Type: tea.KeyHome}
	keyEnd   = tea.KeyMsg{Type: tea.KeyEnd}
	keySpace = tea.KeyMsg{Type: tea.KeySpace}
	keyCtrlC = tea.KeyMsg{Type: tea.KeyCtrlC}
)

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func press(m *model, keys ...tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.Update(k)
	}
	return cmd
}

// 2.8.4 is the newest stable (Java 17), 2.8.1 an older stable that only has a Java 8 pack,
// 2.9.0-RC-1 the newest release overall.
func routeManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	man, err := manifest.Parse([]byte(`{
	  "2.8.4": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/a.zip"}},
	  "2.8.1": {"title":"Stable release","releaseDate":"2025/10/01","mmc":{"java8Url":"https://downloads.gtnewhorizons.com/c.zip"}},
	  "2.9.0-RC-1": {"title":"Beta release","releaseDate":"2026/09/24","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/b.zip"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	return man
}

// routeModel is a model on an instance that's known to be on 2.8.4, server mods decided.
func routeModel(t *testing.T) *model {
	m := sizedModel(t)
	m.manifest = routeManifest(t)
	m.inst = prism.Instance{Dir: t.TempDir(), Name: "Pack", GTNH: true}
	m.insts = []prism.Instance{m.inst}
	m.detect = update.Detection{Version: "2.8.4"}
	m.serverModsAsked = true
	return m
}

var conflictPaths = []string{".minecraft/config/a.cfg", ".minecraft/config/b.cfg", ".minecraft/config/c.cfg"}

// prepared puts m on the conflicts screen with three conflicting configs.
func prepared(t *testing.T, m *model) *update.Plan {
	t.Helper()
	pl := &update.Plan{}
	for _, p := range conflictPaths {
		pl.Actions = append(pl.Actions, update.Action{Kind: update.Conflict, Path: p})
	}
	m.Update(preparedMsg{&update.Session{Plan: pl}})
	if m.screen != scConflicts {
		t.Fatalf("setup: screen %d after preparedMsg, want scConflicts (%d)", m.screen, scConflicts)
	}
	return pl
}

func choices(pl *update.Plan) []update.Choice {
	var out []update.Choice
	for _, p := range conflictPaths {
		out = append(out, pl.ChoiceOf(p))
	}
	return out
}

func allOf(c update.Choice) []update.Choice { return []update.Choice{c, c, c} }

// onResolve moves from the conflicts screen to the file-by-file screen ("pick" is the third item).
func onResolve(t *testing.T, m *model) *update.Plan {
	t.Helper()
	pl := prepared(t, m)
	press(m, keyDown, keyDown, keyEnter)
	if m.screen != scResolve {
		t.Fatalf("setup: screen %d after picking file by file, want scResolve (%d)", m.screen, scResolve)
	}
	return pl
}

func words(s string) string { return strings.Join(strings.Fields(ansi.Strip(s)), " ") }

func pageBody(m *model) string {
	_, body, _, _ := m.page()
	return words(body)
}

// ---- C5 conflicts screen ----

func TestConflictsEnterOnNewTakesAllNewVersions(t *testing.T) { // C5
	m := routeModel(t)
	pl := prepared(t, m)
	pl.ChooseAll(update.KeepMine)
	press(m, keyEnter)
	if m.screen != scConfirm || !slices.Equal(choices(pl), allOf(update.TakeNew)) {
		t.Errorf("enter on new: screen %d, choices %v; want scConfirm (%d), all TakeNew", m.screen, choices(pl), scConfirm)
	}
}

func TestConflictsEnterOnMineKeepsAll(t *testing.T) { // C5
	m := routeModel(t)
	pl := prepared(t, m)
	press(m, keyDown, keyEnter)
	if m.screen != scConfirm || !slices.Equal(choices(pl), allOf(update.KeepMine)) {
		t.Errorf("enter on mine: screen %d, choices %v; want scConfirm (%d), all KeepMine", m.screen, choices(pl), scConfirm)
	}
}

func TestConflictsEnterOnPickOpensFileByFile(t *testing.T) { // C5
	m := routeModel(t)
	prepared(t, m)
	press(m, keyDown, keyDown, keyEnter)
	if m.screen != scResolve {
		t.Errorf("enter on pick: screen %d, want scResolve (%d)", m.screen, scResolve)
	}
}

func TestConflictsEscDropsSessionAndReturnsToVersions(t *testing.T) { // C5
	m := routeModel(t)
	prepared(t, m)
	press(m, keyEsc)
	if m.screen != scTarget || m.session != nil {
		t.Errorf("esc on conflicts: screen %d, session nil %v; want scTarget (%d), nil", m.screen, m.session == nil, scTarget)
	}
}

func TestConflictsEscWithAppliedFilterOnlyClearsFilter(t *testing.T) { // C5
	m := routeModel(t)
	prepared(t, m)
	m.list.SetFilterText("keep")
	press(m, keyEsc)
	if m.screen != scConflicts || m.session == nil || m.list.FilterState() != list.Unfiltered {
		t.Errorf("esc with filter: screen %d, session nil %v, filter %v; want scConflicts (%d), session kept, unfiltered",
			m.screen, m.session == nil, m.list.FilterState(), scConflicts)
	}
}

func TestConflictsQQuits(t *testing.T) { // C5
	m := routeModel(t)
	prepared(t, m)
	cmd := press(m, runes("q"))
	if !m.quitting || cmd == nil {
		t.Errorf("q on conflicts: quitting %v, cmd nil %v; want quitting with a cmd", m.quitting, cmd == nil)
	}
}

func TestConflictsKeysGoToFilterWhileTyping(t *testing.T) { // C5, C6
	m := routeModel(t)
	prepared(t, m)
	press(m, runes("/"), runes("q"))
	if m.quitting || m.screen != scConflicts || m.list.FilterValue() != "q" {
		t.Errorf("typing q into the filter: quitting %v, screen %d, filter %q; want not quitting, scConflicts, \"q\"",
			m.quitting, m.screen, m.list.FilterValue())
	}
}

// ---- C6 file-by-file screen ----

func TestResolveSpaceSwitchesSelectedFile(t *testing.T) { // C6
	m := routeModel(t)
	pl := onResolve(t, m)
	press(m, keySpace)
	want := []update.Choice{update.KeepMine, update.TakeNew, update.TakeNew}
	if got := choices(pl); !slices.Equal(got, want) {
		t.Errorf("space on first file: choices %v, want %v", got, want)
	}
}

func TestResolveSpaceTwiceSwitchesBack(t *testing.T) { // C6
	m := routeModel(t)
	pl := onResolve(t, m)
	press(m, keySpace, keySpace)
	if got := choices(pl); !slices.Equal(got, allOf(update.TakeNew)) {
		t.Errorf("space twice: choices %v, want all TakeNew", got)
	}
}

func TestResolveListShowsEachFilesChoice(t *testing.T) { // C6
	m := routeModel(t)
	onResolve(t, m)
	press(m, keySpace)
	var got []string
	for _, it := range m.list.Items() {
		got = append(got, it.(item).desc)
	}
	want := []string{"keep mine", "new version", "new version"}
	if !slices.Equal(got, want) {
		t.Errorf("resolve item descriptions = %q, want %q", got, want)
	}
}

func TestResolveKKeepsAll(t *testing.T) { // C6
	m := routeModel(t)
	pl := onResolve(t, m)
	press(m, runes("k"))
	if got := choices(pl); !slices.Equal(got, allOf(update.KeepMine)) || m.screen != scResolve {
		t.Errorf("k: choices %v, screen %d; want all KeepMine on scResolve", got, m.screen)
	}
}

func TestResolveNTakesAllNew(t *testing.T) { // C6
	m := routeModel(t)
	pl := onResolve(t, m)
	press(m, runes("k"), runes("n"))
	if got := choices(pl); !slices.Equal(got, allOf(update.TakeNew)) || m.screen != scResolve {
		t.Errorf("k then n: choices %v, screen %d; want all TakeNew on scResolve", got, m.screen)
	}
}

func TestResolveEnterConfirmsKeepingChoices(t *testing.T) { // C6
	m := routeModel(t)
	pl := onResolve(t, m)
	press(m, keySpace, keyEnter)
	want := []update.Choice{update.KeepMine, update.TakeNew, update.TakeNew}
	if got := choices(pl); m.screen != scConfirm || !slices.Equal(got, want) {
		t.Errorf("enter: screen %d, choices %v; want scConfirm (%d), %v", m.screen, got, scConfirm, want)
	}
}

func TestResolveEscGoesBackToConflicts(t *testing.T) { // C6
	m := routeModel(t)
	onResolve(t, m)
	press(m, keyEsc)
	if m.screen != scConflicts || m.session == nil {
		t.Errorf("esc on resolve: screen %d, session nil %v; want scConflicts (%d) with the session", m.screen, m.session == nil, scConflicts)
	}
}

func TestResolveEscWithAppliedFilterStays(t *testing.T) { // C6
	m := routeModel(t)
	onResolve(t, m)
	m.list.SetFilterText("a.cfg")
	press(m, keyEsc)
	if m.screen != scResolve || m.list.FilterState() != list.Unfiltered {
		t.Errorf("esc with filter on resolve: screen %d, filter %v; want scResolve (%d), unfiltered", m.screen, m.list.FilterState(), scResolve)
	}
}

func TestResolveKeysGoToFilterWhileTyping(t *testing.T) { // C6
	m := routeModel(t)
	pl := onResolve(t, m)
	press(m, runes("/"), runes("k"))
	if got := choices(pl); !slices.Equal(got, allOf(update.TakeNew)) || m.list.FilterValue() != "k" {
		t.Errorf("typing k into the filter: choices %v, filter %q; want all TakeNew, \"k\"", got, m.list.FilterValue())
	}
}

func TestResolveQQuits(t *testing.T) { // C6
	m := routeModel(t)
	onResolve(t, m)
	press(m, runes("q"))
	if !m.quitting {
		t.Error("q on resolve: not quitting, want quitting")
	}
}

// ---- C7 list screens ----

func TestIsListScreenExactlyForListScreens(t *testing.T) { // C7
	lists := []screen{scInstance, scInstalled, scTarget, scConflicts, scResolve}
	for sc := scLoading; sc <= scSelfUpdated; sc++ {
		m := &model{screen: sc}
		if got, want := m.isListScreen(), slices.Contains(lists, sc); got != want {
			t.Errorf("isListScreen() on screen %d = %v, want %v", sc, got, want)
		}
	}
}

func TestSelfUpdateKeyIsHelpedOnVersionListButNotOnConflicts(t *testing.T) { // C7
	m := routeModel(t)
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	m.showTargets()
	onTargets := slices.ContainsFunc(m.list.AdditionalShortHelpKeys(), func(b key.Binding) bool { return b.Help().Key == "u" })
	prepared(t, m)
	onConflicts := slices.ContainsFunc(m.list.AdditionalShortHelpKeys(), func(b key.Binding) bool { return b.Help().Key == "u" })
	if !onTargets || onConflicts {
		t.Errorf("u in help: version list %v, conflicts %v; want true, false", onTargets, onConflicts)
	}
}

// ---- list-screen keys ----

func TestVersionListEscReturnsToInstances(t *testing.T) {
	m := routeModel(t)
	m.showTargets()
	press(m, keyEsc)
	if m.screen != scInstance {
		t.Errorf("esc on versions: screen %d, want scInstance (%d)", m.screen, scInstance)
	}
}

func TestVersionListEscWithoutInstancesStays(t *testing.T) {
	m := routeModel(t)
	m.insts = nil
	m.showTargets()
	press(m, keyEsc)
	if m.screen != scTarget {
		t.Errorf("esc on versions with no instances: screen %d, want scTarget (%d)", m.screen, scTarget)
	}
}

func TestVersionListEscWithAppliedFilterOnlyClearsFilter(t *testing.T) {
	m := routeModel(t)
	m.showTargets()
	m.list.SetFilterText("2.8")
	press(m, keyEsc)
	if m.screen != scTarget || m.list.FilterState() != list.Unfiltered {
		t.Errorf("esc with filter on versions: screen %d, filter %v; want scTarget (%d), unfiltered", m.screen, m.list.FilterState(), scTarget)
	}
}

func TestVersionListIOpensInstalledVersionList(t *testing.T) {
	m := routeModel(t)
	m.showTargets()
	press(m, runes("i"))
	if m.screen != scInstalled {
		t.Errorf("i on versions: screen %d, want scInstalled (%d)", m.screen, scInstalled)
	}
}

func TestCreateVersionListIgnoresI(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	press(m, runes("i"))
	if m.screen != scTarget {
		t.Errorf("i on create versions: screen %d, want scTarget (%d)", m.screen, scTarget)
	}
}

func TestInstanceListIgnoresI(t *testing.T) {
	m := routeModel(t)
	m.showInstances()
	press(m, runes("i"))
	if m.screen != scInstance {
		t.Errorf("i on instances: screen %d, want scInstance (%d)", m.screen, scInstance)
	}
}

// ---- after load ----

func TestLoadWithNoInstancesStartsCreate(t *testing.T) { // C9
	m := sizedModel(t)
	m.Update(loadedMsg{routeManifest(t), nil})
	if m.screen != scTarget || !m.creating {
		t.Errorf("loaded with no instances: screen %d, creating %v; want scTarget (%d), true", m.screen, m.creating, scTarget)
	}
}

func TestLoadWithOnlyOtherInstancesListsThem(t *testing.T) {
	m := sizedModel(t)
	other := prism.Instance{Dir: t.TempDir(), Name: "Vanilla"}
	m.Update(loadedMsg{routeManifest(t), []prism.Instance{other}})
	if m.screen != scInstance || m.creating {
		t.Errorf("loaded with one non-GTNH instance: screen %d, creating %v; want scInstance (%d), false", m.screen, m.creating, scInstance)
	}
}

// ---- C8 scrolling ----

// confirmModel is on a confirm screen whose body is much taller than the terminal.
func confirmModel(t *testing.T) *model {
	m := routeModel(t)
	m.screen = scConfirm
	m.target = "2.9.0-RC-1"
	m.session = &update.Session{Plan: busyPlan()}
	for range 4 {
		m.warns = append(m.warns, threeWarns()...)
	}
	return m
}

// bodyRows is how many body lines fit: the terminal minus the blank top line, header and footer.
func bodyRows(m *model) (lines, rows int) {
	header, body, footer, _ := m.page()
	n := func(s string) int { return len(strings.Split(s, "\n")) }
	return n(body), m.height - 1 - n(header) - n(footer)
}

func TestScrollDownMovesOneLine(t *testing.T) { // C8
	m := confirmModel(t)
	press(m, keyDown, keyDown)
	if m.scroll != 2 {
		t.Errorf("down twice: scroll %d, want 2", m.scroll)
	}
}

func TestScrollUpMovesBackOneLine(t *testing.T) { // C8
	m := confirmModel(t)
	press(m, keyDown, keyDown, keyDown, keyUp)
	if m.scroll != 2 {
		t.Errorf("down x3, up: scroll %d, want 2", m.scroll)
	}
}

func TestScrollUpStopsAtTop(t *testing.T) { // C8
	m := confirmModel(t)
	press(m, keyUp)
	if m.scroll != 0 {
		t.Errorf("up at top: scroll %d, want 0", m.scroll)
	}
}

func TestScrollPageKeysMoveHalfTheBody(t *testing.T) { // C8
	m := confirmModel(t)
	lines, rows := bodyRows(m)
	if lines < 2*rows {
		t.Fatalf("setup: body %d lines, want at least %d", lines, 2*rows)
	}
	press(m, keyPgDn, keyPgDn)
	afterDown := m.scroll
	press(m, keyPgUp)
	if afterDown != 2*(rows/2) || m.scroll != rows/2 {
		t.Errorf("pgdown x2 then pgup: scroll %d then %d, want %d then %d", afterDown, m.scroll, 2*(rows/2), rows/2)
	}
}

func TestScrollEndGoesToBottomAndHomeToTop(t *testing.T) { // C8
	m := confirmModel(t)
	lines, rows := bodyRows(m)
	press(m, keyEnd)
	atEnd := m.scroll
	press(m, keyHome)
	if atEnd != lines-rows || m.scroll != 0 {
		t.Errorf("end then home: scroll %d then %d, want %d then 0", atEnd, m.scroll, lines-rows)
	}
}

func TestScrollDownStopsAtBottom(t *testing.T) { // C8
	m := confirmModel(t)
	lines, rows := bodyRows(m)
	press(m, keyEnd, keyDown)
	if m.scroll != lines-rows {
		t.Errorf("down at bottom: scroll %d, want %d", m.scroll, lines-rows)
	}
}

func TestScrollHomeAndEndBelongToNameInput(t *testing.T) { // C8
	m := routeModel(t)
	m.startCreate()
	m.target = "2.8.4"
	m.askName()
	m.nameEr = strings.Repeat("That name can't be used for a folder on your computer. ", 25)
	press(m, keyDown, keyEnd, keyHome)
	if m.scroll != 1 || m.screen != scName {
		t.Errorf("down, end, home on name screen: scroll %d, screen %d; want 1, scName (%d)", m.scroll, m.screen, scName)
	}
}

func TestScrollResetsWhenScreenChanges(t *testing.T) { // C8
	m := confirmModel(t)
	press(m, keyDown, keyDown, keyEsc)
	if m.screen != scTarget || m.scroll != 0 {
		t.Errorf("esc from scrolled confirm: screen %d, scroll %d; want scTarget (%d), 0", m.screen, m.scroll, scTarget)
	}
}

// ---- C9 create flow ----

func TestInstanceListNStartsCreate(t *testing.T) { // C9
	m := routeModel(t)
	m.showInstances()
	press(m, runes("n"))
	if m.screen != scTarget || !m.creating {
		t.Errorf("n on instances: screen %d, creating %v; want scTarget (%d), true", m.screen, m.creating, scTarget)
	}
}

func TestCreateChoosingVersionAsksNamePrefilled(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	m.serverModsAsked = true
	press(m, keyEnter) // the recommended (newest stable) version is preselected
	if m.screen != scName || m.nameIn.Value() != "GT New Horizons 2.8.4" {
		t.Errorf("enter on create versions: screen %d, name %q; want scName (%d), %q", m.screen, m.nameIn.Value(), scName, "GT New Horizons 2.8.4")
	}
}

func TestCreateChoosingVersionAsksServerModsFirstWhenUnasked(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	m.serverModsAsked = false
	press(m, keyEnter)
	if m.screen != scServerMods {
		t.Errorf("enter on create versions, mods unasked: screen %d, want scServerMods (%d)", m.screen, scServerMods)
	}
}

func TestCreateNameUsesPresetNameOnlyOnce(t *testing.T) { // C9
	m := routeModel(t)
	m.cfg.Name = "Preset"
	m.startCreate()
	m.target = "2.8.4"
	m.askName()
	first := m.nameIn.Value()
	m.askName()
	if first != "Preset" || m.nameIn.Value() != "GT New Horizons 2.8.4" {
		t.Errorf("asking the name twice: %q then %q, want \"Preset\" then %q", first, m.nameIn.Value(), "GT New Horizons 2.8.4")
	}
}

func TestCreateBadNameStaysOnNameWithError(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	m.target = "2.8.4"
	m.askName()
	m.nameIn.SetValue("bad/name")
	press(m, keyEnter)
	if m.screen != scName || m.nameEr == "" {
		t.Errorf("enter with bad/name: screen %d, error %q; want scName (%d) with an error", m.screen, m.nameEr, scName)
	}
}

func TestCreateValidNameStartsPreparing(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	m.target = "2.8.4"
	m.askName()
	m.nameIn.SetValue("My Pack")
	cmd := press(m, keyEnter)
	if m.screen != scPreparing || m.newName != "My Pack" || cmd == nil {
		t.Errorf("enter with a good name: screen %d, name %q, cmd nil %v; want scPreparing (%d), \"My Pack\", a cmd",
			m.screen, m.newName, cmd == nil, scPreparing)
	}
}

func TestCreateNameEscGoesBackToVersions(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	m.target = "2.8.4"
	m.askName()
	m.nameEr = "old error"
	press(m, keyEsc)
	if m.screen != scTarget || m.nameEr != "" {
		t.Errorf("esc on name: screen %d, error %q; want scTarget (%d), cleared", m.screen, m.nameEr, scTarget)
	}
}

// halfMade is a Creation whose folder exists without instance.cfg, as after PrepareCreate.
func halfMade(t *testing.T) *update.Creation {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "My Pack")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &update.Creation{Dir: dir, Files: 7, Flavor: manifest.Java17}
}

func TestCreateConfirmEscClosesCreationAndGoesBack(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	m.target, m.newName = "2.8.4", "My Pack"
	c := halfMade(t)
	m.Update(createReady{c})
	press(m, keyEsc)
	_, err := os.Stat(c.Dir)
	if m.screen != scTarget || m.creation != nil || !os.IsNotExist(err) {
		t.Errorf("esc on create confirm: screen %d, creation nil %v, folder stat err %v; want scTarget (%d), nil, folder removed",
			m.screen, m.creation == nil, err, scTarget)
	}
}

func TestQuitDuringCreateConfirmRemovesHalfMadeFolder(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	m.target, m.newName = "2.8.4", "My Pack"
	c := halfMade(t)
	m.Update(createReady{c})
	press(m, keyCtrlC)
	_, err := os.Stat(c.Dir)
	if !m.quitting || !os.IsNotExist(err) {
		t.Errorf("ctrl+c on create confirm: quitting %v, folder stat err %v; want quitting, folder removed", m.quitting, err)
	}
}

// ---- create version list ----

func createTargetItems(t *testing.T, m *model) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, it := range m.list.Items() {
		out[it.(item).key] = it.(item).desc
	}
	return out
}

func TestCreateVersionListTagsRecommendedAndJava8Only(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	items := createTargetItems(t, m)
	checks := []struct {
		version, tag string
		want         bool
	}{
		{"2.8.4", "recommended", true},
		{"2.9.0-RC-1", "recommended", false},
		{"2.8.1", "Java 8 only", true},
		{"2.8.4", "Java 8 only", false},
	}
	for _, c := range checks {
		if got := strings.Contains(items[c.version], c.tag); got != c.want {
			t.Errorf("create list item %s = %q: has %q %v, want %v", c.version, items[c.version], c.tag, got, c.want)
		}
	}
}

func TestCreateVersionListPreselectsRecommended(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	if sel, _ := m.list.SelectedItem().(item); sel.key != "2.8.4" {
		t.Errorf("create list selection = %q, want 2.8.4", sel.key)
	}
}

func TestCreateVersionListKeepsChosenTarget(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	m.target = "2.8.1"
	m.showTargets()
	if sel, _ := m.list.SelectedItem().(item); sel.key != "2.8.1" {
		t.Errorf("create list selection with target 2.8.1 = %q, want 2.8.1", sel.key)
	}
}

func TestCreateVersionListSaysWhenPrismIsEmpty(t *testing.T) { // C9
	m := routeModel(t)
	m.insts = nil
	m.startCreate()
	withNone := m.list.Title
	m.insts = []prism.Instance{m.inst}
	m.showTargets()
	if !strings.HasPrefix(withNone, "Prism has no instances yet.") || strings.Contains(m.list.Title, "no instances") {
		t.Errorf("create list titles: empty Prism %q, with instances %q; want the first to say Prism is empty, the second not", withNone, m.list.Title)
	}
}

func TestCreateVersionListOffersBackOnlyWithInstances(t *testing.T) { // C9
	hasEsc := func(m *model) bool {
		return slices.ContainsFunc(m.list.AdditionalShortHelpKeys(), func(b key.Binding) bool { return b.Help().Key == "esc" })
	}
	m := routeModel(t)
	m.startCreate()
	with := hasEsc(m)
	m.insts = nil
	m.showTargets()
	if !with || hasEsc(m) {
		t.Errorf("esc in create list help: with instances %v, without %v; want true, false", with, hasEsc(m))
	}
}

// ---- going back from an error ----

func errorModel(t *testing.T, phase screen) *model {
	m := routeModel(t)
	m.target = "2.9.0-RC-1"
	m.Update(errMsg{os.ErrPermission})
	m.errPhase = phase
	return m
}

func TestErrorEscGoesBack(t *testing.T) {
	cases := []struct {
		name     string
		instDir  bool
		creating bool
		phase    screen
		want     screen
	}{
		{"failed download goes to versions", true, false, scPreparing, scTarget},
		{"failed self-update goes to versions", true, false, scSelfUpdate, scTarget},
		{"failed create download goes to versions", false, true, scPreparing, scTarget},
		{"no instance picked goes to instances", false, false, scPreparing, scInstance},
		{"instance pick failure goes to instances", true, false, scInstance, scInstance},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := errorModel(t, c.phase)
			if !c.instDir {
				m.inst = prism.Instance{}
			}
			m.creating = c.creating
			press(m, keyEsc)
			if m.screen != c.want {
				t.Errorf("esc on error: screen %d, want %d", m.screen, c.want)
			}
		})
	}
}

// ---- views of the create flow and busy screens ----

func TestCreateConfirmViewDescribesTheNewInstance(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	m.target, m.newName = "2.8.4", "My Pack"
	m.Update(createReady{halfMade(t)})
	m.serverMods = "https://example.com/mods/custom.zip"
	m.warns = []string{"something odd"}
	body := pageBody(m)
	for _, want := range []string{
		"Ready to create My Pack with GTNH 2.8.4",
		"in " + m.creation.Dir + ".",
		"7 files will be installed.",
		"installed from example.com.",
		"Java 17+",
		"Heads up: something odd",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("create confirm body %q lacks %q", body, want)
		}
	}
}

func TestCreateConfirmViewJava8AndNoServerMods(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	m.target, m.newName = "2.8.1", "Old Pack"
	c := halfMade(t)
	c.Flavor = manifest.Java8
	m.Update(createReady{c})
	m.serverMods = ""
	body := pageBody(m)
	if !strings.Contains(body, "Java 8 pack") || strings.Contains(body, "Java 17+") || strings.Contains(body, "extra mods") {
		t.Errorf("create confirm body %q: want the Java 8 line, no Java 17 line, no server-mods line", body)
	}
}

func TestCreatedViewSummarizesTheNewInstance(t *testing.T) { // C9
	m := routeModel(t)
	m.startCreate()
	m.Update(createdMsg{&update.CreateResult{Instance: prism.Instance{Name: "My Pack"}, Files: 1234}})
	m.warns = []string{"something odd"}
	body := pageBody(m)
	for _, want := range []string{"All done! My Pack is ready in Prism.", "1,234 files installed.", "Heads up: something odd"} {
		if !strings.Contains(body, want) {
			t.Errorf("created body %q lacks %q", body, want)
		}
	}
}

func TestDoneViewListsAtMostTwentyKeptConfigs(t *testing.T) {
	m := routeModel(t)
	pl := &update.Plan{}
	for i := range 25 {
		pl.Actions = append(pl.Actions, update.Action{Kind: update.Conflict, Path: ".minecraft/config/f" + string(rune('a'+i)) + ".cfg"})
	}
	pl.ChooseAll(update.KeepMine)
	m.session = &update.Session{Plan: pl}
	m.target = "2.9.0-RC-1"
	m.Update(appliedMsg{&update.Result{From: "2.8.4", To: "2.9.0-RC-1"}})
	body := pageBody(m)
	if !strings.Contains(body, "config/ft.cfg") || strings.Contains(body, "config/fu.cfg") || !strings.Contains(body, "… and 5 more") {
		t.Errorf("done body %q: want the 20th kept file, not the 21st, and \"… and 5 more\"", body)
	}
}

func TestBusyViewTitles(t *testing.T) {
	cases := []struct {
		name     string
		sc       screen
		creating bool
		want     string
	}{
		{"self-update", scSelfUpdate, false, "Updating gtnh-update itself"},
		{"creating", scApplying, true, "Creating My Pack"},
		{"preparing a create", scPreparing, true, "Getting GTNH 2.8.4 ready"},
		{"applying an update", scApplying, false, "Updating Pack to GTNH 2.8.4"},
		{"preparing an update", scPreparing, false, "Getting GTNH 2.8.4 ready for Pack"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := routeModel(t)
			m.target, m.newName, m.creating, m.screen = "2.8.4", "My Pack", c.creating, c.sc
			m.newer = &selfupdate.Release{Version: "9.9.9"}
			if body := pageBody(m); !strings.HasPrefix(body, c.want) {
				t.Errorf("busy body %q, want it to start with %q", body, c.want)
			}
		})
	}
}

func TestLoadingAndSelfUpdatedViews(t *testing.T) {
	m := routeModel(t)
	m.screen = scLoading
	loading := words(m.View())
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	m.screen = scSelfUpdated
	updated := words(m.View())
	if !strings.Contains(loading, "Looking for your GTNH instances") || !strings.Contains(updated, "gtnh-update is now version 9.9.9.") {
		t.Errorf("views: loading %q, self-updated %q; want the loading line and the new version", loading, updated)
	}
}

func TestListViewsShowTheListTitle(t *testing.T) {
	m := routeModel(t)
	m.showTargets()
	targets := words(m.View())
	prepared(t, m)
	conflicts := words(m.View())
	if !strings.Contains(targets, "Pack is on GTNH 2.8.4.") || !strings.Contains(conflicts, "3 config files were changed") {
		t.Errorf("list views: versions %q, conflicts %q; want each to show its title", targets, conflicts)
	}
}

// ---- C11 ctrl+c during a create download ----

// downloadingModel is in the create flow on scPreparing with a fake cancel func that
// counts its calls.
func downloadingModel(t *testing.T) (*model, *int) {
	t.Helper()
	m := routeModel(t)
	m.startCreate()
	m.target, m.newName = "2.8.4", "My Pack"
	m.screen = scPreparing
	calls := 0
	m.cancelCreate = func() { calls++ }
	return m, &calls
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestCtrlCDuringCreateDownloadCancelsWithoutQuitting(t *testing.T) { // C11
	m, calls := downloadingModel(t)
	cmd := press(m, keyCtrlC)
	if *calls != 1 || m.quitting || isQuit(cmd) || m.screen != scPreparing {
		t.Errorf("ctrl+c while downloading: cancel calls %d, quitting %v, quit cmd %v, screen %d; want 1, false, false, scPreparing (%d)",
			*calls, m.quitting, isQuit(cmd), m.screen, scPreparing)
	}
}

func TestCtrlCDuringCreateDownloadShowsCleanupFooter(t *testing.T) { // C11
	m, _ := downloadingModel(t)
	before := words(m.View())
	press(m, keyCtrlC)
	after := words(m.View())
	if !strings.Contains(before, "ctrl+c cancel") || !strings.Contains(after, "Stopping and cleaning up") || strings.Contains(after, "ctrl+c cancel") {
		t.Errorf("busy footer before ctrl+c %q, after %q; want the cancel hint, then only the cleanup note", before, after)
	}
}

func TestErrorAfterCancelledCreateDownloadQuits(t *testing.T) { // C11
	m, _ := downloadingModel(t)
	press(m, keyCtrlC)
	cmd := press(m, errMsg{context.Canceled})
	if !m.quitting || !isQuit(cmd) {
		t.Errorf("errMsg after ctrl+c: quitting %v, quit cmd %v; want true, true", m.quitting, isQuit(cmd))
	}
}

func TestLeftoverErrorAfterCancelledCreateDownloadShowsError(t *testing.T) { // C11
	m, _ := downloadingModel(t)
	press(m, keyCtrlC)
	err := errors.Join(context.Canceled, &update.LeftoverError{Dir: "x", Err: os.ErrPermission})
	cmd := press(m, errMsg{err})
	if m.quitting || isQuit(cmd) || m.screen != scError {
		t.Errorf("LeftoverError after ctrl+c: quitting %v, quit cmd %v, screen %d; want false, false, scError (%d)",
			m.quitting, isQuit(cmd), m.screen, scError)
	}
}

func TestCreateDownloadErrorWithoutCtrlCShowsError(t *testing.T) { // C11
	m, _ := downloadingModel(t)
	cmd := press(m, errMsg{os.ErrPermission})
	if m.quitting || isQuit(cmd) || m.screen != scError || m.errPhase != scPreparing {
		t.Errorf("download error without ctrl+c: quitting %v, quit cmd %v, screen %d, phase %d; want false, false, scError, scPreparing",
			m.quitting, isQuit(cmd), m.screen, m.errPhase)
	}
}

func TestCreateReadyAfterCtrlCClosesCreationAndQuits(t *testing.T) { // C11
	m, _ := downloadingModel(t)
	press(m, keyCtrlC)
	c := halfMade(t)
	cmd := press(m, createReady{c})
	_, err := os.Stat(c.Dir)
	if !m.quitting || !isQuit(cmd) || !os.IsNotExist(err) {
		t.Errorf("createReady after ctrl+c: quitting %v, quit cmd %v, folder stat err %v; want true, true, removed", m.quitting, isQuit(cmd), err)
	}
}

func TestCreateReadyWithoutCtrlCGoesToConfirm(t *testing.T) { // C11
	m, _ := downloadingModel(t)
	c := halfMade(t)
	cmd := press(m, createReady{c})
	if m.quitting || isQuit(cmd) || m.screen != scConfirm || m.creation != c {
		t.Errorf("createReady: quitting %v, quit cmd %v, screen %d; want false, false, scConfirm (%d) holding the creation", m.quitting, isQuit(cmd), m.screen, scConfirm)
	}
}

func TestCtrlCOnApplyingIsIgnored(t *testing.T) { // C11
	m, calls := downloadingModel(t)
	m.screen = scApplying
	cmd := press(m, keyCtrlC)
	if *calls != 0 || m.quitting || isQuit(cmd) || m.screen != scApplying {
		t.Errorf("ctrl+c while applying: cancel calls %d, quitting %v, quit cmd %v, screen %d; want 0, false, false, scApplying",
			*calls, m.quitting, isQuit(cmd), m.screen)
	}
}

func TestPrepareCreateStartsCancellableDownload(t *testing.T) { // C11
	m := routeModel(t)
	m.startCreate()
	m.target, m.newName = "2.8.4", "My Pack"
	_, cmd := m.prepareCreate()
	if m.screen != scPreparing || m.cancelCreate == nil || cmd == nil {
		t.Errorf("prepareCreate: screen %d, cancel set %v, cmd set %v; want scPreparing, true, true", m.screen, m.cancelCreate != nil, cmd != nil)
	}
}

// ---- C12 error view after a creation that couldn't clean up ----

const leftoverSentence = "I couldn't remove the half-made instance folder. Delete it yourself before trying again."

func leftoverErrorModel(t *testing.T, phase screen) *model {
	m := routeModel(t)
	m.startCreate()
	m.target, m.newName = "2.8.4", "My Pack"
	m.screen = phase
	m.Update(errMsg{errors.Join(errors.New("disk full"), &update.LeftoverError{Dir: "/x/My Pack", Err: os.ErrPermission})})
	return m
}

func TestErrorViewLeftoverAfterFailedApplySaysDeleteIt(t *testing.T) { // C12
	body := pageBody(leftoverErrorModel(t, scApplying))
	if strings.Count(body, leftoverSentence) != 1 || strings.Contains(body, "I removed the half-made instance") {
		t.Errorf("error view = %q, want the delete-it-yourself sentence exactly once and no 'I removed' claim", body)
	}
}

func TestErrorViewLeftoverAfterFailedDownloadSaysDeleteIt(t *testing.T) { // C12
	body := pageBody(leftoverErrorModel(t, scPreparing))
	if strings.Count(body, leftoverSentence) != 1 || strings.Contains(body, "Nothing was created.") {
		t.Errorf("error view = %q, want the delete-it-yourself sentence exactly once and no 'Nothing was created'", body)
	}
}

func TestErrorViewFailedCreateApplySaysItCleanedUp(t *testing.T) { // C12
	m := routeModel(t)
	m.startCreate()
	m.screen = scApplying
	m.Update(errMsg{os.ErrPermission})
	body := pageBody(m)
	if !strings.Contains(body, "I removed the half-made instance") || strings.Contains(body, "Delete it yourself") {
		t.Errorf("error view = %q, want the 'I removed' sentence only", body)
	}
}

// ---- C13 -version resolution after load ----

func TestLoadResolvesLatestStableTarget(t *testing.T) { // C13
	m := sizedModel(t)
	m.cfg.Target, m.cfg.Create, m.cfg.ServerMods = "latest-stable", true, "none"
	press(m, loadedMsg{routeManifest(t), nil})
	if m.target != "2.8.4" || m.screen != scName || m.nameIn.Value() != "GT New Horizons 2.8.4" {
		t.Errorf("latest-stable: target %q, screen %d, name %q; want 2.8.4, scName (%d), \"GT New Horizons 2.8.4\"",
			m.target, m.screen, m.nameIn.Value(), scName)
	}
}

func TestLoadResolvesLatestTargetForUpdate(t *testing.T) { // C13
	m := sizedModel(t)
	m.cfg.Target, m.cfg.ServerMods = "latest", "none"
	m.cfg.Installed = "2.8.4"
	inst := prism.Instance{Dir: t.TempDir(), Name: "Pack", GTNH: true}
	press(m, loadedMsg{routeManifest(t), []prism.Instance{inst}})
	if m.target != "2.9.0-RC-1" || m.screen != scPreparing {
		t.Errorf("latest: target %q, screen %d; want 2.9.0-RC-1, scPreparing (%d)", m.target, m.screen, scPreparing)
	}
}

func TestLoadWithUnknownTargetShowsError(t *testing.T) { // C13
	m := sizedModel(t)
	m.cfg.Target = "9.9.9"
	cmd := press(m, loadedMsg{routeManifest(t), nil})
	if cmd == nil {
		t.Fatal("unknown target: no command, want an error message")
	}
	m.Update(cmd())
	if m.screen != scError || !strings.Contains(m.err.Error(), `GTNH has no version called "9.9.9"`) {
		t.Errorf("unknown target: screen %d, err %v; want scError with the no-version sentence", m.screen, m.err)
	}
}

func TestLoadLatestStableWithoutStableShowsManifestError(t *testing.T) { // C13
	man, err := manifest.Parse([]byte(`{"2.9.0-RC-1": {"title":"Beta release","releaseDate":"2026/09/24","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/b.zip"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	m := sizedModel(t)
	m.cfg.Target = "latest-stable"
	cmd := press(m, loadedMsg{man, nil})
	if cmd == nil {
		t.Fatal("latest-stable without stable: no command, want an error message")
	}
	m.Update(cmd())
	if m.screen != scError || !strings.Contains(m.err.Error(), "no stable release") {
		t.Errorf("latest-stable without stable: screen %d, err %v; want scError with the no stable release error", m.screen, m.err)
	}
}

// ---- C14 list help fits ~100 columns ----

func shortHelpWidth(m *model) int {
	return ansi.StringWidth(m.list.Help.ShortHelpView(m.list.ShortHelp()))
}

func wideModel(t *testing.T) *model {
	m := routeModel(t)
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	m.insts = append(m.insts, prism.Instance{Dir: t.TempDir(), Name: "Vanilla"})
	return m
}

func TestListHelpLinesFitHundredColumns(t *testing.T) { // C14
	const maxHelp = 100
	cases := []struct {
		name  string
		setup func(t *testing.T, m *model)
	}{
		{"instances", func(t *testing.T, m *model) { m.showAll = true; m.showInstances() }},
		{"update versions", func(t *testing.T, m *model) { m.showTargets() }},
		{"create versions", func(t *testing.T, m *model) { m.startCreate() }},
		{"conflicts", func(t *testing.T, m *model) { prepared(t, m) }},
		{"resolve", func(t *testing.T, m *model) { onResolve(t, m) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := wideModel(t)
			c.setup(t, m)
			if w := shortHelpWidth(m); w > maxHelp || w == 0 {
				t.Errorf("help line %q is %d columns, want 1..%d", ansi.Strip(m.list.Help.ShortHelpView(m.list.ShortHelp())), w, maxHelp)
			}
		})
	}
}

// ---- background commands of the create flow report failures as errors ----

type offlineTransport struct{}

func (offlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, os.ErrDeadlineExceeded
}

func TestPrepareCreateCommandReportsDownloadFailure(t *testing.T) {
	m := routeModel(t)
	m.cfg.Client = &http.Client{Transport: offlineTransport{}}
	m.send = func(tea.Msg) {}
	m.startCreate()
	m.target, m.newName = "2.8.4", "My Pack"
	_, cmd := m.prepareCreate()
	msg, ok := cmd().(errMsg)
	if !ok || !errors.Is(msg.err, os.ErrDeadlineExceeded) {
		t.Errorf("prepareCreate command = %#v, want errMsg wrapping the transport error", msg)
	}
}

func TestCreateConfirmEnterCommandReportsApplyFailure(t *testing.T) {
	m := routeModel(t)
	m.send = func(tea.Msg) {}
	m.startCreate()
	m.target, m.newName = "2.8.4", "My Pack"
	m.Update(createReady{halfMade(t)}) // never prepared: Apply has nothing to install
	cmd := press(m, keyEnter)
	if m.screen != scApplying || cmd == nil {
		t.Fatalf("enter on create confirm: screen %d, cmd set %v; want scApplying, true", m.screen, cmd != nil)
	}
	if msg := cmd(); !isErrMsg(msg) {
		t.Errorf("Apply command on an unprepared creation returned %T, want errMsg", msg)
	}
}

func isErrMsg(msg tea.Msg) bool {
	_, ok := msg.(errMsg)
	return ok
}

// ---- C4 esc on the create confirm when the folder can't be removed ----

func TestCreateConfirmEscWithRemovableFolderGoesBackToVersions(t *testing.T) { // C4
	m := routeModel(t)
	m.startCreate()
	m.target, m.newName = "2.8.4", "My Pack"
	c := halfMade(t)
	m.Update(createReady{c})
	press(m, keyEsc)
	_, err := os.Stat(c.Dir)
	if m.screen != scTarget || m.err != nil || !os.IsNotExist(err) {
		t.Errorf("esc on create confirm: screen %d, err %v, folder stat err %v; want scTarget (%d), nil, removed", m.screen, m.err, err, scTarget)
	}
}

func TestCreateConfirmEscWithUnremovableFolderShowsLeftoverError(t *testing.T) { // C4
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		return // can't make a folder un-removable portably; the removable path is covered above
	}
	m := routeModel(t)
	m.startCreate()
	m.target, m.newName = "2.8.4", "My Pack"
	c := halfMade(t)
	if err := os.WriteFile(filepath.Join(c.Dir, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(c.Dir)
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o755) })
	m.Update(createReady{c})
	press(m, keyEsc)
	var le *update.LeftoverError
	if m.screen != scError || !errors.As(m.err, &le) || le.Dir != c.Dir {
		t.Errorf("esc on create confirm with an un-removable folder: screen %d, err %v; want scError (%d) with a LeftoverError for %s",
			m.screen, m.err, scError, c.Dir)
	}
	if body := pageBody(m); strings.Count(body, leftoverSentence) != 1 {
		t.Errorf("error view = %q, want the delete-it-yourself sentence exactly once", body)
	}
}
