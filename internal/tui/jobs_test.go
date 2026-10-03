package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"pgregory.net/rapid"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

func jobOn(dir, title, phase string) *job {
	return &job{kind: jobUpdate, dir: dir, title: title, phase: phase}
}

const updating284 = "Updating to GTNH 2.8.4"

// jobModel is a loaded 80x24 model with one full GTNH instance whose update runs in
// phase, page focused.
func jobModel(t *testing.T, phase string) (*model, prism.Instance) {
	t.Helper()
	root := t.TempDir()
	in := makeInst(t, root, fullSpec("Home"))
	m, _ := loadedModel(root, 80, 24, in)
	m.focus = focusPage
	title := checking284
	if phase == "apply" {
		title = updating284
	}
	m.job = jobOn(in.Dir, title, phase)
	return m, in
}

func plainLines(lines []string) []string { return trimLines(strings.Join(lines, "\n")) }

// C5
func TestC5JobShownOnlyOnTheJobsInstance(t *testing.T) {
	m, _ := twoGTNH(t)
	home, second := m.insts[0], m.insts[1]
	before := m.jobShown(home.Dir)

	m.job = jobOn(home.Dir, checking284, "prepare")

	if before || !m.jobShown(home.Dir) || m.jobShown(second.Dir) {
		t.Errorf("jobShown without job %v, home %v, second %v; want false true false",
			before, m.jobShown(home.Dir), m.jobShown(second.Dir))
	}
}

// C5, kills K1
func TestC5BusyApplyingOnlyWhileApplying(t *testing.T) {
	cases := []struct {
		name string
		job  *job
		want bool
	}{
		{"no job", nil, false},
		{"prepare", jobOn("x", checking284, "prepare"), false},
		{"apply", jobOn("x", updating284, "apply"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := oneFull(t)
			m.job = c.job

			if got := m.busyApplying(); got != c.want {
				t.Errorf("busyApplying = %v, want %v", got, c.want)
			}
		})
	}
}

// C3a
func TestC5JobNameIsTheInstanceNameOrTheFolderName(t *testing.T) {
	m, _ := twoGTNH(t)
	cases := []struct {
		name, dir, want string
	}{
		{"listed instance", m.insts[1].Dir, "Second"},
		{"unknown folder", filepath.Join(t.TempDir(), "Lost Pack"), "Lost Pack"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m.job = jobOn(c.dir, checking284, "prepare")

			if got := m.jobName(); got != c.want {
				t.Errorf("jobName = %q, want %q", got, c.want)
			}
		})
	}
}

// C5
func TestC5JobTitleLine(t *testing.T) {
	cases := []struct {
		phase string
		want  string
	}{
		{"prepare", "  ◐ " + checking284},
		{"apply", hinted("  ", "◐ "+updating284, "please wait", 60)},
	}
	for _, c := range cases {
		t.Run(c.phase, func(t *testing.T) {
			m, _ := jobModel(t, c.phase)

			got := plainLines(m.jobLines(60))

			if !eq(got, []string{c.want}) {
				t.Errorf("jobLines = %q, want [%q]", got, c.want)
			}
		})
	}
}

// C5: the job row replaces play/join on its instance; selected it gets the ▸.
func TestC5JobRowReplacesThePlayRows(t *testing.T) {
	m, in := jobModel(t, "prepare")
	if !m.jobShown(in.Dir) {
		t.Fatalf("jobShown false for the job's instance")
	}

	ids := rowIDs(m)
	first := m.rows()[0]

	if len(ids) == 0 || ids[0] != "job" || indexOf(ids, "play") >= 0 || indexOf(ids, "join") >= 0 {
		t.Errorf("rows %v, want job first and no play/join", ids)
	}
	if first.run != nil {
		t.Errorf("job row has a run func")
	}
	if got := plainLines(first.lines(60, true)); len(got) == 0 || got[0] != "▸ ◐ "+checking284 {
		t.Errorf("selected job row = %q", got)
	}
}

// C5
func TestC5OtherInstancesKeepTheirPlayRows(t *testing.T) {
	m, _ := twoGTNH(t)
	home, second := m.insts[0], m.insts[1]
	m.job = jobOn(second.Dir, checking284, "prepare")
	if !m.jobShown(second.Dir) || m.jobShown(home.Dir) {
		t.Fatalf("jobShown wrong for the instances")
	}

	ids := rowIDs(m)

	if len(ids) < 2 || ids[0] != "play" || ids[1] != "join" || indexOf(ids, "job") >= 0 {
		t.Errorf("Home rows %v, want play, join and no job", ids)
	}
}

// C5
func TestC5EnterOnTheJobRowDoesNothing(t *testing.T) {
	m, in := jobModel(t, "prepare")
	if !m.jobShown(in.Dir) {
		t.Fatalf("jobShown false for the job's instance")
	}
	m.row = 0

	cmd := press(m, "enter")

	if cmd != nil || m.dialog != nil || m.job == nil || m.job.title != checking284 {
		t.Errorf("enter on the job row: cmd %v dialog %v job %+v", cmd != nil, m.dialog != nil, m.job)
	}
}

// C5: progress line: bar, percentage and how much.
func TestC5ProgressLineSaysHowFarAlong(t *testing.T) {
	cases := []struct {
		name        string
		done, total int64
		want        string
	}{
		{"files", 500, 1000, "  50% · 500 of 1,000 files"},
		{"integer percent", 2, 3, "  66% · 2 of 3 files"},
		{"a million is files", 500_000, 1_000_000, "  50% · 500,000 of 1,000,000 files"},
		{"above a million is bytes", 0, 1_000_001, "  0% · 0 MB of 1 MB"},
		{"bytes", 300_000_000, 600_000_000, "  50% · 300 MB of 600 MB"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := jobModel(t, "prepare")
			m.Update(stepMsg("Downloading GTNH 2.8.4"))
			m.Update(progressMsg{c.done, c.total})

			got := plainLines(m.jobLines(100))

			if len(got) != 3 || !strings.HasPrefix(got[1], "  ") || !strings.HasSuffix(got[1], c.want) {
				t.Errorf("jobLines = %q, want line 2 ending in %q", got, c.want)
			}
		})
	}
}

// C5
func TestC5ProgressLineAddsTheTimeLeft(t *testing.T) {
	m, _ := jobModel(t, "prepare")
	m.Update(stepMsg("Downloading GTNH 2.8.4"))
	m.Update(progressMsg{500, 1000})
	m.stepStart = time.Now().Add(-20 * time.Second)

	got := plainLines(m.jobLines(100))

	if len(got) < 2 || !strings.HasSuffix(got[1], "  50% · 500 of 1,000 files · about 20 seconds left") {
		t.Errorf("jobLines = %q", got)
	}
}

// C5: bar width max(min(width-14, 48), 8).
func TestC5ProgressBarWidth(t *testing.T) {
	cases := []struct{ width, bar int }{{100, 48}, {62, 48}, {61, 47}, {30, 16}, {22, 8}, {20, 8}}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.width), func(t *testing.T) {
			m, _ := jobModel(t, "prepare")
			m.Update(stepMsg("Downloading"))
			m.Update(progressMsg{1, 2})

			got := plainLines(m.jobLines(c.width))

			i := -1
			if len(got) > 1 {
				i = strings.Index(got[1], "  50%")
			}
			if i < 2 || ansi.StringWidth(got[1][2:i]) != c.bar {
				t.Errorf("width %d: line %q, want a %d-column bar", c.width, got, c.bar)
			}
		})
	}
}

// C5: finished steps, then the spinner and the current step, dim.
func TestC5StepLine(t *testing.T) {
	cases := []struct {
		name  string
		steps []string
		step  string
		want  string
	}{
		{"current only", nil, "Checking your files", "⣾ Checking your files"},
		{"done and current", []string{"Downloading GTNH 2.8.4"}, "Checking your files", "✓ Downloading GTNH 2.8.4 · ⣾ Checking your files"},
		{"done only", []string{"Downloading", "Checking"}, "", "✓ Downloading · ✓ Checking"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := jobModel(t, "prepare")
			m.steps, m.step = c.steps, c.step

			got := plainLines(m.jobLines(100))

			if len(got) != 2 || !strings.HasPrefix(got[1], "  ") || flat(got[1]) != c.want {
				t.Errorf("jobLines = %q, want the step line %q", got, c.want)
			}
		})
	}
}

// C5: the step line wraps to width-2 behind a 2-column indent.
func TestC5StepLineWraps(t *testing.T) {
	m, _ := jobModel(t, "prepare")
	m.steps = []string{"Downloading GTNH 2.8.4", "Reading the old version"}
	m.step = "Checking your files"

	got := plainLines(m.jobLines(30))

	if len(got) < 3 {
		t.Fatalf("jobLines = %q, want the steps wrapped over several lines", got)
	}
	for _, l := range got[1:] {
		if !strings.HasPrefix(l, "  ") || ansi.StringWidth(l) > 30 {
			t.Errorf("step line %q: want a 2-column indent within 30 columns", l)
		}
	}
	if j := flat(strings.Join(got[1:], " ")); j != "✓ Downloading GTNH 2.8.4 · ✓ Reading the old version · ⣾ Checking your files" {
		t.Errorf("steps = %q", j)
	}
}

// C5
func TestC5HeadsUpLinesWrapUnderTheSteps(t *testing.T) {
	m, _ := jobModel(t, "prepare")
	m.Update(stepMsg("Checking"))
	m.Update(warnMsg("The server mods link answered with an error page"))

	got := plainLines(m.jobLines(40))

	want := []string{"  Heads up: The server mods link", "    answered with an error page"}
	if len(got) != 4 || !eq(got[2:], want) {
		t.Errorf("jobLines = %q, want the heads-up as %q", got, want)
	}
}

// C5: the whole view fits and shows the job block.
func TestC5ViewShowsTheJobBlock(t *testing.T) {
	m, in := jobModel(t, "prepare")
	m.Update(stepMsg("Downloading GTNH 2.8.4"))
	m.Update(stepMsg("Checking your files"))
	m.Update(progressMsg{500, 1000})
	m.Update(warnMsg("Your server mods link is down"))
	if !m.jobShown(in.Dir) {
		t.Fatalf("jobShown false for the job's instance")
	}

	lines := screen(m)

	for _, want := range []string{"◐ " + checking284, "50% · 500 of 1,000 files",
		"✓ Downloading GTNH 2.8.4 ·", "Checking your files", "Heads up: Your server mods link is down"} {
		if !containsLine(lines, want) {
			t.Errorf("view lacks %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	if containsLine(lines, "▶ Play") {
		t.Errorf("view still has the Play row")
	}
}

// C5: exactly height lines, none wider than width.
func TestC5ViewWithAJobFitsTheTerminal(t *testing.T) {
	cases := []struct{ width, height int }{{80, 24}, {40, 12}}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%dx%d", c.width, c.height), func(t *testing.T) {
			root := t.TempDir()
			home := makeInst(t, root, fullSpec("Home"))
			m, _ := loadedModel(root, c.width, c.height, home, makeInst(t, root, instSpec{name: "Second", gtnh: true, version: "2.8.4"}))
			m.job = jobOn(home.Dir, updating284, "apply")
			m.Update(stepMsg("Downloading GTNH 2.8.4"))
			m.Update(progressMsg{300_000_000, 700_000_000})
			m.Update(warnMsg("A rather long heads-up that has to wrap over a few lines in a narrow window"))
			if !m.jobShown(home.Dir) {
				t.Fatalf("jobShown false for the job's instance")
			}

			lines := strings.Split(m.View(), "\n")

			if len(lines) != c.height {
				t.Errorf("%d lines, want %d", len(lines), c.height)
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > c.width {
					t.Errorf("line %d is %d wide: %q", i+1, w, ansi.Strip(l))
				}
			}
		})
	}
}

// C5: the job's instance has ◐ in the sidebar.
func TestC5SidebarMarksTheJobsInstance(t *testing.T) {
	m, _ := twoGTNH(t)
	second := m.insts[1]
	m.job = jobOn(second.Dir, checking284, "prepare")
	if !m.jobShown(second.Dir) {
		t.Fatalf("jobShown false for the job's instance")
	}

	lines := trimLines(m.sidebarView(10))

	if !containsLine(lines, "◐ Second") || !containsLine(lines, "▲ Home") {
		t.Errorf("sidebar %q, want ◐ Second and ▲ Home", lines)
	}
}

// C5, kills K1: q and ctrl+c don't quit mid-apply.
func TestC5QuitKeysDoNothingWhileApplying(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		t.Run(k, func(t *testing.T) {
			m, _ := jobModel(t, "apply")
			if !m.busyApplying() {
				t.Fatalf("busyApplying false in phase apply")
			}

			cmd := press(m, k)

			if m.quitting || hasQuit(runCmd(cmd)) {
				t.Errorf("%s quit while applying", k)
			}
		})
	}
}

// C5, kills K1: while preparing q still quits.
func TestC5QQuitsWhilePreparing(t *testing.T) {
	m, _ := jobModel(t, "prepare")
	if m.busyApplying() {
		t.Fatalf("busyApplying true in phase prepare")
	}

	cmd := press(m, "q")

	if !m.quitting || !hasQuit(runCmd(cmd)) {
		t.Errorf("q didn't quit while preparing")
	}
}

// C5/C11
func TestC11StatusBarAsksNotToCloseWhileApplying(t *testing.T) {
	cases := []struct {
		phase, want string
	}{
		{"apply", "↑↓,move,enter,run,n,new instance,updating,please don't close this window"},
		{"prepare", "↑↓,move,enter,run,n,new instance,q,quit"},
	}
	for _, c := range cases {
		t.Run(c.phase, func(t *testing.T) {
			m, _ := jobModel(t, c.phase)
			if m.busyApplying() != (c.phase == "apply") {
				t.Fatalf("busyApplying wrong in phase %s", c.phase)
			}

			if got := strings.Join(m.statusPairs(), ","); got != c.want {
				t.Errorf("statusPairs = %s, want %s", got, c.want)
			}
		})
	}
}

// C11: the protection is global: a job on another instance changes the bar too.
func TestC11StatusBarAsksNotToCloseForAJobElsewhere(t *testing.T) {
	m, _ := twoGTNH(t)
	m.focus = focusSidebar
	m.job = jobOn(m.insts[1].Dir, updating284, "apply")
	if !m.busyApplying() {
		t.Fatalf("busyApplying false in phase apply")
	}

	got := strings.Join(m.statusPairs(), ",")

	if got != "↑↓,instance,enter,play,tab,page,n,new instance,updating,please don't close this window" {
		t.Errorf("statusPairs = %s", got)
	}
}

// C12: whatever the flow state, dialog and keys, the view has exactly height lines,
// none wider than width.
func TestC12ViewFitsTheTerminalDuringTheUpdateFlow(t *testing.T) {
	root := t.TempDir()
	home := makeInst(t, root, fullSpec("Home"))
	second := makeInst(t, root, instSpec{name: "Second", gtnh: true, version: "2.8.4"})
	words := []string{"Downloading", "GTNH", "2.8.4", "checking", "your", "files", "·", "mods", "—",
		"a-very-long-word-without-any-breaks-anywhere-at-all-in-it-whatsoever-really"}
	text := func(rt *rapid.T, label string, most int) string {
		return strings.Join(rapid.SliceOfN(rapid.SampledFrom(words), 1, most).Draw(rt, label), " ")
	}
	texts := func(rt *rapid.T, label string, most int) []string {
		out := make([]string, rapid.IntRange(0, most).Draw(rt, label))
		for i := range out {
			out[i] = text(rt, label, 12)
		}
		return out
	}
	noop := func(*model) tea.Cmd { return nil }
	rapid.Check(t, func(rt *rapid.T) {
		width := rapid.IntRange(40, 160).Draw(rt, "width")
		height := rapid.IntRange(10, 50).Draw(rt, "height")
		insts := []prism.Instance{home}
		if rapid.Bool().Draw(rt, "two") {
			insts = append(insts, second)
		}
		m, _ := loadedModel(root, width, height, insts...)
		m.focus = focusPage

		if phase := rapid.SampledFrom([]string{"", "prepare", "apply"}).Draw(rt, "phase"); phase != "" {
			m.job = jobOn(home.Dir, text(rt, "title", 8), phase)
			if !m.jobShown(home.Dir) {
				rt.Fatalf("jobShown false for the job's instance")
			}
			for _, s := range texts(rt, "steps", 5) {
				m.Update(stepMsg(s))
			}
			total := rapid.Int64Range(0, 2_000_000_000).Draw(rt, "total")
			m.Update(progressMsg{rapid.Int64Range(0, total).Draw(rt, "done"), total})
			for _, w := range texts(rt, "warns", 3) {
				m.Update(warnMsg(w))
			}
		}
		switch rapid.SampledFrom([]string{"none", "list", "input", "confirm", "error"}).Draw(rt, "dialog") {
		case "list":
			items := make([]ditem, rapid.IntRange(1, 40).Draw(rt, "items"))
			for i := range items {
				items[i] = ditem{title: text(rt, "item title", 4), desc: text(rt, "item desc", 6), key: fmt.Sprint(i)}
			}
			m.openList(text(rt, "list title", 6), text(rt, "intro", 20), items, "", nil)
		case "input":
			m.openInput(text(rt, "input title", 6), text(rt, "intro", 20), text(rt, "value", 6),
				"https://…/custom_mods.zip", text(rt, "note", 10), []string{"Continue"},
				func(*model, string, string) tea.Cmd { return nil })
		case "confirm":
			m.confirmDialog(text(rt, "confirm title", 6), texts(rt, "lines", 8), texts(rt, "warn", 3), texts(rt, "bad", 3),
				"Update", noop, noop)
		case "error":
			m.errorDialog(errors.New(text(rt, "err", 30)), "Nothing was changed.")
		}
		for _, k := range rapid.SliceOfN(rapid.SampledFrom([]string{"up", "down", "tab", "shift+tab"}), 0, 30).Draw(rt, "keys") {
			press(m, k)
		}

		lines := strings.Split(m.View(), "\n")
		if len(lines) != height {
			rt.Fatalf("view has %d lines, want %d", len(lines), height)
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > width {
				rt.Fatalf("line %d is %d columns wide, terminal is %d: %q", i+1, w, width, ansi.Strip(l))
			}
		}
	})
}
