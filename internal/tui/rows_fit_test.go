package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Rows that don't fit drop their least useful part instead of being cut mid-word.

// narrowPage is a loaded model at 80 columns whose 21-column instance names leave the
// page 52 columns (sidebar 25 + rule 3).
func narrowPage(t *testing.T, spec instSpec) *model {
	t.Helper()
	root := t.TempDir()
	if spec.name == "" {
		spec.name = "GT New Horizons 2.8.1"
	}
	m, _ := loadedModel(root, 80, 24, makeInst(t, root, spec),
		makeInst(t, root, instSpec{name: "GT New Horizons 2.8.4", gtnh: true, version: "2.8.4"}))
	m.focus = focusPage
	if m.pageWidth() != 52 {
		t.Fatalf("page width %d, want 52", m.pageWidth())
	}
	return m
}

func TestFitUpdateRowDropsTheAgeFirst(t *testing.T) {
	m := narrowPage(t, instSpec{gtnh: true, version: "2.8.1"})

	got := pageLines(m, m.pageWidth())

	if l := lineWith(got, "is out"); flat(l) != "GTNH 2.8.4 is out · stable release u" {
		t.Errorf("update row = %q", l)
	}
	if strings.Contains(strings.Join(got, "\n"), "…  u") {
		t.Errorf("a row is cut: %q", got)
	}
}

func TestFitUndoRowDropsTheAgeThenTheFromVersion(t *testing.T) {
	for _, c := range []struct{ from, to, want string }{
		{"2.8.0", "2.8.1", "Last update 2.8.0 → 2.8.1 · undo b"},
		{"2.9.0-beta-3", "2.9.0-RC-1", "Undo the update to 2.9.0-RC-1 b"},
	} {
		m := narrowPage(t, instSpec{gtnh: true, version: "2.8.1",
			backup: &update.BackupInfo{From: c.from, To: c.to, When: time.Now().Add(-14 * day)}})

		if l := flat(lineWith(pageLines(m, m.pageWidth()), "ndo")); l != c.want {
			t.Errorf("undo row = %q, want %q", l, c.want)
		}
	}
}

func TestFitUnknownVersionRowIsShortOnANarrowPage(t *testing.T) {
	m := narrowPage(t, instSpec{name: "GT New Horizons Mains", gtnh: true})

	if l := lineWith(pageLines(m, m.pageWidth()), "Which version"); flat(l) != "Which version is this? Tell me u" {
		t.Errorf("update row = %q", l)
	}
}

func TestNewestBetaIsNotCalledStable(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.9.0-beta-1"})
	m, _ := newTestModel(Config{PrismDirs: []string{root}}, 80, 24)
	now := time.Now()
	m.Update(loadedMsg{m: &manifest.Manifest{Releases: []manifest.Release{
		{Version: "2.9.0-beta-1", Title: "Beta release", ReleaseDate: now.Add(-3 * day), Java8URL: "https://downloads.gtnewhorizons.com/b.zip"},
		{Version: "2.8.4", Title: "Stable release", ReleaseDate: now.Add(-60 * day), Java8URL: "https://downloads.gtnewhorizons.com/s.zip"},
	}}, insts: []prism.Instance{in}})

	got := pageLines(m, 78)

	if !containsLine(got, "You have the newest version.") || containsLine(got, "newest stable") {
		t.Errorf("page = %q", got)
	}
}
