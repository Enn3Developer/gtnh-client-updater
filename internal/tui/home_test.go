package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Tests for the launcher home screen (home spec C1-C5, C12). Clause numbers in the
// comments refer to the home spec.

// ---- fixtures ----

type findCall struct{ dataDir, override string }

type launchCall struct {
	l       prism.Launcher
	dataDir string
	inst    prism.Instance
	server  string
}

// fakePrism stands in for Prism Launcher: it records what the model asked of it.
type fakePrism struct {
	finds     []findCall
	launches  []launchCall
	findErr   error
	launchErr error
}

var testLauncher = prism.Launcher{Exe: "prism-test", Kind: "custom"}

func (f *fakePrism) install(m *model) {
	m.findLauncher = func(dataDir, override string) (prism.Launcher, error) {
		f.finds = append(f.finds, findCall{dataDir, override})
		if f.findErr != nil {
			return prism.Launcher{}, f.findErr
		}
		return testLauncher, nil
	}
	m.launch = func(l prism.Launcher, dataDir string, inst prism.Instance, server string) error {
		f.launches = append(f.launches, launchCall{l, dataDir, inst, server})
		return f.launchErr
	}
}

// home is a model with two Prism data dirs; both GTNH instances live in the second one.
// older (2.8.1) comes first in m.insts, newest (2.8.4, the newest stable) second.
type home struct {
	m          *model
	prism      *fakePrism
	dir1, dir2 string
	older      prism.Instance
	newest     prism.Instance
}

func gtnhInstance(t *testing.T, dir, name string, st update.State) prism.Instance {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := update.SaveState(dir, &st); err != nil {
		t.Fatal(err)
	}
	return prism.Instance{Dir: dir, Name: name, GTNH: true}
}

func newHome(t *testing.T, width int, olderState update.State) *home {
	t.Helper()
	m := sizedModel(t)
	m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	h := &home{m: m, prism: &fakePrism{}, dir1: t.TempDir(), dir2: t.TempDir()}
	m.cfg.PrismDirs = []string{h.dir1, h.dir2}
	m.manifest = routeManifest(t)
	olderState.Version = "2.8.1"
	h.older = gtnhInstance(t, filepath.Join(h.dir2, "instances", "Older"), "Older", olderState)
	h.newest = gtnhInstance(t, filepath.Join(h.dir2, "instances", "Newest"), "Newest", update.State{Version: "2.8.4"})
	m.insts = []prism.Instance{h.older, h.newest}
	h.prism.install(m)
	return h
}

// msgsOf runs cmd and returns its messages, unpacking batches.
func msgsOf(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, msgsOf(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// msgOf returns the first message of type T that cmd produces.
func msgOf[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	msgs := msgsOf(cmd)
	for _, msg := range msgs {
		if v, ok := msg.(T); ok {
			return v
		}
	}
	var zero T
	t.Fatalf("command produced %#v, want a %T", msgs, zero)
	return zero
}

func view(m *model) string { return words(m.View()) }

func itemDescs(m *model) map[string]string {
	out := map[string]string{}
	for _, it := range m.list.Items() {
		out[it.(item).key] = it.(item).desc
	}
	return out
}

// ---- C1 header ----

func TestHeaderSaysGTNHLauncherAndVersion(t *testing.T) { // C1
	m := sizedModel(t)
	if got := words(m.header()); got != "GTNH Launcher 1.2.3" {
		t.Errorf("header = %q, want %q", got, "GTNH Launcher 1.2.3")
	}
}

// ---- C2 after load ----

func TestLoadWithOneGTNHInstanceLandsOnHome(t *testing.T) { // C2
	m := sizedModel(t)
	inst := prism.Instance{Dir: t.TempDir(), Name: "Pack", GTNH: true}
	press(m, loadedMsg{routeManifest(t), []prism.Instance{inst}})
	if m.screen != scHome || selectedKey(m) != inst.Dir {
		t.Errorf("loaded with one GTNH instance: screen %d, selected %q; want scHome (%d), %q", m.screen, selectedKey(m), scHome, inst.Dir)
	}
}

func TestLoadSelectsFirstGTNHInstanceAndHidesOthers(t *testing.T) { // C2, C3
	m := sizedModel(t)
	other := prism.Instance{Dir: t.TempDir(), Name: "Vanilla"}
	a := prism.Instance{Dir: t.TempDir(), Name: "Alpha", GTNH: true}
	b := prism.Instance{Dir: t.TempDir(), Name: "Beta", GTNH: true}
	press(m, loadedMsg{routeManifest(t), []prism.Instance{other, a, b}})
	if m.screen != scHome || m.list.Title != "Which instance do you want to play?" || selectedKey(m) != a.Dir || m.showAll || len(m.list.Items()) != 2 {
		t.Errorf("loaded with Vanilla, Alpha, Beta: screen %d, title %q, selected %q, showAll %v, %d items; want scHome, the home title, Alpha, false, 2",
			m.screen, m.list.Title, selectedKey(m), m.showAll, len(m.list.Items()))
	}
}

func TestLoadWithInstanceFlagSelectsItOnHome(t *testing.T) { // C2
	m := sizedModel(t)
	a := prism.Instance{Dir: t.TempDir(), Name: "Alpha", GTNH: true}
	b := prism.Instance{Dir: t.TempDir(), Name: "Beta", GTNH: true}
	m.cfg.Instance = "Beta"
	press(m, loadedMsg{routeManifest(t), []prism.Instance{a, b}})
	if m.screen != scHome || selectedKey(m) != b.Dir {
		t.Errorf("-instance Beta: screen %d, selected %q; want scHome (%d), %q", m.screen, selectedKey(m), scHome, b.Dir)
	}
}

func TestLoadWithPlayAndInstanceStartsIt(t *testing.T) { // C2, C6
	h := newHome(t, termW, update.State{})
	m := h.m
	m.cfg.Instance, m.cfg.Play = "Newest", true
	cmd := press(m, loadedMsg{routeManifest(t), m.insts})
	launched := msgOf[launchedMsg](t, cmd)
	if m.screen != scLaunching || len(h.prism.launches) != 1 || h.prism.launches[0].inst.Dir != h.newest.Dir {
		t.Errorf("-play -instance Newest: screen %d, launches %+v, msg %#v; want scLaunching (%d), Newest launched",
			m.screen, h.prism.launches, launched, scLaunching)
	}
}

func TestLoadWithPlayButNoInstanceLandsOnHome(t *testing.T) { // C2
	h := newHome(t, termW, update.State{})
	m := h.m
	m.cfg.Play = true
	press(m, loadedMsg{routeManifest(t), []prism.Instance{h.older}})
	if m.screen != scHome || len(h.prism.finds) != 0 || len(h.prism.launches) != 0 {
		t.Errorf("-play without -instance: screen %d, finds %d, launches %d; want scHome (%d), nothing started",
			m.screen, len(h.prism.finds), len(h.prism.launches), scHome)
	}
}

// ---- C3 home list ----

func TestHomeListTitleAndDescriptions(t *testing.T) { // C3, C14
	h := newHome(t, 100, update.State{})
	other := prism.Instance{Dir: t.TempDir(), Name: "Vanilla"}
	h.m.insts = append(h.m.insts, other)
	h.m.showAll = true
	h.m.showHome()
	want := map[string]string{ // home-card C1
		h.older.Dir:  "GTNH 2.8.1 · never played · ▲ 2.8.4 available",
		h.newest.Dir: "GTNH 2.8.4 · never played · ● up to date",
		other.Dir:    "never played · ○ not a GTNH instance",
	}
	got := strippedDescs(h.m)
	if h.m.list.Title != "Which instance do you want to play?" || got[h.older.Dir] != want[h.older.Dir] ||
		got[h.newest.Dir] != want[h.newest.Dir] || got[other.Dir] != want[other.Dir] || len(got) != 3 {
		t.Errorf("home list: title %q, descs %q; want %q, %q", h.m.list.Title, got, "Which instance do you want to play?", want)
	}
	items := h.m.list.Items()
	if items[0].(item).title != "Older" || items[1].(item).title != "Newest" || items[2].(item).title != "Vanilla" {
		t.Errorf("home item titles = %#v, want the instance names Older, Newest, Vanilla", items)
	}
}

func TestHomeListDescSaysWhenPlayed(t *testing.T) { // C3
	h := newHome(t, termW, update.State{})
	h.m.insts[0].LastLaunch = time.Now().Add(-3 * 24 * time.Hour)
	h.m.showHome()
	if got, want := strippedDescs(h.m)[h.older.Dir], "GTNH 2.8.1 · played 3 days ago · ▲ 2.8.4 available"; got != want {
		t.Errorf("played desc = %q, want %q", got, want)
	}
}

// strippedDescs is itemDescs without the badge colours.
func strippedDescs(m *model) map[string]string {
	out := map[string]string{}
	for k, d := range itemDescs(m) {
		out[k] = ansi.Strip(d)
	}
	return out
}

func TestHomeListDescOfGTNHInstanceWithUnknownVersion(t *testing.T) { // home-card C1, C2
	h := newHome(t, 100, update.State{})
	mystery := prism.Instance{Dir: t.TempDir(), Name: "Mystery", GTNH: true}
	h.m.insts = append(h.m.insts, mystery)
	h.m.showHome()
	if got, want := strippedDescs(h.m)[mystery.Dir], "GTNH · never played · ○ version unknown"; got != want {
		t.Errorf("desc of a GTNH instance without a known version = %q, want %q", got, want)
	}
}

func TestHomeListDescOfOtherInstanceSaysWhenPlayed(t *testing.T) { // home-card C1
	h := newHome(t, 100, update.State{})
	other := prism.Instance{Dir: t.TempDir(), Name: "Vanilla", LastLaunch: time.Now().Add(-72 * time.Hour)}
	h.m.insts = append(h.m.insts, other)
	h.m.showAll = true
	h.m.showHome()
	if got, want := strippedDescs(h.m)[other.Dir], "played 3 days ago · ○ not a GTNH instance"; got != want {
		t.Errorf("desc of a played non-GTNH instance = %q, want %q", got, want)
	}
}

// ---- home-card C2 homeStatus ----

func TestHomeStatusOffersRecommendedVersionWhenItDiffers(t *testing.T) { // home-card C2, K1
	withTrueColor(t)
	got := homeStatus(homeInfo{gtnh: true, version: "2.8.1", rec: "2.8.4"})
	if ansi.Strip(got) != "▲ 2.8.4 available" || !hasStyle(got, fgWarn) {
		t.Errorf("homeStatus(2.8.1, rec 2.8.4) = %q, want the warn badge \"▲ 2.8.4 available\"", got)
	}
}

func TestHomeStatusSaysUpToDateWhenRecommendedIsInstalled(t *testing.T) { // home-card C2, K1
	withTrueColor(t)
	got := homeStatus(homeInfo{gtnh: true, version: "2.8.4", rec: "2.8.4"})
	if ansi.Strip(got) != "● up to date" || !hasStyle(got, fgOK) {
		t.Errorf("homeStatus(2.8.4, rec 2.8.4) = %q, want the ok badge \"● up to date\"", got)
	}
}

func TestHomeStatusSaysVersionUnknownWithoutVersion(t *testing.T) { // home-card C2
	withTrueColor(t)
	got := homeStatus(homeInfo{gtnh: true, version: "", rec: "2.8.4"})
	if ansi.Strip(got) != "○ version unknown" || !hasStyle(got, fgDim) {
		t.Errorf("homeStatus(no version, rec 2.8.4) = %q, want the dim badge \"○ version unknown\"", got)
	}
}

func TestHomeStatusSaysNotGTNHForOtherInstances(t *testing.T) { // home-card C2
	withTrueColor(t)
	got := homeStatus(homeInfo{gtnh: false, version: "2.8.1", rec: "2.8.4"})
	if ansi.Strip(got) != "○ not a GTNH instance" || !hasStyle(got, fgDim) {
		t.Errorf("homeStatus(not GTNH) = %q, want the dim badge \"○ not a GTNH instance\"", got)
	}
}

// escBefore is the escape sequence written right before the first sub in s, or "" when
// sub is missing or not directly preceded by an escape.
func escBefore(s, sub string) string {
	i := strings.Index(s, sub)
	if i < 0 {
		return ""
	}
	j := strings.LastIndex(s[:i], "\x1b[")
	if j < 0 || !strings.HasSuffix(s[:i], "m") || strings.Count(s[j:i], "m") != 1 {
		return ""
	}
	return s[j:i]
}

func TestHomeRowBadgesAreColoured(t *testing.T) { // home-card C8
	withTrueColor(t)
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	descs := itemDescs(h.m)
	if e := escBefore(descs[h.newest.Dir], "● up to date"); !hasStyle(e, fgOK) {
		t.Errorf("Newest desc %q: \"● up to date\" is preceded by %q, want the ok colour %s", descs[h.newest.Dir], e, fgOK)
	}
	if e := escBefore(descs[h.older.Dir], "▲ 2.8.4 available"); !hasStyle(e, fgWarn) {
		t.Errorf("Older desc %q: \"▲ 2.8.4 available\" is preceded by %q, want the warn colour %s", descs[h.older.Dir], e, fgWarn)
	}
}

func TestHomeJAndKDoNotMoveTheCursor(t *testing.T) { // C3
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	press(h.m, runes("j"))
	afterJ := selectedKey(h.m)
	press(h.m, keyDown, runes("k"))
	afterK := selectedKey(h.m)
	if afterJ != h.older.Dir || afterK != h.newest.Dir {
		t.Errorf("j then down, k: selected %q then %q; want Older then Newest (only arrows move)", afterJ, afterK)
	}
}

// ---- C4 card ----

// cardRow is a stripped card body line: label padded to 13 columns, value padded to the
// 25-column value room, inside the panel border and padding (42 columns).
func cardRow(label, value string) string {
	return "│ " + label + strings.Repeat(" ", 13-ansi.StringWidth(label)) +
		value + strings.Repeat(" ", 25-ansi.StringWidth(value)) + " │"
}

// cardLines is cardView() ansi-stripped, split into lines.
func cardLines(m *model) []string { return strings.Split(ansi.Strip(m.cardView()), "\n") }

// wrongWidths lists the lines of card that are not exactly 42 columns wide.
func wrongWidths(card string) []string {
	var bad []string
	for _, l := range strings.Split(card, "\n") {
		if ansi.StringWidth(l) != 42 {
			bad = append(bad, fmt.Sprintf("%d:%q", ansi.StringWidth(l), ansi.Strip(l)))
		}
	}
	return bad
}

func TestHomeCardOfOlderIsATitledPanelWithAlignedRows(t *testing.T) { // home-card C3, C4, C6
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	want := []string{
		"╭─ Older " + strings.Repeat("─", 32) + "╮",
		cardRow("Installed", "GTNH 2.8.1"),
		cardRow("Update", "▲ 2.8.4 available"),
		cardRow("Undo", "nothing to undo"),
		cardRow("Played", "never"),
		cardRow("Server", "none set"),
		cardRow("Server mods", "none"),
		cardRow("Pack", "Java 8"),
		"╰" + strings.Repeat("─", 40) + "╯",
	}
	if got := cardLines(h.m); !slices.Equal(got, want) {
		t.Errorf("card of Older =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestHomeCardAtWidth100ShowsSelectedInstance(t *testing.T) { // C4, C14, home-card C4
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	v := view(h.m)
	want := []string{"Installed GTNH 2.8.1", "Update ▲ 2.8.4 available", "Undo nothing to undo", "Played never",
		"Server none set", "Server mods none", "Pack Java 8"}
	for _, w := range want {
		if !strings.Contains(v, w) {
			t.Errorf("home view at 100 columns lacks %q:\n%s", w, ansi.Strip(h.m.View()))
		}
	}
	if strings.Contains(v, "press u") {
		t.Errorf("home view at 100 columns = %q, want no \"press u\" (\"▲ 2.8.4 available · press u\" is 27 columns)", v)
	}
	card := words(strings.ReplaceAll(h.m.cardView(), "│", " "))
	if !strings.Contains(card, strings.Join(want, " ")) {
		t.Errorf("card of Older = %q, want the rows in order %q", card, strings.Join(want, " "))
	}
}

func TestHomeCardSaysUpToDateForNewestStable(t *testing.T) { // C4, home-card C4
	h := newHome(t, 100, update.State{})
	h.m.inst = h.newest
	h.m.showHome()
	if v := words(h.m.cardView()); !strings.Contains(v, "Installed GTNH 2.8.4") || !strings.Contains(v, "Update ● up to date") || strings.Contains(v, "available") {
		t.Errorf("card for Newest = %q, want Installed GTNH 2.8.4 and Update ● up to date", v)
	}
}

func TestHomeCardShowsServerAndServerModsHost(t *testing.T) { // C4, home-card C4
	h := newHome(t, 100, update.State{ServerAddress: "mc.x:1", CustomModsURL: "https://mods.example.com/a/custom.zip", CustomModsAsked: true})
	h.m.insts[0].LastLaunch = time.Now().Add(-3 * 24 * time.Hour)
	h.m.showHome()
	lines := cardLines(h.m)
	for _, want := range []string{cardRow("Server", "mc.x:1 · press j to join"), cardRow("Server mods", "mods.example.com"), cardRow("Played", "3 days ago")} {
		if !slices.Contains(lines, want) {
			t.Errorf("card lacks the line %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
}

func TestHomeCardDropsJoinHintWhenServerFillsTheRow(t *testing.T) { // home-card C4
	h := newHome(t, 100, update.State{ServerAddress: "mc.example.com:25565"})
	h.m.showHome()
	if lines, want := cardLines(h.m), cardRow("Server", "mc.example.com:25565"); !slices.Contains(lines, want) {
		t.Errorf("card with a 20-column server lacks %q (no hint, no …):\n%s", want, strings.Join(lines, "\n"))
	}
}

func TestHomeCardShowsJoinHintThatExactlyFillsTheRow(t *testing.T) { // home-card C4: 7 + 18 = 25
	h := newHome(t, 100, update.State{ServerAddress: "mc.x:12"})
	h.m.showHome()
	if lines, want := cardLines(h.m), cardRow("Server", "mc.x:12 · press j to join"); !slices.Contains(lines, want) {
		t.Errorf("card with a 7-column server lacks %q:\n%s", want, strings.Join(lines, "\n"))
	}
}

func TestHomeCardDropsJoinHintOneColumnTooWide(t *testing.T) { // home-card C4: 8 + 18 = 26
	h := newHome(t, 100, update.State{ServerAddress: "mc.x:123"})
	h.m.showHome()
	if lines, want := cardLines(h.m), cardRow("Server", "mc.x:123"); !slices.Contains(lines, want) {
		t.Errorf("card with an 8-column server lacks %q:\n%s", want, strings.Join(lines, "\n"))
	}
}

func TestHomeCardShowsUpdateHintThatExactlyFillsTheRow(t *testing.T) { // home-card C4: "▲ 2.9 available · press u" = 25
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	h.m.home[h.older.Dir] = homeInfo{gtnh: true, version: "2.8", rec: "2.9"}
	if lines, want := cardLines(h.m), cardRow("Update", "▲ 2.9 available · press u"); !slices.Contains(lines, want) {
		t.Errorf("card lacks %q:\n%s", want, strings.Join(lines, "\n"))
	}
}

func TestHomeCardDropsUpdateHintOneColumnTooWide(t *testing.T) { // home-card C4: "▲ 2.10 available · press u" = 26
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	h.m.home[h.older.Dir] = homeInfo{gtnh: true, version: "2.8", rec: "2.10"}
	if lines, want := cardLines(h.m), cardRow("Update", "▲ 2.10 available"); !slices.Contains(lines, want) {
		t.Errorf("card lacks %q:\n%s", want, strings.Join(lines, "\n"))
	}
}

func TestHomeCardOffersUndoOfNewestBackup(t *testing.T) { // home-card C4, C8
	withTrueColor(t)
	h := newHome(t, 100, update.State{})
	writeBackup(t, h.older.Dir, "backup-20260101-000000", update.BackupInfo{From: "2.8.0", To: "2.8.1"})
	h.m.showHome()
	card := h.m.cardView()
	if lines, want := strings.Split(ansi.Strip(card), "\n"), cardRow("Undo", "back to 2.8.0 · press b"); !slices.Contains(lines, want) {
		t.Errorf("card with a backup from 2.8.0 lacks %q:\n%s", want, strings.Join(lines, "\n"))
	}
	if e := escBefore(card, "back to 2.8.0"); !hasStyle(e, fgWarn) {
		t.Errorf("\"back to 2.8.0\" is preceded by %q, want the warn colour %s", e, fgWarn)
	}
}

func TestHomeCardShowsJava17Pack(t *testing.T) { // home-card C4
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	h.m.home[h.older.Dir] = homeInfo{gtnh: true, version: "2.8.1", rec: "2.8.4", flavor: manifest.Java17}
	if lines, want := cardLines(h.m), cardRow("Pack", "Java 17+"); !slices.Contains(lines, want) {
		t.Errorf("card of a Java 17 pack lacks %q:\n%s", want, strings.Join(lines, "\n"))
	}
}

func TestHomeCardOfOtherInstanceHasNoUpdateOrPack(t *testing.T) { // C4, home-card C4
	h := newHome(t, 100, update.State{})
	other := prism.Instance{Dir: t.TempDir(), Name: "Vanilla"}
	h.m.insts = append(h.m.insts, other)
	h.m.showAll, h.m.inst = true, other
	h.m.showHome()
	if v := words(h.m.cardView()); !strings.Contains(v, "Update —") || strings.Contains(v, "Pack") || strings.Contains(v, "Undo") {
		t.Errorf("card for a non-GTNH instance = %q, want \"Update —\" and no Undo or Pack row", v)
	}
}

func TestHomeCardOfGTNHInstanceWithUnknownVersion(t *testing.T) { // home-card C2, C4
	h := newHome(t, 100, update.State{})
	mystery := prism.Instance{Dir: t.TempDir(), Name: "Mystery", GTNH: true}
	h.m.insts = append(h.m.insts, mystery)
	h.m.inst = mystery
	h.m.showHome()
	lines := cardLines(h.m)
	for _, want := range []string{cardRow("Installed", "version unknown"), cardRow("Update", "○ version unknown")} {
		if !slices.Contains(lines, want) {
			t.Errorf("card of Mystery lacks %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
}

func TestHomeCardLabelsArePadded(t *testing.T) { // C4, home-card C4
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	v := ansi.Strip(h.m.View())
	for _, want := range []string{"Installed    GTNH 2.8.1", "Update       ▲ 2.8.4 available", "Undo         nothing to undo", "Played       never",
		"Server       none set", "Server mods  none", "Pack         Java 8"} {
		if !strings.Contains(v, want) {
			t.Errorf("home view lacks the line %q padded to the 13-column label:\n%s", want, v)
		}
	}
}

func TestHomeCardLabelsAreDimAndBadgeColoured(t *testing.T) { // home-card C8
	withTrueColor(t)
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	card := h.m.cardView()
	if e := escBefore(card, "Installed"); !hasStyle(e, fgDim) {
		t.Errorf("\"Installed\" is preceded by %q, want the dim colour %s", e, fgDim)
	}
	if e := escBefore(card, "▲ 2.8.4 available"); !hasStyle(e, fgWarn) {
		t.Errorf("\"▲ 2.8.4 available\" is preceded by %q, want the warn colour %s", e, fgWarn)
	}
	h.m.inst = h.newest
	h.m.showHome()
	newest := h.m.cardView()
	if e := escBefore(newest, "● up to date"); !hasStyle(e, fgOK) {
		t.Errorf("\"● up to date\" in the Newest card is preceded by %q, want the ok colour %s", e, fgOK)
	}
}

func TestHomeCardIsEmptyWithoutSelection(t *testing.T) { // home-card C3
	h := newHome(t, 100, update.State{})
	h.m.insts = nil
	h.m.showHome()
	if got := h.m.cardView(); got != "" {
		t.Errorf("cardView() with no instance = %q, want \"\"", got)
	}
}

func TestHomeCardLinesAre42Columns(t *testing.T) { // home-card C3
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	if bad := wrongWidths(h.m.cardView()); len(bad) != 0 {
		t.Errorf("card of Older has lines not 42 columns wide: %v", bad)
	}
}

func TestHomeCardTruncatesLongServerAddress(t *testing.T) { // home-card C3, C5, K2
	addr := strings.Repeat("abcdefghij", 6) // 60 columns
	h := newHome(t, 100, update.State{ServerAddress: addr})
	h.m.showHome()
	card := h.m.cardView()
	if bad := wrongWidths(card); len(bad) != 0 {
		t.Errorf("card with a 60-column server has lines not 42 columns wide: %v", bad)
	}
	if lines, want := strings.Split(ansi.Strip(card), "\n"), cardRow("Server", addr[:24]+"…"); !slices.Contains(lines, want) {
		t.Errorf("card lacks %q:\n%s", want, strings.Join(lines, "\n"))
	}
}

func TestHomeCardKeepsServerThatExactlyFillsTheRoom(t *testing.T) { // home-card C5: 25 columns fit untruncated
	addr := strings.Repeat("a", 20) + ":2556"
	h := newHome(t, 100, update.State{ServerAddress: addr})
	h.m.showHome()
	if lines, want := cardLines(h.m), cardRow("Server", addr); !slices.Contains(lines, want) {
		t.Errorf("card with a 25-column server lacks %q:\n%s", want, strings.Join(lines, "\n"))
	}
}

func TestHomeCardTruncatesServerOneColumnTooWide(t *testing.T) { // home-card C5: 26 columns -> 24 + …
	addr := strings.Repeat("a", 21) + ":2556"
	h := newHome(t, 100, update.State{ServerAddress: addr})
	h.m.showHome()
	if lines, want := cardLines(h.m), cardRow("Server", addr[:24]+"…"); !slices.Contains(lines, want) {
		t.Errorf("card with a 26-column server lacks %q:\n%s", want, strings.Join(lines, "\n"))
	}
}

func TestHomeCardTitleTruncatesLongName(t *testing.T) { // home-card C3, C6
	name := strings.Repeat("Namelong", 7) + "Name" // 60 columns
	h := newHome(t, 100, update.State{})
	h.m.insts[0].Name = name
	h.m.showHome()
	card := h.m.cardView()
	if bad := wrongWidths(card); len(bad) != 0 {
		t.Errorf("card of a 60-column name has lines not 42 columns wide: %v", bad)
	}
	if got, want := strings.Split(ansi.Strip(card), "\n")[0], "╭─ "+name[:33]+"… ───╮"; got != want {
		t.Errorf("card top border = %q, want %q", got, want)
	}
}

func TestHomeCardIsComputedWhenHomeIsShownNotInView(t *testing.T) { // C4, home-card C7
	h := newHome(t, 100, update.State{})
	h.m.showHome()
	if err := update.SaveState(h.older.Dir, &update.State{Version: "2.8.4"}); err != nil {
		t.Fatal(err)
	}
	if v := view(h.m); !strings.Contains(v, "Update ▲ 2.8.4 available") || !strings.Contains(v, "Installed GTNH 2.8.1") {
		t.Errorf("card after the state changed on disk = %q, want the showHome values Installed GTNH 2.8.1, Update ▲ 2.8.4 available", v)
	}
}

func TestHomeCardNeedsNinetyColumns(t *testing.T) { // C4, C14
	at90 := newHome(t, 90, update.State{})
	at90.m.showHome()
	at89 := newHome(t, 89, update.State{})
	at89.m.showHome()
	at80 := newHome(t, 80, update.State{})
	at80.m.showHome()
	if !strings.Contains(view(at90.m), "Installed") || strings.Contains(view(at89.m), "Installed") || strings.Contains(view(at80.m), "Installed") {
		t.Errorf("card shown at 90/89/80 columns: %v/%v/%v; want true/false/false",
			strings.Contains(view(at90.m), "Installed"), strings.Contains(view(at89.m), "Installed"), strings.Contains(view(at80.m), "Installed"))
	}
}

// C4: list (width-46) + card (42) leave the view inside the terminal; without the card
// the list is width-4 as on every list screen.
func TestHomeViewFitsTerminalWithAndWithoutCard(t *testing.T) { // C4, home-card C3
	longName := strings.Repeat("Long name ", 20) // wider than any terminal here: the list clips it
	widthAt := func(w int) int {
		h := newHome(t, w, update.State{ServerAddress: strings.Repeat("abcdefghij", 6)})
		h.m.insts[0].Name = longName
		h.m.showHome()
		_, widest := fit(h.m.View())
		return widest
	}
	at80, at90, at100, at120 := widthAt(80), widthAt(90), widthAt(100), widthAt(120)
	if at80 > 80 || at90 > 90 || at100 > 100 || at120 > 120 {
		t.Errorf("home view widths: %d at 80 columns, %d at 90, %d at 100, %d at 120; want each <= its terminal", at80, at90, at100, at120)
	}
}

// ---- C5 home keys and help ----

func TestHomeHelpLineBasic(t *testing.T) { // C5
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	if v := view(h.m); !strings.Contains(v, "enter play u update s settings b undo n new q quit") || strings.Contains(v, "j join") || strings.Contains(v, "a all") {
		t.Errorf("home view %q: want the help \"enter play u update s settings b undo n new q quit\" without j or a", v)
	}
}

func TestHomeHelpLineOffersJoinWithServer(t *testing.T) { // C5
	h := newHome(t, termW, update.State{ServerAddress: "mc.x:1"})
	h.m.showHome()
	if v := view(h.m); !strings.Contains(v, "enter play j join u update s settings b undo n new q quit") {
		t.Errorf("home view %q: want the help with j join", v)
	}
}

func TestHomeHelpLineOffersAllAndNewVersion(t *testing.T) { // C5
	h := newHome(t, termW, update.State{})
	h.m.insts = append(h.m.insts, prism.Instance{Dir: t.TempDir(), Name: "Vanilla"})
	h.m.newer = &selfupdate.Release{Version: "9.9.9"}
	h.m.showHome()
	if v := view(h.m); !strings.Contains(v, "enter play u update s settings b undo n new a all q quit v new version") {
		t.Errorf("home view %q: want the help with a all and v new version", v)
	}
}

func TestHomeEnterLaunchesSelectedInstance(t *testing.T) { // C5, C6, C14
	h := newHome(t, termW, update.State{})
	h.m.app.PrismExe = "/opt/prism/prismlauncher"
	h.m.showHome()
	cmd := press(h.m, keyEnter)
	if h.m.screen != scLaunching || cmd == nil {
		t.Fatalf("enter on home: screen %d, cmd nil %v; want scLaunching (%d) with a cmd", h.m.screen, cmd == nil, scLaunching)
	}
	msgOf[launchedMsg](t, cmd)
	want := launchCall{testLauncher, h.dir2, h.older, ""}
	if len(h.prism.launches) != 1 || h.prism.launches[0].dataDir != want.dataDir || h.prism.launches[0].inst.Dir != want.inst.Dir ||
		h.prism.launches[0].server != "" || h.prism.launches[0].l.Exe != testLauncher.Exe {
		t.Errorf("launches = %+v, want one %+v", h.prism.launches, want)
	}
	if len(h.prism.finds) != 1 || h.prism.finds[0] != (findCall{h.dir2, "/opt/prism/prismlauncher"}) {
		t.Errorf("findLauncher calls = %+v, want one with (%q, the PrismExe setting)", h.prism.finds, h.dir2)
	}
}

func TestHomePLaunchesLikeEnter(t *testing.T) { // C5
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	press(h.m, keyDown)
	msgOf[launchedMsg](t, press(h.m, runes("p")))
	if h.m.screen != scLaunching || len(h.prism.launches) != 1 || h.prism.launches[0].inst.Dir != h.newest.Dir || h.m.inst.Dir != h.newest.Dir {
		t.Errorf("p on Newest: screen %d, launches %+v; want scLaunching, Newest", h.m.screen, h.prism.launches)
	}
}

func TestHomeJJoinsSavedServer(t *testing.T) { // C5, C6, C14
	h := newHome(t, termW, update.State{ServerAddress: "mc.example.com:25565"})
	h.m.showHome()
	msgOf[launchedMsg](t, press(h.m, runes("j")))
	if h.m.screen != scLaunching || len(h.prism.launches) != 1 || h.prism.launches[0].server != "mc.example.com:25565" || h.prism.launches[0].inst.Dir != h.older.Dir {
		t.Errorf("j with a server: screen %d, launches %+v; want scLaunching, Older with server mc.example.com:25565", h.m.screen, h.prism.launches)
	}
}

func TestHomeJWithoutServerIsIgnored(t *testing.T) { // C5, C14, K1
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	cmd := press(h.m, runes("j"))
	if h.m.screen != scHome || cmd != nil || len(h.prism.finds)+len(h.prism.launches) != 0 || selectedKey(h.m) != h.older.Dir {
		t.Errorf("j without a server: screen %d, cmd nil %v, finds %d, launches %d, selected %q; want scHome, nil, 0, 0, Older",
			h.m.screen, cmd == nil, len(h.prism.finds), len(h.prism.launches), selectedKey(h.m))
	}
}

func TestHomeUGoesToVersionList(t *testing.T) { // C5
	h := newHome(t, termW, update.State{})
	h.m.serverModsAsked = true
	h.m.showHome()
	press(h.m, runes("u"))
	if h.m.screen != scTarget || h.m.inst.Dir != h.older.Dir || h.m.detect.Version != "2.8.1" {
		t.Errorf("u on Older: screen %d, inst %q, detected %q; want scTarget (%d), Older, 2.8.1", h.m.screen, h.m.inst.Name, h.m.detect.Version, scTarget)
	}
}

func TestHomeQQuits(t *testing.T) { // C5
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	cmd := press(h.m, runes("q"))
	if !h.m.quitting || !isQuit(cmd) {
		t.Errorf("q on home: quitting %v, quit cmd %v; want true, true", h.m.quitting, isQuit(cmd))
	}
}

func TestHomeVSelfUpdatesOnlyWithNewer(t *testing.T) { // C5, C14
	without := newHome(t, termW, update.State{})
	without.m.showHome()
	cmdWithout := press(without.m, runes("v"))
	with := newHome(t, termW, update.State{})
	with.m.newer = &selfupdate.Release{Version: "9.9.9"}
	with.m.showHome()
	cmdWith := press(with.m, runes("v"))
	if without.m.screen != scHome || cmdWithout != nil || with.m.screen != scSelfUpdate || cmdWith == nil {
		t.Errorf("v on home: without newer screen %d cmd nil %v, with newer screen %d cmd nil %v; want scHome nil, scSelfUpdate a cmd",
			without.m.screen, cmdWithout == nil, with.m.screen, cmdWith == nil)
	}
}

func TestHomeKeysGoToFilterWhileTyping(t *testing.T) { // C5
	h := newHome(t, termW, update.State{ServerAddress: "mc.x:1"})
	h.m.showHome()
	press(h.m, runes("/"), runes("p"), runes("j"), runes("q"))
	screen, quitting, filter, finds := h.m.screen, h.m.quitting, h.m.list.FilterValue(), len(h.prism.finds)
	press(h.m, keyEsc, runes("p")) // filter closed: p plays again
	if screen != scHome || quitting || filter != "pjq" || finds != 0 {
		t.Errorf("typing pjq into the filter: screen %d, quitting %v, filter %q, finds %d; want scHome, false, \"pjq\", 0",
			screen, quitting, filter, finds)
	}
	if h.m.screen != scLaunching {
		t.Errorf("p after closing the filter: screen %d, want scLaunching (%d)", h.m.screen, scLaunching)
	}
}

// ---- C12 is the game running? ----

func TestPickInstanceWhenGameRunsShowsError(t *testing.T) { // C12, C14
	m := routeModel(t)
	var asked []prism.Instance
	m.isRunning = func(in prism.Instance) (bool, error) { asked = append(asked, in); return true, nil }
	in := prism.Instance{Dir: t.TempDir(), Name: "Other", GTNH: true}
	_, cmd := m.pickInstance(in)
	press(m, msgOf[errMsg](t, cmd))
	want := "Other is running right now. Close Minecraft, then try again."
	if m.screen != scError || m.err.Error() != want || len(asked) != 1 || asked[0].Dir != in.Dir {
		t.Errorf("pickInstance while running: screen %d, err %v, asked %+v; want scError, %q, one call for Other", m.screen, m.err, asked, want)
	}
}

func TestPickInstanceWhenRunningIsUnknownWarnsOnConfirm(t *testing.T) { // C12, C14
	m := routeModel(t)
	m.isRunning = func(prism.Instance) (bool, error) { return false, errors.New("can't tell") }
	in := prism.Instance{Dir: t.TempDir(), Name: "Other", GTNH: true}
	writeState(t, in.Dir, update.State{Version: "2.8.4"})
	m.pickInstance(in)
	unknown := m.runUnknown
	m.screen, m.target = scConfirm, "2.9.0-RC-1"
	m.session = &update.Session{Plan: onePlan(), Flavor: manifest.Java17}
	if body := pageBody(m); !unknown || m.screen != scConfirm || !strings.Contains(body, "Make sure Minecraft is closed before you continue.") {
		t.Errorf("isRunning error: runUnknown %v, confirm body %q; want true and the close-Minecraft line", unknown, body)
	}
}

func TestPickInstanceResetsRunUnknown(t *testing.T) { // C12
	m := routeModel(t)
	m.runUnknown = true
	in := prism.Instance{Dir: t.TempDir(), Name: "Other", GTNH: true}
	writeState(t, in.Dir, update.State{Version: "2.8.4"})
	m.pickInstance(in)
	m.screen, m.target = scConfirm, "2.9.0-RC-1"
	m.session = &update.Session{Plan: onePlan(), Flavor: manifest.Java17}
	if body := pageBody(m); m.runUnknown || strings.Contains(body, "Make sure Minecraft is closed") {
		t.Errorf("isRunning ok after an unknown: runUnknown %v, body %q; want false, no close-Minecraft line", m.runUnknown, body)
	}
}

func TestConfirmCloseMinecraftLineOnlyWhenRunUnknown(t *testing.T) { // C12
	const line = "Make sure Minecraft is closed before you continue."
	build := func(unknown bool) string {
		m := goldenModel(t)
		m.screen, m.target = scConfirm, "2.8.4"
		m.session = &update.Session{Plan: onePlan(), Flavor: manifest.Java17}
		m.runUnknown = unknown
		return pageText(m)
	}
	withLine, without := build(true), build(false)
	if !strings.Contains(withLine, "just in case.\n\n  "+line+"\n") || strings.Contains(without, line) {
		t.Errorf("confirm page: runUnknown=true\n%s\nrunUnknown=false\n%s\nwant the line after the backup bullet only in the first", withLine, without)
	}
}

// ---- polish C3: wrapped home help ----
// Pair widths below: "a 123456" and "b 123456" are 8 columns, "b 1234567" is 9, "c x" is 3.

func TestHintRowsKeepsPairThatExactlyFillsTheRow(t *testing.T) { // polish C3, kill: > -> >=
	// 8 + 4 + 8 = 20 == limit: b stays on the first row; c (20 + 4 + 3) starts a new one.
	got := hintRows(20, "a", "123456", "b", "123456", "c", "x")
	want := hint("a", "123456", "b", "123456") + "\n" + hint("c", "x")
	if got != want {
		t.Errorf("hintRows(20, ...) = %q, want %q", got, want)
	}
}

func TestHintRowsWrapsPairOneColumnTooWide(t *testing.T) { // polish C3
	// 8 + 4 + 9 = 21 > 20: b starts a new row.
	got := hintRows(20, "a", "123456", "b", "1234567")
	want := hint("a", "123456") + "\n" + hint("b", "1234567")
	if got != want {
		t.Errorf("hintRows(20, ...) = %q, want %q", got, want)
	}
}

func TestHintRowsBelowTwentyIsOneRow(t *testing.T) { // polish C3: limit < 20 -> a single row
	got := hintRows(19, "a", "123456", "b", "1234567", "c", "x")
	want := hint("a", "123456", "b", "1234567", "c", "x")
	if got != want {
		t.Errorf("hintRows(19, ...) = %q, want the single row %q", got, want)
	}
}

func TestHintRowsFillsRowsLeftToRight(t *testing.T) { // polish C3
	got := hintRows(20, "a", "123456", "b", "123456", "c", "123456", "d", "123456", "e", "123456")
	want := hint("a", "123456", "b", "123456") + "\n" + hint("c", "123456", "d", "123456") + "\n" + hint("e", "123456")
	if got != want {
		t.Errorf("hintRows(20, five 8-column pairs) = %q, want %q", got, want)
	}
}

func TestHintRowsDropsTrailingUnpairedKeyLikeHint(t *testing.T) { // polish C3: renders pairs exactly like hint()
	got := hintRows(20, "a", "123456", "z")
	want := hint("a", "123456")
	if got != want {
		t.Errorf("hintRows(20, a, 123456, z) = %q, want %q (hint ignores a key without a label)", got, want)
	}
}

func TestHintRowsPlacesOverwidePairAloneOnItsRow(t *testing.T) { // polish C3: never split a pair; the first pair of a row always fits
	wide := "abcdefghijklmnopqrstuvwxyz" // "k " + 26 = 28 columns > 20
	got := hintRows(20, "a", "123456", "k", wide, "c", "x")
	want := hint("a", "123456") + "\n" + hint("k", wide) + "\n" + hint("c", "x")
	if got != want {
		t.Errorf("hintRows(20, ... over-wide pair ...) = %q, want %q", got, want)
	}
}

// fullHome is the home screen with every optional key: j (the selected instance has a
// server), a (a non-GTNH instance) and v (a newer launcher).
func fullHome(t *testing.T, width int) *home {
	t.Helper()
	h := newHome(t, width, update.State{ServerAddress: "mc.x:1"})
	h.m.insts = append(h.m.insts, prism.Instance{Dir: t.TempDir(), Name: "Vanilla"})
	h.m.newer = &selfupdate.Release{Version: "9.9.9"}
	h.m.showHome()
	return h
}

func TestHomeHelpWrapsAtWidth100(t *testing.T) { // polish C3: the full line is 101 columns, limit 94
	h := fullHome(t, 100)
	want := hint("enter", "play", "j", "join", "u", "update", "s", "settings", "b", "undo", "n", "new", "a", "all", "q", "quit") +
		"\n" + hint("v", "new version")
	if got := h.m.homeHelp(); got != want {
		t.Errorf("homeHelp() at width 100 = %q, want %q", got, want)
	}
}

func TestHomeHelpOneRowAtWidth120(t *testing.T) { // polish C3
	h := fullHome(t, 120)
	want := hint("enter", "play", "j", "join", "u", "update", "s", "settings", "b", "undo", "n", "new", "a", "all", "q", "quit", "v", "new version")
	if got := h.m.homeHelp(); got != want {
		t.Errorf("homeHelp() at width 120 = %q, want %q", got, want)
	}
}

func TestHomeListGivesARowToTheWrappedHelp(t *testing.T) { // polish C3, chrome C6: 30 - 2 (title, banner) - 2 (help) - 1 (blank)
	h := fullHome(t, 100)
	if got := h.m.listHeightFor(scHome); got != 25 {
		t.Errorf("listHeightFor(scHome) at 100x30 with a two-row help = %d, want 25", got)
	}
	if got := h.m.list.Height(); got != 25 {
		t.Errorf("home list height after showHome at 100x30 = %d, want 25 (size re-applied with the j key counted)", got)
	}
}

func TestHomeListHeightUnchangedWithOneRowHelp(t *testing.T) { // polish C3, chrome C6: 30 - 2 - 1 - 1
	h := fullHome(t, 120)
	if got := h.m.list.Height(); got != 26 {
		t.Errorf("home list height at 120x30 with a one-row help = %d, want 26", got)
	}
}

func TestHomeViewWithWrappedHelpFitsTheTerminal(t *testing.T) { // polish C3
	h := fullHome(t, 100)
	if n := strings.Count(h.m.View(), "\n") + 1; n > 30 {
		t.Errorf("home view at 100x30 with every optional key is %d lines, want at most 30", n)
	}
}
