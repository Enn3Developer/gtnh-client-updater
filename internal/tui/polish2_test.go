package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Tests of the TUI polish slice (polish2 C1–C8).

// withTrueColor renders styles in 24-bit colour for the rest of the test.
func withTrueColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// accentParams is the SGR parameter run that sets the accent foreground (e.g.
// "38;2;127;179;202"); it appears in every accent sequence, bold or not.
func accentParams(t *testing.T) string {
	t.Helper()
	s := lipgloss.NewStyle().Foreground(accent).Render("x")
	i := strings.Index(s, "x")
	if i <= 0 {
		t.Fatalf("accent style renders no escape sequence: %q", s)
	}
	return strings.TrimSuffix(strings.TrimPrefix(s[:i], "\x1b["), "m")
}

// boxRows is the first and last view line (0-based) of the dialog box in lines.
func boxRows(t *testing.T, lines []string) (top, bottom int) {
	t.Helper()
	top, bottom = -1, -1
	for i, l := range lines {
		s := ansi.Strip(l)
		if top < 0 && strings.Contains(s, "╭") {
			top = i
		}
		if strings.Contains(s, "╰") {
			bottom = i
		}
	}
	if top < 0 || bottom < top {
		t.Fatalf("no dialog box in the view:\n%s", ansi.Strip(strings.Join(lines, "\n")))
	}
	return top, bottom
}

// screenBox is the dialog box as View() draws it, cut out of the screen: its inner lines
// with borders and padding removed and trailing spaces trimmed (like boxText).
func screenBox(t *testing.T, m *model) []string {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	top, bottom := boxRows(t, lines)
	from := strings.Index(lines[top], "╭")
	to := strings.Index(lines[top], "╮") + len("╮")
	x, bw := ansi.StringWidth(lines[top][:from]), ansi.StringWidth(lines[top][from:to])
	var out []string
	for _, l := range lines[top+1 : bottom] {
		l = ansi.Truncate(ansi.TruncateLeft(l, x, ""), bw, "")
		l = strings.TrimPrefix(strings.TrimSuffix(l, "│"), "│ ")
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

// specBar is the stripped progress bar C2 names: a gradient bar of width at frac, no
// percentage.
func specBar(width int, frac float64) string {
	bar := progress.New(progress.WithGradient(string(accent), string(okColor)), progress.WithWidth(width), progress.WithoutPercentage())
	return ansi.Strip(bar.ViewAs(frac))
}

// ---- C1 backdrop ----

// C1
func TestPolish2C1BackdropIsTheStrippedLineInDim(t *testing.T) {
	withTrueColor(t)

	got := backdrop(titleSty.Render("Alpha") + " " + okSty.Render("●"))

	if want := dimSty.Render("Alpha ●"); got != want {
		t.Errorf("backdrop = %q, want %q", got, want)
	}
}

// C1
func TestPolish2C1BackdropOfAnEmptyLineIsAnEmptyDimRender(t *testing.T) {
	withTrueColor(t)

	if got, want := backdrop(""), dimSty.Render(""); got != want {
		t.Errorf("backdrop(\"\") = %q, want %q", got, want)
	}
}

// C1: without a dialog the selected sidebar line is in the accent colour.
func TestPolish2C1WorkspaceShowsTheAccentWithoutADialog(t *testing.T) {
	withTrueColor(t)
	acc := accentParams(t)
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24, threeInsts(t, root)...)
	m.focus = focusSidebar

	lines := strings.Split(m.View(), "\n")

	if !containsLine(lines[2:len(lines)-1], acc) {
		t.Errorf("no workspace line carries the accent %q:\n%q", acc, lines)
	}
}

// C1, kills K1: under a dialog the workspace around the box is dimmed (no accent left),
// while the box keeps its own colours.
func TestPolish2C1WorkspaceAroundADialogIsDimmed(t *testing.T) {
	withTrueColor(t)
	acc := accentParams(t)
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24, threeInsts(t, root)...)
	m.focus = focusSidebar

	m.notify("t", "x")
	lines := strings.Split(m.View(), "\n")
	top, bottom := boxRows(t, lines)

	for i := 2; i <= len(lines)-2; i++ {
		if i >= top && i <= bottom {
			continue
		}
		if strings.Contains(lines[i], acc) {
			t.Errorf("workspace line %d outside the box keeps the accent: %q", i, lines[i])
		}
	}
	if !containsLine(lines[top:bottom+1], acc) {
		t.Errorf("the box lost its accent title:\n%q", lines[top:bottom+1])
	}
}

// C1: dimming doesn't change the text: the workspace outside the box reads the same.
func TestPolish2C1BackdropKeepsTheText(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24, threeInsts(t, root)...)
	m.focus = focusSidebar
	before := screen(m)

	m.notify("t", "x")
	after := screen(m)
	top, bottom := boxRows(t, strings.Split(m.View(), "\n"))

	for i := 2; i <= len(after)-2; i++ {
		if (i < top || i > bottom) && after[i] != before[i] {
			t.Errorf("workspace line %d = %q under the dialog, want %q", i, after[i], before[i])
		}
	}
}

// ---- C2 progress ----

// progressModel is jobModel(prepare) at a fixed clock with 312.61 of 727 MB done, the
// step started 30 s ago, no step log.
func progressModel(t *testing.T, done, total int64) *model {
	t.Helper()
	m, _ := jobModel(t, "prepare")
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.stepStart = now.Add(-30 * time.Second)
	m.steps, m.step = nil, ""
	m.done, m.total = done, total
	return m
}

// C2
func TestPolish2C2ProgressLinesAreTheBarLineThenTheDetail(t *testing.T) {
	m := progressModel(t, 312_610_000, 727_000_000)

	got := trimLines(strings.Join(m.progressLines(59, 40), "\n"))

	want := []string{
		strings.Repeat("█", 17) + strings.Repeat("░", 23) + "  43%",
		"312 MB of 727 MB · about 40 seconds left",
	}
	if !eq(got, want) {
		t.Errorf("progressLines(59, 40) = %q, want %q", got, want)
	}
}

// C2
func TestPolish2C2ProgressDetailIsDim(t *testing.T) {
	withTrueColor(t)
	m := progressModel(t, 312_610_000, 727_000_000)

	got := m.progressLines(59, 40)

	if want := dimSty.Render("312 MB of 727 MB · about 40 seconds left"); len(got) != 2 || got[1] != want {
		t.Errorf("progressLines = %q, want the detail %q", got, want)
	}
}

// C2: the detail wraps to width instead of being cut.
func TestPolish2C2ProgressDetailWrapsToTheWidth(t *testing.T) {
	m := progressModel(t, 312_610_000, 727_000_000)

	got := trimLines(strings.Join(m.progressLines(38, 28), "\n"))

	want := []string{
		specBar(28, 312_610_000.0/727_000_000.0) + "  43%",
		"312 MB of 727 MB · about 40 seconds",
		"left",
	}
	if !eq(got, want) {
		t.Errorf("progressLines(38, 28) = %q, want %q", got, want)
	}
}

// C2: up to a million it counts files; no time left right at the start.
func TestPolish2C2ProgressDetailCountsFiles(t *testing.T) {
	m := progressModel(t, 18, 1204)
	m.stepStart = m.now()

	got := trimLines(strings.Join(m.progressLines(59, 40), "\n"))

	if len(got) != 2 || got[0] != specBar(40, 18.0/1204.0)+"  1%" || got[1] != "18 of 1,204 files" {
		t.Errorf("progressLines = %q, want the 40-column bar with 1%% and 18 of 1,204 files", got)
	}
}

// C2: the job block indents the progress lines, bar min(rowWidth-12, 40), detail wrapped
// to rowWidth-2; nothing is truncated.
func TestPolish2C2JobBlockProgressAtRowWidths(t *testing.T) {
	frac := 312_610_000.0 / 727_000_000.0
	cases := []struct {
		width int
		want  []string
	}{
		{40, []string{"  ◐ " + checking284, "  " + specBar(28, frac) + "  43%", "  312 MB of 727 MB · about 40 seconds", "  left"}},
		{61, []string{"  ◐ " + checking284, "  " + strings.Repeat("█", 17) + strings.Repeat("░", 23) + "  43%",
			"  312 MB of 727 MB · about 40 seconds left"}},
		{80, []string{"  ◐ " + checking284, "  " + specBar(40, frac) + "  43%", "  312 MB of 727 MB · about 40 seconds left"}},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.width), func(t *testing.T) {
			m := progressModel(t, 312_610_000, 727_000_000)

			got := plainLines(m.jobLines(c.width))

			if !eq(got, c.want) {
				t.Errorf("jobLines(%d) = %q, want %q", c.width, got, c.want)
			}
		})
	}
}

// C2: the files form wraps too at width 40 (detail "18 of 1,204 files · about 33 minutes left").
func TestPolish2C2JobBlockFilesProgressWrapsAt40(t *testing.T) {
	m := progressModel(t, 18, 1204)

	got := plainLines(m.jobLines(40))

	if len(got) != 4 || !strings.HasSuffix(got[1], "  1%") || ansi.StringWidth(got[1]) != 2+28+4 ||
		got[2] != "  18 of 1,204 files · about 33 minutes" || got[3] != "  left" {
		t.Errorf("jobLines(40) = %q", got)
	}
}

// C2: on the page at row width 61 (sidebar shown) the progress lines appear whole.
func TestPolish2C2PageShowsTheProgressUntruncated(t *testing.T) {
	m, _ := twoGTNH(t)
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.job = jobOn(m.insts[0].Dir, updating284, "apply")
	m.stepStart = now.Add(-30 * time.Second)
	m.done, m.total = 312_610_000, 727_000_000

	got := pageLines(m, 61)

	bar := "  " + strings.Repeat("█", 17) + strings.Repeat("░", 23) + "  43%"
	i := indexOf(got, bar)
	if i < 0 || i+1 >= len(got) || got[i+1] != "  312 MB of 727 MB · about 40 seconds left" {
		t.Errorf("page lacks the bar line %q followed by the detail:\n%s", bar, strings.Join(got, "\n"))
	}
	if containsLine(got, "…") {
		t.Errorf("page has a truncated line:\n%s", strings.Join(got, "\n"))
	}
}

// C2: the launcher's progress dialog shows progressLines(inner, inner-8), then the step.
func TestPolish2C2ProgressDialogShowsTheWholeProgress(t *testing.T) {
	m := selfRunning(t)
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.Update(stepMsg("Downloading GTNH Launcher 9.9.9"))
	m.stepStart = now.Add(-30 * time.Second)
	m.done, m.total = 312_610_000, 727_000_000

	got := screenBox(t, m)

	want := []string{
		specBar(48, 312_610_000.0/727_000_000.0) + "  43%",
		"312 MB of 727 MB · about 40 seconds left",
		"⣾ Downloading GTNH Launcher 9.9.9",
	}
	if !eq(got, want) {
		t.Errorf("progress dialog box %q, want %q", got, want)
	}
}

// ---- C3 labels ----

// C3
func TestPolish2C3LabelledPadsTheLabelToTheWidth(t *testing.T) {
	cases := []struct {
		name, label, value string
		width              int
		want               string
	}{
		{"padded", "Memory", "8192 MB", 15, "Memory         8192 MB"},
		{"launcher label", "Prism Launcher", "found automatically", 24, "Prism Launcher          found automatically"},
		{"one column short of the width", "ab", "v", 3, "ab v"},
		{"exactly the width keeps one space", "abc", "v", 3, "abc v"},
		{"wider than the width keeps one space", "After I start the game", "quit", 15, "After I start the game quit"},
		{"empty value", "Prism Launcher", "", 24, "Prism Launcher          "},
		{"wide runes count as two", "日本", "v", 6, "日本  v"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := labelled(c.label, c.value, c.width); got != c.want {
				t.Errorf("labelled(%q, %q, %d) = %q, want %q", c.label, c.value, c.width, got, c.want)
			}
		})
	}
}

// C3
func TestPolish2C3LabelWidthIs24ForLauncherRowsElse15(t *testing.T) {
	cases := []struct {
		id   string
		want int
	}{
		{"after", 24}, {"prism", 24},
		{"memory", 15}, {"jvm", 15}, {"java", 15}, {"window", 15}, {"server", 15}, {"mods", 15}, {"play", 15},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			if got := labelWidth(c.id); got != c.want {
				t.Errorf("labelWidth(%s) = %d, want %d", c.id, got, c.want)
			}
		})
	}
}

// C3: settingRow lays the label out over the width it's given.
func TestPolish2C3SettingRowUsesTheGivenLabelWidth(t *testing.T) {
	m, _ := oneFull(t)

	got := trimLines(strings.Join(m.settingRow("prism", "Prism Launcher", "/opt/prism", 24).lines(78, true), "\n"))

	if !eq(got, []string{"▸ Prism Launcher          /opt/prism"}) {
		t.Errorf("settingRow lines = %q", got)
	}
}

// C3: the toggled "after" row says quit in the launcher layout, selected.
func TestPolish2C3ToggledAfterRowSaysQuit(t *testing.T) {
	m, _ := oneFull(t)
	if !m.app.StaysOpen() {
		t.Fatalf("the default app config doesn't stay open")
	}
	m.row = indexOf(rowIDs(m), "after")

	press(m, "enter")

	if got := lineWith(pageLines(m, 78), "After I start the game"); got != "▸ After I start the game  quit ✓ saved" {
		t.Errorf("after row = %q", got)
	}
}

// C3: the edited prism row is the 24-column label and the field.
func TestPolish2C3EditedPrismRowKeepsTheLauncherLabelWidth(t *testing.T) {
	m, _ := oneFull(t)
	m.app.PrismExe = "/opt/prism/prismlauncher"

	m.editSetting("prism")

	if m.edit == nil {
		t.Fatalf("editSetting(prism) didn't start editing")
	}
	if got := lineWith(pageLines(m, 78), "Prism Launcher"); got != "▸ Prism Launcher          /opt/prism/prismlauncher" {
		t.Errorf("editing line = %q", got)
	}
}

// C3: a launcher row's field is pageWidth-2-24-2 wide, at least 10.
func TestPolish2C3LauncherFieldWidthUsesTheLauncherLabelWidth(t *testing.T) {
	alone, _ := oneFull(t)
	both, _ := twoGTNH(t)
	root := t.TempDir()
	narrow, _ := loadedModel(root, 32, 24, makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4"}))
	cases := []struct {
		name string
		m    *model
		want int
	}{
		{"page 78", alone, 78 - 2 - 24 - 2},
		{"page 61", both, 61 - 2 - 24 - 2},
		{"page 30", narrow, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.m.editSetting("prism")

			if c.m.edit == nil || c.m.edit.input.Width != c.want {
				t.Errorf("edit %+v, want a prism field %d wide", c.m.edit, c.want)
			}
		})
	}
}

// ---- C4 play row ----

// C4
func TestPolish2C4UnselectedPlayRowSaysPlay(t *testing.T) {
	m, _ := twoGTNH(t)
	m.focus = focusSidebar

	got := pageLines(m, 61)

	if len(got) < 5 || got[3] != hintedAt("  ", "Play", "enter", 51) || got[4] != hintedAt("  ", "Play and join play.example.org", "j", 51) { // hint column min(max(47+4, 32), 61-5-1)
		t.Errorf("play rows = %q", got)
	}
}

// C4
func TestPolish2C4NoPlayTriangleOnThePage(t *testing.T) {
	m, _ := oneFull(t)

	got := strings.Join(pageLines(m, 78), "\n")

	if strings.Contains(got, "▶") {
		t.Errorf("page has ▶:\n%s", got)
	}
}

// ---- C5 list dialogs as tables ----

// C5, kills K2: version descs share one column after the widest title.
func TestPolish2C5VersionPickerDescsShareAColumn(t *testing.T) {
	m := renderModel(80, 24)

	m.chooseVersion()

	got := screenBox(t, m)
	want := []string{
		"  2.9.0-beta-2  beta · 3 days ago",
		"  2.9.0-beta-1  beta · 10 days ago",
		"▸ 2.8.4         stable release · 2 weeks ago · recommended",
		"  2.8.3         stable release · 7 weeks ago",
	}
	if len(got) < 4 || !eq(got[:4], want) {
		t.Errorf("picker lines %q, want them to start with %q", got, want)
	}
}

// C5: the conflict dialog at 80 columns: 74 wide, descs at column 24, wrapped under it.
func TestPolish2C5ConflictsDialogIsATable(t *testing.T) {
	m, _ := oneFull(t)
	pl := planOf(1, 0, conflictPaths(3)...)
	pl.ChooseAll(update.TakeNew)
	m.session = sessionOf(pl)

	m.conflictsDialog()

	want := []string{
		"What should I do with them?",
		"▸ Use the new versions  Recommended — your old copies go to the backup",
		"                        folder.",
		"  Keep mine             The new ones are saved next to yours with",
		"                        .mcnew at the end.",
		"  Decide file by file",
	}
	if got := screenBox(t, m); !eq(got, want) {
		t.Errorf("box\n%q\nwant\n%q", got, want)
	}
	if w := boxWidth(m); w != 74 {
		t.Errorf("box width %d, want 74", w)
	}
}

// C5: the resolve list's space toggle redraws the descs in their column.
func TestPolish2C5ResolveToggleRedrawsTheDescs(t *testing.T) {
	m, _ := resolving(t, "")

	m.Update(spaceKey)

	got := screenBox(t, m)
	want := []string{"▸ config/zeta.cfg   keep mine", "  config/alpha.cfg  new version"}
	if len(got) < 3 || !eq(got[1:3], want) {
		t.Errorf("box %q, want the items %q", got, want)
	}
}

// twoLineItems are n items "Item i" whose desc ("wi" 25 times) wraps to two lines in
// the 74-wide box (desc room 70-10 = 60: 20 words, then 5).
func twoLineItems(n int) []ditem {
	out := make([]ditem, n)
	for i := range out {
		w := "w" + string(rune('0'+i))
		out[i] = ditem{title: "Item " + string(rune('0'+i)), desc: strings.TrimSpace(strings.Repeat(w+" ", 25)), key: w}
	}
	return out
}

func words(w string, n int) string { return strings.TrimSpace(strings.Repeat(w+" ", n)) }

// C5: a desc wider than the room wraps under its column; the marker is on the first line.
func TestPolish2C5LongDescsWrapUnderTheColumn(t *testing.T) {
	m, _ := oneFull(t)

	m.openList("Pick", "", twoLineItems(2), "w1", nil)

	want := []string{
		"  Item 0  " + words("w0", 20),
		"          " + words("w0", 5),
		"▸ Item 1  " + words("w1", 20),
		"          " + words("w1", 5),
	}
	if got := screenBox(t, m); !eq(got, want) {
		t.Errorf("box\n%q\nwant\n%q", got, want)
	}
}

// C5: the window counts lines: 4 two-line items at height 13 (4 rows) show the first
// item, the first line of the second and a marker.
func TestPolish2C5ListWindowCountsLines(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 13, makeInst(t, root, fullSpec("Home")))

	m.openList("Pick", "", twoLineItems(4), "w0", nil)

	want := []string{
		"▸ Item 0  " + words("w0", 20),
		"          " + words("w0", 5),
		"  Item 1  " + words("w1", 20),
		"↓ 4 more",
	}
	if got := screenBox(t, m); !eq(got, want) {
		t.Errorf("box\n%q\nwant\n%q", got, want)
	}
}

// C5: moving down scrolls so the whole cursor item is visible and off the markers.
func TestPolish2C5ListWindowKeepsTheWholeCursorItemVisible(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 13, makeInst(t, root, fullSpec("Home")))
	m.openList("Pick", "", twoLineItems(4), "w0", nil)

	press(m, "down", "down")

	want := []string{
		"↑ 3 more",
		"▸ Item 2  " + words("w2", 20),
		"          " + words("w2", 5),
		"↓ 1 more",
	}
	if got := screenBox(t, m); !eq(got, want) || listOf(t, m).cursor != 2 {
		t.Errorf("box\n%q\nwant\n%q (cursor %d)", got, want, listOf(t, m).cursor)
	}
}

// C5: at most 3 lines per item, the third ending in "…".
func TestPolish2C5ADescIsCutAfterThreeLines(t *testing.T) {
	m, _ := oneFull(t)
	items := []ditem{{title: "A", desc: words("ab", 100), key: "a"}, {title: "B", key: "b"}}

	m.openList("Pick", "", items, "a", nil)

	got := screenBox(t, m)
	if len(got) != 4 || got[0] != "▸ A  "+words("ab", 22) || got[1] != "     "+words("ab", 22) || got[3] != "  B" {
		t.Fatalf("box %q, want A over three lines, then B", got)
	}
	if !strings.HasPrefix(got[2], "     ab") || !strings.HasSuffix(got[2], "…") || ansi.StringWidth(got[2]) > 70 {
		t.Errorf("third line %q, want the desc column cut with … within 70 columns", got[2])
	}
}

// ---- C6 status pairs ----

// C6
func TestPolish2C6DialogPairsFollowTheButtonCount(t *testing.T) {
	cases := []struct {
		name    string
		buttons []string
		want    string
	}{
		{"one button", []string{"OK"}, " enter ok   esc close"},
		{"two buttons", []string{"Go", "Cancel"}, " ←→ choose   enter ok   esc cancel"},
		{"three buttons", []string{"A", "B", "C"}, " ←→ choose   enter ok   esc cancel"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := oneFull(t)
			m.openDialog(&dialog{title: "T", body: func(int) string { return "x" }, buttons: c.buttons, onButton: closeOnly})

			if got := screen(m)[23]; got != c.want {
				t.Errorf("status line = %q, want %q", got, c.want)
			}
		})
	}
}

// C6: a list with one button keeps the list pairs.
func TestPolish2C6AListWithOneButtonKeepsTheListPairs(t *testing.T) {
	m, _ := resolving(t, "")

	if got := screen(m)[len(screen(m))-1]; got != " ↑↓ move   enter choose   esc cancel   space switch" {
		t.Errorf("status line = %q", got)
	}
}

// C6: the keyless label is dim on the bar, without a key chip.
func TestPolish2C6KeylessLabelIsDimOnTheBar(t *testing.T) {
	withTrueColor(t)
	m := selfRunning(t)

	lines := strings.Split(m.View(), "\n")
	bar := lines[len(lines)-1]

	if want := dimSty.Background(barBg).Render("updating the launcher — please don't close this window"); !strings.Contains(bar, want) {
		t.Errorf("status bar %q lacks the dim label %q", bar, want)
	}
	if strings.Contains(bar, titleSty.Background(barBg).Render("")) {
		t.Errorf("status bar %q draws an empty key chip", bar)
	}
}

// C6
func TestPolish2C6StatusLineOfTheErrorDialog(t *testing.T) {
	m, _ := oneFull(t)
	m.errorDialog(errors.New("boom"), "Nothing was changed.")

	if got := screen(m)[23]; got != " enter ok   esc close" {
		t.Errorf("status line = %q", got)
	}
}

// C6
func TestPolish2C6StatusLineOfTheProgressDialog(t *testing.T) {
	m := selfRunning(t)

	if got := screen(m)[23]; got != " updating the launcher — please don't close this window" {
		t.Errorf("status line = %q", got)
	}
}

// C6: applying with the page on the job row and the sidebar shown: the focus pairs, then
// the keyless warning (no n, a or q); it fits 80 columns exactly.
func TestPolish2C6StatusLineWhileApplying(t *testing.T) {
	m, _ := twoGTNH(t)
	m.job = jobOn(m.insts[0].Dir, updating284, "apply")
	m.focus = focusPage
	m.row = 0

	if got := screen(m)[23]; got != " ↑↓ move   enter run   tab instances   updating — please don't close this window" {
		t.Errorf("status line = %q", got)
	}
}

// C6: one column narrower, the keyless warning is dropped whole.
func TestPolish2C6StatusLineWhileApplyingDropsTheWarningOneColumnShort(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 79, 24,
		makeInst(t, root, fullSpec("Home")),
		makeInst(t, root, instSpec{name: "Second", gtnh: true, version: "2.8.4"}))
	m.job = jobOn(m.insts[0].Dir, updating284, "apply")
	m.focus = focusPage
	m.row = 0

	if got := screen(m)[23]; got != " ↑↓ move   enter run   tab instances" {
		t.Errorf("status line = %q", got)
	}
}

// C6: the sidebar's pairs while applying, even with hidden instances: no a, n or q.
func TestPolish2C6SidebarStatusLineWhileApplyingLeavesOutShowAll(t *testing.T) {
	m, _ := mixedModel(t)
	m.focus = focusSidebar
	m.job = jobOn(m.insts[0].Dir, updating284, "apply")

	if got := screen(m)[23]; got != " ↑↓ instance   enter play   tab page   updating — please don't close this window" {
		t.Errorf("status line = %q", got)
	}
}

// ---- C7 busy update section ----

// C7: while its job runs, the instance's Update section only says it's busy.
func TestPolish2C7UpdateSectionSaysBusyWhileTheJobRuns(t *testing.T) {
	for _, phase := range []string{"prepare", "apply"} {
		t.Run(phase, func(t *testing.T) {
			m, in := jobModel(t, phase)
			m.notices[in.Dir] = notice{text: "Updated to GTNH 2.8.4 just now", info: "2 extra mods from your server installed"}

			got := pageLines(m, 78)

			i := indexOf(got, "Update")
			if i < 0 || i+3 >= len(got) || !eq(got[i:i+4], []string{"Update", "  Busy — wait for it to finish.", "", "Settings"}) {
				t.Errorf("page\n%s\nwant Update, the busy line, a blank line, Settings", strings.Join(got, "\n"))
			}
		})
	}
}

// C7: no update rows while busy; settings and launcher rows stay.
func TestPolish2C7RowsWhileBusyAreTheJobAndTheSettings(t *testing.T) {
	m, _ := jobModel(t, "prepare")

	if got := strings.Join(rowIDs(m), ","); got != "job,memory,jvm,java,window,server,mods,after,prism" {
		t.Errorf("rows = %s", got)
	}
}

// C7: u, o and b answer that the job has to finish first.
func TestPolish2C7UpdateKeysOnTheBusyInstanceNotifyBusy(t *testing.T) {
	for _, k := range []string{"u", "o", "b"} {
		t.Run(k, func(t *testing.T) {
			m, _ := jobModel(t, "prepare")

			cmd := press(m, k)

			if cmd != nil || dialogTitle(m) != "One thing at a time" {
				t.Fatalf("cmd %v dialog %q, want One thing at a time", cmd != nil, dialogTitle(m))
			}
			if got := flat(m.dialog.body(200)); got != "I'm still busy with Home. Let it finish first." {
				t.Errorf("body %q", got)
			}
			if m.job.title != checking284 {
				t.Errorf("job %+v replaced", m.job)
			}
		})
	}
}

// C7: another instance's page keeps its update rows; its keys still say busy.
func TestPolish2C7AnotherInstanceKeepsItsUpdateRows(t *testing.T) {
	m, _ := twoGTNH(t)
	m.job = jobOn(m.insts[1].Dir, checking284, "prepare")

	ids := rowIDs(m)
	cmd := press(m, "u")

	if indexOf(ids, "update") < 0 || indexOf(ids, "versions") < 0 || indexOf(ids, "undo") < 0 {
		t.Errorf("Home rows %v, want update, versions and undo", ids)
	}
	if cmd != nil || dialogTitle(m) != "One thing at a time" || flat(m.dialog.body(200)) != "I'm still busy with Second. Let it finish first." {
		t.Errorf("u: dialog %q body %q, want the busy note naming Second", dialogTitle(m), flat(m.dialog.body(200)))
	}
}

// ---- C8 editing help wraps ----

// C8: at row width 61 the memory help is two dim lines.
func TestPolish2C8EditingHelpWrapsAt61(t *testing.T) {
	m, _ := oneFull(t)
	m.editSetting("memory")
	row := m.rows()[indexOf(rowIDs(m), "memory")]

	got := trimLines(strings.Join(row.lines(61, true), "\n"))

	want := []string{
		"  How much memory the game may use, in MB — GTNH runs well",
		"  with 6144 to 8192. Empty means Prism's default.",
	}
	if len(got) != 3 || !strings.HasPrefix(got[0], "▸ Memory         8192") || !eq(got[1:], want) {
		t.Errorf("edited row %q, want the field and %q", got, want)
	}
}

// C8: the help lines are infoLines (dim).
func TestPolish2C8EditingHelpLinesAreDim(t *testing.T) {
	withTrueColor(t)
	m, _ := oneFull(t)
	m.editSetting("memory")
	row := m.rows()[indexOf(rowIDs(m), "memory")]

	got := row.lines(61, true)

	if len(got) != 3 || got[1] != infoLine("How much memory the game may use, in MB — GTNH runs well") ||
		got[2] != infoLine("with 6144 to 8192. Empty means Prism's default.") {
		t.Errorf("edited row %q, want two dim help lines", got)
	}
}

// C8: the error comes after the wrapped help.
func TestPolish2C8ErrorFollowsTheWrappedHelp(t *testing.T) {
	m, _ := oneFull(t)
	m.editSetting("memory")
	m.edit.input.SetValue("abc")
	m.saveEdit()
	row := m.rows()[indexOf(rowIDs(m), "memory")]

	got := trimLines(strings.Join(row.lines(61, true), "\n"))

	if len(got) != 4 || got[3] != "  Give me a whole number of MB between 1024 and 65536." ||
		got[2] != "  with 6144 to 8192. Empty means Prism's default." {
		t.Errorf("edited row %q, want field, two help lines and the error", got)
	}
}

// C8: at width 40 the help takes exactly three lines, none cut.
func TestPolish2C8EditingHelpAt40HasThreeWholeLines(t *testing.T) {
	m, _ := oneFull(t)
	m.editSetting("memory")
	row := m.rows()[indexOf(rowIDs(m), "memory")]

	got := trimLines(strings.Join(row.lines(40, true), "\n"))

	want := []string{
		"  How much memory the game may use, in",
		"  MB — GTNH runs well with 6144 to 8192.",
		"  Empty means Prism's default.",
	}
	if len(got) != 4 || !eq(got[1:], want) {
		t.Errorf("edited row %q, want the field and %q", got, want)
	}
}

// C8: longer help is cut to three lines, the third ending in "…".
func TestPolish2C8EditingHelpIsCutAfterThreeLines(t *testing.T) {
	m, _ := oneFull(t)
	m.editSetting("memory")
	row := m.rows()[indexOf(rowIDs(m), "memory")]

	got := trimLines(strings.Join(row.lines(30, true), "\n"))

	if len(got) != 4 || got[1] != "  How much memory the game may" || got[2] != "  use, in MB — GTNH runs well" {
		t.Fatalf("edited row %q, want the field and three help lines", got)
	}
	if !strings.HasPrefix(got[3], "  with") || !strings.HasSuffix(got[3], "…") || ansi.StringWidth(got[3]) > 30 {
		t.Errorf("third help line %q, want it cut with … within 30 columns", got[3])
	}
}

// ---- kill step ----

// C6: one button closes, two choose — for both dialog kinds in one run. Kills
// dialog.go `len(d.buttons) == 1` -> `!= 1`.
func TestPolish2C6PairsOfOneAndTwoButtonDialogs(t *testing.T) {
	m, _ := oneFull(t)

	m.notify("Heads up", "Something to know.")
	one := strings.Join(m.dialogPairs(), ",")
	m.closeDialog()
	m.confirmDialog("Do it?", []string{"x"}, nil, nil, "Go",
		func(*model) tea.Cmd { return nil }, func(*model) tea.Cmd { return nil })
	two := strings.Join(m.dialogPairs(), ",")

	if one != "enter,ok,esc,close" {
		t.Errorf("one-button dialogPairs = %s, want enter,ok,esc,close", one)
	}
	if two != "←→,choose,enter,ok,esc,cancel" {
		t.Errorf("two-button dialogPairs = %s, want ←→,choose,enter,ok,esc,cancel", two)
	}
}

// accentBase is n lines of 20 columns, each the accent-coloured letter 'a'+i.
func accentBase(n int) []string {
	sty := lipgloss.NewStyle().Foreground(accent)
	out := make([]string, n)
	for i := range out {
		out[i] = sty.Render(strings.Repeat(string(rune('a'+i)), 20))
	}
	return out
}

// C1: overlay dims every workspace line (0-based 2..height-2), down to the last one,
// and leaves the title, blank and status lines alone. Kills layout.go `n <= height-2`
// -> `n < height-2`.
func TestPolish2C1OverlayDimsTheWholeWorkspace(t *testing.T) {
	withTrueColor(t)
	acc := accentParams(t)
	const height = 10

	got := strings.Split(overlay(strings.Join(accentBase(height), "\n"), "[XY]", 20, height), "\n")

	if len(got) != height {
		t.Fatalf("overlay has %d lines, want %d", len(got), height)
	}
	for i := 2; i <= height-2; i++ {
		if strings.Contains(ansi.Strip(got[i]), "[XY]") {
			continue
		}
		if strings.Contains(got[i], acc) {
			t.Errorf("workspace line %d keeps the accent: %q", i, got[i])
		}
	}
	for _, i := range []int{0, 1, height - 1} {
		if !strings.Contains(got[i], acc) {
			t.Errorf("line %d lost its accent: %q", i, got[i])
		}
	}
}

// C1: a base shorter than height is dimmed as far as it goes, without panicking. Kills
// layout.go `n < len(lines)` -> `n <= len(lines)`.
func TestPolish2C1OverlayOfAShortBaseDimsWhatThereIs(t *testing.T) {
	withTrueColor(t)
	acc := accentParams(t)

	got := strings.Split(overlay(strings.Join(accentBase(6), "\n"), "[XY]", 20, 10), "\n")

	if len(got) != 6 {
		t.Fatalf("overlay has %d lines, want the base's 6", len(got))
	}
	for i := 2; i < 6; i++ {
		if strings.Contains(got[i], acc) {
			t.Errorf("workspace line %d keeps the accent: %q", i, got[i])
		}
	}
}

// C2: a job block at page width 9 still fits.
func TestPolish2C2JobBlockAtWidthNineFits(t *testing.T) {
	m := progressModel(t, 312_610_000, 727_000_000)

	got := m.jobBlock(9, false)

	if len(got) < 2 {
		t.Fatalf("jobBlock(9) = %q, want the title and progress lines", got)
	}
	for i, l := range got {
		if w := ansi.StringWidth(l); w > 9 {
			t.Errorf("line %d is %d wide: %q", i, w, ansi.Strip(l))
		}
	}
}
