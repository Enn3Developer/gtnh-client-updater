package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// C4
func TestC4UnknownVersionAsksWhichVersionItIs(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24, makeInst(t, root, instSpec{name: "Home", gtnh: true}))
	m.focus = focusPage
	const w = 78

	got := pageLines(m, w)

	if meta := got[1]; !strings.HasPrefix(meta, "GTNH · version unknown · ") {
		t.Errorf("meta line = %q", meta)
	}
	text := "I'm not sure which version this is — tell me and I'll check for updates"
	if l := lineWith(got, "I'm not sure"); l != hinted("  ", text, "u", w) {
		t.Errorf("update row = %q", l)
	}
	if ids := strings.Join(rowIDs(m), ","); !strings.HasPrefix(ids, "play,update,versions,memory") {
		t.Errorf("rows = %s", ids)
	}
}

// C4
func TestC4UpToDateSaysSoWithoutAnUpdateRow(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24, makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4"}))
	m.focus = focusPage

	got := pageLines(m, 78)

	found := false
	for _, l := range got {
		if strings.TrimSpace(l) == "You have the newest stable version." {
			found = true
		}
	}
	if !found {
		t.Errorf("no up-to-date line in %q", got)
	}
	if indexOf(rowIDs(m), "update") >= 0 {
		t.Errorf("up to date but rows %v have an update row", rowIDs(m))
	}
}

// C4
func TestC4MetaSaysJava8ForTheJava8Flavor(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24, makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4"}))

	got := pageLines(m, 78)

	if got[1] != "GTNH 2.8.4 · Java 8 · never played" {
		t.Errorf("meta line = %q", got[1])
	}
}

// C4: the kind words of the update row come from the newer release.
func TestC4UpdateRowNamesAReleaseCandidate(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.9.0-RC-1"})
	m, _ := newTestModel(Config{PrismDirs: []string{root}}, 80, 24)
	now := time.Now()
	man := &manifest.Manifest{Releases: []manifest.Release{
		{Version: "2.9.0-RC-2", Title: "Beta release", ReleaseDate: now.Add(-3*day - time.Hour),
			Java8URL: "https://downloads.gtnewhorizons.com/rc2.zip"},
		{Version: "2.9.0-RC-1", Title: "Beta release", ReleaseDate: now.Add(-20 * day),
			Java8URL: "https://downloads.gtnewhorizons.com/rc1.zip"},
		{Version: "2.8.4", Title: "Stable release", ReleaseDate: now.Add(-60 * day),
			Java8URL: "https://downloads.gtnewhorizons.com/s.zip"},
	}}
	m.Update(loadedMsg{m: man, insts: []prism.Instance{in}})
	m.focus = focusPage

	got := pageLines(m, 78)

	if l := lineWith(got, "is out"); l != hinted("  ", "GTNH 2.9.0-RC-2 is out · release candidate · 3 days ago", "u", 78) {
		t.Errorf("update row = %q", l)
	}
}

// C4
func TestC4NoBackupNoUndoRow(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24, makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.1"}))

	ids := strings.Join(rowIDs(m), ",")

	if ids != "play,update,versions,memory,jvm,java,window,server,mods,after,prism" {
		t.Errorf("rows = %s", ids)
	}
}

// C4
func TestC4NonGTNHHasNoUpdateSection(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24, makeInst(t, root, instSpec{name: "Vanilla", version: "2.8.1"}))
	m.showAll = true

	got := pageLines(m, 78)

	if containsLine(got, "Update") || containsLine(got, "Choose another version") {
		t.Errorf("non-GTNH page has an update section: %q", got)
	}
}
