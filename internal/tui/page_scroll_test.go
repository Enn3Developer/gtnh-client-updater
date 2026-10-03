package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// nearestOffset is a naive model of C4's scrolling: among the window offsets of a page of
// total lines in h rows (a "↑" marker on the first row when scrolled, a "↓" marker on the
// last when more follows), the one closest to prev that shows the lines from..to-1 off
// the markers.
func nearestOffset(prev, from, to, total, h int) int {
	if total <= h {
		return 0
	}
	maxOff := total - h
	p := max(min(prev, maxOff), 0)
	best := -1
	for o := 0; o <= maxOff; o++ {
		first, last := o, o+h-1
		if o > 0 {
			first++
		}
		if o < maxOff {
			last--
		}
		if from < first || to-1 > last {
			continue
		}
		if best < 0 || absInt(o-p) < absInt(best-p) {
			best = o
		}
	}
	return best
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

var moreRe = regexp.MustCompile(`^([↑↓]) (\d+) more$`)

// marker parses a "↑ N more" / "↓ N more" line; ok is false for other lines.
func marker(l string) (arrow string, n int, ok bool) {
	sub := moreRe.FindStringSubmatch(strings.TrimSpace(l))
	if sub == nil {
		return "", 0, false
	}
	n, _ = strconv.Atoi(sub[2])
	return sub[1], n, true
}

// C4: moving up and down the page at any height keeps the selected row (one or two
// lines) fully visible off the markers, scrolling no further than needed; the window is
// the page's lines with "↑ N more"/"↓ N more" markers counting what is hidden.
// Kills the scrollTo mutants of page.go (bounds, top/bottom marker allowances, the
// row-0 span) and the "↓ N" count of bodyWindow.
func TestC4PageScrollKeepsTheSelectedRowVisibleMovingTheLeast(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, fullSpec("Home"))
	rapid.Check(t, func(rt *rapid.T) {
		h := rapid.IntRange(4, 30).Draw(rt, "height")
		running := rapid.Bool().Draw(rt, "running")
		keys := rapid.SliceOfN(rapid.SampledFrom([]string{"up", "down"}), 1, 40).Draw(rt, "keys")
		m, _ := loadedModel(root, 80, 24, in)
		m.focus = focusPage
		if running {
			m.play = playMonitor{gen: 1, dir: in.Dir, state: "running", start: time.Now(), since: time.Now()}
		}
		for step, k := range keys {
			press(m, k)
			prev := m.pageScroll
			full := trimLines(m.pageView(78, 1000))
			m.pageScroll = prev
			got := trimLines(m.pageView(78, h))
			off := m.pageScroll

			from := -1
			for i, l := range full {
				if strings.HasPrefix(l, "▸ ") {
					from = i
					break
				}
			}
			to := from + len(m.rows()[m.row].lines(78, true))
			if from < 0 {
				rt.Fatalf("step %d: no selected row in the full page", step)
			}
			if want := nearestOffset(prev, from, to, len(full), h); off != want {
				rt.Fatalf("step %d (%s to row %d, h %d): scrolled from %d to %d, want %d", step, k, m.row, h, prev, off, want)
			}
			checkWindow(rt, step, full, got, off, h)
		}
	})
}

// checkWindow compares the window with the page lines from off, markers aside.
func checkWindow(rt *rapid.T, step int, full, got []string, off, h int) {
	if len(full) <= h {
		if strings.Join(got, "\n") != strings.Join(full, "\n") {
			rt.Fatalf("step %d: page fits but window %q != page %q", step, got, full)
		}
		return
	}
	maxOff := len(full) - h
	if len(got) != h {
		rt.Fatalf("step %d: window has %d lines, want %d", step, len(got), h)
	}
	for i := range h {
		want := full[off+i]
		a, n, isMarker := marker(got[i])
		switch {
		case i == 0 && off > 0:
			// "↑ N more": N counts what is hidden above (with or without the marker's line).
			if !isMarker || a != "↑" || (n != off && n != off+1) {
				rt.Fatalf("step %d: first line %q, want ↑ %d more", step, got[i], off)
			}
		case i == h-1 && off < maxOff:
			if !isMarker || a != "↓" || (n != maxOff-off && n != maxOff-off+1) {
				rt.Fatalf("step %d: last line %q, want ↓ %d more", step, got[i], maxOff-off)
			}
		case got[i] != want:
			rt.Fatalf("step %d: window line %d = %q, want page line %d %q", step, i+1, got[i], off+i+1, want)
		}
	}
}

// C4: when rows disappear under the selection (a reload drops the server and backup),
// the selection is clamped to the last row and still drawn. Kills page.go:117.
func TestC4RowIsClampedWhenRowsDisappear(t *testing.T) {
	root := t.TempDir()
	full := makeInst(t, root, fullSpec("Home"))
	m, _ := loadedModel(root, 80, 40, full)
	m.focus = focusPage
	m.row = len(m.rows()) - 1
	if err := os.RemoveAll(filepath.Join(full.Dir, update.StateDir, "backup-20260101-000000")); err != nil {
		t.Fatal(err)
	}
	saveState(t, full.Dir, "2.8.4", "", "")

	m.Update(reloadedMsg{insts: []prism.Instance{full}})
	lines := screen(m)

	if m.row >= len(m.rows()) || m.rows()[m.row].id != "prism" {
		t.Fatalf("row %d of %v, want clamped to prism", m.row, rowIDs(m))
	}
	if !strings.HasPrefix(strings.TrimSpace(lineWith(lines, "Prism Launcher")), "▸ Prism Launcher") {
		t.Errorf("clamped row not drawn selected:\n%s", strings.Join(lines, "\n"))
	}
}
