package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// autoHome loads Home (selected, linked), Other (linked) and Plain (no link) with cfg and
// returns the command that starts the syncs the launcher runs on its own.
func autoHome(t *testing.T, cfg Config) (*model, *fakes, tea.Cmd, [3]prism.Instance) {
	t.Helper()
	root := t.TempDir()
	home := makeInst(t, root, fullSpec("Home"))
	other := makeInst(t, root, instSpec{name: "Other", gtnh: true, version: "2.8.4", mods: "https://other.example.org/o.zip"})
	plain := makeInst(t, root, instSpec{name: "Plain", gtnh: true, version: "2.8.4"})
	cfg.PrismDirs, cfg.Instance = []string{root}, "Home"
	m, f := newTestModel(cfg, 100, 40)
	_, cmd := m.Update(loadedMsg{m: testManifest(), insts: []prism.Instance{plain, other, home}})
	msgs := runCmd(cmd)
	if len(msgs) != 1 || msgs[0] != (autoModsMsg{}) {
		t.Fatalf("after load %v, want autoModsMsg", msgs)
	}
	_, cmd = m.Update(msgs[0])
	return m, f, cmd, [3]prism.Instance{home, other, plain}
}

func noticeAt(m *model, dir string) notice { return m.notices[dir] }

// At start every linked instance is synced, the selected one first, with no key pressed;
// the result shows on each page as a notice.
func TestStartSyncsEveryLinkedInstance(t *testing.T) {
	m, f, cmd, in := autoHome(t, Config{})
	f.modsSync = &update.ModsSync{Plan: modsPlanOf(update.ModAdd, "a.jar", update.ModAdd, "b.jar")}

	if m.job == nil || !m.job.auto || m.job.dir != in[0].Dir || m.dialog != nil {
		t.Fatalf("job %+v dialog %v, want the quiet sync of Home", m.job, m.dialog != nil)
	}
	if ids := rowIDs(m); ids[0] != "play" {
		t.Errorf("rows %q, want Play to stay while it checks", ids)
	}
	if !containsLine(pageLines(m, 80), "Checking your server's mods…") {
		t.Errorf("page:\n%s\nwant the dim line of the check", strings.Join(pageLines(m, 80), "\n"))
	}
	msgs := runCmd(cmd)
	_, cmd = m.Update(msgs[0]) // prepared
	f.modsSync = nil           // Other: nothing changes
	settle(m, cmd)

	if m.job != nil || len(f.modsSyncs) != 2 || f.modsSyncs[0].Instance.Dir != in[0].Dir || f.modsSyncs[1].Instance.Dir != in[1].Dir {
		t.Fatalf("job %+v syncs %+v, want Home then Other, Plain left alone", m.job, f.modsSyncs)
	}
	if n := noticeAt(m, in[0].Dir); n.text != "Server mods synced just now · 2 new" {
		t.Errorf("Home notice %+v", n)
	}
	if n := noticeAt(m, in[1].Dir); !reflect.DeepEqual(n, notice{info: []string{"Your server's mods are up to date."}}) {
		t.Errorf("Other notice %+v", n)
	}
	if _, ok := m.notices[in[2].Dir]; ok || len(f.launches) != 0 {
		t.Errorf("Plain notice %+v, launches %d; want neither", m.notices[in[2].Dir], len(f.launches))
	}
	if !containsLine(pageLines(m, 80), "Server mods synced just now · 2 new") {
		t.Errorf("page:\n%s\nwant the notice", strings.Join(pageLines(m, 80), "\n"))
	}
}

// What can go wrong is said on the page, never in a dialog.
func TestStartSyncProblemsAreNotices(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *fakes)
		want  notice
	}{
		{"offline", func(f *fakes) {
			f.modsSync = &update.ModsSync{Plan: modsPlanOf(), FetchErr: errors.New("no connection")}
		}, notice{warn: []string{"I couldn't check your server's mods: no connection."}}},
		{"game running", func(f *fakes) { f.modsApplyErr = update.ErrGameRunning },
			notice{info: []string{"The game is running, so I'll sync your server's mods the next time you play."}}},
		{"failed", func(f *fakes) { f.modsApplyErr = update.ErrModsRolledBack },
			notice{warn: []string{"I couldn't sync your server's mods: " + update.ErrModsRolledBack.Error() + "."}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, f, cmd, in := autoHome(t, Config{})
			c.setup(f)
			settle(m, cmd)
			if m.job != nil || m.dialog != nil || !reflect.DeepEqual(noticeAt(m, in[0].Dir), c.want) {
				t.Errorf("job %+v dialog %v notice %+v, want %+v", m.job, m.dialog != nil, noticeAt(m, in[0].Dir), c.want)
			}
		})
	}
}

// Play on the instance being synced waits for that sync, which is all it needs, and the
// queued ones are left to their own Play.
func TestPlayWaitsForTheStartSync(t *testing.T) {
	m, f, cmd, in := autoHome(t, Config{})
	m.focus = focusPage

	if c := press(m, "p"); c != nil || m.dialog != nil {
		t.Fatalf("p gave a command or the dialog %q; want it to wait", dialogTitle(m))
	}
	msgs := settle(m, cmd)

	if !launchedMsgs(msgs) || len(f.modsSyncs) != 1 || f.modsSyncs[0].Instance.Dir != in[0].Dir {
		t.Errorf("msgs %v syncs %+v, want Home synced once, then launched", msgs, f.modsSyncs)
	}
}

// Play on another instance stops the download of the start sync and syncs its own first.
func TestPlayElsewhereStopsTheStartSync(t *testing.T) {
	m, f, cmd, in := autoHome(t, Config{})
	m.selectDir(in[1].Dir)
	runCmd(cmd) // the download starts; it stops on its context

	press(m, "p")
	if ctx := f.modsSyncs[0].Context; ctx == nil || ctx.Err() == nil {
		t.Fatalf("the download of Home's sync wasn't stopped")
	}
	_, cmd = m.Update(errMsg{context.Canceled}) // what the stopped download returns
	msgs := settle(m, cmd)

	if !launchedMsgs(msgs) || len(f.modsSyncs) != 2 || f.modsSyncs[1].Instance.Dir != in[1].Dir {
		t.Errorf("msgs %v syncs %+v, want Other synced, then launched", msgs, f.modsSyncs)
	}
	if _, ok := m.notices[in[0].Dir]; ok {
		t.Errorf("Home notice %+v, want none for a stopped check", m.notices[in[0].Dir])
	}
}

// Anything else waits a moment, and says why.
func TestOtherWorkDuringTheStartSync(t *testing.T) {
	m, _, _, _ := autoHome(t, Config{})
	m.focus = focusPage
	press(m, "u")
	if dialogTitle(m) != "Just a moment" || !strings.Contains(flat(m.dialog.body(200)), "I'm checking your server's mods for Home.") {
		t.Errorf("dialog %q, want Just a moment", dialogTitle(m))
	}
}

// A link from the command line is synced at start on the selected instance; one the
// server can't give isn't saved and changes nothing.
func TestStartSyncUsesTheCommandLinesLink(t *testing.T) {
	const link = "https://cfg.example.org/new.zip"
	m, f, cmd, in := autoHome(t, Config{ServerMods: link})
	f.modsSync = &update.ModsSync{Plan: modsPlanOf(update.ModAdd, "a.jar"), FetchErr: errors.New("the link doesn't lead to a file anymore")}
	msgs := runCmd(cmd)
	_, cmd = m.Update(msgs[0])
	f.modsSync = nil

	if f.modsSyncs[0].URL != link || f.modsApplied != 0 {
		t.Fatalf("syncs %+v applied %d, want the link tried and nothing applied", f.modsSyncs, f.modsApplied)
	}
	want := notice{warn: []string{"I couldn't use the server mods link you gave me, so I didn't save it: the link doesn't lead to a file anymore."}}
	if !reflect.DeepEqual(noticeAt(m, in[0].Dir), want) {
		t.Errorf("notice %+v, want %+v", noticeAt(m, in[0].Dir), want)
	}
	settle(m, cmd)
	if st, _ := update.LoadState(in[0].Dir); st.CustomModsURL != "https://mods.example.org/pack/extra.zip" {
		t.Errorf("link %q, want the old one kept", st.CustomModsURL)
	}

	m, f, cmd, in = autoHome(t, Config{ServerMods: link})
	settle(m, cmd)
	if st, _ := update.LoadState(in[0].Dir); st.CustomModsURL != link || f.modsApplied != 2 {
		t.Errorf("link %q applied %d, want the working link saved", st.CustomModsURL, f.modsApplied)
	}
}

// Starting with something to do (play, update, create) leaves the server mods to it.
func TestNoStartSyncWithACommandLineTask(t *testing.T) {
	root := t.TempDir()
	home := makeInst(t, root, fullSpec("Home"))
	m, f := newTestModel(Config{PrismDirs: []string{root}, Play: true}, 80, 24)
	_, cmd := m.Update(loadedMsg{m: testManifest(), insts: []prism.Instance{home}})
	settle(m, cmd)
	if len(f.modsSyncs) != 1 || len(f.launches) != 1 || m.job != nil {
		t.Errorf("syncs %d launches %d job %+v, want only Play's sync", len(f.modsSyncs), len(f.launches), m.job)
	}
}
