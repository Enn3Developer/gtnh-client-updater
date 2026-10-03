package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/appcfg"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Fixtures of in-place settings editing: one GTNH instance on disk (instance.cfg with the
// given extra lines, state.json), loaded at 80x24 with the page focused.

const couldntSave = "I couldn't save that setting"

func seModel(t *testing.T, s instSpec) (*model, *fakes, prism.Instance) {
	t.Helper()
	root := t.TempDir()
	in := makeInst(t, root, s)
	m, f := loadedModel(root, 80, 24, in)
	m.focus = focusPage
	return m, f, in
}

func seHome(t *testing.T, cfg string) (*model, *fakes, prism.Instance) {
	t.Helper()
	return seModel(t, instSpec{name: "Home", gtnh: true, version: "2.8.4", cfg: cfg})
}

// seSave edits setting id, types v over the prefilled value and saves it.
func seSave(t *testing.T, m *model, id, v string) tea.Cmd {
	t.Helper()
	m.editSetting(id)
	if m.edit == nil {
		t.Fatalf("editSetting(%s) didn't start editing", id)
	}
	m.edit.input.SetValue(v)
	return m.saveEdit()
}

func seSettings(t *testing.T, dir string) prism.Settings {
	t.Helper()
	s, err := prism.ReadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func seState(t *testing.T, dir string) update.State {
	t.Helper()
	st, err := update.LoadState(dir)
	if err != nil || st == nil {
		t.Fatalf("LoadState = %v, %v", st, err)
	}
	return *st
}

func seCfg(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "instance.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// seFile is a regular file in a fresh temp dir.
func seFile(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	writeFile(t, p, "binary")
	return p
}

// ---- C1: refusals ----

// C1
func TestC1EditIsRefusedWhileADialogIsOpen(t *testing.T) {
	for _, id := range []string{"memory", "server", "after", "prism"} {
		t.Run(id, func(t *testing.T) {
			m, f := oneFull(t)
			m.app.AfterPlay = appcfg.AfterPlayStay
			m.notify("Heads up", "Something to know.")

			cmd := m.editSetting(id)

			if cmd != nil || m.edit != nil {
				t.Errorf("editSetting(%s) with a dialog open: cmd %v, edit %+v; want refused", id, cmd != nil, m.edit)
			}
			if len(f.saved) != 0 || m.app.AfterPlay != appcfg.AfterPlayStay {
				t.Errorf("editSetting(%s) with a dialog open changed the app settings: saved %+v, app %+v", id, f.saved, m.app)
			}
		})
	}
}

// C1, kills K2: a job running on this instance refuses every edit.
func TestC1EditIsRefusedWhileAJobRunsOnThisInstance(t *testing.T) {
	for _, id := range []string{"memory", "jvm", "server", "mods", "after", "prism"} {
		t.Run(id, func(t *testing.T) {
			m, f := oneFull(t)
			in, _ := m.current()
			m.app.AfterPlay = appcfg.AfterPlayStay
			m.job = &job{kind: jobUpdate, dir: in.Dir, title: "Updating", phase: "prepare"}

			cmd := m.editSetting(id)

			if cmd != nil || m.edit != nil {
				t.Errorf("editSetting(%s) during a job: cmd %v, edit %+v; want refused", id, cmd != nil, m.edit)
			}
			if len(f.saved) != 0 || m.app.AfterPlay != appcfg.AfterPlayStay {
				t.Errorf("editSetting(%s) during a job changed the app settings: saved %+v, app %+v", id, f.saved, m.app)
			}
		})
	}
}

// C1
func TestC1EditIsRefusedWithoutACurrentInstance(t *testing.T) {
	m, f := loadedModel(t.TempDir(), 80, 24)

	cmd := m.editSetting("memory")

	if cmd != nil || m.edit != nil || len(f.saved) != 0 {
		t.Errorf("editSetting without an instance: cmd %v, edit %+v, saved %+v; want refused", cmd != nil, m.edit, f.saved)
	}
}

// C1: unreadable settings refuse the instance.cfg settings.
func TestC1UnreadableSettingsRefuseTheInstanceCfgSettings(t *testing.T) {
	for _, id := range []string{"memory", "jvm", "java", "window"} {
		t.Run(id, func(t *testing.T) {
			m, _, _ := seModel(t, instSpec{name: "Home", gtnh: true, version: "2.8.4", noCfg: true})

			cmd := m.editSetting(id)

			if cmd != nil || m.edit != nil {
				t.Errorf("editSetting(%s) with unreadable settings: cmd %v, edit %+v; want refused", id, cmd != nil, m.edit)
			}
		})
	}
}

// C1: unreadable settings don't refuse the settings kept elsewhere.
func TestC1UnreadableSettingsStillAllowServerModsAndPrism(t *testing.T) {
	for _, id := range []string{"server", "mods", "prism"} {
		t.Run(id, func(t *testing.T) {
			m, _, _ := seModel(t, instSpec{name: "Home", gtnh: true, version: "2.8.4", noCfg: true})

			m.editSetting(id)

			if m.edit == nil || m.edit.id != id {
				t.Errorf("editSetting(%s) with unreadable settings: edit %+v, want editing %s", id, m.edit, id)
			}
		})
	}
}

// C1
func TestC1UnreadableSettingsStillToggleAfter(t *testing.T) {
	m, f, _ := seModel(t, instSpec{name: "Home", gtnh: true, version: "2.8.4", noCfg: true})
	m.app.AfterPlay = appcfg.AfterPlayStay

	m.editSetting("after")

	if m.app.AfterPlay != appcfg.AfterPlayQuit || len(f.saved) != 1 {
		t.Errorf("after with unreadable settings: app %+v, saved %+v; want toggled to quit and saved", m.app, f.saved)
	}
}

// ---- C1: the "after" toggle ----

// C1/C4
func TestC1AfterTogglesStayToQuitAndSaves(t *testing.T) {
	m, f := oneFull(t)
	m.app.AfterPlay = appcfg.AfterPlayStay

	m.editSetting("after")

	if m.app.StaysOpen() || m.app.AfterPlay != appcfg.AfterPlayQuit {
		t.Errorf("app after toggle = %+v, want quit", m.app)
	}
	if len(f.saved) != 1 || f.saved[0].AfterPlay != appcfg.AfterPlayQuit {
		t.Errorf("saved = %+v, want one config with AfterPlay quit", f.saved)
	}
	if m.edit != nil || m.savedRow != "after" {
		t.Errorf("edit %+v savedRow %q, want no field and after marked saved", m.edit, m.savedRow)
	}
}

// C1/C4
func TestC1AfterTogglesQuitToStayAndSaves(t *testing.T) {
	m, f := oneFull(t)
	m.app.AfterPlay = appcfg.AfterPlayQuit

	m.editSetting("after")

	if !m.app.StaysOpen() || m.app.AfterPlay != appcfg.AfterPlayStay {
		t.Errorf("app after toggle = %+v, want stay", m.app)
	}
	if len(f.saved) != 1 || f.saved[0].AfterPlay != appcfg.AfterPlayStay {
		t.Errorf("saved = %+v, want one config with AfterPlay stay", f.saved)
	}
	if m.edit != nil || m.savedRow != "after" {
		t.Errorf("edit %+v savedRow %q, want no field and after marked saved", m.edit, m.savedRow)
	}
}

// C1
func TestC1AfterSaveErrorRestoresTheSettingAndSaysWhy(t *testing.T) {
	m, _ := oneFull(t)
	m.app.AfterPlay = appcfg.AfterPlayStay
	m.saveApp = func(appcfg.Config) error { return errors.New("the disk is full") }

	m.editSetting("after")

	if m.app.AfterPlay != appcfg.AfterPlayStay {
		t.Errorf("app after a failed save = %+v, want AfterPlay stay restored", m.app)
	}
	if m.dialog == nil || m.dialog.title != couldntSave {
		t.Fatalf("dialog = %+v, want %q", m.dialog, couldntSave)
	}
	if body := flat(m.dialog.body(60)); !strings.Contains(body, "the disk is full") {
		t.Errorf("dialog body %q lacks the error", body)
	}
}

// ---- C1: starting a field ----

// C1
func TestC1EditStartsAFieldPrefilledWithTheStoredValue(t *testing.T) {
	m, _ := oneFull(t) // memory overridden: 8192, at least 4096

	m.editSetting("memory")

	if m.edit == nil {
		t.Fatal("not editing after editSetting(memory)")
	}
	e := m.edit
	if e.id != "memory" || e.input.Value() != "8192" || e.err != "" {
		t.Errorf("edit = id %q value %q err %q, want memory, 8192, no error", e.id, e.input.Value(), e.err)
	}
	if e.input.Position() != 4 || !e.input.Focused() || e.input.Prompt != "" {
		t.Errorf("field cursor %d focused %v prompt %q, want cursor at end (4), focused, no prompt",
			e.input.Position(), e.input.Focused(), e.input.Prompt)
	}
	if e.input.Width != 78-2-15-2 {
		t.Errorf("field width = %d, want %d", e.input.Width, 78-2-15-2)
	}
}

// C1: the field takes the page, on the edited row.
func TestC1EditFocusesThePageOnTheEditedRow(t *testing.T) {
	m, _ := twoGTNH(t) // page 61 wide
	m.focus = focusSidebar

	m.editSetting("window")

	if m.edit == nil || m.focus != focusPage || m.row != indexOf(rowIDs(m), "window") {
		t.Fatalf("after editSetting(window): edit %+v focus %v row %d, want page on window", m.edit, m.focus, m.row)
	}
	if m.edit.input.Width != 61-2-15-2 {
		t.Errorf("field width = %d, want %d", m.edit.input.Width, 61-2-15-2)
	}
}

// C1: the field is at least 10 columns wide (page 28 wide: 28-19 = 9).
func TestC1FieldIsAtLeastTenColumns(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 30, 24, makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4"}))
	m.focus = focusPage

	m.editSetting("jvm")

	if m.edit == nil || m.edit.input.Width != 10 {
		t.Errorf("edit %+v, want a field 10 wide", m.edit)
	}
}

// C1: an unset setting starts empty.
func TestC1EditOfADefaultSettingStartsEmpty(t *testing.T) {
	m, _ := oneFull(t)

	m.editSetting("jvm")

	if m.edit == nil || m.edit.input.Value() != "" {
		t.Errorf("edit %+v, want an empty field", m.edit)
	}
}

// C1
func TestC1RawSettingValueIsTheStoredValue(t *testing.T) {
	set := homeInfo{
		server: "mc.example.net:25565", modsURL: "https://dl.example.com/pack/extra.zip",
		settings: prism.Settings{
			OverrideMemory: true, MinMemMB: 2048, MaxMemMB: 6144,
			OverrideJavaArgs: true, JvmArgs: "-XX:+UseZGC",
			OverrideJavaLocation: true, JavaPath: "/usr/lib/jvm/java-21/bin/java",
			OverrideWindow: true, WinWidth: 1920, WinHeight: 1080,
		},
	}
	off := homeInfo{settings: prism.Settings{
		MinMemMB: 2048, MaxMemMB: 6144, JvmArgs: "-XX:+UseZGC", JavaPath: "/usr/bin/java", WinWidth: 1920, WinHeight: 1080,
	}}
	app := appcfg.Config{PrismExe: "/opt/prism/prismlauncher"}
	cases := []struct {
		name, id string
		info     homeInfo
		app      appcfg.Config
		want     string
	}{
		{"memory set", "memory", set, app, "6144"},
		{"memory off", "memory", off, app, ""},
		{"jvm set", "jvm", set, app, "-XX:+UseZGC"},
		{"jvm off", "jvm", off, app, ""},
		{"java set", "java", set, app, "/usr/lib/jvm/java-21/bin/java"},
		{"java off", "java", off, app, ""},
		{"window set", "window", set, app, "1920x1080"},
		{"window off", "window", off, app, ""},
		{"server set", "server", set, app, "mc.example.net:25565"},
		{"server none", "server", off, app, ""},
		{"mods set", "mods", set, app, "https://dl.example.com/pack/extra.zip"},
		{"mods none", "mods", off, app, ""},
		{"prism set", "prism", off, app, "/opt/prism/prismlauncher"},
		{"prism auto", "prism", off, appcfg.Config{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rawSettingValue(c.id, c.info, c.app); got != c.want {
				t.Errorf("rawSettingValue(%s) = %q, want %q", c.id, got, c.want)
			}
		})
	}
}

// C1: with every setting stored at once, each id returns exactly its own value.
func TestC1RawSettingValueWithEverythingSetPicksEachOwnValue(t *testing.T) {
	info := homeInfo{
		server: "mc.example.net:25565", modsURL: "https://dl.example.com/pack/extra.zip",
		settings: prism.Settings{
			OverrideMemory: true, MinMemMB: 2048, MaxMemMB: 6144,
			OverrideJavaArgs: true, JvmArgs: "-XX:+UseZGC",
			OverrideJavaLocation: true, JavaPath: "/usr/lib/jvm/java-21/bin/java",
			OverrideWindow: true, WinWidth: 1920, WinHeight: 1080,
		},
	}
	app := appcfg.Config{PrismExe: "/opt/prism/prismlauncher"}
	cases := []struct{ id, want string }{
		{"memory", "6144"},
		{"jvm", "-XX:+UseZGC"},
		{"java", "/usr/lib/jvm/java-21/bin/java"},
		{"window", "1920x1080"},
		{"server", "mc.example.net:25565"},
		{"mods", "https://dl.example.com/pack/extra.zip"},
		{"prism", "/opt/prism/prismlauncher"},
		{"after", ""},
		{"unknown", ""},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			if got := rawSettingValue(c.id, info, app); got != c.want {
				t.Errorf("rawSettingValue(%s) = %q, want %q", c.id, got, c.want)
			}
		})
	}
}

// ---- C3: keys while editing ----

// C3
func TestC3EnterSavesTheField(t *testing.T) {
	m, _, in := seHome(t, "")
	m.editSetting("memory")
	m.edit.input.SetValue("6144")

	press(m, "enter")

	if m.edit != nil || m.savedRow != "memory" {
		t.Errorf("after enter: edit %+v savedRow %q, want saved", m.edit, m.savedRow)
	}
	if s := seSettings(t, in.Dir); !s.OverrideMemory || s.MaxMemMB != 6144 {
		t.Errorf("settings on disk = %+v, want memory 6144", s)
	}
}

// C3
func TestC3EscCancelsWithoutSaving(t *testing.T) {
	m, _, in := seHome(t, "OverrideMemory=true\nMinMemAlloc=4096\nMaxMemAlloc=8192\n")
	before := seCfg(t, in.Dir)
	m.editSetting("memory")
	m.edit.input.SetValue("2048")

	press(m, "esc")

	if m.edit != nil {
		t.Errorf("still editing after esc: %+v", m.edit)
	}
	if after := seCfg(t, in.Dir); after != before {
		t.Errorf("instance.cfg changed by esc:\n%q\nwas\n%q", after, before)
	}
}

// C3
func TestC3MoveKeysAreIgnoredWhileEditing(t *testing.T) {
	for _, k := range []string{"up", "down", "tab", "shift+tab", "left", "right"} {
		t.Run(k, func(t *testing.T) {
			m, _ := twoGTNH(t)
			m.focus = focusPage
			m.editSetting("memory")
			row, sel, pos := m.row, m.sel, m.edit.input.Position()

			press(m, k)

			if m.edit == nil || m.edit.id != "memory" {
				t.Fatalf("%s stopped editing", k)
			}
			if m.row != row || m.sel != sel || m.focus != focusPage {
				t.Errorf("%s moved: row %d→%d sel %d→%d focus %v", k, row, m.row, sel, m.sel, m.focus)
			}
			if m.edit.input.Value() != "8192" || m.edit.input.Position() != pos {
				t.Errorf("%s changed the field: %q cursor %d, want 8192 cursor %d", k, m.edit.input.Value(), m.edit.input.Position(), pos)
			}
		})
	}
}

// C3: q is typed, not quit.
func TestC3QIsTypedIntoTheFieldWhileEditing(t *testing.T) {
	m, _ := oneFull(t)
	m.editSetting("memory")

	cmd := press(m, "q")

	if m.quitting || hasQuit(runCmd(cmd)) {
		t.Errorf("q quit while editing")
	}
	if m.edit == nil || m.edit.input.Value() != "8192q" {
		t.Errorf("edit %+v, want field value 8192q", m.edit)
	}
}

// C3: letters bound on the page go to the field.
func TestC3LettersGoToTheFieldNotThePage(t *testing.T) {
	m, f := oneFull(t)
	m.editSetting("jvm")

	press(m, "p", "j", "a")

	if len(f.launches) != 0 || m.showAll {
		t.Errorf("page keys acted while editing: launches %+v showAll %v", f.launches, m.showAll)
	}
	if m.edit == nil || m.edit.input.Value() != "pja" {
		t.Errorf("edit %+v, want field value pja", m.edit)
	}
}

// C3
func TestC3StatusWhileEditingIsSaveAndCancel(t *testing.T) {
	m, _ := oneFull(t)
	m.editSetting("server")

	got := strings.Join(m.statusPairs(), ",")

	if got != "enter,save,esc,cancel" {
		t.Errorf("statusPairs while editing = %s", got)
	}
}

// C3: the save/cancel pairs are only for editing; after esc the page's pairs are back.
func TestC3StatusIsSaveAndCancelOnlyWhileEditing(t *testing.T) {
	m, _ := oneFull(t)
	m.row = indexOf(rowIDs(m), "server")
	m.editSetting("server")
	editing := m.statusPairs()

	press(m, "esc")
	after := m.statusPairs()

	if strings.Join(editing, ",") != "enter,save,esc,cancel" {
		t.Errorf("statusPairs while editing = %q", editing)
	}
	if m.edit != nil || len(after) < 4 || strings.Join(after[:4], ",") != "↑↓,move,enter,edit" {
		t.Errorf("statusPairs after esc = %q, want to start with ↑↓ move enter edit", after)
	}
}

// ---- C4: saving ----

// C4
func TestC4ARefusedValueKeepsEditingWithTheReason(t *testing.T) {
	m, _, in := seHome(t, "OverrideMemory=true\nMinMemAlloc=4096\nMaxMemAlloc=8192\n")
	before := seCfg(t, in.Dir)

	seSave(t, m, "memory", "abc")

	if m.edit == nil || m.edit.err != "Give me a whole number of MB between 1024 and 65536." {
		t.Fatalf("edit after a bad value = %+v, want still editing with the memory message", m.edit)
	}
	if after := seCfg(t, in.Dir); after != before {
		t.Errorf("instance.cfg changed by a refused value")
	}
}

// C4: a successful save ends editing, marks the row and shows the new value.
func TestC4ASuccessfulSaveMarksTheRowWithTheNewValue(t *testing.T) {
	m, _, _ := seHome(t, "")

	cmd := seSave(t, m, "memory", "6144")

	if cmd != nil || m.edit != nil || m.savedRow != "memory" {
		t.Errorf("after saving: cmd %v edit %+v savedRow %q, want nil, nil, memory", cmd != nil, m.edit, m.savedRow)
	}
	if got := pageLines(m, 78); !containsLine(got, "Memory         6144 MB (at least 1024 MB) ✓ saved") {
		t.Errorf("no saved memory row:\n%s", strings.Join(got, "\n"))
	}
}

// C4: the value is trimmed before it's checked and saved.
func TestC4TheValueIsTrimmed(t *testing.T) {
	m, _, in := seHome(t, "")

	seSave(t, m, "memory", "  6144  ")

	if s := seSettings(t, in.Dir); m.edit != nil || !s.OverrideMemory || s.MaxMemMB != 6144 {
		t.Errorf("edit %+v settings %+v, want memory 6144 saved", m.edit, s)
	}
}

// C4
func TestC4MemoryWithoutOverrideSetsMaxAndMinOf1024(t *testing.T) {
	m, _, in := seHome(t, "")

	seSave(t, m, "memory", "6144")

	if s := seSettings(t, in.Dir); !s.OverrideMemory || s.MaxMemMB != 6144 || s.MinMemMB != 1024 {
		t.Errorf("settings = %+v, want override on, max 6144, min 1024", s)
	}
}

// C4, kills K1: lowering the maximum below the old minimum lowers the minimum too.
func TestC4MemoryLoweredBelowTheMinimumLowersTheMinimum(t *testing.T) {
	m, _, in := seHome(t, "OverrideMemory=true\nMinMemAlloc=4096\nMaxMemAlloc=8192\n")

	seSave(t, m, "memory", "2048")

	if s := seSettings(t, in.Dir); !s.OverrideMemory || s.MaxMemMB != 2048 || s.MinMemMB != 2048 {
		t.Errorf("settings = %+v, want max 2048, min 2048", s)
	}
}

// C4: raising the maximum keeps the old minimum.
func TestC4MemoryRaisedKeepsTheMinimum(t *testing.T) {
	m, _, in := seHome(t, "OverrideMemory=true\nMinMemAlloc=4096\nMaxMemAlloc=8192\n")

	seSave(t, m, "memory", "12288")

	if s := seSettings(t, in.Dir); !s.OverrideMemory || s.MaxMemMB != 12288 || s.MinMemMB != 4096 {
		t.Errorf("settings = %+v, want max 12288, min 4096", s)
	}
}

// C4
func TestC4MemoryClearedTurnsTheOverrideOffAndKeepsTheNumbers(t *testing.T) {
	m, _, in := seHome(t, "OverrideMemory=true\nMinMemAlloc=4096\nMaxMemAlloc=8192\n")

	seSave(t, m, "memory", "")

	if s := seSettings(t, in.Dir); s.OverrideMemory || s.MaxMemMB != 8192 || s.MinMemMB != 4096 {
		t.Errorf("settings = %+v, want override off, max 8192, min 4096", s)
	}
}

// C4
func TestC4JvmArgsAreSaved(t *testing.T) {
	m, _, in := seHome(t, "")

	seSave(t, m, "jvm", "-Xss2m")

	if s := seSettings(t, in.Dir); !s.OverrideJavaArgs || s.JvmArgs != "-Xss2m" {
		t.Errorf("settings = %+v, want jvm -Xss2m", s)
	}
}

// C4
func TestC4JvmArgsClearedTurnTheOverrideOffAndKeepTheArgs(t *testing.T) {
	m, _, in := seHome(t, "OverrideJavaArgs=true\nJvmArgs=-Xss4m\n")

	seSave(t, m, "jvm", "")

	if s := seSettings(t, in.Dir); s.OverrideJavaArgs || s.JvmArgs != "-Xss4m" {
		t.Errorf("settings = %+v, want override off, args -Xss4m kept", s)
	}
}

// C4
func TestC4JavaPathIsSaved(t *testing.T) {
	m, _, in := seHome(t, "")
	java := seFile(t, "java")

	seSave(t, m, "java", java)

	if s := seSettings(t, in.Dir); !s.OverrideJavaLocation || s.JavaPath != java {
		t.Errorf("settings = %+v, want java %q", s, java)
	}
}

// C4
func TestC4JavaPathClearedTurnsTheOverrideOffAndKeepsThePath(t *testing.T) {
	m, _, in := seHome(t, "OverrideJavaLocation=true\nJavaPath=/opt/java/bin/java\n")

	seSave(t, m, "java", "")

	if s := seSettings(t, in.Dir); s.OverrideJavaLocation || s.JavaPath != "/opt/java/bin/java" {
		t.Errorf("settings = %+v, want override off, path kept", s)
	}
}

// C4
func TestC4WindowSizeIsSaved(t *testing.T) {
	m, _, in := seHome(t, "")

	seSave(t, m, "window", "1920x1080")

	if s := seSettings(t, in.Dir); !s.OverrideWindow || s.WinWidth != 1920 || s.WinHeight != 1080 {
		t.Errorf("settings = %+v, want window 1920x1080", s)
	}
}

// C4
func TestC4WindowSizeWithTheTimesSignIsSaved(t *testing.T) {
	m, _, in := seHome(t, "")

	seSave(t, m, "window", "1280×720")

	if s := seSettings(t, in.Dir); !s.OverrideWindow || s.WinWidth != 1280 || s.WinHeight != 720 {
		t.Errorf("settings = %+v, want window 1280x720", s)
	}
}

// C4
func TestC4WindowClearedTurnsTheOverrideOffAndKeepsTheSize(t *testing.T) {
	m, _, in := seHome(t, "OverrideWindow=true\nMinecraftWinWidth=1920\nMinecraftWinHeight=1080\n")

	seSave(t, m, "window", "")

	if s := seSettings(t, in.Dir); s.OverrideWindow || s.WinWidth != 1920 || s.WinHeight != 1080 {
		t.Errorf("settings = %+v, want override off, 1920x1080 kept", s)
	}
}

// C4
func TestC4ServerIsSaved(t *testing.T) {
	m, _, in := seHome(t, "")

	seSave(t, m, "server", "play.example.com:25565")

	if st := seState(t, in.Dir); st.ServerAddress != "play.example.com:25565" {
		t.Errorf("ServerAddress = %q, want play.example.com:25565", st.ServerAddress)
	}
}

// C4
func TestC4ServerCleared(t *testing.T) {
	m, _, in := seModel(t, instSpec{name: "Home", gtnh: true, version: "2.8.4", server: "old.example.org"})

	seSave(t, m, "server", "")

	if st := seState(t, in.Dir); st.ServerAddress != "" {
		t.Errorf("ServerAddress = %q, want none", st.ServerAddress)
	}
}

// C4
func TestC4ModsLinkIsSavedAndMarkedAsked(t *testing.T) {
	m, _, in := seHome(t, "")

	seSave(t, m, "mods", "https://dl.example.com/extra.zip")

	if st := seState(t, in.Dir); st.CustomModsURL != "https://dl.example.com/extra.zip" || !st.CustomModsAsked {
		t.Errorf("state = url %q asked %v, want the link and asked", st.CustomModsURL, st.CustomModsAsked)
	}
}

// C4
func TestC4ModsClearedIsNoneButAsked(t *testing.T) {
	m, _, in := seModel(t, instSpec{name: "Home", gtnh: true, version: "2.8.4", mods: "https://old.example.org/m.zip"})

	seSave(t, m, "mods", "")

	if st := seState(t, in.Dir); st.CustomModsURL != "" || !st.CustomModsAsked {
		t.Errorf("state = url %q asked %v, want none and asked", st.CustomModsURL, st.CustomModsAsked)
	}
}

// C4
func TestC4PrismLocationIsSavedToTheAppSettings(t *testing.T) {
	m, f, _ := seHome(t, "")
	exe := seFile(t, "prismlauncher")

	seSave(t, m, "prism", exe)

	if len(f.saved) != 1 || f.saved[0].PrismExe != exe || m.app.PrismExe != exe {
		t.Errorf("saved %+v app %+v, want PrismExe %q", f.saved, m.app, exe)
	}
}

// C4
func TestC4PrismLocationCleared(t *testing.T) {
	m, f, _ := seHome(t, "")
	m.app.PrismExe = "/opt/prism/prismlauncher"

	seSave(t, m, "prism", "")

	if len(f.saved) != 1 || f.saved[0].PrismExe != "" || m.app.PrismExe != "" {
		t.Errorf("saved %+v app %+v, want PrismExe empty", f.saved, m.app)
	}
}

// C4
func TestC4ASaveErrorEndsEditingAndSaysWhy(t *testing.T) {
	m, _, _ := seHome(t, "")
	m.saveApp = func(appcfg.Config) error { return errors.New("the disk is full") }
	exe := seFile(t, "prismlauncher")

	seSave(t, m, "prism", exe)

	if m.edit != nil {
		t.Errorf("still editing after a failed save: %+v", m.edit)
	}
	if m.dialog == nil || m.dialog.title != couldntSave {
		t.Fatalf("dialog = %+v, want %q", m.dialog, couldntSave)
	}
	if body := flat(m.dialog.body(60)); !strings.Contains(body, "the disk is full") {
		t.Errorf("dialog body %q lacks the error", body)
	}
}

// ---- C5: the saved marker ----

// C5
func TestC5TheNextKeyClearsTheSavedMarker(t *testing.T) {
	m, _, _ := seHome(t, "")
	seSave(t, m, "memory", "6144")

	press(m, "down")
	got := pageLines(m, 78)

	if m.savedRow != "" || containsLine(got, "✓ saved") {
		t.Errorf("savedRow %q, marker still on the page:\n%s", m.savedRow, strings.Join(got, "\n"))
	}
	if !containsLine(got, "Memory         6144 MB (at least 1024 MB)") {
		t.Errorf("memory row lost its value:\n%s", strings.Join(got, "\n"))
	}
}

// C5
func TestC5EditingAnotherSettingClearsTheSavedMarker(t *testing.T) {
	m, _, _ := seHome(t, "")
	seSave(t, m, "memory", "6144")

	m.editSetting("jvm")

	if m.savedRow != "" {
		t.Errorf("savedRow = %q after editing jvm, want none", m.savedRow)
	}
}

// ---- C6: checkSetting ----

// C6
func TestC6CheckSettingMessages(t *testing.T) {
	const (
		badServer = "That doesn't look like a server address — try play.example.com or play.example.com:25565."
		badMods   = "That doesn't look like a download link — it should start with https://"
		badMemory = "Give me a whole number of MB between 1024 and 65536."
		badFile   = "I can't find a file there."
		badWindow = "Give me width and height like 1920x1080."
	)
	file := seFile(t, "java")
	dir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "nothing-here")
	cases := []struct {
		id, v, want string
	}{
		{"memory", "", ""}, {"jvm", "", ""}, {"java", "", ""}, {"window", "", ""},
		{"server", "", ""}, {"mods", "", ""}, {"prism", "", ""},

		{"memory", "1023", badMemory}, {"memory", "1024", ""}, {"memory", "65536", ""},
		{"memory", "65537", badMemory}, {"memory", "abc", badMemory}, {"memory", "6144.5", badMemory},

		{"window", "319x240", badWindow}, {"window", "320x320", ""}, {"window", "16384x16384", ""},
		{"window", "16385x100", badWindow}, {"window", "1920×1080", ""}, {"window", "1920", badWindow},
		{"window", "1920x1080x2", badWindow}, {"window", "320x319", badWindow}, {"window", "320x16385", badWindow},

		{"server", "host:0", badServer}, {"server", "host:65535", ""}, {"server", "host:65536", badServer},
		{"server", "a b", badServer}, {"server", "http://x", badServer}, {"server", "play.example.com", ""},
		{"server", ":25565", badServer}, {"server", "host:abc", badServer}, {"server", "host:1", ""},
		{"server", "host:25565:1", badServer},

		{"mods", "http://x", badMods}, {"mods", "https://dl.example.com/extra.zip", ""}, {"mods", "ftp://x", badMods},

		{"java", file, ""}, {"java", dir, badFile}, {"java", missing, badFile},
		{"prism", file, ""}, {"prism", dir, badFile}, {"prism", missing, badFile},
	}
	for _, c := range cases {
		t.Run(c.id+"="+c.v, func(t *testing.T) {
			if got := checkSetting(c.id, c.v); got != c.want {
				t.Errorf("checkSetting(%s, %q) = %q, want %q", c.id, c.v, got, c.want)
			}
		})
	}
}

// ---- C7: instance.cfg keeps its other lines and line endings ----

// C7
func TestC7SavingKeepsOtherLinesAndCRLFEndings(t *testing.T) {
	const crlf = "[General]\r\nname=GTNH Pack\r\nMyCustomKey=hello\r\nOverrideMemory=false\r\nMinMemAlloc=1024\r\nMaxMemAlloc=4096\r\n"
	java := seFile(t, "java")
	cases := []struct{ id, v string }{
		{"memory", "6144"}, {"jvm", "-Xss2m"}, {"java", java}, {"window", "1920x1080"},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			root := t.TempDir()
			in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4"})
			writeFile(t, filepath.Join(in.Dir, "instance.cfg"), crlf)
			m, _ := loadedModel(root, 80, 24, in)
			m.focus = focusPage

			seSave(t, m, c.id, c.v)
			got := seCfg(t, in.Dir)

			if m.edit != nil || m.savedRow != c.id {
				t.Fatalf("%s=%q wasn't saved: edit %+v savedRow %q", c.id, c.v, m.edit, m.savedRow)
			}
			if !strings.Contains(got, "MyCustomKey=hello\r\n") || !strings.Contains(got, "name=GTNH Pack\r\n") {
				t.Errorf("other lines changed:\n%q", got)
			}
			if strings.Count(got, "\n") != strings.Count(got, "\r\n") {
				t.Errorf("a line lost its CRLF ending:\n%q", got)
			}
		})
	}
}

// ---- C2: the edited row on screen ----

// C2: at 80x24 the view fits while editing and shows the field, its help and, after a
// refused value, the reason.
func TestC2EditingRendersTheFieldHelpAndErrorAt80x24(t *testing.T) {
	m, _ := oneFull(t)

	m.editSetting("memory")
	editing := strings.Split(m.View(), "\n")
	plain := screen(m)
	m.edit.input.SetValue("abc")
	press(m, "enter")
	refused := screen(m)

	if len(editing) != 24 {
		t.Fatalf("view has %d lines, want 24", len(editing))
	}
	for i, l := range editing {
		if w := ansi.StringWidth(l); w > 80 {
			t.Errorf("line %d is %d columns", i+1, w)
		}
	}
	field := lineWith(plain, "▸ Memory")
	if !strings.HasPrefix(strings.TrimLeft(field, " "), "▸ Memory         8192") {
		t.Errorf("field line = %q, want ▸ Memory and the value 8192", field)
	}
	if !containsLine(plain, "How much memory the game may use") {
		t.Errorf("no help line:\n%s", strings.Join(plain, "\n"))
	}
	if !containsLine(refused, "Give me a whole number of MB between 1024 and 65536.") {
		t.Errorf("no error line after a refused value:\n%s", strings.Join(refused, "\n"))
	}
}
