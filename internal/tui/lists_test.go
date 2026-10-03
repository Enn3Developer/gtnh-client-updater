package tui

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// The list status bar ("N choices") shows only on the installed/target/resolve lists
// when they hold more than 8 items, and never on home, settings, conflicts or backups.

// wantItems fails the setup unless the list on screen holds exactly n items.
func wantItems(t *testing.T, m *model, n int) {
	t.Helper()
	if got := len(m.list.Items()); got != n {
		t.Fatalf("setup: list holds %d items, want %d", got, n)
	}
}

// targetsWith is routeModel on the target list of a manifest with n releases.
func targetsWith(t *testing.T, n int) *model {
	t.Helper()
	m := routeModel(t)
	m.manifest = manyReleasesManifest(t, n)
	m.showTargets()
	if m.screen != scTarget {
		t.Fatalf("setup: screen %d after showTargets, want scTarget (%d)", m.screen, scTarget)
	}
	wantItems(t, m, n)
	return m
}

func TestListStatusBar(t *testing.T) {
	t.Run("settings with 10 rows hides it", func(t *testing.T) { // C1
		f := settingsFixture(t, plainCfg, true)
		f.open("")
		if f.m.screen != scSettings {
			t.Fatalf("setup: screen %d, want scSettings (%d)", f.m.screen, scSettings)
		}
		wantItems(t, f.m, 10)
		if f.m.list.ShowStatusBar() {
			t.Errorf("status bar shown on settings with 10 rows, want hidden")
		}
	})

	t.Run("target with 30 releases shows it", func(t *testing.T) { // C2
		m := targetsWith(t, 30)
		if !m.list.ShowStatusBar() {
			t.Errorf("status bar hidden on target list with 30 releases, want shown")
		}
	})

	t.Run("conflicts with 3 items hides it", func(t *testing.T) { // C3
		m := routeModel(t)
		prepared(t, m)
		wantItems(t, m, 3)
		if m.list.ShowStatusBar() {
			t.Errorf("status bar shown on conflicts, want hidden")
		}
	})

	t.Run("target with 8 releases hides it", func(t *testing.T) { // C4: 8 is not more than 8
		m := targetsWith(t, 8)
		if m.list.ShowStatusBar() {
			t.Errorf("status bar shown on target list with 8 releases, want hidden")
		}
	})

	t.Run("target with 9 releases shows it", func(t *testing.T) { // C4: 9 is more than 8
		m := targetsWith(t, 9)
		if !m.list.ShowStatusBar() {
			t.Errorf("status bar hidden on target list with 9 releases, want shown")
		}
	})

	t.Run("home with 10 GTNH instances hides it", func(t *testing.T) { // C5
		h := newHome(t, termW, update.State{})
		for i := range 8 {
			name := fmt.Sprintf("Extra%d", i)
			h.m.insts = append(h.m.insts,
				gtnhInstance(t, filepath.Join(h.dir2, "instances", name), name, update.State{Version: "2.8.4"}))
		}
		h.m.showHome()
		if h.m.screen != scHome {
			t.Fatalf("setup: screen %d, want scHome (%d)", h.m.screen, scHome)
		}
		wantItems(t, h.m, 10)
		if h.m.list.ShowStatusBar() {
			t.Errorf("status bar shown on home with 10 instances, want hidden")
		}
	})

	t.Run("resolve with 9 conflicting files shows it", func(t *testing.T) { // C6
		m := routeModel(t)
		pl := &update.Plan{}
		for i := range 9 {
			pl.Actions = append(pl.Actions, update.Action{Kind: update.Conflict, Path: fmt.Sprintf(".minecraft/config/f%d.cfg", i)})
		}
		m.Update(preparedMsg{&update.Session{Plan: pl}})
		if m.screen != scConflicts {
			t.Fatalf("setup: screen %d after preparedMsg, want scConflicts (%d)", m.screen, scConflicts)
		}
		press(m, keyDown, keyDown, keyEnter) // "Let me choose file by file" is the third item
		if m.screen != scResolve {
			t.Fatalf("setup: screen %d after picking file by file, want scResolve (%d)", m.screen, scResolve)
		}
		wantItems(t, m, 9)
		if !m.list.ShowStatusBar() {
			t.Errorf("status bar hidden on resolve with 9 files, want shown")
		}
	})

	t.Run("backups with 9 backups hides it", func(t *testing.T) { // T: never on backups
		m := routeModel(t)
		for i := range 9 {
			writeBackup(t, m.inst.Dir, fmt.Sprintf("backup-2026010%d-000000", i+1), downgradeInfo())
		}
		m.showBackups()
		if m.screen != scBackups {
			t.Fatalf("setup: screen %d, want scBackups (%d)", m.screen, scBackups)
		}
		if got := len(m.list.Items()); got < 9 {
			t.Fatalf("setup: backups list holds %d items, want at least 9", got)
		}
		if m.list.ShowStatusBar() {
			t.Errorf("status bar shown on backups with 9 backups, want hidden")
		}
	})
}
