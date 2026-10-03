package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// C3
func TestC3GlyphMarksTheInstanceStatus(t *testing.T) {
	cases := []struct {
		name string
		info homeInfo
		want string
	}{
		{"up to date", homeInfo{gtnh: true, version: "2.8.4", rec: "2.8.4"}, "●"},
		{"newer out", homeInfo{gtnh: true, version: "2.8.1", rec: "2.8.4"}, "▲"},
		{"version unknown", homeInfo{gtnh: true, version: "", rec: "2.8.4"}, "○"},
		{"not GTNH", homeInfo{gtnh: false, version: "2.8.4", rec: "2.8.4"}, "○"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := glyph(c.info); got != c.want {
				t.Errorf("glyph = %q, want %q", got, c.want)
			}
		})
	}
}

// C3
func TestC3SidebarShowsOnlyWithMoreThanOneVisibleInstance(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24,
		makeInst(t, root, instSpec{name: "Home", gtnh: true}),
		makeInst(t, root, instSpec{name: "Vanilla"}))

	hidden, hiddenWidth := m.sidebarShows(), m.sidebarWidth()
	m.showAll = true

	if hidden || hiddenWidth != 0 {
		t.Errorf("one visible instance: shows %v width %d, want false 0", hidden, hiddenWidth)
	}
	if !m.sidebarShows() {
		t.Errorf("two visible instances with showAll: sidebar hidden")
	}
}

// C3
func TestC3SidebarWidthIsLongestNamePlusFourClamped(t *testing.T) {
	cases := []struct {
		names []string
		want  int
	}{
		{[]string{"Ab", "Cd"}, 16},                        // 6 -> 16
		{[]string{"Ab", "Fifteen chars!!"}, 19},           // 15 + 4
		{[]string{"Ab", "Twelve chars"}, 16},              // 12 + 4 = 16 exactly
		{[]string{"Ab", "Twenty four chars name!!"}, 28},  // 24 + 4 = 28 exactly
		{[]string{"Ab", "Twenty five chars name!!!"}, 28}, // 29 -> 28
		{[]string{"Ab", "A name far longer than the sidebar can hold"}, 28},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.names), func(t *testing.T) {
			root := t.TempDir()
			var insts []prism.Instance
			for _, n := range c.names {
				insts = append(insts, makeInst(t, root, instSpec{name: n, gtnh: true}))
			}
			m, _ := loadedModel(root, 120, 24, insts...)

			if got := m.sidebarWidth(); got != c.want {
				t.Errorf("sidebarWidth = %d, want %d", got, c.want)
			}
		})
	}
}

// C3
func TestC3SidebarListsTheInstancesUnderAHeading(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24,
		makeInst(t, root, instSpec{name: "Current", gtnh: true, version: "2.8.4"}),
		makeInst(t, root, instSpec{name: "Behind", gtnh: true, version: "2.8.1"}),
		makeInst(t, root, instSpec{name: "Unknown", gtnh: true}))

	lines := trimLines(m.sidebarView(10))

	want := []string{"Instances", "● Current", "▲ Behind", "○ Unknown"}
	if len(lines) < len(want) {
		t.Fatalf("sidebar %q shorter than %q", lines, want)
	}
	for i, w := range want {
		if strings.TrimSpace(lines[i]) != w {
			t.Errorf("sidebar line %d = %q, want %q", i+1, lines[i], w)
		}
	}
}

// C3
func TestC3LongNamesAreTruncatedWithAnEllipsis(t *testing.T) {
	root := t.TempDir()
	long := "A name far longer than the sidebar can hold"
	m, _ := loadedModel(root, 120, 24,
		makeInst(t, root, instSpec{name: "Ab", gtnh: true, version: "2.8.4"}),
		makeInst(t, root, instSpec{name: long, gtnh: true, version: "2.8.4"}))

	line := lineWith(trimLines(m.sidebarView(10)), "A name")

	if !strings.HasPrefix(strings.TrimSpace(line), "● A name far") || !strings.HasSuffix(line, "…") {
		t.Errorf("long name line %q isn't glyph + truncated name with …", line)
	}
	if w := ansi.StringWidth(line); w > 28 {
		t.Errorf("long name line is %d columns, sidebar is 28", w)
	}
}

// C3
func TestC3SidebarScrollsToKeepTheSelectionVisible(t *testing.T) {
	root := t.TempDir()
	var insts []prism.Instance
	for i := range 10 {
		insts = append(insts, makeInst(t, root, instSpec{name: fmt.Sprintf("Pack %02d", i), gtnh: true}))
	}
	m, _ := loadedModel(root, 80, 24, insts...)
	m.focus = focusSidebar
	press(m, "down", "down", "down", "down", "down", "down", "down", "down", "down")

	lines := trimLines(m.sidebarView(5))

	if len(lines) > 5 || !containsLine(lines, "Pack 09") {
		t.Errorf("sidebar of height 5 %q doesn't show the selected Pack 09", lines)
	}
}

// C3: with more instances than room, the sidebar is the heading and height-1 names from
// the top while the first one is selected. Kills sidebar.go:36 (one name too many).
func TestC3SidebarNeverExceedsItsHeight(t *testing.T) {
	root := t.TempDir()
	var insts []prism.Instance
	for i := range 10 {
		insts = append(insts, makeInst(t, root, instSpec{name: fmt.Sprintf("Pack %02d", i), gtnh: true}))
	}
	m, _ := loadedModel(root, 80, 24, insts...)

	lines := trimLines(m.sidebarView(5))

	if len(lines) != 5 || !strings.HasSuffix(lines[1], "Pack 00") || !strings.HasSuffix(lines[4], "Pack 03") {
		t.Errorf("sidebar of height 5 = %q, want Instances and Pack 00..03", lines)
	}
}

// C3/C6
func TestC3PlayingInstanceShowsTheHalfCircle(t *testing.T) {
	for _, state := range []string{"starting", "slow", "running"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			a := makeInst(t, root, instSpec{name: "Alpha", gtnh: true, version: "2.8.4"})
			b := makeInst(t, root, instSpec{name: "Bravo", gtnh: true, version: "2.8.4"})
			m, _ := loadedModel(root, 80, 24, a, b)
			m.play = playMonitor{gen: 1, dir: b.Dir, state: state}

			lines := trimLines(m.sidebarView(10))

			if !containsLine(lines, "◐ Bravo") || !containsLine(lines, "● Alpha") {
				t.Errorf("sidebar %q: want ◐ Bravo and ● Alpha", lines)
			}
		})
	}
}

// C3/C6
func TestC3ClosedGameShowsTheNormalGlyph(t *testing.T) {
	root := t.TempDir()
	a := makeInst(t, root, instSpec{name: "Alpha", gtnh: true, version: "2.8.4"})
	b := makeInst(t, root, instSpec{name: "Bravo", gtnh: true, version: "2.8.4"})
	m, _ := loadedModel(root, 80, 24, a, b)
	m.play = playMonitor{gen: 1, dir: b.Dir, state: "closed"}

	lines := trimLines(m.sidebarView(10))

	if !containsLine(lines, "● Bravo") {
		t.Errorf("sidebar %q: want ● Bravo once the game closed", lines)
	}
}
