package tui

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// The instances are listed without the network; the manifest arrives on its own.

func offlineErr() error {
	return fmt.Errorf("cannot fetch manifest: %w", &url.Error{Op: "Get", URL: "https://downloads.gtnewhorizons.com/versions.json",
		Err: errors.New("dial tcp: lookup downloads.gtnewhorizons.com: no such host")})
}

// offlineModel has loaded the instances, and the manifest fetch failed.
func offlineModel(t *testing.T, insts ...prism.Instance) (*model, *fakes) {
	t.Helper()
	root := t.TempDir()
	if len(insts) == 0 {
		insts = []prism.Instance{makeInst(t, root, fullSpec("Home"))}
	}
	m, f := newTestModel(Config{PrismDirs: []string{root}}, 80, 24)
	m.Update(loadedMsg{insts: insts})
	m.Update(manifestMsg{err: offlineErr()})
	return m, f
}

func TestOfflineLoadListsTheInstancesBeforeTheManifest(t *testing.T) {
	root := t.TempDir()
	m, _ := newTestModel(Config{PrismDirs: []string{root}}, 80, 24)
	m.Update(loadedMsg{insts: []prism.Instance{makeInst(t, root, fullSpec("Home"))}})

	got := pageLines(m, 78)

	if !m.loaded || got[0] != "Home" {
		t.Fatalf("not loaded without the manifest: %q", got)
	}
	if !containsLine(got, "Checking for new GTNH versions…") {
		t.Errorf("no checking line while the manifest is fetched: %q", got)
	}
	if ids := strings.Join(rowIDs(m), ","); !strings.HasPrefix(ids, "play,join,undo,memory") {
		t.Errorf("rows = %s", ids)
	}
}

func TestOfflinePageOffersTryAgainAndKeepsPlayAndUndo(t *testing.T) {
	m, f := offlineModel(t)

	got := pageLines(m, 78)

	if !containsLine(got, "I can't reach the GTNH download server") {
		t.Errorf("no offline line: %q", got)
	}
	if ids := strings.Join(rowIDs(m), ","); !strings.HasPrefix(ids, "play,join,retry,undo,memory") {
		t.Errorf("rows = %s", ids)
	}
	if glyph(m.home[m.insts[0].Dir]) != "●" {
		t.Errorf("an unchecked version is marked %q, want a plain ●", glyph(m.home[m.insts[0].Dir]))
	}
	settle(m, press(m, "p")) // syncs the server's mods first, from the copy of the last archive
	if len(f.launches) != 1 {
		t.Errorf("Play offline launched %d times, want 1", len(f.launches))
	}
}

func TestOfflineRetryFetchesAgain(t *testing.T) {
	m, _ := offlineModel(t)

	cmd := press(m, "r")

	if cmd == nil || m.manifestErr != nil {
		t.Fatalf("r gave cmd %v, manifestErr %v; want a fetch and the error cleared", cmd, m.manifestErr)
	}
	if !containsLine(pageLines(m, 78), "Checking for new GTNH versions…") {
		t.Errorf("no checking line while retrying")
	}
	m.Update(manifestMsg{m: testManifest()})
	if !containsLine(pageLines(m, 78), "GTNH 2.8.4 is out") {
		t.Errorf("no update row once the manifest is here: %q", pageLines(m, 78))
	}
	if press(m, "r") != nil {
		t.Errorf("r with the manifest here should do nothing")
	}
}

func TestOfflineUpdateAndNewInstanceSayWhy(t *testing.T) {
	// The update rows aren't there offline; the flows refuse on their own too.
	for name, start := range map[string]func(m *model){
		"update":   func(m *model) { m.startUpdate("2.8.4") },
		"versions": func(m *model) { m.chooseVersion() },
		"new":      func(m *model) { press(m, "n") },
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := offlineModel(t)

			start(m)

			if m.dialog == nil || m.dialog.title != cantReachGTNH {
				t.Fatalf("dialog = %+v", m.dialog)
			}
			if body := m.dialog.body(60); !strings.Contains(body, "press r to try again") ||
				!strings.Contains(body, "(dial tcp: lookup downloads.gtnewhorizons.com: no such host)") {
				t.Errorf("body = %q", body)
			}
		})
	}
}

func TestOfflineCommandLineUpdateWaitsForTheManifest(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.1"})
	m, _ := newTestModel(Config{PrismDirs: []string{root}, Target: "2.8.4"}, 80, 24)
	m.Update(loadedMsg{insts: []prism.Instance{in}})

	if m.job != nil || !m.startPending {
		t.Fatalf("job %v, pending %v before the manifest", m.job, m.startPending)
	}
	m.Update(manifestMsg{m: testManifest()})
	if m.job == nil || m.job.kind != jobUpdate || m.startPending {
		t.Errorf("job %+v, pending %v once the manifest is here; want the update", m.job, m.startPending)
	}
}

func TestOfflineCommandLineCreateWithoutTheManifestSaysWhy(t *testing.T) {
	root := t.TempDir()
	m, _ := newTestModel(Config{PrismDirs: []string{root}, Create: true}, 80, 24)
	m.Update(loadedMsg{})
	m.Update(manifestMsg{err: offlineErr()})

	if m.dialog == nil || m.dialog.title != cantReachGTNH || m.startPending {
		t.Errorf("dialog %+v, pending %v", m.dialog, m.startPending)
	}
}
