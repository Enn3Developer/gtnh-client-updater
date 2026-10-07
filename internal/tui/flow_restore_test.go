package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Fixtures of the undo flow: an injected restore that records its calls and never
// touches the disk or its Reporter.

const (
	undoLine1  = "Files the update added are removed and the files it replaced are put back exactly as they were"
	undoLine2  = "Worlds, maps and settings aren't touched"
	undoLine3  = "Settings you changed since then are kept (server, mods link, memory, Java)"
	undoMods   = "Your server's mods stay as they are: they follow the server, not the GTNH version"
	renameLine = "The instance name in Prism goes back too"
	closeFirst = "Make sure Minecraft is closed before you continue."
)

type restoreCall struct {
	inst prism.Instance
	b    update.Backup
}

type fakeRestore struct {
	calls []restoreCall
	res   *update.RestoreResult
	err   error
}

// inject makes m.restore record its calls and answer with f.res / f.err.
func (f *fakeRestore) inject(m *model) {
	m.restore = func(inst prism.Instance, b update.Backup, _ update.Reporter) (*update.RestoreResult, error) {
		f.calls = append(f.calls, restoreCall{inst, b})
		return f.res, f.err
	}
}

// fixtureBackupDir is where makeInst writes an instance's backup.
func fixtureBackupDir(in prism.Instance) string {
	return filepath.Join(in.Dir, update.StateDir, "backup-20260101-000000")
}

// undoHome is a loaded 80x24 model with one fullSpec("Home") instance (backup 2.8.0 →
// 2.8.1), page focused, and an injected restore.
func undoHome(t *testing.T) (*model, *fakes, *fakeRestore, prism.Instance) {
	t.Helper()
	root := t.TempDir()
	in := makeInst(t, root, fullSpec("Home"))
	m, f := loadedModel(root, 80, 24, in)
	m.focus = focusPage
	r := &fakeRestore{res: &update.RestoreResult{From: "2.8.1", To: "2.8.0", MovedBack: 1}}
	r.inject(m)
	return m, f, r, in
}

// dialogButtons is the open dialog's buttons; nil without a dialog.
func dialogButtons(m *model) []string {
	if m.dialog == nil {
		return nil
	}
	return m.dialog.buttons
}

// backupOf is the instance's backup as the page knows it.
func backupOf(t *testing.T, m *model, in prism.Instance) update.Backup {
	t.Helper()
	b := m.home[in.Dir].backup
	if b == nil {
		t.Fatalf("no backup known for %s", in.Name)
	}
	return *b
}

// undoing is undoHome with the undo confirmed: the restore job runs.
func undoing(t *testing.T) (*model, *fakeRestore, prism.Instance, update.Backup) {
	t.Helper()
	m, _, r, in := undoHome(t)
	b := backupOf(t, m, in)
	m.confirmUndo(in, b)
	if cmd := press(m, "enter"); cmd == nil {
		t.Fatalf("Undo returned no command")
	}
	return m, r, in, b
}

// ---- C1 startUndo ----

// C1: b only acts on an instance with a backup.
func TestC1BOpensTheUndoConfirmationOnlyWithABackup(t *testing.T) {
	root := t.TempDir()
	home := makeInst(t, root, fullSpec("Home"))
	plain := makeInst(t, root, instSpec{name: "Plain", gtnh: true, version: "2.8.4"})
	m, _ := loadedModel(root, 80, 24, home, plain)
	m.focus = focusPage
	m.sel = 1

	plainCmd := press(m, "b")
	plainDialog := dialogTitle(m)
	m.sel = 0
	press(m, "b")

	if plainCmd != nil || plainDialog != "<no dialog>" {
		t.Errorf("b without a backup: cmd %v dialog %q, want nothing", plainCmd != nil, plainDialog)
	}
	if dialogTitle(m) != "Go back to GTNH 2.8.0?" {
		t.Errorf("b with a backup: dialog %q, want Go back to GTNH 2.8.0?", dialogTitle(m))
	}
}

// C1: enter on the undo row asks too.
func TestC1EnterOnTheUndoRowAsksToGoBack(t *testing.T) {
	m, _, r, _ := undoHome(t)
	m.row = indexOf(rowIDs(m), "undo")
	if m.row < 0 {
		t.Fatalf("no undo row in %v", rowIDs(m))
	}

	press(m, "enter")

	if dialogTitle(m) != "Go back to GTNH 2.8.0?" || len(r.calls) != 0 {
		t.Errorf("dialog %q restore calls %d, want the confirmation and no restore yet", dialogTitle(m), len(r.calls))
	}
}

// C1
func TestC1UndoRefusesWhileAJobRuns(t *testing.T) {
	m, _, r, _ := undoHome(t)
	other := filepath.Join(t.TempDir(), "Other Pack")
	m.job = &job{kind: jobUpdate, dir: other, title: "Busy elsewhere", phase: "prepare"}

	cmd := press(m, "b")

	if cmd != nil || dialogTitle(m) != "One thing at a time" {
		t.Fatalf("cmd %v dialog %q, want One thing at a time", cmd != nil, dialogTitle(m))
	}
	if got := flat(m.dialog.body(200)); got != "I'm still busy with Other Pack. Let it finish first." {
		t.Errorf("body %q", got)
	}
	if m.job.dir != other || len(r.calls) != 0 {
		t.Errorf("job %+v restore calls %d, want the other job kept and no restore", *m.job, len(r.calls))
	}
}

// C1
func TestC1UndoRefusesWhileTheGameRuns(t *testing.T) {
	m, f, r, _ := undoHome(t)
	f.running = true

	cmd := press(m, "b")

	if cmd != nil || m.job != nil || len(r.calls) != 0 {
		t.Errorf("cmd %v job %+v restore calls %d, want nothing started", cmd != nil, m.job, len(r.calls))
	}
	if dialogTitle(m) != "The game is running" {
		t.Fatalf("dialog %q, want The game is running", dialogTitle(m))
	}
	if got := flat(m.dialog.body(200)); got != "Home is running right now. Close Minecraft, then try again." {
		t.Errorf("body %q", got)
	}
}

// C1/C2: a failed check goes on and the confirmation warns.
func TestC1UnknownRunStateContinuesWithAWarning(t *testing.T) {
	m, f, _, _ := undoHome(t)
	f.runErr = errors.New("process list unavailable")

	press(m, "b")

	if !m.runUnknown {
		t.Errorf("runUnknown false after an isRunning error")
	}
	if dialogTitle(m) != "Go back to GTNH 2.8.0?" {
		t.Fatalf("dialog %q, want the confirmation", dialogTitle(m))
	}
	if got := bodyLines(t, m); !containsLine(got, closeFirst) {
		t.Errorf("body %q lacks %q", got, closeFirst)
	}
}

// ---- C2 confirmUndo ----

// C2: fullSpec's backup 2.8.0 → 2.8.1 is undone to a downgrade; "Home" has no version
// in its name.
func TestC2UndoConfirmationSaysWhatGoingBackDoes(t *testing.T) {
	m, _, _, in := undoHome(t)

	m.confirmUndo(in, backupOf(t, m, in))

	if dialogTitle(m) != "Go back to GTNH 2.8.0?" || !eq(dialogButtons(m), []string{"Undo", "Cancel"}) || m.dialog.btn != 0 {
		t.Fatalf("dialog %q buttons %q, want Go back to GTNH 2.8.0? [Undo Cancel] on Undo", dialogTitle(m), dialogButtons(m))
	}
	want := []string{undoLine1, undoLine2, undoLine3, undoMods, "", downgradeWarning("2.8.1")}
	if got := bodyLines(t, m); !eq(got, want) {
		t.Errorf("body\n%q\nwant\n%q", got, want)
	}
}

// C2: without server mods the confirmation doesn't mention them.
func TestC2UndoConfirmationWithoutServerModsLeavesThemOut(t *testing.T) {
	root := t.TempDir()
	spec := fullSpec("Home")
	spec.mods = ""
	in := makeInst(t, root, spec)
	m, _ := loadedModel(root, 80, 24, in)

	m.confirmUndo(in, update.Backup{Dir: fixtureBackupDir(in), Info: update.BackupInfo{From: "2.8.4", To: "2.8.1"}})

	if got := bodyLines(t, m); !eq(got, []string{undoLine1, undoLine2, undoLine3}) {
		t.Errorf("body %q, want the three lines without the server mods", got)
	}
}

// C2, kills K1: the rename line only when the update renamed the instance.
func TestC2RenameLineOnlyWhenTheNameCarriesTheUpdatedVersion(t *testing.T) {
	cases := []struct {
		name     string
		instName string
		from, to string
		want     []string
	}{
		{"name has To", "GT New Horizons 2.8.1", "2.8.0", "2.8.1",
			[]string{undoLine1, undoLine2, undoLine3, undoMods, renameLine, "", downgradeWarning("2.8.1")}},
		{"name lacks To", "Home", "2.8.0", "2.8.1",
			[]string{undoLine1, undoLine2, undoLine3, undoMods, "", downgradeWarning("2.8.1")}},
		{"To equals From", "GT New Horizons 2.8.1", "2.8.1", "2.8.1",
			[]string{undoLine1, undoLine2, undoLine3, undoMods}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _, in := undoHome(t)
			in.Name = c.instName
			b := update.Backup{Dir: fixtureBackupDir(in), Info: update.BackupInfo{From: c.from, To: c.to}}

			m.confirmUndo(in, b)

			if got := bodyLines(t, m); !eq(got, c.want) {
				t.Errorf("body\n%q\nwant\n%q", got, c.want)
			}
		})
	}
}

// C2: going back to an older version only warns when it is a downgrade; the warn block
// comes before the bad one.
func TestC2UndoWarningsFollowTheRunStateAndTheDirection(t *testing.T) {
	cases := []struct {
		name       string
		runUnknown bool
		from, to   string
		want       []string
	}{
		{"back up to a newer version", false, "2.8.4", "2.8.1",
			[]string{undoLine1, undoLine2, undoLine3, undoMods}},
		{"unknown run state", true, "2.8.4", "2.8.1",
			[]string{undoLine1, undoLine2, undoLine3, undoMods, "", closeFirst}},
		{"unknown run state and a downgrade", true, "2.8.0", "2.8.4",
			[]string{undoLine1, undoLine2, undoLine3, undoMods, "", closeFirst, "", downgradeWarning("2.8.4")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _, in := undoHome(t)
			m.runUnknown = c.runUnknown
			b := update.Backup{Dir: fixtureBackupDir(in), Info: update.BackupInfo{From: c.from, To: c.to}}

			m.confirmUndo(in, b)

			if dialogTitle(m) != "Go back to GTNH "+c.from+"?" {
				t.Errorf("dialog %q", dialogTitle(m))
			}
			if got := bodyLines(t, m); !eq(got, c.want) {
				t.Errorf("body\n%q\nwant\n%q", got, c.want)
			}
		})
	}
}

// C2: Undo starts the restore job and clears the instance's notice.
func TestC2UndoStartsTheRestoreJob(t *testing.T) {
	m, _, r, in := undoHome(t)
	b := backupOf(t, m, in)
	m.notices[in.Dir] = notice{text: "Updated to GTNH 2.8.1 just now · 1 file updated, 0 removed"}
	m.notices["elsewhere"] = notice{text: "kept"}
	m.confirmUndo(in, b)

	cmd := press(m, "enter")

	want := job{kind: jobRestore, dir: in.Dir, title: "Going back to GTNH 2.8.0", phase: "apply", backup: fixtureBackupDir(in)}
	if m.job == nil || *m.job != want {
		t.Fatalf("job %+v, want %+v", m.job, want)
	}
	if m.dialog != nil || cmd == nil {
		t.Errorf("dialog %q cmd %v, want closed and the restore command", dialogTitle(m), cmd != nil)
	}
	if _, ok := m.notices[in.Dir]; ok || m.notices["elsewhere"].text != "kept" {
		t.Errorf("notices %+v, want only the instance's notice cleared", m.notices)
	}
	if len(r.calls) != 0 {
		t.Errorf("restore ran %d times before the command did", len(r.calls))
	}
}

// C2: the command restores inst from b once and reports the result.
func TestC2TheRestoreCommandRestoresTheBackupOnce(t *testing.T) {
	m, _, r, in := undoHome(t)
	b := backupOf(t, m, in)
	m.confirmUndo(in, b)
	cmd := press(m, "enter")

	msgs := runCmd(cmd)

	if len(r.calls) != 1 {
		t.Fatalf("restore called %d times, want 1", len(r.calls))
	}
	call := r.calls[0]
	if call.inst.Dir != in.Dir || call.inst.Name != "Home" {
		t.Errorf("restored instance %+v, want Home", call.inst)
	}
	if call.b.Dir != fixtureBackupDir(in) || call.b.Info.From != "2.8.0" || call.b.Info.To != "2.8.1" {
		t.Errorf("restored backup %+v, want 2.8.0 → 2.8.1 in %s", call.b, fixtureBackupDir(in))
	}
	if len(msgs) != 1 {
		t.Fatalf("command yielded %v, want one restoredMsg", msgs)
	}
	if got, ok := msgs[0].(restoredMsg); !ok || got.r != r.res {
		t.Errorf("command yielded %#v, want restoredMsg with the result", msgs[0])
	}
}

// C2
func TestC2AFailedRestoreCommandYieldsTheError(t *testing.T) {
	m, _, r, in := undoHome(t)
	r.res, r.err = nil, errors.New("disk full")
	m.confirmUndo(in, backupOf(t, m, in))
	cmd := press(m, "enter")

	msgs := runCmd(cmd)

	if len(msgs) != 1 {
		t.Fatalf("command yielded %v, want one errMsg", msgs)
	}
	if got, ok := msgs[0].(errMsg); !ok || got.err != r.err {
		t.Errorf("command yielded %#v, want errMsg disk full", msgs[0])
	}
}

// C2
func TestC2CancelingTheUndoChangesNothing(t *testing.T) {
	for _, keys := range [][]string{{"right", "enter"}, {"esc"}} {
		t.Run(strings.Join(keys, "+"), func(t *testing.T) {
			m, _, r, in := undoHome(t)
			m.confirmUndo(in, backupOf(t, m, in))

			cmd := press(m, keys...)
			runCmd(cmd)

			if m.dialog != nil || m.job != nil || len(r.calls) != 0 {
				t.Errorf("dialog %q job %+v restore calls %d, want all gone", dialogTitle(m), m.job, len(r.calls))
			}
		})
	}
}

// ---- C3 restoredMsg ----

// C3
func TestC3RestoredEndsTheJobWithANotice(t *testing.T) {
	cases := []struct {
		name string
		r    update.RestoreResult
		text string
	}{
		{"one file", update.RestoreResult{From: "2.8.1", To: "2.8.0", MovedBack: 1, Removed: 0},
			"Back on GTNH 2.8.0 just now · 1 file put back, 0 removed"},
		{"files", update.RestoreResult{From: "2.8.1", To: "2.8.0", MovedBack: 3, Removed: 2},
			"Back on GTNH 2.8.0 just now · 3 files put back, 2 removed"},
		{"thousands and a rename", update.RestoreResult{From: "2.8.1", To: "2.8.0", MovedBack: 1234, Removed: 1, Renamed: "GT New Horizons 2.8.0"},
			`Back on GTNH 2.8.0 just now · 1,234 files put back, 1 removed · renamed to "GT New Horizons 2.8.0"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, in, _ := undoing(t)
			r := c.r

			_, cmd := m.Update(restoredMsg{&r})

			if m.job != nil {
				t.Errorf("job %+v, want none", m.job)
			}
			if got := m.notices[in.Dir]; !reflect.DeepEqual(got, notice{text: c.text}) {
				t.Errorf("notice %+v, want text %q", got, c.text)
			}
			msgs := runCmd(cmd)
			if len(msgs) != 1 {
				t.Fatalf("command yielded %v, want one reloadedMsg", msgs)
			}
			if _, ok := msgs[0].(reloadedMsg); !ok {
				t.Errorf("command yielded %T, want reloadedMsg", msgs[0])
			}
		})
	}
}

// C3: files outside the instance folder are named in a warning.
func TestC3SkippedFilesAreWarnedAbout(t *testing.T) {
	m, _, in, b := undoing(t)
	r := &update.RestoreResult{From: "2.8.1", To: "2.8.0", MovedBack: 2, Removed: 1,
		Skipped: []string{"../shared/options.txt", "../shared/servers.dat"}}

	m.Update(restoredMsg{r})

	want := notice{
		text: "Back on GTNH 2.8.0 just now · 2 files put back, 1 removed",
		warn: []string{"I couldn't put these back myself — they live outside the instance folder: ../shared/options.txt, ../shared/servers.dat. They're still in " + b.Dir + "."},
	}
	if got := m.notices[in.Dir]; !reflect.DeepEqual(got, want) {
		t.Errorf("notice\n%+v\nwant\n%+v", got, want)
	}
}

// C3: the notice is on the page after the reload.
func TestC3RestoreNoticeShowsAfterTheReload(t *testing.T) {
	m, _, in, _ := undoing(t)
	_, cmd := m.Update(restoredMsg{&update.RestoreResult{From: "2.8.1", To: "2.8.0", MovedBack: 3, Removed: 2,
		Skipped: []string{"../outside.txt"}}})
	msgs := runCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("command yielded %v, want one reloadedMsg", msgs)
	}

	m.Update(msgs[0])
	page := flat(strings.Join(pageLines(m, 78), "\n"))

	for _, want := range []string{"Back on GTNH 2.8.0 just now · 3 files put back, 2 removed",
		"I couldn't put these back myself — they live outside the instance folder: ../outside.txt."} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
	if cur, ok := m.current(); !ok || cur.Dir != in.Dir {
		t.Errorf("current %q after the reload, want Home", cur.Name)
	}
}

// ---- C4 errors ----

// C4
func TestC4RestoreErrorsSayWhatHappened(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		outcome func(b update.Backup) string
	}{
		{"game running", fmt.Errorf("restore: %w", update.ErrGameRunning),
			func(update.Backup) string { return "Nothing was changed." }},
		{"other", errors.New("disk full"),
			func(b update.Backup) string {
				return "Some files may have changed. The backup folder is still there, so you can try again: " + b.Dir
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, in, b := undoing(t)

			m.Update(errMsg{c.err})

			if m.job != nil || m.jobShown(in.Dir) {
				t.Errorf("job %+v, want none", m.job)
			}
			if dialogTitle(m) != "Something went wrong" {
				t.Fatalf("dialog %q, want Something went wrong", dialogTitle(m))
			}
			if got, want := bodyLines(t, m), []string{c.err.Error(), "", c.outcome(b)}; !eq(got, want) {
				t.Errorf("body %q, want %q", got, want)
			}
		})
	}
}

// C4
func TestC4JobOutcomeOfARestore(t *testing.T) {
	backup := filepath.Join(t.TempDir(), "Home", ".gtnh-updater", "backup-1")
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"game running", update.ErrGameRunning, "Nothing was changed."},
		{"wrapped game running", fmt.Errorf("x: %w", update.ErrGameRunning), "Nothing was changed."},
		{"other", errors.New("disk full"), "Some files may have changed. The backup folder is still there, so you can try again: " + backup},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := oneFull(t)
			m.job = &job{kind: jobRestore, dir: m.insts[0].Dir, title: "Going back to GTNH 2.8.0", phase: "apply", backup: backup}

			if got := m.jobOutcome(c.err); got != c.want {
				t.Errorf("jobOutcome = %q, want %q", got, c.want)
			}
		})
	}
}
