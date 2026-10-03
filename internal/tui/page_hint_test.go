package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// C1
func TestC1MeasureIsThePageWidthUpTo84(t *testing.T) {
	cases := []struct{ width, want int }{
		{60, 60},
		{84, 84},
		{85, 84},
		{200, 84},
	}
	for _, c := range cases {
		if got := measure(c.width); got != c.want {
			t.Errorf("measure(%d) = %d, want %d", c.width, got, c.want)
		}
	}
}

// C1: a wide page draws inside the 84-column measure.
func TestC1WidePageLinesStayInsideTheMeasure(t *testing.T) {
	m, _ := oneFull(t)

	got := pageLines(m, 200)

	for i, l := range got {
		if ansi.StringWidth(l) > 84 {
			t.Errorf("page line %d is %d wide, want ≤ 84: %q", i+1, ansi.StringWidth(l), l)
		}
	}
}

// C2, kills K1: at width 200 the hints share column min(max(47+4, 32), 84-5-1) = 51,
// not the right edge of the measure.
func TestC2WidePageHintsShareAColumnNearTheText(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 200, 40, makeInst(t, root, fullSpec("Home")))
	m.focus = focusPage

	got := pageLines(m, 200)

	cols := map[string]int{
		"enter": hintColumn(lineWith(got, "▸ Play"), "enter"),
		"j":     hintColumn(lineWith(got, "Play and join"), "j"),
		"u":     hintColumn(lineWith(got, "is out"), "u"),
		"o":     hintColumn(lineWith(got, "Choose another version"), "o"),
		"b":     hintColumn(lineWith(got, "Last update"), "b"),
	}
	want := map[string]int{"enter": 51, "j": 51, "u": 51, "o": 51, "b": 51}
	for h, c := range want {
		if cols[h] != c {
			t.Errorf("hint %q at column %d, want %d (all hints in one column < 70):\n%s", h, cols[h], c, strings.Join(got, "\n"))
		}
	}
}

// C2: a text too long for the hint column is cut with … and its hint stays in the column.
// 70-char server: widest text 84, so hintCol = min(88, 60-5-1) = 54; the join text keeps
// 54-3 = 51 columns.
func TestC2LongHintedTextIsTruncatedAndItsHintStaysInTheColumn(t *testing.T) {
	root := t.TempDir()
	s := fullSpec("Home")
	s.server = strings.Repeat("x", 70)
	m, _ := loadedModel(root, 80, 24, makeInst(t, root, s))
	m.focus = focusPage

	got := pageLines(m, 60)

	if l, want := lineWith(got, "Play and join"), "  Play and join "+strings.Repeat("x", 36)+"… j"; l != want {
		t.Errorf("join row = %q, want %q", l, want)
	}
	if l, want := lineWith(got, "▸ Play"), hintedAt("▸ ", "Play", "enter", 54); l != want {
		t.Errorf("play row = %q, want %q", l, want)
	}
	if l, want := lineWith(got, "Choose another version"), hintedAt("  ", "Choose another version…", "o", 54); l != want {
		t.Errorf("versions row = %q, want %q", l, want)
	}
}

// C2: widest text + 4 when that lies between the floor and the cap.
func TestC2HintColIsWidestTextPlusFour(t *testing.T) {
	rs := []row{
		{text: "Play", hint: "enter"},
		{text: strings.Repeat("t", 40), hint: "u"},
	}

	if got := hintCol(84, rs); got != 44 {
		t.Errorf("hintCol = %d, want 40+4", got)
	}
}

// C2: short texts put the hints at the 32 floor.
func TestC2HintColHasAFloorOf32(t *testing.T) {
	rs := []row{
		{text: "Play", hint: "enter"},
		{text: "Short", hint: "j"},
	}

	if got := hintCol(84, rs); got != 32 {
		t.Errorf("hintCol = %d, want 32", got)
	}
}

// C2: only hinted rows count towards the widest text.
func TestC2HintColIgnoresRowsWithoutAHint(t *testing.T) {
	rs := []row{
		{text: "Play", hint: "enter"},
		{text: strings.Repeat("t", 50), hint: ""},
	}

	if got := hintCol(84, rs); got != 32 {
		t.Errorf("hintCol = %d, want 32 (the unhinted 50-wide text ignored)", got)
	}
}

// C2: texts wider than the measure cap the column at measure - widest hint - 1.
func TestC2HintColIsCappedByTheMeasureAndWidestHint(t *testing.T) {
	rs := []row{
		{text: "Play", hint: "enter"},
		{text: strings.Repeat("t", 70), hint: "u"},
	}

	if got := hintCol(60, rs); got != 54 {
		t.Errorf("hintCol = %d, want 60-5-1", got)
	}
}

// C2: the cap wins over the 32 floor.
func TestC2HintColCapWinsOverTheFloor(t *testing.T) {
	rs := []row{
		{text: "Play", hint: "enter"},
	}

	if got := hintCol(34, rs); got != 28 {
		t.Errorf("hintCol = %d, want min(32, 34-5-1)", got)
	}
}

// C2
func TestC2RowLineAtPutsTheHintAtTheColumn(t *testing.T) {
	got := strings.TrimRight(ansi.Strip(rowLineAt(40, 20, true, "Play", "enter")), " ")

	if want := "▸ Play" + strings.Repeat(" ", 14) + "enter"; got != want {
		t.Errorf("rowLineAt = %q, want %q", got, want)
	}
}

// C2: column 3 is the first that holds a hint: the text is cut to nothing, the hint stays
// at the column. Kills page.go:222:9 (`col < 3` -> `col <= 3`, which falls back to flush right).
func TestC2RowLineAtColumnThreeCutsTheTextToNothing(t *testing.T) {
	got := strings.TrimRight(ansi.Strip(rowLineAt(20, 3, false, "Play", "u")), " ")

	if want := "   u"; got != want {
		t.Errorf("rowLineAt = %q, want %q", got, want)
	}
}

// C2: a hint ending exactly at the width still sits at its column. Kills page.go:222:43
// (`>` -> `>=`, whose flush-right fallback has no room for text and drops the hint).
func TestC2RowLineAtHintEndingAtTheWidthStaysAtTheColumn(t *testing.T) {
	got := strings.TrimRight(ansi.Strip(rowLineAt(4, 3, false, "Play", "u")), " ")

	if want := "   u"; got != want {
		t.Errorf("rowLineAt = %q, want %q", got, want)
	}
}

// C2b: a hint that would run past the width falls back to flush right. Kills page.go:222:19
// (`col+hw` -> `col-hw`, which puts "enter" at 18 and makes the line 23 wide).
func TestC2bRowLineAtFallsBackToFlushRightWhenTheHintDoesNotFit(t *testing.T) {
	got := ansi.Strip(rowLineAt(20, 18, false, "Play", "enter"))

	if want := "  Play         enter"; got != want {
		t.Errorf("rowLineAt = %q, want %q", got, want)
	}
}

// C2: the text is cut to col - 3 columns and one space precedes the hint.
func TestC2RowLineAtTruncatesTextBeforeTheColumn(t *testing.T) {
	got := strings.TrimRight(ansi.Strip(rowLineAt(40, 20, false, "A rather long row text that goes on", "u")), " ")

	if want := "  A rather long ro… u"; got != want {
		t.Errorf("rowLineAt = %q, want %q", got, want)
	}
}
