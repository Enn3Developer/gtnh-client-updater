package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"pgregory.net/rapid"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Tests for the TUI chrome (chrome spec C1-C12): title bar, banner, key bar, panel,
// badge and button primitives, the one rendering path and the list sizes. Clause
// numbers in the comments refer to the chrome spec.

// ---- colour helpers ----

// SGR parameters of the spec's Kanagawa palette as lipgloss writes them in 24-bit
// colour (termenv rounds some channels, so they're derived, not hand-computed). Set by
// withTrueColor.
var fgAccent, bgAccent, fgDim, bgBar, bgChip, fgDark, bgBanner, fgOK, fgWarn, fgBad string

// sgrOf is the SGR parameter string style writes around "x" (e.g. "38;2;127;179;202").
func sgrOf(style lipgloss.Style) string {
	s := strings.TrimPrefix(style.Render("x"), "\x1b[")
	return s[:strings.IndexByte(s, 'm')]
}

func fgOf(hex string) string { return sgrOf(lipgloss.NewStyle().Foreground(lipgloss.Color(hex))) }
func bgOf(hex string) string { return sgrOf(lipgloss.NewStyle().Background(lipgloss.Color(hex))) }

// withTrueColor makes lipgloss write 24-bit colours for the rest of the test, so the
// styles can be checked; the profile is restored afterwards.
func withTrueColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	fgAccent, bgAccent = fgOf("#7FB4CA"), bgOf("#7FB4CA")
	fgDim, bgBar, bgChip = fgOf("#727169"), bgOf("#2A2A37"), bgOf("#363646")
	fgDark, bgBanner = fgOf("#1F1F28"), bgOf("#E6C384")
	fgOK, fgWarn, fgBad = fgOf("#98BB6C"), fgOf("#E6C384"), fgOf("#E46876")
}

// sgrParams lists the parameters of every SGR escape (ESC [ ... m) in s.
func sgrParams(s string) []string {
	var out []string
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return out
		}
		s = s[i+2:]
		j := strings.IndexByte(s, 'm')
		if j < 0 {
			return out
		}
		out = append(out, ";"+s[:j]+";")
		s = s[j+1:]
	}
}

// hasStyle reports whether a single SGR escape of s sets every one of codes (e.g. "1"
// for bold and fgAccent).
func hasStyle(s string, codes ...string) bool {
	for _, p := range sgrParams(s) {
		if !slices.ContainsFunc(codes, func(c string) bool { return !strings.Contains(p, ";"+c+";") }) {
			return true
		}
	}
	return false
}

// firstLine is the first line of s.
func firstLine(s string) string { return strings.Split(s, "\n")[0] }

// ---- C1 titleBar ----

func TestTitleBarPadsLeftAndRightToExactlyTheWidth(t *testing.T) { // C1
	out := titleBar("GTNH Launcher 1.2.3", "Pack › Settings", 60)
	want := " GTNH Launcher 1.2.3" + strings.Repeat(" ", 24) + "Pack › Settings "
	if strings.Contains(out, "\n") || ansi.StringWidth(out) != 60 || ansi.Strip(out) != want {
		t.Errorf("titleBar(..., 60) = %q (width %d), want one line %q", ansi.Strip(out), ansi.StringWidth(out), want)
	}
}

func TestTitleBarStylesLeftBoldAccentRightDimOnBarBackground(t *testing.T) { // C1
	withTrueColor(t)
	out := titleBar("Left", "Right", 40)
	if !hasStyle(out, "1", fgAccent) || !hasStyle(out, fgDim) || !strings.Contains(out, bgBar) || ansi.StringWidth(out) != 40 {
		t.Errorf("titleBar(Left, Right, 40) = %q; want bold accent left, dim right, the #2A2A37 background, 40 columns", out)
	}
}

func TestTitleBarTruncatesRightFirst(t *testing.T) { // C1
	out := ansi.Strip(titleBar("Left", "abcdefghijklmnopqrstuvwxyz", 20))
	if ansi.StringWidth(out) != 20 || !strings.HasPrefix(out, " Left") || !strings.HasSuffix(out, "… ") || strings.Contains(out, "z") {
		t.Errorf("titleBar(Left, 26 letters, 20) = %q (width %d), want \" Left\" kept and the right side cut with … , 20 wide", out, ansi.StringWidth(out))
	}
}

func TestTitleBarTruncatesLeftWhenItAloneDoesNotFit(t *testing.T) { // C1
	out := ansi.Strip(titleBar(strings.Repeat("L", 30), "R", 15))
	if ansi.StringWidth(out) != 15 || !strings.HasPrefix(out, " LLLL") || !strings.Contains(out, "…") || strings.Contains(out, "R") {
		t.Errorf("titleBar(30 L, R, 15) = %q (width %d), want the left cut with …, no right, 15 wide", out, ansi.StringWidth(out))
	}
}

// A left one column too wide for " <left> " (19 + 2 > 20) is cut, the right dropped.
func TestTitleBarTruncatesLeftOneColumnTooWideForItsMargins(t *testing.T) { // C1
	out := titleBar(strings.Repeat("L", 19), "R", 20)
	want := " " + strings.Repeat("L", 17) + "… "
	if ansi.Strip(out) != want || ansi.StringWidth(out) != 20 {
		t.Errorf("titleBar(19 L, R, 20) = %q (width %d), want %q", ansi.Strip(out), ansi.StringWidth(out), want)
	}
}

func TestTitleBarBelowTenColumnsIsJustTheTruncatedLeft(t *testing.T) { // C1: width < 10
	out := titleBar("GTNH Launcher", "Home", 9)
	if ansi.Strip(out) != "GTNH Lau…" || strings.Contains(out, "\n") {
		t.Errorf("titleBar(GTNH Launcher, Home, 9) = %q, want %q", ansi.Strip(out), "GTNH Lau…")
	}
}

func TestTitleBarAtTenColumnsIsAFullBar(t *testing.T) { // C1: width 10 is not < 10
	out := titleBar("ab", "cd", 10)
	if ansi.Strip(out) != " ab    cd " {
		t.Errorf("titleBar(ab, cd, 10) = %q, want %q", ansi.Strip(out), " ab    cd ")
	}
}

// ---- C2 crumb ----

func TestCrumbNamesInstanceAndArea(t *testing.T) { // C2
	cases := []struct {
		name     string
		screen   screen
		creating bool
		setting  string
		want     string
	}{
		{"home", scHome, false, "", "Home"},
		{"installed", scInstalled, false, "", "Pack › Update"},
		{"target", scTarget, false, "", "Pack › Update"},
		{"server mods", scServerMods, false, "", "Pack › Update"},
		{"preparing", scPreparing, false, "", "Pack › Update"},
		{"confirm", scConfirm, false, "", "Pack › Update"},
		{"applying", scApplying, false, "", "Pack › Update"},
		{"done", scDone, false, "", "Pack › Update"},
		{"target creating", scTarget, true, "", "New instance"},
		{"server mods creating", scServerMods, true, "", "New instance"},
		{"preparing creating", scPreparing, true, "", "New instance"},
		{"confirm creating", scConfirm, true, "", "New instance"},
		{"applying creating", scApplying, true, "", "New instance"},
		{"done creating", scDone, true, "", "New instance"},
		{"name", scName, true, "", "New instance"},
		{"conflicts", scConflicts, false, "", "Pack › Update › Config files"},
		{"resolve", scResolve, false, "", "Pack › Update › Config files"},
		{"settings", scSettings, false, "", "Pack › Settings"},
		{"setting edit memory", scSettingEdit, false, "memory", "Pack › Settings › Memory for the game"},
		{"setting edit prism", scSettingEdit, false, "prism", "Pack › Settings › Prism Launcher location"},
		{"backups", scBackups, false, "", "Pack › Undo"},
		{"restore confirm", scRestoreConfirm, false, "", "Pack › Undo"},
		{"restoring", scRestoring, false, "", "Pack › Undo"},
		{"restored", scRestored, false, "", "Pack › Undo"},
		{"launching", scLaunching, false, "", "Pack › Playing"},
		{"playing", scPlaying, false, "", "Pack › Playing"},
		{"self update", scSelfUpdate, false, "", "Launcher update"},
		{"self updated", scSelfUpdated, false, "", "Launcher update"},
		{"error", scError, false, "", "Problem"},
		{"loading", scLoading, false, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := sizedModel(t)
			m.inst = prism.Instance{Dir: t.TempDir(), Name: "Pack", GTNH: true}
			m.screen, m.creating, m.setting = c.screen, c.creating, c.setting
			if got := m.crumb(); got != c.want {
				t.Errorf("crumb() on %s = %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func TestCrumbOfNonGTNHInstanceIsAreaOnly(t *testing.T) { // C2
	m := sizedModel(t)
	m.inst = prism.Instance{Dir: t.TempDir(), Name: "Vanilla"}
	m.screen = scSettings
	if got := m.crumb(); got != "Settings" {
		t.Errorf("crumb() on settings of a non-GTNH instance = %q, want %q", got, "Settings")
	}
}

func TestCrumbWithoutInstanceNameIsAreaOnly(t *testing.T) { // C2
	m := sizedModel(t)
	m.inst = prism.Instance{Dir: t.TempDir(), GTNH: true}
	m.screen = scBackups
	if got := m.crumb(); got != "Undo" {
		t.Errorf("crumb() on undo of a nameless instance = %q, want %q", got, "Undo")
	}
}

// ---- C3 keyBar ----

func TestKeyBarReadsKeysAndLabels(t *testing.T) { // C3
	if got := words(keyBar(80, "enter", "play", "q", "quit")); got != "enter play q quit" {
		t.Errorf("keyBar(80, enter play q quit) reads %q, want %q", got, "enter play q quit")
	}
}

func TestKeyBarChipIsBoldAccentOnChipBackgroundWithoutPadding(t *testing.T) { // C3, D1
	withTrueColor(t)
	out := keyBar(80, "a", "123456")
	if !hasStyle(out, "1", fgAccent, bgChip) || !hasStyle(out, fgDim) || ansi.StringWidth(out) != 8 || ansi.Strip(out) != "a 123456" {
		t.Errorf("keyBar(80, a, 123456) = %q (width %d); want a bold accent chip on #363646, a dim label, 8 columns \"a 123456\"",
			out, ansi.StringWidth(out))
	}
}

// K1: limit 24 - 4 = 20; "a 123456    b 123456" is exactly 20 columns and stays on one row.
func TestKeyBarKeepsPairThatExactlyFillsTheRow(t *testing.T) { // C3, K1
	lines := strip(strings.Split(keyBar(24, "a", "123456", "b", "123456", "c", "x"), "\n"))
	want := []string{"a 123456    b 123456", "c x"}
	if !slices.Equal(lines, want) {
		t.Errorf("keyBar(24, a b c) rows = %q, want %q", lines, want)
	}
}

func TestKeyBarWrapsPairOneColumnTooWide(t *testing.T) { // C3
	lines := strip(strings.Split(keyBar(24, "a", "123456", "b", "1234567"), "\n"))
	want := []string{"a 123456", "b 1234567"}
	if !slices.Equal(lines, want) {
		t.Errorf("keyBar(24, a, b 1234567) rows = %q, want %q", lines, want)
	}
}

func TestKeyBarBelowTwentyFourColumnsIsOneRow(t *testing.T) { // C3: width-4 < 20
	got := ansi.Strip(keyBar(23, "a", "123456", "b", "1234567", "c", "x"))
	if want := "a 123456    b 1234567    c x"; got != want {
		t.Errorf("keyBar(23, ...) = %q, want the single row %q", got, want)
	}
}

func TestKeyBarDropsTrailingUnpairedKey(t *testing.T) { // C3
	if got := ansi.Strip(keyBar(80, "a", "123456", "z")); got != "a 123456" {
		t.Errorf("keyBar(80, a, 123456, z) = %q, want %q", got, "a 123456")
	}
}

func TestKeyBarIsHintRowsFourColumnsNarrower(t *testing.T) { // C3
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(0, 160).Draw(t, "width")
		pairs := rapid.SliceOfN(rapid.SampledFrom([]string{"enter", "q", "↑↓", "esc", "play", "new version", "keep all", "x"}), 0, 20).Draw(t, "pairs")
		if got, want := keyBar(width, pairs...), hintRows(width-4, pairs...); got != want {
			t.Fatalf("keyBar(%d, %q) = %q, want hintRows(%d, ...) = %q", width, pairs, got, width-4, want)
		}
	})
}

// ---- C4/C5 the one rendering path ----

func TestViewIsTheFrameOfThePageInTheChrome(t *testing.T) { // C5
	cases := []struct {
		name  string
		build func(t *testing.T) *model
	}{
		{"error", func(t *testing.T) *model {
			return goldenErrorModel(t, false, scPreparing, errors.New("I couldn't download the pack."))
		}},
		{"scrolled confirm", func(t *testing.T) *model {
			m := confirmModel(t)
			m.scroll = 3
			return m
		}},
		{"targets with banner", func(t *testing.T) *model {
			m := routeModel(t)
			m.showTargets()
			m.newer = &selfupdate.Release{Version: "9.9.9"}
			return m
		}},
		{"home with card", func(t *testing.T) *model {
			h := newHome(t, 100, update.State{})
			h.m.showHome()
			return h.m
		}},
		{"loading", func(t *testing.T) *model {
			m := goldenModel(t)
			m.screen = scLoading
			return m
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.build(t)
			body, footer, scroll := m.page()
			want := frame(m.width, m.height, titleBar(m.header(), m.crumb(), m.width), m.banner(), body, footer, scroll)
			if got := m.View(); got != want {
				t.Errorf("View() on %s differs from frame(page()) in the chrome\n--- got ---\n%s\n--- want ---\n%s", c.name, ansi.Strip(got), ansi.Strip(want))
			}
		})
	}
}

func TestChromeFramesBodyAndFooterUnderTitleBarAndBanner(t *testing.T) { // C5
	m := routeModel(t)
	m.screen = scHome
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	want := frame(m.width, m.height, titleBar(m.header(), m.crumb(), m.width), m.banner(), "b0\nb1", "F", 1)
	if got := m.chrome("b0\nb1", "F", 1); got != want {
		t.Errorf("chrome(b0/b1, F, 1) = %q, want %q", ansi.Strip(got), ansi.Strip(want))
	}
}

func TestTitleBarShowsAppAndCrumb(t *testing.T) { // C12
	t.Run("home", func(t *testing.T) {
		h := newHome(t, termW, update.State{})
		h.m.showHome()
		line := firstLine(h.m.View())
		want := " GTNH Launcher 1.2.3" + strings.Repeat(" ", termW-20-5) + "Home "
		if ansi.Strip(line) != want || ansi.StringWidth(line) != termW {
			t.Errorf("home first line = %q (width %d), want %q (width %d)", ansi.Strip(line), ansi.StringWidth(line), want, termW)
		}
	})
	t.Run("settings edit", func(t *testing.T) {
		m := routeModel(t)
		m.setting, m.screen = "memory", scSettingEdit
		line := firstLine(m.View())
		want := " GTNH Launcher 1.2.3" + strings.Repeat(" ", termW-20-38) + "Pack › Settings › Memory for the game "
		if ansi.Strip(line) != want || ansi.StringWidth(line) != termW {
			t.Errorf("settings edit first line = %q (width %d), want %q (width %d)", ansi.Strip(line), ansi.StringWidth(line), want, termW)
		}
	})
	t.Run("undo", func(t *testing.T) {
		m := routeModel(t)
		m.showBackups()
		line := firstLine(m.View())
		want := " GTNH Launcher 1.2.3" + strings.Repeat(" ", termW-20-12) + "Pack › Undo "
		if m.screen != scBackups || ansi.Strip(line) != want || ansi.StringWidth(line) != termW {
			t.Errorf("undo screen %d first line = %q (width %d), want scBackups and %q (width %d)", m.screen, ansi.Strip(line), ansi.StringWidth(line), want, termW)
		}
	})
}

func TestListScreensTurnTheBubblesHelpOff(t *testing.T) { // C5
	cases := []struct {
		name  string
		build func(t *testing.T) *model
	}{
		{"home", func(t *testing.T) *model {
			h := newHome(t, termW, update.State{})
			h.m.showHome()
			return h.m
		}},
		{"installed", func(t *testing.T) *model {
			m := routeModel(t)
			m.showInstalled()
			return m
		}},
		{"target", func(t *testing.T) *model {
			m := routeModel(t)
			m.showTargets()
			return m
		}},
		{"settings", func(t *testing.T) *model {
			f := settingsFixture(t, plainCfg, true)
			f.open("")
			return f.m
		}},
		{"conflicts", func(t *testing.T) *model {
			m := routeModel(t)
			prepared(t, m)
			return m
		}},
		{"resolve", func(t *testing.T) *model {
			m := routeModel(t)
			onResolve(t, m)
			return m
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if m := c.build(t); m.list.ShowHelp() {
				t.Errorf("list help shown on %s, want it off (the key bar replaces it)", c.name)
			}
		})
	}
}

func TestListTitleStartsAtColumnTwoWithoutPadding(t *testing.T) { // C5
	m := routeModel(t)
	m.showTargets()
	lines := strip(strings.Split(m.View(), "\n"))
	title := "Pack is on GTNH 2.8.4. Which version do you want?"
	if m.list.Styles.Title.GetPaddingLeft() != 0 || m.list.Styles.Title.GetPaddingRight() != 0 || !strings.HasPrefix(lines[2], "  "+title) {
		t.Errorf("title padding %d/%d, view line 3 %q; want 0/0 and the title at column 2", m.list.Styles.Title.GetPaddingLeft(),
			m.list.Styles.Title.GetPaddingRight(), lines[2])
	}
}

func TestTargetListFooterIsTheKeyBar(t *testing.T) { // C5, C8
	m := routeModel(t)
	m.showTargets()
	_, footer, _ := m.page()
	if got := words(footer); got != "↑↓ move i not this one m mods esc back / filter" {
		t.Errorf("target footer reads %q, want %q", got, "↑↓ move i not this one m mods esc back / filter")
	}
}

func TestHomeFooterIsTheHomeHelp(t *testing.T) { // C5, D3
	h := newHome(t, termW, update.State{})
	h.m.showHome()
	_, footer, _ := h.m.page()
	if footer != h.m.homeHelp() || words(footer) != "enter play u update s settings b undo n new q quit" {
		t.Errorf("home footer = %q, want homeHelp() reading %q", words(footer), "enter play u update s settings b undo n new q quit")
	}
}

// ---- C6 sizes ----

func TestTargetListSizeAt82x25(t *testing.T) { // C6: 25 - 2 - 1 - 1
	m := routeModel(t)
	m.showTargets()
	if m.list.Width() != 78 || m.list.Height() != 21 {
		t.Errorf("target list at 82x25 = %dx%d, want 78x21", m.list.Width(), m.list.Height())
	}
}

// At 40 columns the target key bar wraps into two rows (limit 36):
// "↑↓ move    i not this one    m mods" and "esc back    / filter".
func TestTargetListGivesARowToAWrappedKeyBar(t *testing.T) { // C6
	m := routeModel(t)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: termH})
	m.showTargets()
	_, footer, _ := m.page()
	if strings.Count(footer, "\n") != 1 || m.list.Width() != 36 || m.list.Height() != 20 {
		t.Errorf("target list at 40x25: footer rows %d, list %dx%d; want 2 rows, 36x20", strings.Count(footer, "\n")+1, m.list.Width(), m.list.Height())
	}
}

func TestListHeightAboveTheFloor(t *testing.T) { // C6: 10 - 2 - 1 - 1 = 6
	m := routeModel(t)
	m.Update(tea.WindowSizeMsg{Width: termW, Height: 10})
	m.showTargets()
	if m.list.Height() != 6 {
		t.Errorf("target list height at 82x10 = %d, want 6", m.list.Height())
	}
}

func TestListHeightFloorsAtFive(t *testing.T) { // C6: 8 - 2 - 1 - 1 = 4 -> 5
	m := routeModel(t)
	m.Update(tea.WindowSizeMsg{Width: termW, Height: 8})
	m.showTargets()
	if m.list.Height() != 5 {
		t.Errorf("target list height at 82x8 = %d, want 5", m.list.Height())
	}
}

func TestResizeReappliesTheListSize(t *testing.T) { // C6
	m := routeModel(t)
	m.showTargets()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if m.list.Width() != 96 || m.list.Height() != 36 {
		t.Errorf("target list after resizing to 100x40 = %dx%d, want 96x36", m.list.Width(), m.list.Height())
	}
}

func TestHomeListLeavesRoomForTheCard(t *testing.T) { // C6
	wide := fullHome(t, 100)
	narrow := newHome(t, 89, update.State{})
	narrow.m.showHome()
	if wide.m.list.Width() != 54 || narrow.m.list.Width() != 85 {
		t.Errorf("home list width at 100 / 89 columns = %d / %d, want 54 / 85", wide.m.list.Width(), narrow.m.list.Width())
	}
}

func TestSettingsListHeightAt82x25(t *testing.T) { // C6: one-row key bar
	f := settingsFixture(t, plainCfg, true)
	f.open("")
	if f.m.list.Height() != 21 {
		t.Errorf("settings list height at 82x25 = %d, want 21", f.m.list.Height())
	}
}

func TestBodyRowsLeaveRoomForChromeAndFooter(t *testing.T) { // C4, C6
	m := confirmModel(t)      // 82x25, one-row footer: 25 - 2 - 1 - 1
	h := fullHome(t, 100)     // 100x30, two-row help: 30 - 2 - 2 - 1
	loading := goldenModel(t) // 82x25, no footer: 25 - 2
	loading.screen = scLoading
	if m.bodyRows() != 21 || h.m.bodyRows() != 25 || loading.bodyRows() != 23 {
		t.Errorf("bodyRows: confirm %d, home %d, loading %d; want 21, 25, 23", m.bodyRows(), h.m.bodyRows(), loading.bodyRows())
	}
}

// manyReleasesManifest has n stable Java 17 releases 2.0.0 ... 2.<n-1>.0.
func manyReleasesManifest(t *testing.T, n int) *manifest.Manifest {
	t.Helper()
	var parts []string
	for i := range n {
		parts = append(parts, fmt.Sprintf(`"2.%d.0": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/%d.zip"}}`, i, i))
	}
	man, err := manifest.Parse([]byte("{" + strings.Join(parts, ",") + "}"))
	if err != nil {
		t.Fatal(err)
	}
	return man
}

var chromeScreenNames = []string{
	"home", "installed", "target 30 releases", "settings", "backups list", "backups empty", "conflicts", "resolve",
	"confirm update", "confirm create", "done update", "done create", "error", "applying", "preparing",
	"preparing cancelling", "server mods", "name", "setting edit", "restore confirm", "restored",
	"playing starting", "playing slow", "playing running", "playing closed", "playing unknown",
	"launching", "self update", "self updated", "loading",
}

// chromeScreen is a w x h model on the screen called name.
func chromeScreen(t *testing.T, name string, w, h int) *model {
	t.Helper()
	newer := &selfupdate.Release{Version: "9.9.9"}
	if name == "home" {
		hm := newHome(t, w, update.State{ServerAddress: "mc.x:1"})
		hm.m.insts = append(hm.m.insts, prism.Instance{Dir: t.TempDir(), Name: "Vanilla"})
		hm.m.newer = newer
		hm.m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		hm.m.showHome()
		return hm.m
	}
	if name == "settings" {
		f := settingsFixture(t, plainCfg, true)
		f.m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		f.open("")
		return f.m
	}
	m := routeModel(t)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	long := strings.Repeat("this warning is long ", 8)[:150]
	switch name {
	case "installed":
		m.showInstalled()
	case "target 30 releases":
		m.manifest = manyReleasesManifest(t, 30)
		m.newer = newer
		m.showTargets()
	case "backups list":
		writeBackup(t, m.inst.Dir, "backup-20260101-000000", downgradeInfo())
		m.showBackups()
	case "backups empty":
		m.showBackups()
	case "conflicts":
		prepared(t, m)
	case "resolve":
		onResolve(t, m)
	case "confirm update":
		m.screen, m.target = scConfirm, "2.9.0-RC-1"
		m.session = &update.Session{Plan: busyPlan()}
		m.warns = threeWarns()
	case "confirm create":
		m.creating, m.inst, m.target, m.newName = true, prism.Instance{}, "2.8.4", "My Pack"
		m.screen = scConfirm
		m.creation = &update.Creation{Dir: "My Pack", Files: 16234, Flavor: manifest.Java17}
	case "done update":
		m.screen, m.target = scDone, "2.9.0-RC-1"
		m.session = &update.Session{Plan: mixedPlan(1)}
		m.result = &update.Result{From: "2.8.4", To: "2.9.0-RC-1", BackupDir: "backup-20260101-120000",
			CustomErr: errors.New("the server answered with an error page")}
		m.warns = threeWarns()
	case "done create":
		m.creating, m.inst, m.screen = true, prism.Instance{}, scDone
		m.created = &update.CreateResult{Instance: prism.Instance{Name: "My Pack"}, Files: 16234}
	case "error":
		m.screen, m.errPhase = scError, scApplying
		m.err = errors.New(strings.Repeat("could not write a file ", 27)[:600])
	case "applying":
		m.screen, m.target = scApplying, "2.8.4"
		for i := range 8 {
			m.steps = append(m.steps, fmt.Sprintf("Finished step number %d", i))
		}
		m.step = "Writing files"
		m.warns = []string{long, long, long, long, long}
	case "preparing":
		m.screen, m.target = scPreparing, "2.9.0-RC-1"
		m.steps = []string{"Checking which version you have"}
		m.step, m.stepStart, m.done, m.total = "Comparing files", time.Now(), 4000, 16000
	case "preparing cancelling":
		m.creating, m.inst, m.target = true, prism.Instance{}, "2.8.4"
		m.screen, m.quitAfterCancel, m.step = scPreparing, true, "Downloading GTNH 2.8.4"
	case "server mods":
		m.askServerMods(true)
		m.inputEr = msgBadModsLink
	case "name":
		m.creating, m.inst, m.target = true, prism.Instance{}, "2.8.4"
		m.askName()
	case "setting edit":
		m.setting, m.screen = "memory", scSettingEdit
	case "restore confirm":
		m.backup = update.Backup{Dir: "backup-20260101-000000", Info: downgradeInfo()}
		m.screen = scRestoreConfirm
	case "restored":
		m.backup = update.Backup{Dir: "backup-20260101-000000", Info: downgradeInfo()}
		m.restored = &update.RestoreResult{From: "2.8.4", To: "2.8.1", MovedBack: 1}
		m.screen = scRestored
	case "playing starting", "playing slow", "playing running", "playing closed", "playing unknown":
		m.screen, m.playState = scPlaying, strings.TrimPrefix(name, "playing ")
		m.runningSince = time.Date(2026, 10, 3, 7, 5, 0, 0, time.UTC)
	case "launching":
		m.screen = scLaunching
	case "self update":
		m.newer, m.screen, m.step = newer, scSelfUpdate, "Downloading gtnh-update 9.9.9"
	case "self updated":
		m.newer, m.screen = newer, scSelfUpdated
	case "loading":
		m.screen = scLoading
	default:
		t.Fatalf("no chrome fixture called %q", name)
	}
	return m
}

func TestEveryScreenFitsTheTerminalAndShowsItsFooter(t *testing.T) { // C6, K2
	rapid.Check(t, func(rt *rapid.T) {
		w := rapid.IntRange(40, 160).Draw(rt, "width")
		h := rapid.IntRange(10, 50).Draw(rt, "height")
		name := rapid.SampledFrom(chromeScreenNames).Draw(rt, "screen")
		m := chromeScreen(t, name, w, h)

		out := m.View()
		if n := strings.Count(out, "\n") + 1; n > h {
			rt.Fatalf("%s at %dx%d: view has %d lines, want at most %d", name, w, h, n, h)
		}
		for i, l := range strings.Split(out, "\n") {
			if lw := ansi.StringWidth(l); lw > w {
				rt.Fatalf("%s at %dx%d: line %d is %d columns wide, want at most %d", name, w, h, i, lw, w)
			}
		}
		_, footer, _ := m.page()
		if f := strings.Fields(ansi.Strip(footer)); len(f) > 0 && !strings.Contains(ansi.Strip(out), f[0]) {
			rt.Fatalf("%s at %dx%d: view lacks the footer's first key %q:\n%s", name, w, h, f[0], ansi.Strip(out))
		}
	})
}

// ---- C7 banner ----

const bannerText = "A new version of this updater is out (9.9.9) — press v to get it" // 64 columns

func TestBannerOffersTheNewVersionOnTheVersionList(t *testing.T) { // C7
	m := routeModel(t)
	m.showTargets()
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	if got := ansi.Strip(m.banner()); got != " "+bannerText+" " {
		t.Errorf("banner() = %q, want %q", got, " "+bannerText+" ")
	}
}

func TestBannerIsDarkTextOnYellow(t *testing.T) { // C7
	withTrueColor(t)
	m := sizedModel(t)
	m.screen, m.newer = scHome, &selfupdate.Release{Version: "9.9.9"}
	if b := m.banner(); !hasStyle(b, fgDark, bgBanner) {
		t.Errorf("banner() = %q, want #1F1F28 text on #E6C384", b)
	}
}

func TestBannerFitsInWidthMinusFour(t *testing.T) { // C7: 68 - 4 = 64, the whole text
	m := sizedModel(t)
	m.Update(tea.WindowSizeMsg{Width: 68, Height: termH})
	m.screen, m.newer = scHome, &selfupdate.Release{Version: "9.9.9"}
	if got := ansi.Strip(m.banner()); got != " "+bannerText+" " {
		t.Errorf("banner() at 68 columns = %q, want %q", got, " "+bannerText+" ")
	}
}

func TestBannerIsTruncatedToWidthMinusFour(t *testing.T) { // C7: 66 - 4 = 62 columns
	m := sizedModel(t)
	m.Update(tea.WindowSizeMsg{Width: 66, Height: termH})
	m.screen, m.newer = scHome, &selfupdate.Release{Version: "9.9.9"}
	want := " A new version of this updater is out (9.9.9) — press v to get… "
	if got := ansi.Strip(m.banner()); got != want {
		t.Errorf("banner() at 66 columns = %q, want %q", got, want)
	}
}

func TestBannerOnlyOnScreensWhereVWorks(t *testing.T) { // C7, D2
	cases := []struct {
		screen screen
		shown  bool
	}{
		{scHome, true}, {scInstalled, true}, {scTarget, true},
		{scSettings, false}, {scSettingEdit, false}, {scBackups, false}, {scRestoreConfirm, false},
		{scConflicts, false}, {scResolve, false}, {scConfirm, false}, {scPreparing, false},
		{scApplying, false}, {scDone, false}, {scError, false}, {scServerMods, false}, {scName, false},
		{scPlaying, false}, {scLaunching, false}, {scSelfUpdate, false}, {scSelfUpdated, false}, {scLoading, false},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.screen), func(t *testing.T) {
			m := sizedModel(t)
			m.screen, m.newer = c.screen, &selfupdate.Release{Version: "9.9.9"}
			if got := m.banner(); (got != "") != c.shown || (c.shown && !strings.Contains(ansi.Strip(got), "press v")) {
				t.Errorf("banner() on screen %d = %q, want shown %v", c.screen, ansi.Strip(got), c.shown)
			}
		})
	}
}

func TestBannerEmptyWithoutNewerVersion(t *testing.T) { // C7
	m := sizedModel(t)
	m.screen = scTarget
	if got := m.banner(); got != "" {
		t.Errorf("banner() without a newer version = %q, want empty", got)
	}
}

func TestViewLineTwoIsTheBannerOrBlank(t *testing.T) { // C7
	with := routeModel(t)
	with.showTargets()
	with.newer = &selfupdate.Release{Version: "9.9.9"}
	without := routeModel(t)
	without.showTargets()
	gotWith := ansi.Strip(strings.Split(with.View(), "\n")[1])
	gotWithout := ansi.Strip(strings.Split(without.View(), "\n")[1])
	if gotWith != " "+bannerText+" " || strings.TrimSpace(gotWithout) != "" {
		t.Errorf("view line 2 with a newer version %q, without %q; want the banner, then blank", gotWith, gotWithout)
	}
}

// ---- C8 listKeyPairs ----

func TestListKeyPairsOnTheVersionList(t *testing.T) { // C8
	m := routeModel(t)
	m.showTargets()
	want := []string{"↑↓", "move", "i", "not this one", "m", "mods", "esc", "back", "/", "filter"}
	if got := m.listKeyPairs(); !slices.Equal(got, want) {
		t.Errorf("listKeyPairs() on versions = %q, want %q", got, want)
	}
}

func TestListKeyPairsOfferNewVersionOnTheVersionList(t *testing.T) { // C8, D2
	m := routeModel(t)
	m.showTargets()
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	want := []string{"↑↓", "move", "i", "not this one", "m", "mods", "esc", "back", "/", "filter", "v", "new version"}
	if got := m.listKeyPairs(); !slices.Equal(got, want) {
		t.Errorf("listKeyPairs() on versions with a newer launcher = %q, want %q", got, want)
	}
}

func TestListKeyPairsOnTheCreateVersionList(t *testing.T) { // C8
	m := routeModel(t)
	m.startCreate()
	want := []string{"↑↓", "move", "m", "mods", "esc", "back", "/", "filter"}
	if got := m.listKeyPairs(); m.screen != scTarget || !slices.Equal(got, want) {
		t.Errorf("listKeyPairs() on create versions (screen %d) = %q, want %q", m.screen, got, want)
	}
}

func TestListKeyPairsOnTheCreateVersionListWithoutInstances(t *testing.T) { // C8: esc only with instances
	m := routeModel(t)
	m.insts = nil
	m.startCreate()
	want := []string{"↑↓", "move", "m", "mods", "/", "filter"}
	if got := m.listKeyPairs(); m.screen != scTarget || !slices.Equal(got, want) {
		t.Errorf("listKeyPairs() on create versions without instances (screen %d) = %q, want %q", m.screen, got, want)
	}
}

func TestListKeyPairsOnTheInstalledListWithNewVersion(t *testing.T) { // C8
	m := routeModel(t)
	m.showInstalled()
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	want := []string{"↑↓", "move", "esc", "back", "/", "filter", "v", "new version"}
	if got := m.listKeyPairs(); !slices.Equal(got, want) {
		t.Errorf("listKeyPairs() on installed with a newer launcher = %q, want %q", got, want)
	}
}

func TestListKeyPairsWithOneItemHaveNoFilter(t *testing.T) { // C8: filter only with more than 1 item
	m := routeModel(t)
	m.manifest = manyReleasesManifest(t, 1)
	m.showInstalled()
	want := []string{"↑↓", "move", "esc", "back"}
	if got := m.listKeyPairs(); len(m.list.Items()) != 1 || !slices.Equal(got, want) {
		t.Errorf("listKeyPairs() on a %d-item installed list = %q, want %q", len(m.list.Items()), got, want)
	}
}

func TestListKeyPairsOnSettingsNeverOfferNewVersion(t *testing.T) { // C8, D2
	f := settingsFixture(t, plainCfg, true)
	f.m.newer = &selfupdate.Release{Version: "9.9.9"}
	f.open("")
	want := []string{"↑↓", "move", "enter", "change", "esc", "back", "q", "quit", "/", "filter"}
	if got := f.m.listKeyPairs(); !slices.Equal(got, want) {
		t.Errorf("listKeyPairs() on settings = %q, want %q", got, want)
	}
}

func TestListKeyPairsOnUndoListWithOneBackup(t *testing.T) { // C8
	m, _ := undoModel(t, termW, downgradeInfo())
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	press(m, runes("b"))
	want := []string{"↑↓", "move", "enter", "choose", "esc", "back"}
	if got := m.listKeyPairs(); m.screen != scBackups || !slices.Equal(got, want) {
		t.Errorf("listKeyPairs() on undo with one backup (screen %d) = %q, want %q", m.screen, got, want)
	}
}

func TestListKeyPairsOnUndoListWithTwoBackupsOfferFilter(t *testing.T) { // C8
	older := update.BackupInfo{From: "2.8.0", To: "2.8.1"}
	m, _ := undoModel(t, termW, older, downgradeInfo())
	press(m, runes("b"))
	want := []string{"↑↓", "move", "enter", "choose", "esc", "back", "/", "filter"}
	if got := m.listKeyPairs(); m.screen != scBackups || !slices.Equal(got, want) {
		t.Errorf("listKeyPairs() on undo with two backups (screen %d) = %q, want %q", m.screen, got, want)
	}
}

func TestListKeyPairsOnConflictsNeverOfferNewVersion(t *testing.T) { // C8, D2
	m := routeModel(t)
	prepared(t, m)
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	want := []string{"↑↓", "move", "enter", "choose", "esc", "back", "/", "filter"}
	if got := m.listKeyPairs(); !slices.Equal(got, want) {
		t.Errorf("listKeyPairs() on conflicts = %q, want %q", got, want)
	}
}

func TestListKeyPairsOnFileByFileIncludeKeepAll(t *testing.T) { // C8
	m := routeModel(t)
	onResolve(t, m)
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	want := []string{"↑↓", "move", "space", "switch", "n", "all new", "k", "keep all", "enter", "done", "esc", "back", "/", "filter"}
	if got := m.listKeyPairs(); !slices.Equal(got, want) {
		t.Errorf("listKeyPairs() on file by file = %q, want %q", got, want)
	}
}

// ---- C10 buttons ----

func TestButtonsRenderLabelsAsChipsTwoSpacesApart(t *testing.T) { // C10
	if got := ansi.Strip(buttons([]string{"A", "B"}, 0)); got != "[ A ]  [ B ]" {
		t.Errorf("buttons([A B], 0) = %q, want %q", got, "[ A ]  [ B ]")
	}
}

func TestButtonsHighlightExactlyTheSelectedChip(t *testing.T) { // C10
	withTrueColor(t)
	out := buttons([]string{"Alpha", "Beta", "Gamma"}, 1)
	sel := strings.Index(out, bgAccent)
	if strings.Count(out, bgAccent) != 1 || sel < strings.Index(out, "Alpha") || sel > strings.Index(out, "Beta") ||
		!hasStyle(out, "1", fgDark, bgAccent) || strings.Count(out, bgChip) < 2 {
		t.Errorf("buttons([Alpha Beta Gamma], 1) = %q; want only Beta bold #1F1F28 on accent, the others on #363646", out)
	}
}

func TestButtonsClampNegativeSelectionToTheFirst(t *testing.T) { // C10
	withTrueColor(t)
	out := buttons([]string{"Alpha", "Beta", "Gamma"}, -1)
	sel := strings.Index(out, bgAccent)
	if strings.Count(out, bgAccent) != 1 || sel < 0 || sel > strings.Index(out, "Alpha") {
		t.Errorf("buttons(..., -1) = %q; want only Alpha highlighted", out)
	}
}

func TestButtonsClampLargeSelectionToTheLast(t *testing.T) { // C10
	withTrueColor(t)
	out := buttons([]string{"Alpha", "Beta", "Gamma"}, 99)
	sel := strings.Index(out, bgAccent)
	if strings.Count(out, bgAccent) != 1 || sel < strings.Index(out, "Beta") || sel > strings.Index(out, "Gamma") {
		t.Errorf("buttons(..., 99) = %q; want only Gamma highlighted", out)
	}
}

func TestButtonsWithoutLabelsAreEmpty(t *testing.T) { // C10
	if got := buttons(nil, 0); got != "" {
		t.Errorf("buttons(nil, 0) = %q, want empty", got)
	}
}

func TestScreenChangeResetsSelectedButtonAndScroll(t *testing.T) { // C10
	m := confirmModel(t)
	press(m, keyDown)
	m.btn = 3
	press(m, keyEsc)
	if m.screen != scTarget || m.btn != 0 || m.scroll != 0 {
		t.Errorf("esc from confirm with btn 3: screen %d, btn %d, scroll %d; want scTarget (%d), 0, 0", m.screen, m.btn, m.scroll, scTarget)
	}
}

func TestButtonSelectionSurvivesKeysThatKeepTheScreen(t *testing.T) { // C10
	m := confirmModel(t)
	m.btn = 2
	press(m, keyDown)
	if m.screen != scConfirm || m.btn != 2 || m.scroll != 1 {
		t.Errorf("down on confirm with btn 2: screen %d, btn %d, scroll %d; want scConfirm, 2, 1", m.screen, m.btn, m.scroll)
	}
}

// ---- C11 panel and badge ----

func TestPanelBoxesTitleAndBodyLinesInOrder(t *testing.T) { // C11
	lines := strip(strings.Split(panel("Info", "a\nbb", 20), "\n"))
	want := []string{
		"╭─ Info " + strings.Repeat("─", 11) + "╮",
		"│ a" + strings.Repeat(" ", 16) + "│",
		"│ bb" + strings.Repeat(" ", 15) + "│",
		"╰" + strings.Repeat("─", 18) + "╯",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("panel(Info, a/bb, 20) = %q, want %q", lines, want)
	}
}

func TestPanelWithoutTitleHasPlainTopBorder(t *testing.T) { // C11
	top := ansi.Strip(firstLine(panel("", "a", 20)))
	if want := "╭" + strings.Repeat("─", 18) + "╮"; top != want {
		t.Errorf("panel(\"\", a, 20) top = %q, want %q", top, want)
	}
}

func TestPanelTitleNarrowerThanWidthMinusFourIsShown(t *testing.T) { // C11: 15 < 20 - 4
	top := firstLine(panel("abcdefghijklmno", "a", 20))
	if s := ansi.Strip(top); !strings.HasPrefix(s, "╭─ abcdefghijklmno") || !strings.HasSuffix(s, "╮") || ansi.StringWidth(top) != 20 {
		t.Errorf("panel(15-col title, a, 20) top = %q (width %d), want the title in the border, 20 wide", s, ansi.StringWidth(top))
	}
}

func TestPanelTitleOfWidthMinusFourIsLeftOut(t *testing.T) { // C11: 16 is not < 20 - 4
	top := ansi.Strip(firstLine(panel("abcdefghijklmnop", "a", 20)))
	if want := "╭" + strings.Repeat("─", 18) + "╮"; top != want {
		t.Errorf("panel(16-col title, a, 20) top = %q, want the plain border %q", top, want)
	}
}

func TestPanelBorderIsDimAndTitleBold(t *testing.T) { // C11
	withTrueColor(t)
	out := panel("Info", "a", 20)
	if !hasStyle(out, fgDim) || !hasStyle(out, "1") {
		t.Errorf("panel(Info, a, 20) = %q, want a dim border and a bold title", out)
	}
}

func TestPanelLinesAreExactlyTheWidth(t *testing.T) { // C11
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(6, 80).Draw(t, "width")
		title := rapid.SampledFrom([]string{"", "Info", "A much longer panel title", "x"}).Draw(t, "title")
		body := rapid.SliceOfN(rapid.SampledFrom([]string{"a", "word", "two words", "", "a rather long line of text that wraps"}), 1, 6).Draw(t, "body")
		for i, l := range strings.Split(panel(title, strings.Join(body, "\n"), width), "\n") {
			if w := ansi.StringWidth(l); w != width {
				t.Fatalf("panel(%q, %q, %d) line %d %q is %d columns, want %d", title, body, width, i, ansi.Strip(l), w, width)
			}
		}
	})
}

func TestBadgeSymbolsPerKind(t *testing.T) { // C11
	got := []string{
		ansi.Strip(badge(badgeOK, "ready")), ansi.Strip(badge(badgeWarn, "ready")),
		ansi.Strip(badge(badgeDim, "ready")), ansi.Strip(badge(badgeBad, "ready")),
	}
	want := []string{"● ready", "▲ ready", "○ ready", "✕ ready"}
	if !slices.Equal(got, want) {
		t.Errorf("badges ok/warn/dim/bad = %q, want %q", got, want)
	}
}

func TestBadgeColoursPerKind(t *testing.T) { // C11
	withTrueColor(t)
	ok, warn, dim, bad := badge(badgeOK, "x"), badge(badgeWarn, "x"), badge(badgeDim, "x"), badge(badgeBad, "x")
	if ok == bad || !hasStyle(ok, fgOK) || !hasStyle(warn, fgWarn) || !hasStyle(dim, fgDim) || !hasStyle(bad, fgBad) {
		t.Errorf("badges ok %q, warn %q, dim %q, bad %q; want the ok, warn, dim and bad colours", ok, warn, dim, bad)
	}
}
