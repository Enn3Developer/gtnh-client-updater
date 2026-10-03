package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
)

// Fixtures of the launcher self-update: newerMsg announces 9.9.9; the download
// (startSelfUpdate's command) is never run.

const progressTitle = "Updating the launcher"

// selfModel is oneFull told that launcher 9.9.9 is out.
func selfModel(t *testing.T) (*model, *fakes) {
	t.Helper()
	m, f := oneFull(t)
	m.Update(newerMsg{r: &selfupdate.Release{Version: "9.9.9"}})
	return m, f
}

// selfRunning is selfModel with the self-update downloading behind its progress dialog.
func selfRunning(t *testing.T) *model {
	t.Helper()
	m, _ := selfModel(t)
	m.job = &job{kind: jobSelf, title: "Downloading GTNH Launcher 9.9.9", phase: "apply"}
	m.progressDialog(progressTitle)
	return m
}

func titleLine(m *model) string { return screen(m)[0] }

// ---- C12 selfUpdate ----

// C12: v only acts once a newer launcher is known.
func TestC12VAsksToUpdateOnlyWhenANewerLauncherIsOut(t *testing.T) {
	m, _ := oneFull(t)

	before := press(m, "v")
	beforeDialog := dialogTitle(m)
	m.Update(newerMsg{r: &selfupdate.Release{Version: "9.9.9"}})
	press(m, "v")

	if before != nil || beforeDialog != "<no dialog>" {
		t.Errorf("v without a newer launcher: cmd %v dialog %q, want nothing", before != nil, beforeDialog)
	}
	if dialogTitle(m) != "Update the launcher to 9.9.9?" || !eq(dialogButtons(m), []string{"Update", "Cancel"}) {
		t.Fatalf("dialog %q buttons %q, want the confirmation", dialogTitle(m), dialogButtons(m))
	}
	want := []string{"I'll download GTNH Launcher 9.9.9, replace myself and restart.", "Your instances aren't touched."}
	if got := bodyLines(t, m); !eq(got, want) {
		t.Errorf("body %q, want %q", got, want)
	}
}

// C12
func TestC12SelfUpdateRefusesWhileAJobRuns(t *testing.T) {
	m, _ := selfModel(t)
	m.job = jobOn(m.insts[0].Dir, checking284, "prepare")

	cmd := press(m, "v")

	if cmd != nil || dialogTitle(m) != "One thing at a time" || m.job.title != checking284 {
		t.Errorf("cmd %v dialog %q job %+v, want One thing at a time and the job kept", cmd != nil, dialogTitle(m), m.job)
	}
}

// C12
func TestC12UpdateStartsTheDownloadBehindAProgressDialog(t *testing.T) {
	m, _ := selfModel(t)
	press(m, "v")

	cmd := press(m, "enter")

	want := job{kind: jobSelf, dir: "", title: "Downloading GTNH Launcher 9.9.9", phase: "apply"}
	if m.job == nil || *m.job != want || cmd == nil {
		t.Fatalf("job %+v cmd %v, want %+v and the download command", m.job, cmd != nil, want)
	}
	if dialogTitle(m) != progressTitle {
		t.Fatalf("dialog %q, want %q", dialogTitle(m), progressTitle)
	}
	if len(m.dialog.buttons) != 0 || m.dialog.width != 60 {
		t.Errorf("buttons %q width %d, want the buttonless progress dialog 60 wide", m.dialog.buttons, m.dialog.width)
	}
}

// C12
func TestC12CancelingTheLauncherUpdateChangesNothing(t *testing.T) {
	for _, keys := range [][]string{{"right", "enter"}, {"esc"}} {
		t.Run(strings.Join(keys, "+"), func(t *testing.T) {
			m, _ := selfModel(t)
			press(m, "v")
			if dialogTitle(m) != "Update the launcher to 9.9.9?" {
				t.Fatalf("dialog %q, want the confirmation", dialogTitle(m))
			}

			cmd := press(m, keys...)

			if cmd != nil || m.dialog != nil || m.job != nil {
				t.Errorf("cmd %v dialog %q job %+v, want nothing", cmd != nil, dialogTitle(m), m.job)
			}
		})
	}
}

// C12: before the size is known the dialog shows the step alone.
func TestC12ProgressDialogShowsTheStepBeforeTheSize(t *testing.T) {
	m := selfRunning(t)
	m.Update(stepMsg("Downloading GTNH Launcher 9.9.9"))

	got := flat(m.dialog.body(200))

	if !strings.Contains(got, "Downloading GTNH Launcher 9.9.9") || got != flat(m.stepText()) || strings.Contains(got, "%") {
		t.Errorf("body %q, want the step text alone", got)
	}
}

// C12: the dialog follows the progress that arrives after it opened.
func TestC12ProgressDialogShowsTheProgress(t *testing.T) {
	m := selfRunning(t)
	m.Update(stepMsg("Downloading GTNH Launcher 9.9.9"))

	m.Update(progressMsg{3_000_000, 12_000_000})
	got := flat(m.dialog.body(200))

	if !strings.Contains(got, "25%") || !strings.Contains(got, "3 MB of 12 MB") {
		t.Errorf("body %q, want 25%% and 3 MB of 12 MB", got)
	}
}

// C12: no key closes the progress dialog or quits.
func TestC12ProgressDialogSwallowsEveryKey(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c", "esc", "enter", "v"} {
		t.Run(k, func(t *testing.T) {
			m := selfRunning(t)
			before := *m.job

			cmd := press(m, k)

			if dialogTitle(m) != progressTitle || m.quitting || hasQuit(runCmd(cmd)) {
				t.Errorf("%s: dialog %q quitting %v, want the progress dialog kept", k, dialogTitle(m), m.quitting)
			}
			if m.job == nil || *m.job != before {
				t.Errorf("%s: job %+v, want %+v", k, m.job, before)
			}
		})
	}
}

// C12: the busy note names the launcher.
func TestC12BusyNoteNamesTheLauncher(t *testing.T) {
	m := selfRunning(t)

	m.notifyBusy()

	got := flat(m.dialog.body(200))
	if !strings.Contains(got, "launcher") || strings.Contains(got, "busy with .") {
		t.Errorf("body %q, want it to name the launcher", got)
	}
}

// ---- C13 done / failed ----

// C13
func TestC13DoneOffersARestart(t *testing.T) {
	m := selfRunning(t)

	m.Update(selfDoneMsg{})

	if m.job != nil || m.newer != nil {
		t.Errorf("job %+v newer %v, want both gone", m.job, m.newer)
	}
	if strings.Contains(titleLine(m), "is out") {
		t.Errorf("title bar %q still announces the launcher", titleLine(m))
	}
	if dialogTitle(m) != "The launcher is now version 9.9.9." || !eq(dialogButtons(m), []string{"Restart now", "Later"}) {
		t.Errorf("dialog %q buttons %q, want the restart offer", dialogTitle(m), dialogButtons(m))
	}
}

// C13
func TestC13RestartNowQuitsToRestart(t *testing.T) {
	m := selfRunning(t)
	m.Update(selfDoneMsg{})

	cmd := press(m, "enter")

	if !m.restart || !hasQuit(runCmd(cmd)) {
		t.Errorf("restart %v, want a restart and quit", m.restart)
	}
}

// C13
func TestC13LaterKeepsRunning(t *testing.T) {
	for _, keys := range [][]string{{"right", "enter"}, {"esc"}} {
		t.Run(strings.Join(keys, "+"), func(t *testing.T) {
			m := selfRunning(t)
			m.Update(selfDoneMsg{})

			cmd := press(m, keys...)

			if m.dialog != nil || m.restart || m.quitting || hasQuit(runCmd(cmd)) {
				t.Errorf("dialog %q restart %v quitting %v, want the dialog closed and no restart", dialogTitle(m), m.restart, m.quitting)
			}
		})
	}
}

// C13
func TestC13AFailedLauncherUpdateChangesNothing(t *testing.T) {
	m := selfRunning(t)
	err := errors.New("GitHub answered 503")

	m.Update(errMsg{err})

	if m.job != nil || m.newer == nil {
		t.Errorf("job %+v newer %v, want no job and the newer launcher still offered", m.job, m.newer)
	}
	if dialogTitle(m) != "Something went wrong" {
		t.Fatalf("dialog %q, want Something went wrong", dialogTitle(m))
	}
	if got, want := bodyLines(t, m), []string{err.Error(), "", "The launcher wasn't changed."}; !eq(got, want) {
		t.Errorf("body %q, want %q", got, want)
	}
	if !strings.Contains(titleLine(m), "launcher 9.9.9 is out") {
		t.Errorf("title bar %q, want it to still announce 9.9.9", titleLine(m))
	}
}

// C13
func TestC13JobOutcomeOfALauncherUpdate(t *testing.T) {
	m, _ := selfModel(t)
	m.job = &job{kind: jobSelf, title: "Downloading GTNH Launcher 9.9.9", phase: "apply"}

	if got := m.jobOutcome(errors.New("broke")); got != "The launcher wasn't changed." {
		t.Errorf("jobOutcome = %q", got)
	}
}

// ---- C14 status bar ----

// C14
func TestC14StatusBarDuringTheLauncherUpdate(t *testing.T) {
	cases := []struct {
		name string
		open func(m *model)
		want []string
	}{
		{"progress dialog", func(m *model) {
			m.job = &job{kind: jobSelf, title: "Downloading GTNH Launcher 9.9.9", phase: "apply"}
			m.progressDialog(progressTitle)
		}, []string{"", "updating the launcher — please don't close this window"}},
		{"restart offer", func(m *model) {
			m.job = &job{kind: jobSelf, title: "Downloading GTNH Launcher 9.9.9", phase: "apply"}
			m.progressDialog(progressTitle)
			m.Update(selfDoneMsg{})
		}, []string{"←→", "choose", "enter", "ok", "esc", "cancel"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := selfModel(t)
			c.open(m)

			if got := m.statusPairs(); !eq(got, c.want) {
				t.Errorf("statusPairs %q, want %q", got, c.want)
			}
		})
	}
}
