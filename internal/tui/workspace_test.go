package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"pgregory.net/rapid"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// C1: every width/height, before and after load, with any mix of instances and keys:
// exactly height lines, none wider than width, title bar, blank line, workspace (sidebar
// with its rule, or a 2-column margin) and a full-width status bar.
func TestC1ViewFitsTheTerminalWithTitleWorkspaceAndStatusBar(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	pool := []prism.Instance{
		makeInst(t, root, instSpec{name: "Alpha", gtnh: true, version: "2.8.4", server: "play.example.org", java17: true,
			backup: &update.BackupInfo{From: "2.8.1", To: "2.8.4", When: now.Add(-3 * day)}, lastLaunch: now.Add(-2 * day)}),
		makeInst(t, root, instSpec{name: "Bravo", gtnh: true, version: "2.8.1"}),
		makeInst(t, root, instSpec{name: "Charlie with a really long instance name that goes on", gtnh: true}),
		makeInst(t, root, instSpec{name: "Delta", gtnh: false}),
		makeInst(t, root, instSpec{name: "Echo", gtnh: false, noCfg: true}),
		makeInst(t, root, instSpec{name: "Foxtrot", gtnh: true, version: "2.8.0", server: "mc.example.net:25565",
			mods: "https://mods.example.net/m.zip", noCfg: true}),
	}
	rapid.Check(t, func(rt *rapid.T) {
		width := rapid.IntRange(40, 160).Draw(rt, "width")
		height := rapid.IntRange(10, 50).Draw(rt, "height")
		order := rapid.Permutation(pool).Draw(rt, "order")
		insts := order[:rapid.IntRange(0, len(pool)).Draw(rt, "n")]
		loaded := rapid.Bool().Draw(rt, "loaded")
		newer := rapid.Bool().Draw(rt, "newer")
		keys := rapid.SliceOfN(rapid.SampledFrom([]string{"up", "down", "tab", "shift+tab", "a"}), 0, 14).Draw(rt, "keys")

		m, _ := newTestModel(Config{PrismDirs: []string{root}}, width, height)
		if newer {
			m.Update(newerMsg{r: &selfupdate.Release{Version: "9.9.9"}})
		}
		showAll := false
		if loaded {
			m.Update(loadedMsg{m: testManifest(), insts: insts})
			for _, k := range keys {
				press(m, k)
				if k == "a" {
					showAll = !showAll
				}
			}
		}
		visible := 0
		for _, in := range insts {
			if in.GTNH || showAll {
				visible++
			}
		}
		sidebar := loaded && visible > 1

		lines := strings.Split(m.View(), "\n")
		if len(lines) != height {
			rt.Fatalf("view has %d lines, want %d", len(lines), height)
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > width {
				rt.Fatalf("line %d is %d columns wide, terminal is %d: %q", i+1, w, width, ansi.Strip(l))
			}
		}
		title := ansi.Strip(lines[0])
		if !strings.HasPrefix(title, " GTNH Launcher "+testAppVersion) {
			rt.Fatalf("title bar %q doesn't start with the product name and version", title)
		}
		if newer && width >= 60 && !strings.Contains(title, "launcher 9.9.9 is out") {
			rt.Fatalf("title bar %q doesn't announce the newer launcher", title)
		}
		if !newer && strings.Contains(title, "is out") {
			rt.Fatalf("title bar %q announces a newer launcher that doesn't exist", title)
		}
		if strings.TrimSpace(ansi.Strip(lines[1])) != "" {
			rt.Fatalf("line 2 should be blank, got %q", ansi.Strip(lines[1]))
		}
		if w := ansi.StringWidth(lines[height-1]); w != width {
			rt.Fatalf("status bar is %d columns, want %d", w, width)
		}
		for i := 2; i < height-1; i++ {
			l := ansi.Strip(lines[i])
			if sidebar && !strings.Contains(l, " │ ") {
				rt.Fatalf("workspace line %d %q lacks the sidebar rule", i+1, l)
			}
			if !sidebar && l != "" && !strings.HasPrefix(l, "  ") {
				rt.Fatalf("workspace line %d %q lacks the 2-column margin", i+1, l)
			}
		}
	})
}

// C1/C8/C12: with an instance being created (any number of real ones, any progress) or
// the launcher's progress dialog open, the view still has exactly height lines, none
// wider than width.
func TestC1ViewFitsTheTerminalWithAPendingInstanceOrTheProgressDialog(t *testing.T) {
	root := t.TempDir()
	pool := []prism.Instance{
		makeInst(t, root, fullSpec("Alpha")),
		makeInst(t, root, instSpec{name: "Bravo", gtnh: true, version: "2.8.4"}),
	}
	names := []string{"My Pack", "GT New Horizons 2.8.4", "A new instance with a really long name that goes on and on"}
	words := []string{"Downloading", "GTNH", "2.8.4", "checking", "files", "·",
		"a-very-long-word-without-any-breaks-anywhere-at-all-in-it-whatsoever-really"}
	rapid.Check(t, func(rt *rapid.T) {
		width := rapid.IntRange(40, 160).Draw(rt, "width")
		height := rapid.IntRange(10, 50).Draw(rt, "height")
		insts := pool[:rapid.IntRange(0, len(pool)).Draw(rt, "n")]
		m, _ := newTestModel(Config{PrismDirs: []string{root}}, width, height)
		m.Update(newerMsg{r: &selfupdate.Release{Version: "9.9.9"}})
		m.Update(loadedMsg{m: testManifest(), insts: insts})

		if rapid.Bool().Draw(rt, "pending") {
			target := rapid.SampledFrom([]string{"2.8.4", "2.8.1", "2.8.0"}).Draw(rt, "target")
			m.beginCreate(target, rapid.SampledFrom(names).Draw(rt, "name"))
		} else {
			m.job = &job{kind: jobSelf, title: "Downloading GTNH Launcher 9.9.9", phase: "apply"}
			m.progressDialog("Updating the launcher")
		}
		for _, s := range rapid.SliceOfN(rapid.SampledFrom(words), 0, 4).Draw(rt, "steps") {
			m.Update(stepMsg(s))
		}
		total := rapid.Int64Range(0, 2_000_000_000).Draw(rt, "total")
		m.Update(progressMsg{rapid.Int64Range(0, total).Draw(rt, "done"), total})
		for _, k := range rapid.SliceOfN(rapid.SampledFrom([]string{"up", "down", "tab", "shift+tab"}), 0, 10).Draw(rt, "keys") {
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

// C2
func TestC2BeforeLoadThePageSaysItIsLookingAndOnlyQuitIsOffered(t *testing.T) {
	m, _ := newTestModel(Config{}, 80, 24)

	lines := screen(m)

	if !containsLine(lines, " Looking for your GTNH instances…") {
		t.Errorf("no looking message in %q", lines)
	}
	if containsLine(lines, "Instances") {
		t.Errorf("sidebar drawn before load: %q", lines)
	}
	if got := strings.TrimSpace(lines[23]); got != "q quit" {
		t.Errorf("status bar %q, want %q", got, "q quit")
	}
}

// C2
func TestC2ALoadFailureShowsTheErrorOnThePage(t *testing.T) {
	m, _ := newTestModel(Config{}, 80, 24)

	m.Update(errMsg{errors.New("the download server is unreachable")})
	lines := screen(m)

	if !containsLine(lines, "the download server is unreachable") {
		t.Errorf("error not on the page: %q", lines)
	}
	if got := strings.TrimSpace(lines[23]); got != "q quit" {
		t.Errorf("status bar %q, want %q", got, "q quit")
	}
	if m.dialog != nil {
		t.Errorf("a load failure is shown on the page, not in a dialog")
	}
}

// C2
func TestC2NoInstancesOffersToMakeOne(t *testing.T) {
	m, _ := loadedModel(t.TempDir(), 80, 24)

	lines := screen(m)

	if !containsLine(lines, "No GTNH instances in Prism yet.") || !containsLine(lines, "Press n to make one.") {
		t.Errorf("empty workspace text missing: %q", lines)
	}
	if containsLine(lines, "Instances") {
		t.Errorf("sidebar drawn without instances: %q", lines)
	}
}

func threeInsts(t *testing.T, root string) []prism.Instance {
	return []prism.Instance{
		makeInst(t, root, instSpec{name: "Alpha", gtnh: true, version: "2.8.4"}),
		makeInst(t, root, instSpec{name: "Bravo", gtnh: true, version: "2.8.4"}),
		makeInst(t, root, instSpec{name: "Charlie", gtnh: true, version: "2.8.4"}),
	}
}

// C2
func TestC2ConfigInstancePreselectsByName(t *testing.T) {
	root := t.TempDir()
	m, _ := newTestModel(Config{PrismDirs: []string{root}, Instance: "Bravo"}, 80, 24)

	m.Update(loadedMsg{m: testManifest(), insts: threeInsts(t, root)})
	in, ok := m.current()

	if !ok || in.Name != "Bravo" {
		t.Errorf("current = %q (%v), want Bravo", in.Name, ok)
	}
}

// C2
func TestC2ConfigInstancePreselectsByFolder(t *testing.T) {
	root := t.TempDir()
	insts := threeInsts(t, root)
	m, _ := newTestModel(Config{PrismDirs: []string{root}, Instance: insts[2].Dir}, 80, 24)

	m.Update(loadedMsg{m: testManifest(), insts: insts})
	in, ok := m.current()

	if !ok || in.Name != "Charlie" {
		t.Errorf("current = %q (%v), want Charlie", in.Name, ok)
	}
	// the folder matched a listed instance: nothing is added (kills flow_load.go:79)
	if len(m.insts) != 3 {
		t.Errorf("%d instances after preselecting a listed folder, want 3", len(m.insts))
	}
}

// C2
func TestC2ConfigPlayLaunchesThePreselectedInstanceAfterLoad(t *testing.T) {
	root := t.TempDir()
	m, f := newTestModel(Config{PrismDirs: []string{root}, Instance: "Bravo", Play: true}, 80, 24)

	_, cmd := m.Update(loadedMsg{m: testManifest(), insts: threeInsts(t, root)})
	runCmd(cmd)

	if len(f.launches) != 1 || f.launches[0].inst.Name != "Bravo" {
		t.Fatalf("launches = %+v, want one of Bravo", f.launches)
	}
	if len(f.findCalls) != 1 {
		t.Errorf("findLauncher called %d times, want 1", len(f.findCalls))
	}
}

// C9
func TestC9ReloadKeepsTheSelectionOnTheSameFolder(t *testing.T) {
	root := t.TempDir()
	insts := threeInsts(t, root)
	m, _ := loadedModel(root, 80, 24, insts...)
	m.focus = focusSidebar
	press(m, "down", "down")

	m.Update(reloadedMsg{insts: []prism.Instance{insts[0], insts[2]}})
	in, _ := m.current()

	if in.Name != "Charlie" {
		t.Errorf("after reload current = %q, want Charlie", in.Name)
	}
}

// C9
func TestC9ReloadWithoutTheSelectedFolderSelectsTheFirst(t *testing.T) {
	root := t.TempDir()
	insts := threeInsts(t, root)
	m, _ := loadedModel(root, 80, 24, insts...)
	m.focus = focusSidebar
	press(m, "down", "down")

	m.Update(reloadedMsg{insts: []prism.Instance{insts[1], insts[0]}})
	in, _ := m.current()

	if in.Name != "Bravo" {
		t.Errorf("after reload current = %q, want Bravo (index 0)", in.Name)
	}
}

// C9
func TestC9ReloadRereadsTheInstances(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Alpha", gtnh: true, version: "2.8.4"})
	m, _ := loadedModel(root, 80, 30, in)
	saveState(t, in.Dir, "2.8.4", "fresh.example.org", "")

	m.Update(reloadedMsg{insts: []prism.Instance{in}})

	if !containsLine(screen(m), "fresh.example.org") {
		t.Errorf("reload didn't refresh: %q", screen(m))
	}
}

// C9
func TestC9TheViewDoesNoIOUntilRefresh(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Alpha", gtnh: true, version: "2.8.4", server: "old.example.org"})
	m, _ := loadedModel(root, 80, 30, in)
	saveState(t, in.Dir, "2.8.4", "new.example.org", "")

	before := screen(m)
	m.refresh()
	after := screen(m)

	if containsLine(before, "new.example.org") || !containsLine(before, "old.example.org") {
		t.Errorf("view read the disk before refresh: %q", before)
	}
	if !containsLine(after, "new.example.org") {
		t.Errorf("refresh didn't pick up the new server: %q", after)
	}
}

// C10
func TestC10InitSetsTheWindowTitle(t *testing.T) {
	m, _ := newTestModel(Config{}, 80, 24)

	msgs := runCmd(m.Init())

	found := false
	for _, msg := range msgs {
		if strings.Contains(fmt.Sprintf("%T", msg), "setWindowTitleMsg") && fmt.Sprint(msg) == "GTNH Launcher" {
			found = true
		}
	}
	if !found {
		t.Errorf("Init's messages %v don't set the window title to GTNH Launcher", msgs)
	}
}

// C5 (q works before load)
func TestC5BeforeLoadOnlyQuitKeysAct(t *testing.T) {
	m, f := newTestModel(Config{}, 80, 24)
	before := screen(m)

	press(m, "down", "tab", "p", "enter", "a", "n")
	mid := screen(m)
	cmd := press(m, "q")

	if strings.Join(mid, "\n") != strings.Join(before, "\n") || len(f.launches) != 0 {
		t.Errorf("keys acted before load")
	}
	if !m.quitting || !hasQuit(runCmd(cmd)) {
		t.Errorf("q before load didn't quit")
	}
}

var _ tea.Model = (*model)(nil)
