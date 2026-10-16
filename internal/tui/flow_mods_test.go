package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/appcfg"
	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// settle runs cmd and, while it is the server-mods sync, delivers each of its messages
// to m and runs the command that follows; it returns the messages of the first command
// that isn't the sync (what waited for it, like the launch).
func settle(m *model, cmd tea.Cmd) []tea.Msg {
	msgs := runCmd(cmd)
	for len(msgs) == 1 {
		switch msgs[0].(type) {
		case modsPreparedMsg, modsSyncedMsg:
		case errMsg:
			if m.job == nil || m.job.kind != jobMods {
				return msgs
			}
		default:
			return msgs
		}
		_, cmd = m.Update(msgs[0])
		msgs = runCmd(cmd)
	}
	return msgs
}

// modsPlanOf is a server-mods plan of (kind, jar name) pairs: each change works on the
// file of its name, a ModSkip on none (GTNH has the jar).
func modsPlanOf(pairs ...any) *update.ModsPlan {
	p := &update.ModsPlan{Managed: map[string]pack.Fingerprint{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		k, n := pairs[i].(update.ModKind), pairs[i+1].(string)
		c := update.ModChange{Kind: k, Name: n, Disk: n}
		if k == update.ModSkip {
			c.Disk, c.With = "", n
		}
		p.Changes = append(p.Changes, c)
	}
	return p
}

// modsHome is a loaded 80x24 model with two GTNH instances, "Home" (fullSpec: a server
// and its mods link) selected with the page focused, and "Second" without either.
func modsHome(t *testing.T) (*model, *fakes, prism.Instance, prism.Instance) {
	t.Helper()
	root := t.TempDir()
	home := makeInst(t, root, fullSpec("Home"))
	second := makeInst(t, root, instSpec{name: "Second", gtnh: true, version: "2.8.4"})
	m, f := loadedModel(root, 80, 24, home, second)
	m.selectDir(home.Dir)
	m.focus = focusPage
	return m, f, home, second
}

func launchedMsgs(msgs []tea.Msg) bool {
	return len(msgs) == 1 && msgs[0] == (launchedMsg{})
}

// Play syncs the server's mods first, as a job on the instance, then launches.
func TestPlaySyncsTheServerModsFirst(t *testing.T) {
	m, f, home, _ := modsHome(t)
	f.modsSync = &update.ModsSync{Plan: modsPlanOf(update.ModAdd, "a.jar", update.ModUpdate, "b.jar")}

	cmd := press(m, "p")

	if m.job == nil || m.job.kind != jobMods || m.job.dir != home.Dir || m.job.title != "Syncing your server's mods" || len(f.launches) != 0 {
		t.Fatalf("job %+v launches %d, want the sync of Home and no launch yet", m.job, len(f.launches))
	}
	if got := rowIDs(m); got[0] != "job" {
		t.Errorf("rows %q, want the job row in place of Play", got)
	}
	msgs := settle(m, cmd)

	if len(f.modsSyncs) != 1 || f.modsSyncs[0].Instance.Dir != home.Dir || f.modsSyncs[0].URL != "" || f.modsApplied != 1 {
		t.Errorf("syncs %+v applied %d, want one of Home with its own link", f.modsSyncs, f.modsApplied)
	}
	if m.job != nil || !launchedMsgs(msgs) || len(f.launches) != 1 || f.launches[0].inst.Dir != home.Dir {
		t.Fatalf("job %+v msgs %v launches %+v, want Home launched after the sync", m.job, msgs, f.launches)
	}
	want := notice{text: "Server mods synced just now · 1 new, 1 updated"}
	if got := m.notices[home.Dir]; !reflect.DeepEqual(got, want) {
		t.Errorf("notice %+v, want %+v", got, want)
	}
}

// The game starts even when the sync failed; the warning keeps the launcher open.
func TestPlayStartsTheGameWhenTheSyncFails(t *testing.T) {
	cases := []struct {
		name string
		set  func(f *fakes)
		warn []string
		text string
	}{
		{"prepare fails", func(f *fakes) { f.modsPrepErr = errors.New("I couldn't read my notes") },
			[]string{"I couldn't sync your server's mods: I couldn't read my notes."}, ""},
		{"apply fails", func(f *fakes) { f.modsApplyErr = errors.New("the game is still running from this instance") },
			[]string{"I couldn't sync your server's mods: the game is still running from this instance."}, ""},
		{"from the copy of last time", func(f *fakes) {
			f.modsSync = &update.ModsSync{Plan: modsPlanOf(update.ModAdd, "a.jar"), FetchErr: errors.New("the link doesn't lead to a file anymore")}
		}, []string{"I couldn't check your server's mods: the link doesn't lead to a file anymore."}, "Server mods synced just now · 1 new"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, f, home, _ := modsHome(t)
			m.app.AfterPlay = appcfg.AfterPlayQuit
			c.set(f)

			msgs := settle(m, press(m, "p"))
			_, cmd := m.Update(launchedMsg{})

			if !launchedMsgs(msgs) || len(f.launches) != 1 {
				t.Fatalf("msgs %v launches %d, want the game started anyway", msgs, len(f.launches))
			}
			if got := m.notices[home.Dir]; !reflect.DeepEqual(got, notice{text: c.text, warn: c.warn}) {
				t.Errorf("notice %+v, want text %q warn %q", got, c.text, c.warn)
			}
			if hasQuit(runCmdNoTick(cmd)) || m.quitting || m.play.state != "starting" {
				t.Errorf("quit %v play %q, want the launcher open and watching the game", m.quitting, m.play.state)
			}
		})
	}
}

// runCmdNoTick runs cmd unless it's a poll tick (which would wait 2 s).
func runCmdNoTick(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan []tea.Msg, 1)
	go func() { done <- runCmd(cmd) }()
	select {
	case msgs := <-done:
		return msgs
	case <-time.After(200 * time.Millisecond):
		return nil
	}
}

// A sync without anything to say lets the launcher quit after Play as set.
func TestPlayQuitsAfterAQuietSync(t *testing.T) {
	m, f, _, _ := modsHome(t)
	m.app.AfterPlay = appcfg.AfterPlayQuit

	settle(m, press(m, "p"))
	_, cmd := m.Update(launchedMsg{})

	if len(f.launches) != 1 || !hasQuit(runCmd(cmd)) {
		t.Errorf("launches %d, want the launch and then quit", len(f.launches))
	}
}

// Without a link nor jars from an old one, Play launches right away.
func TestPlayWithoutServerModsLaunchesRightAway(t *testing.T) {
	m, f, _, second := modsHome(t)
	m.selectDir(second.Dir)

	msgs := runCmd(press(m, "p"))

	if !launchedMsgs(msgs) || len(f.modsSyncs) != 0 || m.job != nil {
		t.Errorf("msgs %v syncs %d job %+v, want a launch and no sync", msgs, len(f.modsSyncs), m.job)
	}
}

// While another instance's job runs, Play doesn't wait for a sync: it says so, starts
// the game and stays open.
func TestPlayDuringAnotherJobLeavesTheSyncForNextTime(t *testing.T) {
	m, f, home, second := modsHome(t)
	m.app.AfterPlay = appcfg.AfterPlayQuit
	m.job = jobOn(second.Dir, "Updating to GTNH 2.8.4", "apply")

	msgs := runCmd(press(m, "p"))
	_, cmd := m.Update(launchedMsg{})

	if !launchedMsgs(msgs) || len(f.modsSyncs) != 0 || m.job.dir != second.Dir {
		t.Fatalf("msgs %v syncs %d job %+v, want a launch, no sync, the other job untouched", msgs, len(f.modsSyncs), m.job)
	}
	want := []string{"I didn't check your server's mods because I'm busy with Second. I'll do it the next time you play."}
	if got := m.notices[home.Dir]; !eq(got.warn, want) {
		t.Errorf("notice %+v, want warn %q", got, want)
	}
	if hasQuit(runCmdNoTick(cmd)) || m.quitting {
		t.Errorf("quit while another job applies")
	}
}

// While a job runs on the instance itself, Play refuses.
func TestPlayDuringTheInstancesOwnJobRefuses(t *testing.T) {
	m, f, home, _ := modsHome(t)
	m.job = jobOn(home.Dir, "Updating to GTNH 2.8.4", "apply")

	cmd := press(m, "p")

	if cmd != nil || len(f.launches) != 0 || dialogTitle(m) != "One thing at a time" {
		t.Errorf("cmd %v launches %d dialog %q, want One thing at a time", cmd != nil, len(f.launches), dialogTitle(m))
	}
}

// joinHome is a loaded model with one GTNH instance that has a server but was never
// asked about its mods.
func joinHome(t *testing.T) (*model, *fakes, prism.Instance) {
	t.Helper()
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4", server: "play.example.org"})
	m, f := loadedModel(root, 80, 24, in)
	m.focus = focusPage
	return m, f, in
}

// Play and join asks once about the server's mods; the answer is saved, synced and the
// game joins the server.
func TestPlayAndJoinAsksAboutTheServersModsOnce(t *testing.T) {
	m, f, in := joinHome(t)

	cmd := press(m, "j")

	if cmd != nil || dialogTitle(m) != modsTitle || m.dialog.input == nil || len(f.launches) != 0 {
		t.Fatalf("cmd %v dialog %q launches %d, want the question first", cmd != nil, dialogTitle(m), len(f.launches))
	}
	if got := m.dialog.body(200); got != serverModsIntro {
		t.Errorf("intro %q", got)
	}
	m.dialog.input.SetValue("https://mods.example.org/m.zip")
	msgs := settle(m, press(m, "enter"))

	if st := seState(t, in.Dir); st.CustomModsURL != "https://mods.example.org/m.zip" || !st.CustomModsAsked {
		t.Errorf("state %+v, want the link saved as asked", st)
	}
	if len(f.modsSyncs) != 1 || !launchedMsgs(msgs) || len(f.launches) != 1 || f.launches[0].server != "play.example.org" {
		t.Fatalf("syncs %d msgs %v launches %+v, want a sync then the join", len(f.modsSyncs), msgs, f.launches)
	}

	settle(m, press(m, "j"))
	if m.dialog != nil || len(f.launches) != 2 {
		t.Errorf("dialog %q launches %d, want the second join without asking", dialogTitle(m), len(f.launches))
	}
}

// An empty answer means none: saved as asked, no sync, the game joins.
func TestPlayAndJoinWithNoExtraMods(t *testing.T) {
	m, f, in := joinHome(t)
	press(m, "j")

	msgs := runCmd(press(m, "enter"))

	if st := seState(t, in.Dir); st.CustomModsURL != "" || !st.CustomModsAsked {
		t.Errorf("state %+v, want asked with no link", st)
	}
	if len(f.modsSyncs) != 0 || !launchedMsgs(msgs) {
		t.Errorf("syncs %d msgs %v, want the join without a sync", len(f.modsSyncs), msgs)
	}
}

// esc on the question starts nothing; a bad link keeps the question open.
func TestPlayAndJoinQuestionEscAndBadLinks(t *testing.T) {
	m, f, in := joinHome(t)
	press(m, "j")
	press(m, "h", "t", "t", "p", ":", "/", "/", "x", "enter")
	if dialogTitle(m) != modsTitle || m.dialog.inputErr != msgBadModsLink {
		t.Fatalf("dialog %q err %q, want the question with msgBadModsLink", dialogTitle(m), m.dialog.inputErr)
	}

	cmd := press(m, "esc")

	if cmd != nil || m.dialog != nil || len(f.launches) != 0 {
		t.Errorf("cmd %v dialog %q launches %d, want nothing started", cmd != nil, dialogTitle(m), len(f.launches))
	}
	if st, _ := update.LoadState(in.Dir); st.CustomModsAsked {
		t.Errorf("esc saved an answer: %+v", st)
	}
}

// Plain Play never asks.
func TestPlayNeverAsksAboutServerMods(t *testing.T) {
	m, f, _ := joinHome(t)

	msgs := runCmd(press(m, "p"))

	if m.dialog != nil || !launchedMsgs(msgs) || len(f.launches) != 1 {
		t.Errorf("dialog %q msgs %v, want the game started", dialogTitle(m), msgs)
	}
}

// editTo saves value in the setting id of the current instance and returns the command.
func editTo(m *model, id, value string) tea.Cmd {
	m.editSetting(id)
	m.edit.input.SetValue(value)
	return press(m, "enter")
}

// openModsRow selects the Server mods row of the current instance and presses enter.
func openModsRow(m *model) tea.Cmd {
	m.focus = focusPage
	m.selectRow("mods")
	return press(m, "enter")
}

// dialogText is the open dialog's body, flattened.
func dialogText(m *model) string {
	if m.dialog == nil {
		return ""
	}
	return flat(m.dialog.body(200))
}

// Without a link the Server mods row asks for one; a pasted link (quotes and all) is
// checked by syncing from it, and the dialog says what the sync did.
func TestServerModsRowWithoutALinkAsksForOne(t *testing.T) {
	m, f, _, second := modsHome(t)
	m.selectDir(second.Dir)
	f.modsSync = &update.ModsSync{Plan: modsPlanOf(update.ModAdd, "a.jar")}
	f.modsManaged = []string{"a.jar"}

	openModsRow(m)
	if dialogTitle(m) != modsDialogTitle || m.dialog.input == nil || m.dialog.input.Value() != "" {
		t.Fatalf("dialog %q, want the empty field for the link", dialogTitle(m))
	}
	m.dialog.input.SetValue(` "https://mods.example.org/m.zip" `)
	cmd := press(m, "enter")

	if dialogTitle(m) != "Checking your server's mods" || m.job == nil || m.job.kind != jobMods {
		t.Fatalf("dialog %q job %+v, want the check in a progress dialog", dialogTitle(m), m.job)
	}
	if got := m.statusPairs(); !eq(got, []string{"esc", "cancel"}) {
		t.Errorf("status %q, want esc cancel while it downloads", got)
	}
	if st, _ := update.LoadState(second.Dir); st.CustomModsURL != "" {
		t.Errorf("link saved before the check: %+v", st)
	}
	settle(m, cmd)

	if len(f.modsSyncs) != 1 || f.modsSyncs[0].URL != "https://mods.example.org/m.zip" || f.modsSyncs[0].Context == nil {
		t.Fatalf("syncs %+v, want one from the cleaned link, cancellable", f.modsSyncs)
	}
	if st := seState(t, second.Dir); st.CustomModsURL != "https://mods.example.org/m.zip" || !st.CustomModsAsked {
		t.Errorf("state %+v, want the link saved once it worked", st)
	}
	if dialogTitle(m) != modsDialogTitle || m.job != nil || m.modsFlow != nil {
		t.Fatalf("dialog %q job %+v, want the result", dialogTitle(m), m.job)
	}
	if got, want := dialogText(m), "Done: 1 new. From mods.example.org now: a.jar"; got != want {
		t.Errorf("result %q, want %q", got, want)
	}
	if _, ok := m.notices[second.Dir]; ok {
		t.Errorf("notice %+v, want the result in the dialog only", m.notices[second.Dir])
	}
}

// A new link the server can't give isn't saved: the field comes back with the reason
// and nothing in mods/ changes.
func TestABrokenNewLinkIsNotSaved(t *testing.T) {
	m, f, home, _ := modsHome(t)
	f.modsSync = &update.ModsSync{Plan: modsPlanOf(), FetchErr: update.ErrModsWebPage}

	openModsRow(m)
	press(m, "down", "enter") // Use another link
	if m.dialog == nil || m.dialog.input == nil || m.dialog.input.Value() != "https://mods.example.org/pack/extra.zip" {
		t.Fatalf("dialog %q, want the field with the current link", dialogTitle(m))
	}
	m.dialog.input.SetValue("https://www.dropbox.com/s/abc/mods.zip?dl=0")
	settle(m, press(m, "enter"))

	if m.dialog == nil || m.dialog.input == nil || m.dialog.input.Value() != "https://www.dropbox.com/s/abc/mods.zip?dl=0" {
		t.Fatalf("dialog %q, want the field back with the link as typed", dialogTitle(m))
	}
	if want := "I couldn't use that link: " + update.ErrModsWebPage.Error() + "."; m.dialog.inputErr != want {
		t.Errorf("err %q, want %q", m.dialog.inputErr, want)
	}
	if f.modsApplied != 0 || m.job != nil {
		t.Errorf("applied %d job %+v, want nothing applied", f.modsApplied, m.job)
	}
	if st := seState(t, home.Dir); st.CustomModsURL != "https://mods.example.org/pack/extra.zip" {
		t.Errorf("link %q, want the old one kept", st.CustomModsURL)
	}
}

// With a link the row opens a dialog that shows the link and the mods; "Check for new
// mods" syncs now from the same link, and says so even when nothing changed.
func TestCheckForNewModsFromTheRow(t *testing.T) {
	m, f, home, _ := modsHome(t)
	if err := update.UpdateState(home.Dir, func(st *update.State) {
		st.CustomMods, st.CustomModsSynced = []string{"a.jar", "b.jar"}, time.Now().Add(-3*day)
	}); err != nil {
		t.Fatal(err)
	}
	m.refresh()

	openModsRow(m)
	if dialogTitle(m) != modsDialogTitle || m.dialog.list == nil {
		t.Fatalf("dialog %q, want the Server mods dialog", dialogTitle(m))
	}
	if got, want := dialogText(m), "From https://mods.example.org/pack/extra.zip 2 mods, synced 3 days ago: a.jar, b.jar"; got != want {
		t.Errorf("intro %q, want %q", got, want)
	}
	var titles []string
	for _, it := range m.dialog.list.items {
		titles = append(titles, it.title)
	}
	if want := []string{"Check for new mods", "Use another link", "Stop using them"}; !eq(titles, want) {
		t.Errorf("items %q, want %q", titles, want)
	}
	settle(m, press(m, "enter"))

	if len(f.modsSyncs) != 1 || f.modsSyncs[0].URL != "" || f.modsApplied != 1 {
		t.Fatalf("syncs %+v applied %d, want one from the instance's own link", f.modsSyncs, f.modsApplied)
	}
	if got, want := dialogText(m), "You have all of the server's mods already. From mods.example.org now: a.jar, b.jar"; got != want {
		t.Errorf("result %q, want %q", got, want)
	}

	// the server can't be reached: the dialog says so, from the copy of last time
	press(m, "enter")
	f.modsSync = &update.ModsSync{Plan: modsPlanOf(update.ModUpdate, "a.jar"), FetchErr: errors.New("the download stopped")}
	openModsRow(m)
	settle(m, press(m, "enter"))
	if got, want := dialogText(m), "Done: 1 updated. I couldn't check your server's mods: the download stopped. From mods.example.org now: a.jar, b.jar"; got != want {
		t.Errorf("result %q, want %q", got, want)
	}
}

// "Stop using them" takes the server's mods out and forgets the link.
func TestStopUsingTheServersMods(t *testing.T) {
	m, f, home, _ := modsHome(t)
	f.modsSync = &update.ModsSync{Plan: modsPlanOf(update.ModRemove, "a.jar", update.ModRemove, "b.jar")}
	f.modsManaged = []string{}

	openModsRow(m)
	if dialogTitle(m) != modsDialogTitle {
		t.Fatalf("dialog %q", dialogTitle(m))
	}
	cmd := press(m, "down", "down", "enter")
	if dialogTitle(m) != "Taking out your server's mods" {
		t.Errorf("progress %q", dialogTitle(m))
	}
	settle(m, cmd)

	if len(f.modsSyncs) != 1 || f.modsSyncs[0].URL != "none" {
		t.Fatalf("syncs %+v, want one with no link", f.modsSyncs)
	}
	if st := seState(t, home.Dir); st.CustomModsURL != "" || !st.CustomModsAsked {
		t.Errorf("state %+v, want no link, asked", st)
	}
	if got := dialogText(m); got != "I took out 2 mods of your server." {
		t.Errorf("result %q", got)
	}
	if got := settingValue("mods", m.home[home.Dir], m.now()); got != "none" {
		t.Errorf("row %q, want none", got)
	}
}

// esc while the link downloads stops it and goes back to the field, quietly.
func TestEscStopsTheCheckOfALink(t *testing.T) {
	m, f, _, second := modsHome(t)
	m.selectDir(second.Dir)
	openModsRow(m)
	m.dialog.input.SetValue("https://mods.example.org/m.zip")
	cmd := press(m, "enter")

	press(m, "esc")
	if len(f.modsSyncs) != 0 {
		// the fake prepares only when cmd runs; the options carry the context
		t.Fatalf("syncs %+v before the command ran", f.modsSyncs)
	}
	settle(m, cmd)

	if err := f.modsSyncs[0].Context.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("context %v, want cancelled", err)
	}
	if m.dialog == nil || m.dialog.input == nil || m.dialog.input.Value() != "https://mods.example.org/m.zip" || m.dialog.inputErr != "" {
		t.Fatalf("dialog %q, want the field back as typed, without an error", dialogTitle(m))
	}
	if f.modsApplied != 0 || m.job != nil {
		t.Errorf("applied %d job %+v, want nothing", f.modsApplied, m.job)
	}

	// the real prepare answers a cancelled download with an error: same outcome
	cmd = press(m, "enter")
	press(m, "esc")
	m.Update(errMsg{context.Canceled})
	if m.dialog == nil || m.dialog.input == nil || m.dialog.inputErr != "" || m.job != nil {
		t.Errorf("dialog %q job %+v, want the field back", dialogTitle(m), m.job)
	}
	_ = cmd
}

// A sync from the dialog that fails ends in an error dialog that says what changed.
func TestASyncFromTheDialogThatFails(t *testing.T) {
	cases := []struct {
		name    string
		set     func(f *fakes)
		outcome string
	}{
		{"prepare", func(f *fakes) { f.modsPrepErr = errors.New("I couldn't read my notes") }, "Nothing was changed."},
		{"rolled back", func(f *fakes) { f.modsApplyErr = fmt.Errorf("disk full (%w)", update.ErrModsRolledBack) },
			"Everything was put back the way it was."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, f, home, _ := modsHome(t)
			c.set(f)
			openModsRow(m)
			settle(m, press(m, "enter"))

			if dialogTitle(m) != "Something went wrong" || !strings.Contains(dialogText(m), c.outcome) ||
				!strings.HasPrefix(dialogText(m), "I couldn't sync your server's mods: ") {
				t.Errorf("dialog %q %q, want the error with %q", dialogTitle(m), dialogText(m), c.outcome)
			}
			if _, ok := m.notices[home.Dir]; ok || m.job != nil {
				t.Errorf("notice %+v job %+v, want neither", m.notices[home.Dir], m.job)
			}
		})
	}
}

// While a job runs, the Server mods row says one thing at a time.
func TestServerModsRowWhileBusy(t *testing.T) {
	m, f, _, second := modsHome(t)
	m.job = jobOn(second.Dir, "Updating to GTNH 2.8.4", "prepare")

	if cmd := openModsRow(m); cmd != nil || dialogTitle(m) != "One thing at a time" || len(f.modsSyncs) != 0 {
		t.Errorf("cmd %v dialog %q syncs %d, want One thing at a time", cmd != nil, dialogTitle(m), len(f.modsSyncs))
	}
}

// Play and join checks the answer to the question too: a broken link comes back to
// the question, an empty one joins without mods.
func TestPlayAndJoinQuestionChecksTheLink(t *testing.T) {
	m, f, in := joinHome(t)
	f.modsSync = &update.ModsSync{Plan: modsPlanOf(), FetchErr: update.ErrModsNotFound}
	press(m, "j")
	m.dialog.input.SetValue("https://mods.example.org/gone.zip")

	settle(m, press(m, "enter"))
	if dialogTitle(m) != modsTitle || !strings.Contains(m.dialog.inputErr, update.ErrModsNotFound.Error()) || len(f.launches) != 0 {
		t.Fatalf("dialog %q err %q launches %d, want the question back with the reason", dialogTitle(m), m.dialog.inputErr, len(f.launches))
	}
	m.dialog.input.SetValue("")
	msgs := settle(m, press(m, "enter"))

	if st := seState(t, in.Dir); st.CustomModsURL != "" || !st.CustomModsAsked {
		t.Errorf("state %+v, want asked with no link", st)
	}
	if !launchedMsgs(msgs) || len(f.launches) != 1 || f.launches[0].server != "play.example.org" {
		t.Errorf("msgs %v launches %+v, want the join", msgs, f.launches)
	}
}

// Saving a server on an instance never asked about its mods asks, and syncs a link.
func TestSavingAServerAsksAboutItsMods(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4"})
	m, f := loadedModel(root, 80, 24, in)
	m.focus = focusPage

	editTo(m, "server", "play.example.org")

	if dialogTitle(m) != modsTitle || m.savedRow != "server" {
		t.Fatalf("dialog %q saved %q, want the question after saving", dialogTitle(m), m.savedRow)
	}
	m.dialog.input.SetValue("https://mods.example.org/m.zip")
	if msgs := settle(m, press(m, "enter")); len(msgs) != 0 || len(f.launches) != 0 {
		t.Errorf("msgs %v launches %d, want no launch", msgs, len(f.launches))
	}
	if st := seState(t, in.Dir); st.CustomModsURL != "https://mods.example.org/m.zip" || len(f.modsSyncs) != 1 {
		t.Errorf("state %+v syncs %d, want the link saved and synced", st, len(f.modsSyncs))
	}
	if dialogTitle(m) != modsDialogTitle {
		t.Errorf("dialog %q, want what the sync did", dialogTitle(m))
	}
	press(m, "enter")

	editTo(m, "server", "other.example.org")
	if m.dialog != nil {
		t.Errorf("asked again: %q", dialogTitle(m))
	}
}

// The first server adds the join row above the settings: the saved row stays selected.
func TestSavingTheFirstServerKeepsItsRowSelected(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4", modsAsked: true})
	m, _ := loadedModel(root, 80, 24, in)
	m.focus = focusPage

	editTo(m, "server", "play.example.org")

	if rs := m.rows(); m.row >= len(rs) || rs[m.row].id != "server" || indexOf(rowIDs(m), "join") < 0 {
		t.Errorf("selected %q of %q, want the server row with the join row added", rowIDs(m)[m.row], rowIDs(m))
	}
}

// -play with -server-mods syncs that link the first time only.
func TestPlayFromTheCommandLineSyncsItsLinkOnce(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, fullSpec("Home"))
	m, f := newTestModel(Config{PrismDirs: []string{root}, Play: true, ServerMods: "https://cfg.example.org/m.zip"}, 80, 24)
	_, cmd := m.Update(loadedMsg{m: testManifest(), insts: []prism.Instance{in}})
	settle(m, cmd)
	m.Update(launchedMsg{})
	m.play.state = "closed"
	settle(m, press(m, "p"))

	if len(f.modsSyncs) != 2 || f.modsSyncs[0].URL != "https://cfg.example.org/m.zip" || f.modsSyncs[1].URL != "" {
		t.Errorf("syncs %+v, want the command line's link once, then the instance's", f.modsSyncs)
	}
}

// q quits during the download of a sync, never while it changes mods/.
func TestQuitDuringASync(t *testing.T) {
	m, _, _, _ := modsHome(t)
	press(m, "p")

	m.job.phase = "apply"
	if cmd := press(m, "q"); cmd != nil || m.quitting {
		t.Errorf("quit while the sync applies")
	}
	m.job.phase = "prepare"
	if cmd := press(m, "q"); cmd == nil || !m.quitting {
		t.Errorf("q during the download didn't quit")
	}
}

func TestModsNotices(t *testing.T) {
	mine := modsPlanOf(update.ModAdd, "a.jar")
	mine.Changes = append(mine.Changes, update.ModChange{Kind: update.ModReplace, Name: "x.jar", Disk: "x.jar", Preserve: true},
		update.ModChange{Kind: update.ModSetAside, Name: "y-1.0.jar", Disk: "y-1.0.jar", With: "y.jar", Preserve: true},
		update.ModChange{Kind: update.ModKeep, Name: "old.jar", Disk: "old.jar"})
	cases := []struct {
		name   string
		p      *update.ModsPlan
		err    error
		linked bool
		want   notice
		ok     bool
	}{
		{"nothing changed", modsPlanOf(update.ModAdopt, "a.jar"), nil, true, notice{}, false},
		{"the player's jars", mine, nil, true, notice{text: "Server mods synced just now · 1 new, 1 updated",
			warn: []string{"I moved your own x.jar, y-1.0.jar to .gtnh-updater/replaced-mods inside the instance to make way for the server's"},
			info: []string{"The server dropped old.jar, but you changed it, so it stays"}}, true},
		{"server out of reach, nothing changed", modsPlanOf(), errors.New("the download stopped"), true,
			notice{warn: []string{"I couldn't check your server's mods: the download stopped."}}, true},
		{"old server's mods", modsPlanOf(update.ModRemove, "a.jar"), nil, false,
			notice{text: "Removed 1 mod of your old server just now"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := modsNotice(c.p, c.err, c.linked)
			if ok != c.ok || !reflect.DeepEqual(got, c.want) {
				t.Errorf("modsNotice = %+v %v, want %+v %v", got, ok, c.want, c.ok)
			}
		})
	}
}
