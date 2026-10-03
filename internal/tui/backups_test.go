package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Tests for the undo screens (backups spec C1-C7). Clause numbers in the comments refer
// to the backups spec.

// ---- fixtures ----

const gameRunningText = "the game is still running from this instance -- close Minecraft and try again"

// downgradeInfo is the last update going 2.8.1 -> 2.8.4: undoing it goes back to 2.8.1.
func downgradeInfo() update.BackupInfo {
	return update.BackupInfo{From: "2.8.1", To: "2.8.4", Added: []string{".minecraft/mods/new.jar"}}
}

// writeBackup makes <instDir>/.gtnh-updater/<name>/gtnh-backup.json holding info.
func writeBackup(t *testing.T, instDir, name string, info update.BackupInfo) string {
	t.Helper()
	dir := filepath.Join(instDir, update.StateDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, update.BackupManifest), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// undoModel is routeModel (instance "Pack") with one backup dir per info, on the home
// screen, terminal width w. dirs[i] is the backup dir of infos[i].
func undoModel(t *testing.T, w int, infos ...update.BackupInfo) (*model, []string) {
	t.Helper()
	m := routeModel(t)
	m.Update(tea.WindowSizeMsg{Width: w, Height: termH})
	m.send = func(tea.Msg) {}
	names := []string{"backup-20260101-000000", "backup-20260102-000000", "backup-20260103-000000"}
	var dirs []string
	for i, info := range infos {
		dirs = append(dirs, writeBackup(t, m.inst.Dir, names[i], info))
	}
	m.showHome()
	return m, dirs
}

// toConfirm presses b and enter on home: the confirm screen for the newest backup.
func toConfirm(t *testing.T, m *model) {
	t.Helper()
	press(m, runes("b"), keyEnter)
	if m.screen != scRestoreConfirm {
		t.Fatalf("setup: screen %d after b, enter; want scRestoreConfirm (%d)", m.screen, scRestoreConfirm)
	}
}

// realUndoInstance is the C7 instance: a real Prism instance "Pack 2.8.4" under
// <prismDir>/instances/Pack with one backup that put back config/x.cfg and removes
// mods/new.jar.
func realUndoInstance(t *testing.T, prismDir string) (prism.Instance, string) {
	t.Helper()
	dir := filepath.Join(prismDir, "instances", "Pack")
	writeFile(t, filepath.Join(dir, "instance.cfg"), "name=Pack 2.8.4\n")
	writeFile(t, filepath.Join(dir, "mmc-pack.json"), `{"components":[{"uid":"net.minecraft","version":"1.7.10"}]}`)
	info := downgradeInfo()
	info.When = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bdir := writeBackup(t, dir, "backup-20260101-000000", info)
	writeFile(t, filepath.Join(bdir, ".minecraft", "config", "x.cfg"), "old content")
	writeFile(t, filepath.Join(dir, ".minecraft", "mods", "new.jar"), "jar")
	writeFile(t, filepath.Join(dir, ".minecraft", "config", "x.cfg"), "newer content")
	inst := prism.Instance{Dir: dir, Name: "Pack 2.8.4", GTNH: true, GameDir: filepath.Join(dir, ".minecraft")}
	return inst, bdir
}

// realUndoModel is a 100-column model whose only Prism dir holds realUndoInstance, on home.
func realUndoModel(t *testing.T) (*model, prism.Instance, string) {
	t.Helper()
	m := sizedModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	prismDir := t.TempDir()
	m.cfg.PrismDirs = []string{prismDir}
	m.manifest = routeManifest(t)
	inst, bdir := realUndoInstance(t, prismDir)
	m.insts = []prism.Instance{inst}
	m.send = func(tea.Msg) {}
	m.showHome()
	return m, inst, bdir
}

// ---- C1 card, C2 list, C7 real restore ----

func TestUndoCardOffersBackupThenBListsIt(t *testing.T) { // C1, C2, C7
	m, inst, bdir := realUndoModel(t)
	if v := view(m); !strings.Contains(v, "Undo back to 2.8.1 — press b") {
		t.Errorf("home card %q lacks %q", v, "Undo back to 2.8.1 — press b")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Undo       back to 2.8.1 — press b") {
		t.Errorf("home card lacks the padded line %q:\n%s", "Undo       back to 2.8.1 — press b", v)
	}
	press(m, runes("b"))
	items := m.list.Items()
	if m.screen != scBackups || !m.isListScreen() || m.inst.Dir != inst.Dir || m.list.Title != "Undo the last update of Pack 2.8.4?" || len(items) != 1 {
		t.Fatalf("b on home: screen %d, list screen %v, inst %q, title %q, %d items; want scBackups (%d), true, Pack, %q, 1",
			m.screen, m.isListScreen(), m.inst.Dir, m.list.Title, len(items), scBackups, "Undo the last update of Pack 2.8.4?")
	}
	if it := items[0].(item); it.key != bdir || it.title != "Go back to GTNH 2.8.1" {
		t.Errorf("backup item = %+v, want key %q, title %q", it, bdir, "Go back to GTNH 2.8.1")
	}
}

func TestUndoCardLineSitsRightAfterUpdateLine(t *testing.T) { // C1
	m, _, _ := realUndoModel(t)
	v := ansi.Strip(m.View())
	lineOf := func(label string) int {
		for i, l := range strings.Split(v, "\n") {
			if strings.Contains(l, label) {
				return i
			}
		}
		return -1
	}
	upd, undo, played := lineOf("Update     "), lineOf("Undo       "), lineOf("Played     ")
	press(m, runes("b"))
	if upd < 0 || undo != upd+1 || played != undo+1 || m.screen != scBackups {
		t.Errorf("card lines Update/Undo/Played at %d/%d/%d, then b gave screen %d; want consecutive lines, then scBackups (%d):\n%s",
			upd, undo, played, m.screen, scBackups, v)
	}
}

func TestUndoRealRestorePutsFilesBackAndSummarises(t *testing.T) { // C3, C4, C5, C7
	m, inst, bdir := realUndoModel(t)
	toConfirm(t, m)
	if body := pageBody(m); !strings.Contains(body, "! This goes BACK to an older version.") {
		t.Errorf("confirm body %q lacks the downgrade warning", body)
	}
	cmd := press(m, keyEnter)
	if m.screen != scRestoring {
		t.Fatalf("enter on confirm: screen %d, want scRestoring (%d)", m.screen, scRestoring)
	}
	press(m, msgOf[restoredMsg](t, cmd))
	if _, err := os.Stat(filepath.Join(inst.Dir, ".minecraft", "mods", "new.jar")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("new.jar after restore: stat err %v, want not exist", err)
	}
	if data, err := os.ReadFile(filepath.Join(inst.Dir, ".minecraft", "config", "x.cfg")); err != nil || string(data) != "old content" {
		t.Errorf("x.cfg after restore = %q (err %v), want %q", data, err, "old content")
	}
	if _, err := os.Stat(bdir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("backup dir after restore: stat err %v, want not exist", err)
	}
	if body := pageBody(m); m.screen != scRestored || !strings.Contains(body, "• 1 file put back, 1 removed.") {
		t.Errorf("after restore: screen %d, body %q; want scRestored (%d) with %q", m.screen, body, scRestored, "• 1 file put back, 1 removed.")
	}
}

func TestUndoRestoredEnterReloadsHomeWithNothingToUndo(t *testing.T) { // C5, C7
	m, inst, _ := realUndoModel(t)
	toConfirm(t, m)
	press(m, msgOf[restoredMsg](t, press(m, keyEnter)))
	cmd := press(m, keyEnter)
	if m.screen != scLoading {
		t.Fatalf("enter on restored: screen %d, want scLoading (%d)", m.screen, scLoading)
	}
	press(m, msgOf[reloadedMsg](t, cmd))
	if v := view(m); m.screen != scHome || len(m.insts) != 1 || m.insts[0].Dir != inst.Dir || !strings.Contains(v, "Undo nothing to undo") {
		t.Errorf("after reload: screen %d, %d instances, view %q; want scHome, Pack, \"Undo nothing to undo\"", m.screen, len(m.insts), v)
	}
}

func TestUndoNonGTNHCardHasNoUndoLine(t *testing.T) { // C1, C2
	m := routeModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: termH})
	other := prism.Instance{Dir: t.TempDir(), Name: "Vanilla"}
	m.insts = append(m.insts, other)
	m.showAll, m.inst = true, other
	m.showHome()
	v := view(m)
	press(m, runes("b"))
	if strings.Contains(v, "Undo") || !strings.Contains(v, "Update —") || m.screen != scBackups || !strings.Contains(pageBody(m), "Nothing to undo for Vanilla") {
		t.Errorf("non-GTNH card %q, then b: screen %d, body %q; want no Undo line, then the Nothing to undo page", v, m.screen, pageBody(m))
	}
}

// ---- C2 nothing to undo ----

func TestUndoWithoutBackupCardSaysNothingAndBExplains(t *testing.T) { // C1, C2, C7
	m, _ := undoModel(t, 100)
	v := view(m)
	press(m, runes("b"))
	want := "Nothing to undo for Pack I only keep the files from the last update I did here, and there isn't one."
	if !strings.Contains(v, "Undo nothing to undo") || m.screen != scBackups || m.isListScreen() || !strings.HasPrefix(pageBody(m), want) {
		t.Errorf("no backup: card %q, then b: screen %d, list screen %v, body %q; want \"Undo nothing to undo\", scBackups page starting %q",
			v, m.screen, m.isListScreen(), pageBody(m), want)
	}
	if f := pageFooter(m); f != "esc back" {
		t.Errorf("nothing-to-undo footer = %q, want %q", f, "esc back")
	}
	if v := view(m); !strings.Contains(v, "Nothing to undo for Pack") || !strings.Contains(v, "esc back") {
		t.Errorf("nothing-to-undo view %q: want the page with its title and footer", v)
	}
}

func TestUndoNothingPageEscGoesHome(t *testing.T) { // C2, C7
	m, _ := undoModel(t, termW)
	insts := m.insts
	press(m, runes("b"), keyEsc)
	if m.screen != scHome || len(m.insts) != 1 || m.insts[0].Dir != insts[0].Dir || m.quitting {
		t.Errorf("esc on nothing to undo: screen %d, insts %+v, quitting %v; want scHome (%d), the same instance, false", m.screen, m.insts, m.quitting, scHome)
	}
}

func TestUndoNothingPageQQuits(t *testing.T) { // C2
	m, _ := undoModel(t, termW)
	press(m, runes("b"))
	cmd := press(m, runes("q"))
	if !m.quitting || !isQuit(cmd) {
		t.Errorf("q on nothing to undo: quitting %v, quit cmd %v; want true, true", m.quitting, isQuit(cmd))
	}
}

func TestUndoWithoutBackupDoesNotAskWhetherGameRuns(t *testing.T) { // C3
	m, _ := undoModel(t, termW)
	calls := 0
	m.isRunning = func(prism.Instance) (bool, error) { calls++; return true, nil }
	press(m, runes("b"))
	if calls != 0 || m.screen != scBackups || !strings.HasPrefix(pageBody(m), "Nothing to undo for Pack") {
		t.Errorf("b without backup and the game running: isRunning calls %d, screen %d, body %q; want 0, the Nothing to undo page", calls, m.screen, pageBody(m))
	}
}

// ---- C2 backup list ----

func TestUndoListShowsEveryBackupNewestFirst(t *testing.T) { // C2
	older := update.BackupInfo{From: "2.8.0", To: "2.8.1", When: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	newer := update.BackupInfo{From: "2.8.1", To: "2.8.4", When: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
	m, dirs := undoModel(t, termW, older, newer)
	press(m, runes("b"))
	items := m.list.Items()
	if len(items) != 2 {
		t.Fatalf("backup list has %d items, want 2", len(items))
	}
	first, second := items[0].(item), items[1].(item)
	if first.key != dirs[1] || first.title != "Go back to GTNH 2.8.1" || second.key != dirs[0] || second.title != "Go back to GTNH 2.8.0" {
		t.Errorf("backup items = %+v, %+v; want the 2.8.1 backup (%s) first, then 2.8.0 (%s)", first, second, dirs[1], dirs[0])
	}
}

func TestUndoListItemDescCountsAddedFilesAndModsWithoutAgo(t *testing.T) { // C2
	info := update.BackupInfo{From: "2.8.1", To: "2.8.4", Added: []string{".minecraft/mods/new.jar"}, AddedMods: []string{"extra.jar"}}
	m, _ := undoModel(t, termW, info)
	press(m, runes("b"))
	want := "updated to 2.8.4 · 2 added files removed, replaced files put back"
	if got := m.list.Items()[0].(item).desc; got != want {
		t.Errorf("backup desc = %q, want %q", got, want)
	}
}

func TestUndoListViewRendersTheBackupList(t *testing.T) { // C2
	info := update.BackupInfo{From: "2.8.1", To: "2.8.4", Added: []string{".minecraft/mods/new.jar"}}
	m, _ := undoModel(t, termW, info)
	press(m, runes("b"))
	v := view(m)
	_, widest := fit(m.View())
	for _, want := range []string{"GTNH Launcher 1.2.3", "Undo the last update of Pack?", "Go back to GTNH 2.8.1", "updated to 2.8.4 · 1 added file removed, replaced files put back"} {
		if !strings.Contains(v, want) {
			t.Errorf("backup list view %q lacks %q", v, want)
		}
	}
	if strings.Contains(v, "Nothing to undo") || widest > termW {
		t.Errorf("backup list view %q (widest line %d): want the list, not the nothing-to-undo page, within %d columns", v, widest, termW)
	}
}

func TestUndoListEnterChoosesSelectedBackup(t *testing.T) { // C2
	older := update.BackupInfo{From: "2.8.0", To: "2.8.1", When: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	newer := update.BackupInfo{From: "2.8.1", To: "2.8.4", When: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
	m, dirs := undoModel(t, termW, older, newer)
	press(m, runes("b"), keyDown, keyEnter)
	if m.screen != scRestoreConfirm || m.backup.Dir != dirs[0] || m.backup.Info.From != "2.8.0" {
		t.Errorf("down, enter on the list: screen %d, backup %+v; want scRestoreConfirm (%d), the 2.8.0 backup in %s", m.screen, m.backup, scRestoreConfirm, dirs[0])
	}
}

func TestUndoListEscGoesHome(t *testing.T) { // C2
	m, _ := undoModel(t, termW, downgradeInfo())
	press(m, runes("b"), keyEsc)
	if m.screen != scHome || m.list.Title != "Which instance do you want to play?" {
		t.Errorf("esc on the backup list: screen %d, title %q; want scHome (%d) with the home list", m.screen, m.list.Title, scHome)
	}
}

func TestUndoListQQuits(t *testing.T) { // C2
	m, _ := undoModel(t, termW, downgradeInfo())
	press(m, runes("b"))
	cmd := press(m, runes("q"))
	if !m.quitting || !isQuit(cmd) {
		t.Errorf("q on the backup list: quitting %v, quit cmd %v; want true, true", m.quitting, isQuit(cmd))
	}
}

func TestBackupDescSaysWhenAndCountsOneFile(t *testing.T) { // C2
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	b := update.Backup{Dir: "d", Info: update.BackupInfo{From: "2.8.1", To: "2.8.4", When: now.Add(-72 * time.Hour), Added: []string{".minecraft/mods/new.jar"}}}
	want := "updated to 2.8.4 3 days ago · 1 added file removed, replaced files put back"
	if got := backupDesc(b, now); got != want {
		t.Errorf("backupDesc = %q, want %q", got, want)
	}
}

func TestBackupDescSumsAddedFilesAndMods(t *testing.T) { // C2
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	b := update.Backup{Dir: "d", Info: update.BackupInfo{From: "2.8.0", To: "2.8.1", When: now.Add(-time.Hour),
		Added: []string{"a", "b"}, AddedMods: []string{"c.jar"}}}
	want := "updated to 2.8.1 today · 3 added files removed, replaced files put back"
	if got := backupDesc(b, now); got != want {
		t.Errorf("backupDesc = %q, want %q", got, want)
	}
}

func TestBackupDescOnlyAddedModsSingular(t *testing.T) { // C2
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	b := update.Backup{Dir: "d", Info: update.BackupInfo{From: "2.8.1", To: "2.8.4", AddedMods: []string{"c.jar"}}}
	want := "updated to 2.8.4 · 1 added file removed, replaced files put back"
	if got := backupDesc(b, now); got != want {
		t.Errorf("backupDesc = %q, want %q", got, want)
	}
}

func TestBackupDescNothingAddedIsPlural(t *testing.T) { // C2
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	b := update.Backup{Dir: "d", Info: update.BackupInfo{From: "2.8.1", To: "2.8.4"}}
	want := "updated to 2.8.4 · 0 added files removed, replaced files put back"
	if got := backupDesc(b, now); got != want {
		t.Errorf("backupDesc = %q, want %q", got, want)
	}
}

// ---- C3 is the game running? ----

func TestUndoWhenGameRunsShowsErrorAndEscGoesHome(t *testing.T) { // C3, C7
	m, _ := undoModel(t, termW, downgradeInfo())
	var asked []prism.Instance
	m.isRunning = func(in prism.Instance) (bool, error) { asked = append(asked, in); return true, nil }
	cmd := press(m, runes("b"))
	press(m, msgOf[errMsg](t, cmd))
	want := "Pack is running right now. Close Minecraft, then try again."
	if m.screen != scError || m.err == nil || m.err.Error() != want || len(asked) != 1 || asked[0].Dir != m.inst.Dir {
		t.Fatalf("b while the game runs: screen %d, err %v, asked %+v; want scError (%d), %q, one call for Pack", m.screen, m.err, asked, scError, want)
	}
	press(m, keyEsc)
	if m.screen != scHome || m.quitting {
		t.Errorf("esc on the running error: screen %d, quitting %v; want scHome (%d), false", m.screen, m.quitting, scHome)
	}
}

func TestUndoWhenRunningIsUnknownWarnsOnConfirm(t *testing.T) { // C3, C7
	m, _ := undoModel(t, termW, downgradeInfo())
	m.isRunning = func(prism.Instance) (bool, error) { return false, errors.New("can't tell") }
	toConfirm(t, m)
	if body := pageBody(m); !m.runUnknown || !strings.Contains(body, "Make sure Minecraft is closed before you continue.") {
		t.Errorf("isRunning error: runUnknown %v, confirm body %q; want true and the close-Minecraft line", m.runUnknown, body)
	}
}

func TestUndoWhenGameIsNotRunningHasNoCloseLine(t *testing.T) { // C3
	m, _ := undoModel(t, termW, downgradeInfo())
	m.runUnknown = true
	toConfirm(t, m)
	if body := pageBody(m); m.runUnknown || strings.Contains(body, "Make sure Minecraft is closed") {
		t.Errorf("isRunning (false, nil): runUnknown %v, confirm body %q; want false, no close-Minecraft line", m.runUnknown, body)
	}
}

// ---- C3 confirm page ----

func TestUndoConfirmTitleWarningAndBulletsInOrder(t *testing.T) { // C3
	info := downgradeInfo()
	info.PrevName = "Pack old"
	m, _ := undoModel(t, termW, info)
	m.isRunning = func(prism.Instance) (bool, error) { return false, errors.New("can't tell") }
	toConfirm(t, m)
	m.warns = []string{"one mod looked edited by hand"}
	body := pageBody(m)
	parts := []string{
		"Ready to put Pack back on GTNH 2.8.1",
		"! This goes BACK to an older version. Worlds you played on 2.8.4 may lose blocks and items or not load at all. Copy your saves folder somewhere safe first.",
		"• Files that update added are removed and the files it replaced are put back exactly as they were.",
		"• Your worlds, screenshots, maps and game settings stay exactly as they are.",
		"• If you changed settings since, they're kept (server address, server mods link, Java and memory).",
		"• The instance name in Prism goes back too.",
		"Make sure Minecraft is closed before you continue.",
		"Heads up: one mod looked edited by hand",
	}
	at := 0
	for _, p := range parts {
		i := strings.Index(body[at:], p)
		if i < 0 {
			t.Fatalf("confirm body lacks %q after position %d (or out of order):\n%s", p, at, body)
		}
		at += i + len(p)
	}
	if !strings.HasPrefix(body, parts[0]) {
		t.Errorf("confirm body %q should start with the title %q", body, parts[0])
	}
}

func TestUndoConfirmFooter(t *testing.T) { // C3
	m, _ := undoModel(t, termW, downgradeInfo())
	toConfirm(t, m)
	if f := pageFooter(m); f != "enter undo now esc back" { // chrome C9
		t.Errorf("confirm footer = %q, want %q", f, "enter undo now esc back")
	}
}

func TestUndoConfirmNoDowngradeWarningWhenGoingBackToNewer(t *testing.T) { // C3, C7, K1
	m, _ := undoModel(t, termW, update.BackupInfo{From: "2.8.4", To: "2.8.1"})
	toConfirm(t, m)
	if body := pageBody(m); strings.Contains(body, "This goes BACK") || !strings.HasPrefix(body, "Ready to put Pack back on GTNH 2.8.4") {
		t.Errorf("confirm 2.8.1 -> back to 2.8.4: body %q; want the title and no downgrade warning", body)
	}
}

func TestUndoConfirmNoNameBulletWhenNameKeepsNoVersion(t *testing.T) { // C3
	m, _ := undoModel(t, termW, update.BackupInfo{From: "2.8.1", To: "2.8.4"})
	toConfirm(t, m)
	if body := pageBody(m); strings.Contains(body, "The instance name in Prism goes back too.") || !strings.Contains(body, "• Your worlds, screenshots") {
		t.Errorf("confirm for Pack without PrevName: body %q; want the bullets without the name one", body)
	}
}

func TestUndoConfirmNameBulletWhenNameHasTheVersion(t *testing.T) { // C3
	m, _ := undoModel(t, termW, update.BackupInfo{From: "2.8.1", To: "2.8.4"})
	m.insts[0].Name = "Pack 2.8.4"
	m.showHome()
	toConfirm(t, m)
	if body := pageBody(m); !strings.Contains(body, "• The instance name in Prism goes back too.") {
		t.Errorf("confirm for \"Pack 2.8.4\" undoing 2.8.4: body %q lacks the name bullet", body)
	}
}

func TestUndoConfirmEscGoesBackToList(t *testing.T) { // C3
	m, dirs := undoModel(t, termW, downgradeInfo())
	toConfirm(t, m)
	press(m, keyEsc)
	if m.screen != scBackups || !m.isListScreen() || len(m.list.Items()) != 1 || m.list.Items()[0].(item).key != dirs[0] {
		t.Errorf("esc on confirm: screen %d, list screen %v, items %+v; want the backup list", m.screen, m.isListScreen(), m.list.Items())
	}
}

// ---- C4 restoring ----

func TestUndoEnterOnConfirmRunsRestoreSeamInBackground(t *testing.T) { // C4
	m, dirs := undoModel(t, termW, downgradeInfo())
	var sent []tea.Msg
	m.send = func(msg tea.Msg) { sent = append(sent, msg) }
	want := &update.RestoreResult{From: "2.8.4", To: "2.8.1", MovedBack: 1, Removed: 1}
	var gotInst prism.Instance
	var gotBackup update.Backup
	m.restore = func(in prism.Instance, b update.Backup, rep update.Reporter) (*update.RestoreResult, error) {
		gotInst, gotBackup = in, b
		rep.Step("Removing files the update added")
		return want, nil
	}
	toConfirm(t, m)
	m.steps, m.step, m.warns = []string{"old"}, "old step", []string{"old warning"}
	cmd := press(m, keyEnter)
	if m.screen != scRestoring || m.steps != nil || m.step != "" || m.warns != nil || cmd == nil {
		t.Fatalf("enter on confirm: screen %d, steps %v, step %q, warns %v, cmd nil %v; want scRestoring (%d), reset, a cmd",
			m.screen, m.steps, m.step, m.warns, cmd == nil, scRestoring)
	}
	msg := msgOf[restoredMsg](t, cmd)
	if msg.r != want || gotInst.Dir != m.inst.Dir || gotBackup.Dir != dirs[0] || len(sent) != 1 || sent[0] != stepMsg("Removing files the update added") {
		t.Errorf("restore cmd: msg %+v, seam got inst %q backup %q, sent %#v; want the seam's result, Pack, %q, its step through m.send",
			msg, gotInst.Dir, gotBackup.Dir, sent, dirs[0])
	}
	press(m, msg)
	if m.screen != scRestored || m.restored != want {
		t.Errorf("restoredMsg: screen %d, restored %+v; want scRestored (%d), the result", m.screen, m.restored, scRestored)
	}
}

func TestUndoRestoringViewShowsTitleStepsAndFooter(t *testing.T) { // C4
	m, _ := undoModel(t, termW, downgradeInfo())
	toConfirm(t, m)
	press(m, keyEnter, stepMsg("Removing files the update added"), stepMsg("Putting the old files back"), progressMsg{1, 2})
	body := pageBody(m)
	for _, want := range []string{"Putting Pack back on GTNH 2.8.1", "✓ Removing files the update added", "Putting the old files back", "1 of 2 files"} {
		if !strings.Contains(body, want) {
			t.Errorf("restoring body %q lacks %q", body, want)
		}
	}
	if f := pageFooter(m); !strings.Contains(f, "Please don't close this window until I'm done.") {
		t.Errorf("restoring footer = %q, want the don't-close line", f)
	}
}

func TestUndoRestoringIgnoresCtrlCAndKeys(t *testing.T) { // C4, C7
	m, _ := undoModel(t, termW, downgradeInfo())
	toConfirm(t, m)
	press(m, keyEnter)
	ctrlC := press(m, keyCtrlC)
	q := press(m, runes("q"))
	esc := press(m, keyEsc)
	if m.screen != scRestoring || m.quitting || ctrlC != nil || q != nil || esc != nil {
		t.Errorf("ctrl+c, q, esc while restoring: screen %d, quitting %v, cmds nil %v/%v/%v; want scRestoring (%d), false, all nil",
			m.screen, m.quitting, ctrlC == nil, q == nil, esc == nil, scRestoring)
	}
}

// ---- C5 restored page ----

// restoredModel ran a restore of the 2.8.1 -> 2.8.4 backup through a seam returning r.
func restoredModel(t *testing.T, r *update.RestoreResult) *model {
	t.Helper()
	m, _ := undoModel(t, termW, downgradeInfo())
	m.restore = func(prism.Instance, update.Backup, update.Reporter) (*update.RestoreResult, error) { return r, nil }
	toConfirm(t, m)
	press(m, msgOf[restoredMsg](t, press(m, keyEnter)))
	return m
}

func TestUndoRestoredPageSummary(t *testing.T) { // C5
	m := restoredModel(t, &update.RestoreResult{From: "2.8.4", To: "2.8.1", MovedBack: 2})
	body := pageBody(m)
	if !strings.HasPrefix(body, "All done! Pack is back on GTNH 2.8.1.") || !strings.HasSuffix(body, "• 2 files put back, 0 removed.") ||
		strings.Contains(body, "Press p") || // chrome C9: the footer says it
		strings.Contains(body, "Renamed") || strings.Contains(body, "couldn't put these back") {
		t.Errorf("restored body %q; want the All done title, \"2 files put back, 0 removed.\" last, no play sentence, no rename/skipped bullets", body)
	}
	if f := pageFooter(m); f != "enter back p play now q quit" {
		t.Errorf("restored footer = %q, want %q", f, "enter back p play now q quit")
	}
}

func TestUndoRestoredPageRenamedSkippedAndWarnings(t *testing.T) { // C5, C7
	m := routeModel(t)
	m.Update(tea.WindowSizeMsg{Width: 300, Height: termH})
	bdir := filepath.Join("Pack", ".gtnh-updater", "backup-20260101-000000")
	m.screen = scRestored
	m.backup = update.Backup{Dir: bdir, Info: downgradeInfo()}
	m.restored = &update.RestoreResult{From: "2.8.4", To: "2.8.1", MovedBack: 1, Removed: 1, Renamed: "Pack 2.8.1",
		Skipped: []string{"_external/a.cfg", "_external/b.cfg"}}
	m.warns = []string{"could not restore something"}
	body := pageBody(m)
	parts := []string{
		"All done! Pack is back on GTNH 2.8.1.",
		"• 1 file put back, 1 removed.",
		"• Renamed the instance in Prism to \"Pack 2.8.1\".",
		"• I couldn't put these back myself (they live outside the instance folder): _external/a.cfg, _external/b.cfg. They're still in " + bdir + ".",
		"Heads up: could not restore something",
	}
	if strings.Contains(body, "Press p") { // chrome C9
		t.Errorf("restored body still has the play sentence:\n%s", body)
	}
	at := 0
	for _, p := range parts {
		i := strings.Index(body[at:], p)
		if i < 0 {
			t.Fatalf("restored body lacks %q after position %d (or out of order):\n%s", p, at, body)
		}
		at += i + len(p)
	}
}

func TestUndoRestoredEscAlsoReloadsHome(t *testing.T) { // C5
	m := restoredModel(t, &update.RestoreResult{From: "2.8.4", To: "2.8.1"})
	cmd := press(m, keyEsc)
	if m.screen != scLoading || m.restored != nil {
		t.Fatalf("esc on restored: screen %d, restored %+v; want scLoading (%d), nil", m.screen, m.restored, scLoading)
	}
	msgOf[reloadedMsg](t, cmd)
}

func TestUndoRestoredPPlays(t *testing.T) { // C5
	m := restoredModel(t, &update.RestoreResult{From: "2.8.4", To: "2.8.1"})
	fp := &fakePrism{}
	fp.install(m)
	cmd := press(m, runes("p"))
	msgOf[launchedMsg](t, cmd)
	if m.screen != scLaunching || len(fp.launches) != 1 || fp.launches[0].inst.Dir != m.inst.Dir || fp.launches[0].server != "" {
		t.Errorf("p on restored: screen %d, launches %+v; want scLaunching (%d), Pack launched without a server", m.screen, fp.launches, scLaunching)
	}
}

func TestUndoRestoredQQuits(t *testing.T) { // C5
	m := restoredModel(t, &update.RestoreResult{From: "2.8.4", To: "2.8.1"})
	cmd := press(m, runes("q"))
	if !m.quitting || !isQuit(cmd) {
		t.Errorf("q on restored: quitting %v, quit cmd %v; want true, true", m.quitting, isQuit(cmd))
	}
}

// ---- C6 restore errors ----

// failedRestoreModel ran a restore of the 2.8.1 -> 2.8.4 backup through a seam failing
// with err, on a 300-column terminal so paths aren't wrapped.
func failedRestoreModel(t *testing.T, err error) (*model, string) {
	t.Helper()
	m, dirs := undoModel(t, 300, downgradeInfo())
	m.restore = func(prism.Instance, update.Backup, update.Reporter) (*update.RestoreResult, error) { return nil, err }
	toConfirm(t, m)
	press(m, msgOf[errMsg](t, press(m, keyEnter)))
	return m, dirs[0]
}

func TestUndoRestoreFailureShowsBackupPathAndCannotGoBack(t *testing.T) { // C6, C7, K2
	m, bdir := failedRestoreModel(t, errors.New("disk full"))
	body := pageBody(m)
	want := "Some files may have changed. The backup folder is still there, so you can try again: " + bdir
	if m.screen != scError || m.errPhase != scRestoring || !strings.Contains(body, "disk full "+want) || strings.Contains(body, "Nothing was changed.") {
		t.Errorf("failed restore: screen %d, errPhase %d, body %q; want scError, scRestoring (%d), the error then %q, no \"Nothing was changed.\"",
			m.screen, m.errPhase, body, scRestoring, want)
	}
	if f := pageFooter(m); f != "enter quit" {
		t.Errorf("failed restore footer = %q, want %q", f, "enter quit")
	}
}

func TestUndoRestoreFailureEscQuits(t *testing.T) { // C6
	m, _ := failedRestoreModel(t, errors.New("disk full"))
	cmd := press(m, keyEsc)
	if !m.quitting || !isQuit(cmd) {
		t.Errorf("esc after a failed restore: quitting %v, quit cmd %v; want true, true", m.quitting, isQuit(cmd))
	}
}

func TestUndoRestoreRefusedWhileGameRunsChangedNothing(t *testing.T) { // C6, C7, K2
	m, bdir := failedRestoreModel(t, update.ErrGameRunning)
	body := pageBody(m)
	if m.screen != scError || m.errPhase != scRestoring || !strings.Contains(body, gameRunningText+" Nothing was changed.") ||
		strings.Contains(body, "Some files may have changed") || strings.Contains(body, bdir) {
		t.Errorf("restore refused (game running): screen %d, errPhase %d, body %q; want scError, scRestoring, \"Nothing was changed.\" and no backup path",
			m.screen, m.errPhase, body)
	}
	if f := pageFooter(m); f != "esc back enter quit" {
		t.Errorf("game-running footer = %q, want %q", f, "esc back enter quit")
	}
}

func TestUndoRestoreRefusedWhileGameRunsEscGoesHome(t *testing.T) { // C6, C7
	m, _ := failedRestoreModel(t, update.ErrGameRunning)
	press(m, keyEsc)
	if m.screen != scHome || m.quitting {
		t.Errorf("esc after the game-running error: screen %d, quitting %v; want scHome (%d), false", m.screen, m.quitting, scHome)
	}
}

func TestUndoRestoreRefusedWhileGameRunsWrappedChangedNothing(t *testing.T) { // polish C2: errors.Is sees through wrapping
	m, bdir := failedRestoreModel(t, fmt.Errorf("restore: %w", update.ErrGameRunning))
	body := pageBody(m)
	if !strings.Contains(body, gameRunningText+" Nothing was changed.") || strings.Contains(body, "Some files may have changed") || strings.Contains(body, bdir) {
		t.Errorf("restore refused (wrapped ErrGameRunning): body %q; want \"Nothing was changed.\" and no backup path", body)
	}
	if f := pageFooter(m); f != "esc back enter quit" {
		t.Errorf("wrapped game-running footer = %q, want %q", f, "esc back enter quit")
	}
}

func TestUndoRestoreLookAlikeErrorIsNotGameRunning(t *testing.T) { // polish C2: matched by identity, not by text
	m, bdir := failedRestoreModel(t, errors.New(gameRunningText))
	body := pageBody(m)
	want := "Some files may have changed. The backup folder is still there, so you can try again: " + bdir
	if !strings.Contains(body, want) || strings.Contains(body, "Nothing was changed.") {
		t.Errorf("restore failed with a look-alike error: body %q; want %q and no \"Nothing was changed.\"", body, want)
	}
	if f := pageFooter(m); f != "enter quit" {
		t.Errorf("look-alike error footer = %q, want %q", f, "enter quit")
	}
}

func TestRestoreErrorNoteMatchesGameRunningWithErrorsIs(t *testing.T) { // polish C2
	cases := []struct {
		name    string
		err     error
		nothing bool // the note says "Nothing was changed." rather than "Some files may have changed"
	}{
		{"ErrGameRunning", update.ErrGameRunning, true},
		{"wrapped ErrGameRunning", fmt.Errorf("restore: %w", update.ErrGameRunning), true},
		{"same text, different error", errors.New(gameRunningText), false},
		{"disk full", errors.New("disk full"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := failedRestoreModel(t, c.err)
			note := m.restoreErrorNote()
			nothing := strings.Contains(note, "Nothing was changed.")
			some := strings.Contains(note, "Some files may have changed")
			if nothing != c.nothing || some == c.nothing {
				t.Errorf("restoreErrorNote() = %q; has \"Nothing was changed.\" %v, has \"Some files may have changed\" %v; want %v, %v",
					note, nothing, some, c.nothing, !c.nothing)
			}
		})
	}
}
