package tui

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"pgregory.net/rapid"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// strip removes ANSI escapes from every line so markers can be compared as text.
func strip(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

func tenLines() []string {
	return []string{"l0", "l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9"}
}

// ---- bodyWindow ----

func TestBodyWindowReturnsWholeBodyWhenItFits(t *testing.T) { // C1
	body := []string{"a", "b", "c"}
	visible, offset := bodyWindow(body, 3, 0)
	if !slices.Equal(visible, body) || offset != 0 {
		t.Errorf("bodyWindow(%q, 3, 0) = %q, %d, want %q, 0", body, visible, offset, body)
	}
}

func TestBodyWindowIgnoresScrollWhenBodyFits(t *testing.T) { // C1
	body := []string{"a", "b"}
	visible, offset := bodyWindow(body, 5, 7)
	if !slices.Equal(visible, body) || offset != 0 {
		t.Errorf("bodyWindow(%q, 5, 7) = %q, %d, want %q, 0", body, visible, offset, body)
	}
}

func TestBodyWindowNilBodyGivesEmptyVisible(t *testing.T) { // C1
	visible, offset := bodyWindow(nil, 4, 0)
	if len(visible) != 0 || offset != 0 {
		t.Errorf("bodyWindow(nil, 4, 0) = %q, %d, want [], 0", visible, offset)
	}
}

func TestBodyWindowTreatsZeroRowsAsOneForSingleLine(t *testing.T) { // C1
	body := []string{"only"}
	visible, offset := bodyWindow(body, 0, 0)
	if !slices.Equal(visible, body) || offset != 0 {
		t.Errorf("bodyWindow(%q, 0, 0) = %q, %d, want %q, 0", body, visible, offset, body)
	}
}

func TestBodyWindowTreatsNegativeRowsAsOne(t *testing.T) { // C1, C2
	visible, offset := bodyWindow([]string{"a", "b", "c"}, -3, 0)
	want := []string{"↓ 2 more (↑/↓ to scroll)"}
	if got := strip(visible); !slices.Equal(got, want) || offset != 0 {
		t.Errorf("bodyWindow(3 lines, -3, 0) = %q, %d, want %q, 0", got, offset, want)
	}
}

func TestBodyWindowAtTopShowsOnlyBottomMarker(t *testing.T) { // C2, K1
	visible, offset := bodyWindow(tenLines(), 4, 0)
	want := []string{"l0", "l1", "l2", "↓ 6 more (↑/↓ to scroll)"}
	if got := strip(visible); !slices.Equal(got, want) || offset != 0 {
		t.Errorf("bodyWindow(10 lines, 4, 0) = %q, %d, want %q, 0", got, offset, want)
	}
}

func TestBodyWindowAtBottomShowsOnlyTopMarker(t *testing.T) { // C2
	maxOff := 6
	visible, offset := bodyWindow(tenLines(), 4, maxOff)
	want := []string{"↑ 6 more", "l7", "l8", "l9"}
	if got := strip(visible); !slices.Equal(got, want) || offset != 6 {
		t.Errorf("bodyWindow(10 lines, 4, 6) = %q, %d, want %q, 6", got, offset, want)
	}
}

func TestBodyWindowInMiddleShowsBothMarkers(t *testing.T) { // C2
	visible, offset := bodyWindow(tenLines(), 4, 3)
	want := []string{"↑ 3 more", "l4", "l5", "↓ 3 more (↑/↓ to scroll)"}
	if got := strip(visible); !slices.Equal(got, want) || offset != 3 {
		t.Errorf("bodyWindow(10 lines, 4, 3) = %q, %d, want %q, 3", got, offset, want)
	}
}

func TestBodyWindowOneBeforeBottomStillShowsBottomMarker(t *testing.T) { // C2, K2
	visible, offset := bodyWindow(tenLines(), 4, 5)
	want := []string{"↑ 5 more", "l6", "l7", "↓ 1 more (↑/↓ to scroll)"}
	if got := strip(visible); !slices.Equal(got, want) || offset != 5 {
		t.Errorf("bodyWindow(10 lines, 4, 5) = %q, %d, want %q, 5", got, offset, want)
	}
}

func TestBodyWindowClampsScrollOnePastBottom(t *testing.T) { // C2, K2
	visible, offset := bodyWindow(tenLines(), 4, 7)
	want := []string{"↑ 6 more", "l7", "l8", "l9"}
	if got := strip(visible); !slices.Equal(got, want) || offset != 6 {
		t.Errorf("bodyWindow(10 lines, 4, 7) = %q, %d, want %q, 6", got, offset, want)
	}
}

func TestBodyWindowClampsHugeScrollToBottom(t *testing.T) { // C2
	visible, offset := bodyWindow(tenLines(), 4, 1000)
	want := []string{"↑ 6 more", "l7", "l8", "l9"}
	if got := strip(visible); !slices.Equal(got, want) || offset != 6 {
		t.Errorf("bodyWindow(10 lines, 4, 1000) = %q, %d, want %q, 6", got, offset, want)
	}
}

func TestBodyWindowClampsNegativeScrollToTop(t *testing.T) { // C2
	visible, offset := bodyWindow(tenLines(), 4, -5)
	want := []string{"l0", "l1", "l2", "↓ 6 more (↑/↓ to scroll)"}
	if got := strip(visible); !slices.Equal(got, want) || offset != 0 {
		t.Errorf("bodyWindow(10 lines, 4, -5) = %q, %d, want %q, 0", got, offset, want)
	}
}

func TestBodyWindowOneRowAtTopIsBottomMarker(t *testing.T) { // C2 rows == 1
	visible, offset := bodyWindow([]string{"a", "b", "c"}, 1, 0)
	want := []string{"↓ 2 more (↑/↓ to scroll)"}
	if got := strip(visible); !slices.Equal(got, want) || offset != 0 {
		t.Errorf("bodyWindow(3 lines, 1, 0) = %q, %d, want %q, 0", got, offset, want)
	}
}

func TestBodyWindowOneRowAtBottomIsTopMarker(t *testing.T) { // C2 rows == 1
	visible, offset := bodyWindow([]string{"a", "b", "c"}, 1, 2)
	want := []string{"↑ 2 more"}
	if got := strip(visible); !slices.Equal(got, want) || offset != 2 {
		t.Errorf("bodyWindow(3 lines, 1, 2) = %q, %d, want %q, 2", got, offset, want)
	}
}

// ---- frame ----

func frameLines(out string) []string { return strip(strings.Split(out, "\n")) }

func TestFrameLaysOutTitleBannerBodyAndFooter(t *testing.T) { // chrome C4
	lines := frameLines(frame(40, 10, "TITLE", "BANNER", "b0\nb1", "F", 0))
	want := []string{"TITLE", "BANNER", "  b0", "  b1", "", "  F"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 10, TITLE, BANNER, b0/b1, F, 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameWithoutBannerLeavesLineTwoBlank(t *testing.T) { // chrome C4
	lines := frameLines(frame(40, 10, "TITLE", "", "b0", "F", 0))
	want := []string{"TITLE", "", "  b0", "", "  F"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 10, TITLE, \"\", b0, F, 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameIndentsEveryFooterLine(t *testing.T) { // chrome C4
	lines := frameLines(frame(40, 10, "T", "", "b0", "F1\nF2", 0))
	want := []string{"T", "", "  b0", "", "  F1", "  F2"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 10, T, \"\", b0, F1/F2, 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameWithoutFooterHasNoBlankLineAfterBody(t *testing.T) { // chrome C4
	lines := frameLines(frame(40, 10, "T", "", "b0\nb1", "", 0))
	want := []string{"T", "", "  b0", "  b1"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 10, T, \"\", b0/b1, \"\", 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameHasNoTrailingNewline(t *testing.T) { // chrome C4
	lines := frameLines(frame(40, 10, "T", "B", "b", "F", 0))
	if last := lines[len(lines)-1]; last != "  F" {
		t.Errorf("frame(...) last line = %q, want the footer %q (no trailing newline)", last, "  F")
	}
}

func TestFrameEmptyBodyAndFooterIsTitleAndBlankLine(t *testing.T) { // chrome C4
	lines := frameLines(frame(40, 10, "T", "", "", "", 0))
	want := []string{"T", ""}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 10, T, \"\", \"\", \"\", 0) lines = %q, want %q", lines, want)
	}
}

// K2: 8 rows - title - banner - blank - footer leave 4 body rows; a 10-line body fills
// exactly 8 lines with the footer on the last one.
func TestFramePinsFooterToLastLineWhenBodyOverflows(t *testing.T) { // chrome C4, K2
	body := strings.Join(tenLines(), "\n")
	lines := frameLines(frame(40, 8, "T", "", body, "F", 0))
	want := []string{"T", "", "  l0", "  l1", "  l2", "  ↓ 6 more (↑/↓ to scroll)", "", "  F"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 8, T, \"\", 10 lines, F, 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameBodyOneLineLongerThanRowsStillGivesExactlyHeightLines(t *testing.T) { // chrome C4, K2
	body := "a\nb\nc\nd\ne" // 5 lines, 4 rows at height 8 with a one-line footer
	lines := frameLines(frame(40, 8, "T", "", body, "F", 0))
	want := []string{"T", "", "  a", "  b", "  c", "  ↓ 1 more (↑/↓ to scroll)", "", "  F"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 8, T, \"\", 5 lines, F, 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameBodyThatExactlyFitsIsNotWindowed(t *testing.T) { // chrome C4
	body := "a\nb\nc\nd" // 4 lines, 4 rows at height 8 with a one-line footer
	lines := frameLines(frame(40, 8, "T", "", body, "F", 0))
	want := []string{"T", "", "  a", "  b", "  c", "  d", "", "  F"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 8, T, \"\", 4 lines, F, 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameTwoRowFooterTakesTwoBodyRows(t *testing.T) { // chrome C4
	body := strings.Join(tenLines(), "\n")
	lines := frameLines(frame(40, 9, "T", "B", body, "F1\nF2", 0))
	want := []string{"T", "B", "  l0", "  l1", "  l2", "  ↓ 6 more (↑/↓ to scroll)", "", "  F1", "  F2"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 9, T, B, 10 lines, F1/F2, 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameWithoutFooterGivesTheBlankLineToTheBody(t *testing.T) { // chrome C4
	body := strings.Join(tenLines(), "\n")
	lines := frameLines(frame(40, 6, "T", "", body, "", 0))
	want := []string{"T", "", "  l0", "  l1", "  l2", "  ↓ 6 more (↑/↓ to scroll)"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 6, T, \"\", 10 lines, \"\", 0) lines = %q, want %q", lines, want)
	}
}

func TestFramePassesScrollToBody(t *testing.T) { // chrome C4
	body := strings.Join(tenLines(), "\n")
	lines := frameLines(frame(40, 8, "T", "", body, "F", 2))
	want := []string{"T", "", "  ↑ 2 more", "  l3", "  l4", "  ↓ 4 more (↑/↓ to scroll)", "", "  F"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 8, T, \"\", 10 lines, F, 2) lines = %q, want %q", lines, want)
	}
}

func TestFrameMaxScrollShowsEndOfBody(t *testing.T) { // chrome C4
	body := strings.Join(tenLines(), "\n")
	lines := frameLines(frame(40, 8, "T", "", body, "F", math.MaxInt32))
	want := []string{"T", "", "  ↑ 6 more", "  l7", "  l8", "  l9", "", "  F"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 8, T, \"\", 10 lines, F, MaxInt32) lines = %q, want %q", lines, want)
	}
}

func TestFrameShowsOneBodyRowAtTheSmallestHeight(t *testing.T) { // chrome C4: 5 - 2 - 1 - 1 = 1 row
	lines := frameLines(frame(40, 5, "T", "", "a\nb\nc", "F", 0))
	want := []string{"T", "", "  ↓ 2 more (↑/↓ to scroll)", "", "  F"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(40, 5, T, \"\", 3 lines, F, 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameKeepsLineOfExactlyWidthColumns(t *testing.T) { // chrome C4
	lines := frameLines(frame(10, 10, "", "", "abcdefgh", "", 0))
	want := []string{"", "", "  abcdefgh"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(10, 10, \"\", \"\", abcdefgh, \"\", 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameClipsLineOneColumnTooWide(t *testing.T) { // chrome C4
	lines := frameLines(frame(10, 10, "", "", "abcdefghi", "", 0))
	want := []string{"", "", "  abcdefg…"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(10, 10, \"\", \"\", abcdefghi, \"\", 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameClipsFooterToWidth(t *testing.T) { // chrome C4
	lines := frameLines(frame(10, 10, "", "", "b", "abcdefghi", 0))
	want := []string{"", "", "  b", "", "  abcdefg…"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(10, 10, \"\", \"\", b, abcdefghi, 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameClipsTitleAndBannerToWidth(t *testing.T) { // chrome C4
	lines := frameLines(frame(9, 10, "界界界界界界", "abcdefghijk", "b", "", 0))
	want := []string{"界界界界…", "abcdefgh…", "  b"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(9, 10, 12-col wide title, 11-col banner, b, \"\", 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameMeasuresStyledLinesByVisibleWidth(t *testing.T) { // chrome C4
	styled := "\x1b[1;31mabcdefgh\x1b[0m" // 8 visible columns, many more bytes
	lines := frameLines(frame(10, 10, "", "", styled, "", 0))
	want := []string{"", "", "  abcdefgh"}
	if !slices.Equal(lines, want) {
		t.Errorf("frame(10, 10, \"\", \"\", styled 8 cols, \"\", 0) lines = %q, want %q", lines, want)
	}
}

func TestFrameClipsStyledLineToVisibleWidth(t *testing.T) { // chrome C4
	styled := "\x1b[1;31m" + strings.Repeat("x", 30) + "\x1b[0m"
	out := frame(20, 10, "", "", styled, "", 0)
	raw := strings.Split(out, "\n")
	last := raw[len(raw)-1]
	if w := ansi.StringWidth(last); w != 20 || ansi.Strip(last) != "  "+strings.Repeat("x", 17)+"…" {
		t.Errorf("frame(20, ..., styled 30 cols, ...) last line = %q (width %d), want 20 wide ending in …", last, w)
	}
}

// genLine builds a non-empty line from plain text, wide runes and ANSI styles.
func genLine(t *rapid.T, label string) string {
	tokens := rapid.SliceOfN(rapid.SampledFrom([]string{
		"a", "word ", "Z", "界", "✓", "\x1b[31m", "\x1b[1;38;2;230;195;132m", "\x1b[0m", "—",
	}), 1, 40).Draw(t, label)
	return strings.Join(tokens, "")
}

func genLines(t *rapid.T, label string, maxN int) []string {
	n := rapid.IntRange(0, maxN).Draw(t, label+"N")
	out := make([]string, n)
	for i := range out {
		out[i] = genLine(t, fmt.Sprintf("%s%d", label, i))
	}
	return out
}

func TestFramePropertiesHold(t *testing.T) { // chrome C4
	rapid.Check(t, func(t *rapid.T) {
		title := genLine(t, "title")
		banner := ""
		if rapid.Bool().Draw(t, "hasBanner") {
			banner = genLine(t, "banner")
		}
		body := append([]string{genLine(t, "body")}, genLines(t, "moreBody", 30)...)
		footer := genLines(t, "footer", 3)
		width := rapid.IntRange(4, 120).Draw(t, "width")
		gap := 0
		if len(footer) > 0 {
			gap = 1
		}
		rows := rapid.IntRange(1, 30).Draw(t, "rows")
		height := 2 + len(footer) + gap + rows
		scroll := rapid.IntRange(-5, 50).Draw(t, "scroll")

		out := frame(width, height, title, banner, strings.Join(body, "\n"), strings.Join(footer, "\n"), scroll)
		lines := strings.Split(out, "\n")

		wantCount := 2 + min(len(body), rows) + gap + len(footer)
		if len(lines) != wantCount {
			t.Fatalf("frame produced %d lines, want %d (height %d, %d body lines, %d rows)", len(lines), wantCount, height, len(body), rows)
		}
		if len(lines) > height || (len(body) > rows && len(lines) != height) {
			t.Fatalf("frame produced %d lines at height %d with %d body lines; want at most height, exactly height on overflow", len(lines), height, len(body))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > width {
				t.Fatalf("line %d %q is %d columns wide, want <= %d", i, l, w, width)
			}
		}
		if got, want := ansi.Strip(lines[0]), ansi.Strip(ansi.Truncate(title, width, "…")); got != want {
			t.Fatalf("line 1 = %q, want the clipped title %q", got, want)
		}
		if got, want := ansi.Strip(lines[1]), ansi.Strip(ansi.Truncate(banner, width, "…")); got != want {
			t.Fatalf("line 2 = %q, want the clipped banner %q", got, want)
		}
		for i, f := range footer {
			want := "  " + ansi.Strip(f)
			got := ansi.Strip(lines[len(lines)-len(footer)+i])
			if ansi.StringWidth(want) <= width && got != want {
				t.Fatalf("footer line %d = %q, want %q", i, got, want)
			}
		}
	})
}

// ---- screens at 82x25 ----

const (
	termW = 82
	termH = 25
)

func sizedModel(t *testing.T) *model {
	m := newModel(Config{AppVersion: "1.2.3", PrismDirs: []string{t.TempDir()}})
	m.Update(tea.WindowSizeMsg{Width: termW, Height: termH})
	// Never touch the real OS: the game is not running and Prism is never started.
	m.isRunning = func(prism.Instance) (bool, error) { return false, nil }
	m.findLauncher = func(string, string) (prism.Launcher, error) {
		t.Errorf("unexpected findLauncher call")
		return prism.Launcher{}, prism.ErrLauncherNotFound
	}
	m.launch = func(prism.Launcher, string, prism.Instance, string) error {
		t.Errorf("unexpected launch call")
		return nil
	}
	return m
}

func busyPlan() *update.Plan {
	pl := &update.Plan{BaselineMatch: 0.5}
	for i := range 20 {
		pl.Actions = append(pl.Actions, update.Action{Kind: update.Install, Path: fmt.Sprintf(".minecraft/mods/mod%d.jar", i)})
	}
	for i := range 12 {
		p := fmt.Sprintf(".minecraft/config/some-mod-with-a-long-name-%02d/settings.cfg", i)
		pl.Actions = append(pl.Actions, update.Action{Kind: update.Conflict, Path: p})
	}
	for i := range 10 {
		pl.ExtraMods = append(pl.ExtraMods, fmt.Sprintf("a-very-long-hand-added-mod-name-number-%02d-1.7.10.jar", i))
	}
	for i, p := range pl.Conflicts() {
		if i < 6 {
			pl.Choose(p, update.TakeNew)
		} else {
			pl.Choose(p, update.KeepMine)
		}
	}
	return pl
}

func threeWarns() []string {
	w := "a mod in your instance looks like it was changed by hand, and I left it exactly where it was so you can check it after the update"
	return []string{w + " (1)", w + " (2)", w + " (3)"}
}

// fit reports how many lines a view has and how wide its widest line is.
func fit(out string) (lines, widest int) {
	all := strings.Split(out, "\n")
	for _, l := range all {
		widest = max(widest, ansi.StringWidth(l))
	}
	return len(all), widest
}

func TestConfirmViewFitsTerminal(t *testing.T) { // C6
	m := sizedModel(t)
	m.screen = scConfirm
	m.inst = prism.Instance{Dir: t.TempDir(), Name: "GTNH", GTNH: true}
	m.detect = update.Detection{Version: "2.9.0"}
	m.target = "2.8.4" // older than installed: downgrade warning
	m.session = &update.Session{Plan: busyPlan()}
	m.warns = threeWarns()
	m.serverMods = "https://example.com/server/custom_mods.zip"

	out := m.View()
	n, w := fit(out)
	if n > termH || w > termW || !strings.Contains(ansi.Strip(out), "enter") {
		t.Errorf("confirm view: %d lines, widest %d, has enter hint %v; want <= %d lines, <= %d wide, hint shown",
			n, w, strings.Contains(ansi.Strip(out), "enter"), termH, termW)
	}
}

func TestDoneViewFitsTerminal(t *testing.T) { // C6
	m := sizedModel(t)
	m.screen = scDone
	m.inst = prism.Instance{Dir: t.TempDir(), Name: "GTNH", GTNH: true}
	m.detect = update.Detection{Version: "2.9.0"}
	m.target = "2.8.4"
	m.session = &update.Session{Plan: busyPlan()}
	m.serverMods = "https://example.com/server/custom_mods.zip"
	m.result = &update.Result{
		From: "2.9.0", To: "2.8.4",
		BackupDir: "/home/player/.local/share/PrismLauncher/instances/GTNH/.gtnh-updater/backup-20260101-120000",
		CustomMods: &update.CustomModsResult{
			Installed: []string{"one.jar", "two.jar"},
			Skipped:   []string{"NotEnoughItems-2.6.0.jar", "journeymap-1.7.10-5.2.6.jar", "gregtech-5.09.jar"},
		},
		CustomErr: errors.New("the server answered with an error page instead of the mods archive"),
	}
	m.warns = threeWarns()

	out := m.View()
	n, w := fit(out)
	if n > termH || w > termW || !strings.Contains(ansi.Strip(out), "enter") {
		t.Errorf("done view: %d lines, widest %d, has enter hint %v; want <= %d lines, <= %d wide, hint shown",
			n, w, strings.Contains(ansi.Strip(out), "enter"), termH, termW)
	}
}

func TestErrorViewFitsTerminal(t *testing.T) { // C6
	m := sizedModel(t)
	m.screen = scError
	m.errPhase = scApplying
	m.err = errors.New(strings.Repeat("could not write a file ", 27)[:600])

	out := m.View()
	n, w := fit(out)
	if n > termH || w > termW || !strings.Contains(ansi.Strip(out), "enter") {
		t.Errorf("error view: %d lines, widest %d, has enter hint %v; want <= %d lines, <= %d wide, hint shown",
			n, w, strings.Contains(ansi.Strip(out), "enter"), termH, termW)
	}
}

func TestServerModsViewFitsTerminal(t *testing.T) { // C6
	m := sizedModel(t)
	m.screen = scServerMods
	m.inputEr = "That doesn't look like a link I can use: it has to start with https:// and point at a .zip file on a web server."

	out := m.View()
	n, w := fit(out)
	if n > termH || w > termW || !strings.Contains(ansi.Strip(out), "enter") {
		t.Errorf("server mods view: %d lines, widest %d, has enter hint %v; want <= %d lines, <= %d wide, hint shown",
			n, w, strings.Contains(ansi.Strip(out), "enter"), termH, termW)
	}
}

func TestApplyingViewFitsTerminal(t *testing.T) { // C6
	m := sizedModel(t)
	m.screen = scApplying
	m.inst = prism.Instance{Dir: t.TempDir(), Name: "GTNH", GTNH: true}
	m.target = "2.8.4"
	for i := range 8 {
		m.steps = append(m.steps, fmt.Sprintf("Finished step number %d", i))
	}
	m.step = "Writing files"
	w150 := strings.Repeat("this warning is long ", 8)[:150]
	m.warns = []string{w150, w150, w150, w150, w150}

	out := m.View()
	n, w := fit(out)
	hint := "don't close this window"
	if n > termH || w > termW || !strings.Contains(ansi.Strip(out), hint) {
		t.Errorf("applying view: %d lines, widest %d, has %q %v; want <= %d lines, <= %d wide, hint shown",
			n, w, hint, strings.Contains(ansi.Strip(out), hint), termH, termW)
	}
}

func TestNameViewFitsTerminal(t *testing.T) { // C6
	m := sizedModel(t)
	m.screen = scName
	m.creating = true
	m.nameEr = strings.Repeat("That name can't be used for a folder on your computer. ", 25)

	out := m.View()
	n, w := fit(out)
	if n > termH || w > termW || !strings.Contains(ansi.Strip(out), "enter") {
		t.Errorf("name view: %d lines, widest %d, has enter hint %v; want <= %d lines, <= %d wide, hint shown",
			n, w, strings.Contains(ansi.Strip(out), "enter"), termH, termW)
	}
}

// ---- long list titles ----

func longNameModel(t *testing.T) *model {
	man, err := manifest.Parse([]byte(`{
	  "2.8.4": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/a.zip"}},
	  "2.9.0-RC-1": {"title":"Beta release","releaseDate":"2026/09/24","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/b.zip"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	m := sizedModel(t)
	m.manifest = man
	m.inst = prism.Instance{Dir: t.TempDir(), Name: strings.Repeat("N", 120), GTNH: true}
	m.insts = []prism.Instance{m.inst}
	m.detect = update.Detection{Version: "2.8.4"}
	m.serverModsAsked = true
	return m
}

// longWordsName is an instance name of many short words, far wider than the list.
var longWordsName = strings.TrimSpace(strings.Repeat("Long Name ", 15))

// titleShape is the line count and widest (trailing spaces trimmed) line of title.
func titleShape(title string) (lines, widest int) {
	all := strings.Split(ansi.Strip(title), "\n")
	for _, l := range all {
		widest = max(widest, ansi.StringWidth(strings.TrimRight(l, " ")))
	}
	return len(all), widest
}

func TestTargetListTitleIsClippedToListWidth(t *testing.T) { // C7, screens C7: wrapped to the width, not cut
	m := longNameModel(t)
	m.inst.Name = longWordsName
	m.insts = []prism.Instance{m.inst}
	m.showTargets()
	if got := m.buttonLabels(); got != nil {
		t.Fatalf("buttonLabels() on versions = %q, want nil", got)
	}
	want := longWordsName + " is on GTNH 2.8.4. Which version do you want?"
	lines, widest := titleShape(m.list.Title)
	if got := words(m.list.Title); got != want || lines < 2 || widest > m.listWidthFor(scTarget)-2 || strings.Contains(m.list.Title, "…") {
		t.Errorf("target title reads %q on %d lines, widest %d; want all of %q on several lines, each <= %d, nothing cut",
			got, lines, widest, want, m.listWidthFor(scTarget)-2)
	}
}

func TestInstalledListTitleIsClippedToListWidth(t *testing.T) { // C7, screens C7: wrapped to the width, not cut
	m := longNameModel(t)
	m.inst.Name = longWordsName
	m.insts = []prism.Instance{m.inst}
	m.showInstalled()
	if got := m.buttonLabels(); got != nil {
		t.Fatalf("buttonLabels() on installed = %q, want nil", got)
	}
	want := "Which GTNH version is " + longWordsName + " on right now?"
	lines, widest := titleShape(m.list.Title)
	if got := words(m.list.Title); got != want || lines < 2 || widest > m.listWidthFor(scInstalled)-2 || strings.Contains(m.list.Title, "…") {
		t.Errorf("installed title reads %q on %d lines, widest %d; want all of %q on several lines, each <= %d, nothing cut",
			got, lines, widest, want, m.listWidthFor(scInstalled)-2)
	}
}

// ---- after Prepare (AGENTS.md: conflicts are asked after Prepare, "new" preselected) ----

func TestPreparedWithConflictsAsksAboutThemWithNewPreselected(t *testing.T) {
	m := sizedModel(t)
	path := ".minecraft/config/a.cfg"
	pl := &update.Plan{Actions: []update.Action{{Kind: update.Conflict, Path: path}}}
	m.Update(preparedMsg{&update.Session{Plan: pl}})
	if m.screen != scConflicts || pl.ChoiceOf(path) != update.TakeNew {
		t.Errorf("after preparedMsg with 1 conflict: screen %d, choice %d; want scConflicts (%d), TakeNew (%d)",
			m.screen, pl.ChoiceOf(path), scConflicts, update.TakeNew)
	}
}

func TestPreparedWithoutConflictsGoesStraightToConfirm(t *testing.T) {
	m := sizedModel(t)
	pl := &update.Plan{Actions: []update.Action{{Kind: update.Install, Path: ".minecraft/mods/a.jar"}}}
	m.Update(preparedMsg{&update.Session{Plan: pl}})
	if m.screen != scConfirm {
		t.Errorf("after preparedMsg with no conflicts: screen %d, want scConfirm (%d)", m.screen, scConfirm)
	}
}
