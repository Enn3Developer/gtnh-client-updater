package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// Characterization tests: they pin what tui.go does today so a behaviour-preserving
// refactor can be checked against them. Expected values were recorded from the code.

// ---- C1 golden page bodies ----

// goldenWarns are the two warnings every golden screen carries.
var goldenWarns = []string{
	"one mod looked edited by hand",
	"a second, much longer warning that goes on and on so that it has to wrap onto another line of the terminal",
}

// goldenInstancesDir stands in for the OS-specific instances path in goldens.
const goldenInstancesDir = "{INSTANCES}"

// goldenModel is an 82x25 model with fixed, path-free data: two warnings, the routing
// manifest, an instance "Pack" known to be on 2.8.4.
func goldenModel(t *testing.T) *model {
	t.Helper()
	m := newModel(Config{AppVersion: "1.2.3", PrismDirs: []string{"prism"}})
	m.Update(tea.WindowSizeMsg{Width: termW, Height: termH})
	m.manifest = routeManifest(t)
	m.inst = prism.Instance{Dir: "pack-dir", Name: "Pack", GTNH: true}
	m.insts = []prism.Instance{m.inst}
	m.detect = update.Detection{Version: "2.8.4"}
	m.serverModsAsked = true
	m.warns = append([]string{}, goldenWarns...)
	return m
}

// goldenCreateModel is goldenModel in the create flow, making "My Pack" from 2.8.4.
func goldenCreateModel(t *testing.T) *model {
	t.Helper()
	m := goldenModel(t)
	m.creating, m.inst = true, prism.Instance{}
	m.target, m.newName = "2.8.4", "My Pack"
	return m
}

// mixedPlan has 3 installs, 2 removes, 4 conflicts (2 new, 2 mine), a kept config and
// two hand-added mods.
func mixedPlan(match float64) *update.Plan {
	pl := &update.Plan{BaselineMatch: match}
	for _, p := range []string{".minecraft/mods/a.jar", ".minecraft/mods/b.jar", "patches/c.json"} {
		pl.Actions = append(pl.Actions, update.Action{Kind: update.Install, Path: p})
	}
	for _, p := range []string{".minecraft/mods/old1.jar", ".minecraft/mods/old2.jar"} {
		pl.Actions = append(pl.Actions, update.Action{Kind: update.Remove, Path: p})
	}
	for _, p := range []string{".minecraft/config/w.cfg", ".minecraft/config/x.cfg", ".minecraft/config/y.cfg", ".minecraft/config/z.cfg"} {
		pl.Actions = append(pl.Actions, update.Action{Kind: update.Conflict, Path: p})
	}
	pl.Choose(".minecraft/config/w.cfg", update.TakeNew)
	pl.Choose(".minecraft/config/x.cfg", update.TakeNew)
	pl.Choose(".minecraft/config/y.cfg", update.KeepMine)
	pl.Choose(".minecraft/config/z.cfg", update.KeepMine)
	pl.Kept = []string{".minecraft/config/kept.cfg"}
	pl.ExtraMods = []string{"handmade-1.0.jar", "other-2.0.jar"}
	return pl
}

// onePlan has one of each: install, conflict taken new, conflict kept.
func onePlan() *update.Plan {
	pl := &update.Plan{BaselineMatch: 1}
	pl.Actions = []update.Action{
		{Kind: update.Install, Path: ".minecraft/mods/a.jar"},
		{Kind: update.Conflict, Path: ".minecraft/config/n.cfg"},
		{Kind: update.Conflict, Path: ".minecraft/config/m.cfg"},
	}
	pl.Choose(".minecraft/config/n.cfg", update.TakeNew)
	pl.Choose(".minecraft/config/m.cfg", update.KeepMine)
	pl.Kept = []string{".minecraft/config/kept.cfg"}
	return pl
}

func goldenErrorModel(t *testing.T, creating bool, phase screen, err error) *model {
	t.Helper()
	var m *model
	if creating {
		m = goldenCreateModel(t)
	} else {
		m = goldenModel(t)
		m.target = "2.9.0-RC-1"
	}
	m.screen, m.errPhase, m.err = scError, phase, err
	return m
}

// rolledBackErr is an apply error that wraps update.ErrRolledBack but keeps its own text.
type rolledBackErr struct{ msg string }

func (e rolledBackErr) Error() string { return e.msg }
func (e rolledBackErr) Unwrap() error { return update.ErrRolledBack }

var leftoverErr = errors.Join(errors.New("disk full"), &update.LeftoverError{Dir: "/x/My Pack", Err: errors.New("access denied")})

type goldenCase struct {
	name  string
	build func(t *testing.T) *model
}

func goldenCases() []goldenCase {
	return []goldenCase{
		{"loading", func(t *testing.T) *model {
			m := goldenModel(t)
			m.screen = scLoading
			return m
		}},
		{"server mods empty", func(t *testing.T) *model {
			m := goldenModel(t)
			m.askServerMods(true)
			return m
		}},
		{"server mods bad link", func(t *testing.T) *model {
			m := goldenModel(t)
			m.askServerMods(false)
			m.input.SetValue("http://x")
			press(m, keyEnter)
			return m
		}},
		{"server mods create", func(t *testing.T) *model {
			m := goldenCreateModel(t)
			m.serverMods = "https://example.com/mods.zip"
			m.askServerMods(true)
			return m
		}},
		{"name", func(t *testing.T) *model {
			m := goldenCreateModel(t)
			m.askName()
			return m
		}},
		{"name with error", func(t *testing.T) *model {
			m := goldenCreateModel(t)
			m.askName()
			m.nameEr = "That name is already taken by another instance in Prism, so please pick a different one."
			return m
		}},
		{"preparing update", func(t *testing.T) *model {
			m := goldenModel(t)
			m.screen, m.target = scPreparing, "2.9.0-RC-1"
			m.steps = []string{"Checking which version you have", "Reading what came with 2.8.4, which is a pretty long step description that wraps"}
			m.step, m.stepStart, m.done, m.total = "Comparing files", time.Now(), 4000, 16000
			return m
		}},
		{"preparing create", func(t *testing.T) *model {
			m := goldenCreateModel(t)
			m.screen = scPreparing
			m.step, m.stepStart, m.done, m.total = "Downloading GTNH 2.8.4", time.Now(), 363_500_000, 727_000_000
			return m
		}},
		{"preparing create cancelling", func(t *testing.T) *model {
			m := goldenCreateModel(t)
			m.screen, m.quitAfterCancel = scPreparing, true
			m.step = "Downloading GTNH 2.8.4"
			return m
		}},
		{"applying update", func(t *testing.T) *model {
			m := goldenModel(t)
			m.screen, m.target = scApplying, "2.9.0-RC-1"
			m.steps = []string{"Backing up"}
			m.step = "Writing files"
			return m
		}},
		{"applying create", func(t *testing.T) *model {
			m := goldenCreateModel(t)
			m.screen = scApplying
			m.step, m.stepStart, m.done, m.total = "Writing files", time.Now(), 1500, 16000
			return m
		}},
		{"self update", func(t *testing.T) *model {
			m := goldenModel(t)
			m.newer = &selfupdate.Release{Version: "9.9.9"}
			m.screen = scSelfUpdate
			m.step, m.stepStart, m.done, m.total = "Downloading gtnh-update 9.9.9", time.Now(), 2_000_000, 8_000_000
			return m
		}},
		{"confirm update", func(t *testing.T) *model {
			m := goldenModel(t)
			m.screen, m.target = scConfirm, "2.9.0-RC-1"
			m.serverMods = "https://mods.example.com/server/custom_mods.zip"
			m.session = &update.Session{Plan: mixedPlan(0.5), Flavor: manifest.Java8}
			return m
		}},
		{"confirm downgrade nothing to do", func(t *testing.T) *model {
			m := goldenModel(t)
			m.screen, m.target = scConfirm, "2.8.1"
			m.session = &update.Session{Plan: &update.Plan{BaselineMatch: 1}, Flavor: manifest.Java17}
			return m
		}},
		{"confirm refresh singular", func(t *testing.T) *model {
			m := goldenModel(t)
			m.screen, m.target = scConfirm, "2.8.4"
			m.session = &update.Session{Plan: onePlan(), Flavor: manifest.Java17}
			return m
		}},
		{"confirm create", func(t *testing.T) *model {
			m := goldenCreateModel(t)
			m.screen = scConfirm
			m.serverMods = "https://mods.example.com/server/custom_mods.zip"
			m.creation = &update.Creation{Dir: "/x/My Pack", Files: 16234, Flavor: manifest.Java17}
			return m
		}},
		{"confirm create java 8", func(t *testing.T) *model {
			m := goldenCreateModel(t)
			m.screen = scConfirm
			m.creation = &update.Creation{Dir: "/x/My Pack", Files: 999, Flavor: manifest.Java8}
			return m
		}},
		{"done update", func(t *testing.T) *model {
			m := goldenModel(t)
			m.screen, m.target = scDone, "2.9.0-RC-1"
			m.serverMods = "https://mods.example.com/server/custom_mods.zip"
			m.session = &update.Session{Plan: mixedPlan(1)}
			m.result = &update.Result{
				From: "2.8.4", To: "2.9.0-RC-1", Renamed: "GTNH 2.9.0-RC-1",
				BackupDir: "/home/player/instances/Pack/.gtnh-updater/backup-20260101-120000",
				CustomMods: &update.CustomModsResult{
					Installed: []string{"one.jar", "two.jar"},
					Added:     []string{"two.jar"},
					Removed:   []string{"gone.jar"},
					Skipped:   []string{"NotEnoughItems-2.6.0.jar", "journeymap-5.2.6.jar"},
				},
				CustomErr: errors.New("the server answered with an error page"),
			}
			return m
		}},
		{"done update singular removed old server mods", func(t *testing.T) *model {
			m := goldenModel(t)
			m.screen, m.target = scDone, "2.8.4"
			m.session = &update.Session{Plan: onePlan()}
			m.result = &update.Result{
				From: "2.8.4", To: "2.8.4",
				CustomMods: &update.CustomModsResult{Removed: []string{"gone.jar"}},
			}
			return m
		}},
		{"done update nothing changed", func(t *testing.T) *model {
			m := goldenModel(t)
			m.screen, m.target = scDone, "2.8.4"
			m.serverMods = "https://mods.example.com/m.zip"
			m.session = &update.Session{Plan: &update.Plan{}}
			m.result = &update.Result{
				From: "2.8.4", To: "2.8.4",
				CustomMods: &update.CustomModsResult{Installed: []string{"one.jar"}},
			}
			return m
		}},
		{"done create", func(t *testing.T) *model {
			m := goldenCreateModel(t)
			m.screen = scDone
			m.serverMods = "https://mods.example.com/m.zip"
			m.created = &update.CreateResult{
				Instance:   prism.Instance{Name: "My Pack"},
				Files:      16234,
				CustomMods: &update.CustomModsResult{Skipped: []string{"dup.jar"}},
				CustomErr:  errors.New("timeout"),
			}
			return m
		}},
		{"error update preparing", func(t *testing.T) *model {
			return goldenErrorModel(t, false, scPreparing, errors.New("I couldn't download the pack."))
		}},
		{"error update applying rolled back", func(t *testing.T) *model {
			return goldenErrorModel(t, false, scApplying, rolledBackErr{"writing a file failed; everything was rolled back"})
		}},
		{"error update applying says rolled back without ErrRolledBack", func(t *testing.T) *model {
			return goldenErrorModel(t, false, scApplying, errors.New("writing a file failed; everything was rolled back"))
		}},
		{"error update applying", func(t *testing.T) *model {
			return goldenErrorModel(t, false, scApplying, errors.New("writing a file failed and the rollback failed too"))
		}},
		{"error loading", func(t *testing.T) *model {
			m := goldenErrorModel(t, false, scLoading, errors.New("I couldn't find Prism Launcher on this computer."))
			m.manifest = nil
			return m
		}},
		{"error create preparing", func(t *testing.T) *model {
			return goldenErrorModel(t, true, scPreparing, errors.New("I couldn't download the pack."))
		}},
		{"error create applying", func(t *testing.T) *model {
			return goldenErrorModel(t, true, scApplying, errors.New("writing a file failed"))
		}},
		{"error create preparing leftover", func(t *testing.T) *model {
			return goldenErrorModel(t, true, scPreparing, leftoverErr)
		}},
		{"error create applying leftover", func(t *testing.T) *model {
			return goldenErrorModel(t, true, scApplying, leftoverErr)
		}},
		{"self updated", func(t *testing.T) *model {
			m := goldenModel(t)
			m.newer = &selfupdate.Release{Version: "9.9.9"}
			m.screen = scSelfUpdated
			return m
		}},
	}
}

// cleanLines strips ANSI escapes and trailing spaces from every line, keeping newlines.
func cleanLines(s string) string {
	lines := strings.Split(ansi.Strip(s), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

// pageText renders m.page() as plain text with section markers.
func pageText(m *model) string {
	body, footer, scroll := m.page()
	return fmt.Sprintf("[body]\n%s\n[footer]\n%s\n[scroll %d]", cleanLines(body), cleanLines(footer), scroll)
}

// expectedGolden fills in what depends on the OS: the instances path.
func expectedGolden(g string) string {
	return strings.ReplaceAll(g, goldenInstancesDir, filepath.Join("prism", "instances"))
}

// firstDiff describes the first differing line of two texts.
func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(w), len(g)) {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl || i >= len(w) || i >= len(g) {
			return fmt.Sprintf("line %d: got %q, want %q (got %d lines, want %d)", i+1, gl, wl, len(g), len(w))
		}
	}
	return "no difference"
}

func TestC1PageGoldens(t *testing.T) {
	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			want, ok := pageGoldens[c.name]
			if !ok {
				t.Fatalf("no golden for %q", c.name)
			}
			want = expectedGolden(want)
			got := pageText(c.build(t))
			if got != want {
				t.Errorf("page %q differs from golden at %s\n--- got ---\n%s", c.name, firstDiff(want, got), got)
			}
		})
	}
}

// pageGoldens were recorded from the code at 82x25 (goldenModel) and re-recorded by hand
// for the chrome spec (no header section, footers without a leading blank line, C9
// labels, no "Press p…" sentence on the done screens). Lines are ANSI-stripped with
// trailing spaces trimmed; {INSTANCES} is the instances path.
//
// Intended behaviour (spec): the counts sentence uses plural() for the install count
// ("1 file will be updated", "1 file updated"), and the bullet after the "couldn't be
// synced" warning starts at the normal bullet indentation.
var pageGoldens = map[string]string{
	"loading": `[body]
⣾  Looking for your GTNH instances…
[footer]

[scroll 2147483647]`,
	"server mods empty": `[body]
Does your server have its own extra mods?

Some servers add a few mods on top of GTNH. If the server owner gave you a
link for them, paste it here — I'll install them now and keep them in sync
every time you update.

> https://…/custom_mods.zip

No link? Leave it empty — you can add one later with m on the version list.
[footer]
enter continue    esc back
[scroll 0]`,
	"server mods bad link": `[body]
Does your server have its own extra mods?

Some servers add a few mods on top of GTNH. If the server owner gave you a
link for them, paste it here — I'll install them now and keep them in sync
every time you update.

> http://x
That doesn't look like a download link — it should start with https://

No link? Leave it empty — you can add one later with m on the version list.
[footer]
enter continue    esc back
[scroll 0]`,
	"server mods create": `[body]
Does your server have its own extra mods?

Some servers add a few mods on top of GTNH. If the server owner gave you a
link for them, paste it here — I'll install them now and keep them in sync
every time you update.

> https://example.com/mods.zip

No link? Leave it empty — you can add one later with m on the version list.
[footer]
enter continue    esc back
[scroll 0]`,
	"name": `[body]
What should the new instance be called?

That's the name you'll see in Prism; it's also the folder name.

> GT New Horizons 2.8.4

It'll be created in {INSTANCES}
[footer]
enter continue    esc back
[scroll 0]`,
	"name with error": `[body]
What should the new instance be called?

That's the name you'll see in Prism; it's also the folder name.

> GT New Horizons 2.8.4
That name is already taken by another instance in Prism, so please pick a
different one.

It'll be created in {INSTANCES}
[footer]
enter continue    esc back
[scroll 0]`,
	"preparing update": `[body]
Getting GTNH 2.9.0-RC-1 ready for Pack

  ✓ Checking which version you have
  ✓ Reading what came with 2.8.4, which is a pretty long step description that
    wraps
  ⣾ Comparing files

    ███████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  25%
    4,000 of 16,000 files

  Heads up: one mod looked edited by hand
  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
ctrl+c cancel
[scroll 2147483647]`,
	"preparing create": `[body]
Getting GTNH 2.8.4 ready

  ⣾ Downloading GTNH 2.8.4

    ██████████████████████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  50%
    363 MB of 727 MB

  Heads up: one mod looked edited by hand
  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
ctrl+c cancel
[scroll 2147483647]`,
	"preparing create cancelling": `[body]
Getting GTNH 2.8.4 ready

  ⣾ Downloading GTNH 2.8.4

  Heads up: one mod looked edited by hand
  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
Stopping and cleaning up…
[scroll 2147483647]`,
	"applying update": `[body]
Updating Pack to GTNH 2.9.0-RC-1

  ✓ Backing up
  ⣾ Writing files

  Heads up: one mod looked edited by hand
  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
  Please don't close this window until I'm done.
[scroll 2147483647]`,
	"applying create": `[body]
Creating My Pack

  ⣾ Writing files

    ██████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░   9%
    1,500 of 16,000 files

  Heads up: one mod looked edited by hand
  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
  Please don't close this window until I'm done.
[scroll 2147483647]`,
	"self update": `[body]
Updating gtnh-update itself

  ⣾ Downloading gtnh-update 9.9.9

    ███████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  25%
    2 MB of 8 MB

  Heads up: one mod looked edited by hand
  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
  Please don't close this window until I'm done.
[scroll 2147483647]`,
	"confirm update": `[body]
Ready to update Pack from 2.8.4 to 2.9.0-RC-1

! Only 50% of the mods that come with 2.8.4 are in this instance, so it's
probably not on 2.8.4. Press esc, then i to tell me the right version —
otherwise old mods could be left behind.

  • 3 files will be updated and 2 removed.
  • Your worlds, screenshots, maps and game settings stay exactly as they are.
  • 1 config file you changed will be kept as you have it.
  • 2 config files you changed get the new version. Your old ones go to the
    backup folder.
  • 2 config files you changed stay as you have them; the new ones are saved
    next to them with .mcnew at the end.
  • Mods you added yourself stay: handmade-1.0.jar, other-2.0.jar. Make sure
    they work with 2.9.0-RC-1.
  • Your server's extra mods will be synced from mods.example.com.
  • This instance uses the Java 8 version of the pack, so that's what you'll
    get.
  • Everything that gets replaced is backed up first, just in case.

  Heads up: one mod looked edited by hand

  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
enter update now    esc back
[scroll 0]`,
	"confirm downgrade nothing to do": `[body]
Ready to update Pack from 2.8.4 to 2.8.1

! This goes BACK to an older version. Worlds you played on 2.8.4 may lose
blocks and items or not load at all. Copy your saves folder somewhere safe
first.

  • Your GTNH files are already exactly as they should be.
  • Your worlds, screenshots, maps and game settings stay exactly as they are.
  • Everything that gets replaced is backed up first, just in case.

  Heads up: one mod looked edited by hand

  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
enter update now    esc back
[scroll 0]`,
	"confirm refresh singular": `[body]
Ready to refresh Pack on GTNH 2.8.4

  • 1 file will be updated and 0 removed.
  • Your worlds, screenshots, maps and game settings stay exactly as they are.
  • 1 config file you changed will be kept as you have it.
  • 1 config file you changed gets the new version. Your old one goes to the
    backup folder.
  • 1 config file you changed stays as you have it; the new one is saved next
    to it with .mcnew at the end.
  • Everything that gets replaced is backed up first, just in case.

  Heads up: one mod looked edited by hand

  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
enter update now    esc back
[scroll 0]`,
	"confirm create": `[body]
Ready to create My Pack with GTNH 2.8.4

  • It'll be a new instance in Prism, in /x/My Pack.
  • 16,234 files will be installed.
  • Your other instances aren't touched.
  • Your server's extra mods will be installed from mods.example.com.
  • It uses the Java 17+ version of the pack.

  Heads up: one mod looked edited by hand

  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
enter create it    esc back
[scroll 0]`,
	"confirm create java 8": `[body]
Ready to create My Pack with GTNH 2.8.4

  • It'll be a new instance in Prism, in /x/My Pack.
  • 999 files will be installed.
  • Your other instances aren't touched.
  • This version only comes as a Java 8 pack, so that's what you'll get.

  Heads up: one mod looked edited by hand

  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
enter create it    esc back
[scroll 0]`,
	"done update": `[body]
All done! Pack is now on GTNH 2.9.0-RC-1.

  • 3 files updated, 2 removed.
  • Renamed the instance in Prism to "GTNH 2.9.0-RC-1".
  • 2 extra mods from your server installed (1 new or updated, 1 removed).
  • Skipped NotEnoughItems-2.6.0.jar, journeymap-5.2.6.jar — GTNH already
    ships a mod with that name.
  • Your server's extra mods couldn't be synced this time (the server answered
    with an error page). Run me again later to retry.
  • 2 config files you had changed were replaced with the new version; the old
    ones are in the backup folder.

2 config files were changed on your side and in the new version. I kept yours
and saved the new ones next to them as .mcnew. It's usually fine to ignore
this — many mods rewrite their own config when the game starts.
    config/y.cfg
    config/z.cfg

If something's wrong, the old files are in /home/player/instances/Pack/.gtnh-
updater/backup-20260101-120000

  Heads up: one mod looked edited by hand
  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
enter back    p play now    q quit
[scroll 0]`,
	"done update singular removed old server mods": `[body]
All done! Pack is now on GTNH 2.8.4.

  • 1 file updated, 0 removed.
  • Removed 1 extra mod from your old server.
  • 1 config file you had changed was replaced with the new version; the old
    one is in the backup folder.

1 config file was changed on your side and in the new version. I kept yours
and saved the new one next to it as .mcnew. It's usually fine to ignore this —
many mods rewrite their own config when the game starts.
    config/m.cfg

  Heads up: one mod looked edited by hand
  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
enter back    p play now    q quit
[scroll 0]`,
	"done update nothing changed": `[body]
All done! Pack is now on GTNH 2.8.4.

  • Your GTNH files were already up to date.
  • 1 extra mod from your server installed.

  Heads up: one mod looked edited by hand
  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
enter back    p play now    q quit
[scroll 0]`,
	"done create": `[body]
All done! My Pack is ready in Prism.

  • 16,234 files installed.
  • Your server has no extra mods right now.
  • Skipped dup.jar — GTNH already ships a mod with that name.
  • Your server's extra mods couldn't be synced this time (timeout). Run me
    again later to retry.
  • If Prism is already open and doesn't show it, restart Prism.

  Heads up: one mod looked edited by hand
  Heads up: a second, much longer warning that goes on and on so that it has
to wrap onto another line of the terminal
[footer]
enter back    p play now    q quit
[scroll 0]`,
	"error update preparing": `[body]
Something went wrong

I couldn't download the pack.

Nothing in your instance was changed.
[footer]
esc back    enter quit
[scroll 0]`,
	"error update applying rolled back": `[body]
Something went wrong

writing a file failed; everything was rolled back

Everything was put back the way it was, so your instance is exactly as before.
[footer]
enter quit
[scroll 0]`,
	"error update applying says rolled back without ErrRolledBack": `[body]
Something went wrong

writing a file failed; everything was rolled back

Some files may have changed. The originals are in the .gtnh-updater folder
inside the instance.
[footer]
enter quit
[scroll 0]`,
	"error update applying": `[body]
Something went wrong

writing a file failed and the rollback failed too

Some files may have changed. The originals are in the .gtnh-updater folder
inside the instance.
[footer]
enter quit
[scroll 0]`,
	"error loading": `[body]
Something went wrong

I couldn't find Prism Launcher on this computer.
[footer]
enter quit
[scroll 0]`,
	"error create preparing": `[body]
Something went wrong

I couldn't download the pack.

Nothing was created.
[footer]
esc back    enter quit
[scroll 0]`,
	"error create applying": `[body]
Something went wrong

writing a file failed

I removed the half-made instance, so there's nothing to clean up.
[footer]
enter quit
[scroll 0]`,
	"error create preparing leftover": `[body]
Something went wrong

disk full
I couldn't remove the half-made instance folder. Delete it yourself before
trying again. It's here: /x/My Pack (access denied)
[footer]
esc back    enter quit
[scroll 0]`,
	"error create applying leftover": `[body]
Something went wrong

disk full
I couldn't remove the half-made instance folder. Delete it yourself before
trying again. It's here: /x/My Pack (access denied)
[footer]
enter quit
[scroll 0]`,
	"self updated": `[body]
gtnh-update is now version 9.9.9.
[footer]
enter restart now    q quit
[scroll 0]`,
}

// C1 (rollback switch): only an error wrapping update.ErrRolledBack says everything was
// put back; the words "rolled back" alone don't (K3).
func TestC1ErrorViewTrustsErrRolledBackNotTheText(t *testing.T) {
	wrapped := pageBody(goldenErrorModel(t, false, scApplying, rolledBackErr{"apply failed"}))
	textOnly := pageBody(goldenErrorModel(t, false, scApplying, errors.New("apply failed; all changes rolled back")))
	const putBack, mayHaveChanged = "Everything was put back the way it was", "Some files may have changed."
	if !strings.Contains(wrapped, putBack) || strings.Contains(textOnly, putBack) || !strings.Contains(textOnly, mayHaveChanged) {
		t.Errorf("error bodies: wrapped %q, text only %q; want %q only in the first and %q in the second", wrapped, textOnly, putBack, mayHaveChanged)
	}
}

// ---- C2 server-mods key matrix ----

const badLinkMsg = "That doesn't look like a download link — it should start with https://"

// serverModsModel is on the server-mods screen (never asked before) with value typed in.
func serverModsModel(t *testing.T, creating, thenPrepare bool, value string) *model {
	t.Helper()
	m := routeModel(t)
	m.serverModsAsked = false
	if creating {
		m.startCreate()
		m.newName = "My Pack"
	}
	m.target = "2.8.4"
	m.askServerMods(thenPrepare)
	m.input.SetValue(value)
	return m
}

func TestC2ServerModsEnterMatrix(t *testing.T) {
	cases := []struct {
		name        string
		value       string
		thenPrepare bool
		creating    bool
		wantScreen  screen
		wantMods    string
		wantAsked   bool
		wantErr     string
	}{
		{"empty, back to list, update", "", false, false, scTarget, "", true, ""},
		{"empty, back to list, create", "", false, true, scTarget, "", true, ""},
		{"empty, then prepare, update", "", true, false, scPreparing, "", true, ""},
		{"empty, then prepare, create asks name", "", true, true, scName, "", true, ""},
		{"bad, back to list, update", "http://x", false, false, scServerMods, "", false, badLinkMsg},
		{"bad, back to list, create", "http://x", false, true, scServerMods, "", false, badLinkMsg},
		{"bad, then prepare, update", "http://x", true, false, scServerMods, "", false, badLinkMsg},
		{"bad, then prepare, create", "http://x", true, true, scServerMods, "", false, badLinkMsg},
		{"good, back to list, update", "https://x/y.zip", false, false, scTarget, "https://x/y.zip", true, ""},
		{"good, back to list, create", "https://x/y.zip", false, true, scTarget, "https://x/y.zip", true, ""},
		{"good, then prepare, update", "https://x/y.zip", true, false, scPreparing, "https://x/y.zip", true, ""},
		{"good, then prepare, create asks name", "https://x/y.zip", true, true, scName, "https://x/y.zip", true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := serverModsModel(t, c.creating, c.thenPrepare, c.value)
			press(m, keyEnter)
			if m.screen != c.wantScreen || m.serverMods != c.wantMods || m.serverModsAsked != c.wantAsked || m.inputEr != c.wantErr {
				t.Errorf("enter with %q: screen %d, serverMods %q, asked %v, inputEr %q; want %d, %q, %v, %q",
					c.value, m.screen, m.serverMods, m.serverModsAsked, m.inputEr, c.wantScreen, c.wantMods, c.wantAsked, c.wantErr)
			}
		})
	}
}

func TestC2ServerModsEnterTrimsSpaces(t *testing.T) {
	m := serverModsModel(t, false, false, "  https://x/y.zip  ")
	press(m, keyEnter)
	if m.serverMods != "https://x/y.zip" {
		t.Errorf("enter with padded link: serverMods %q, want %q", m.serverMods, "https://x/y.zip")
	}
}

func TestC2ServerModsGoodEnterClearsEarlierError(t *testing.T) {
	m := serverModsModel(t, false, false, "http://x")
	press(m, keyEnter)
	m.input.SetValue("https://x/y.zip")
	press(m, keyEnter)
	if m.inputEr != "" || m.screen != scTarget {
		t.Errorf("good enter after a bad one: inputEr %q, screen %d; want empty, scTarget (%d)", m.inputEr, m.screen, scTarget)
	}
}

func TestC2ServerModsEscClearsErrorKeepsModsAndShowsVersions(t *testing.T) {
	m := serverModsModel(t, false, true, "http://x")
	m.serverMods = "https://old/m.zip"
	press(m, keyEnter, keyEsc)
	if m.screen != scTarget || m.inputEr != "" || m.serverMods != "https://old/m.zip" || m.serverModsAsked {
		t.Errorf("esc on server mods: screen %d, inputEr %q, serverMods %q, asked %v; want scTarget (%d), \"\", old link, false",
			m.screen, m.inputEr, m.serverMods, m.serverModsAsked, scTarget)
	}
}

func TestC2ServerModsUpDownScrollWithoutTouchingInput(t *testing.T) {
	m := serverModsModel(t, false, true, "https://x/y.zip")
	press(m, keyUp, keyDown, keyDown, keyUp)
	if m.input.Value() != "https://x/y.zip" || m.screen != scServerMods {
		t.Errorf("up/down on server mods: value %q, screen %d; want unchanged, scServerMods (%d)", m.input.Value(), m.screen, scServerMods)
	}
}

func TestC2ServerModsTypingGoesIntoInput(t *testing.T) {
	m := serverModsModel(t, false, true, "")
	press(m, runes("h"), runes("q"))
	if m.input.Value() != "hq" || m.screen != scServerMods || m.quitting {
		t.Errorf("typing on server mods: value %q, screen %d, quitting %v; want \"hq\", scServerMods, false", m.input.Value(), m.screen, m.quitting)
	}
}

// ---- C3 installed-version list ----

func TestC3InstalledEnterSetsToldVersionAndShowsTargets(t *testing.T) {
	m := routeModel(t)
	m.showInstalled()
	press(m, keyDown, keyEnter) // 2.8.4 is preselected; down is 2.8.1
	want := update.Detection{Version: "2.8.1", Source: "you told me"}
	if m.detect != want || m.screen != scTarget {
		t.Errorf("enter on installed: detect %+v, screen %d; want %+v, scTarget (%d)", m.detect, m.screen, want, scTarget)
	}
}

func TestC3InstalledEscGoesToInstances(t *testing.T) {
	m := routeModel(t)
	m.showInstalled()
	press(m, keyEsc)
	if m.screen != scHome {
		t.Errorf("esc on installed: screen %d, want scHome (%d)", m.screen, scHome)
	}
}

// ---- C4 end screens ----

func TestC4DoneKeysQuit(t *testing.T) { // home spec C11: enter/esc go home now, only q quits
	m := routeModel(t)
	m.session = &update.Session{Plan: &update.Plan{}}
	m.Update(appliedMsg{&update.Result{From: "2.8.4", To: "2.8.4"}})
	cmd := press(m, runes("q"))
	if !m.quitting || !isQuit(cmd) {
		t.Errorf("q on done: quitting %v, quit cmd %v; want true, true", m.quitting, isQuit(cmd))
	}
}

func TestC4SelfUpdatedEnterRestartsAndQuits(t *testing.T) {
	m := routeModel(t)
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	m.screen = scSelfUpdated
	cmd := press(m, keyEnter)
	if !m.restart || !m.quitting || !isQuit(cmd) {
		t.Errorf("enter on self-updated: restart %v, quitting %v, quit cmd %v; want all true", m.restart, m.quitting, isQuit(cmd))
	}
}

func TestC4SelfUpdatedQAndEscQuitWithoutRestart(t *testing.T) {
	for _, k := range []tea.KeyMsg{runes("q"), keyEsc} {
		t.Run(k.String(), func(t *testing.T) {
			m := routeModel(t)
			m.newer = &selfupdate.Release{Version: "9.9.9"}
			m.screen = scSelfUpdated
			cmd := press(m, k)
			if m.restart || !m.quitting || !isQuit(cmd) {
				t.Errorf("%s on self-updated: restart %v, quitting %v, quit cmd %v; want false, true, true", k, m.restart, m.quitting, isQuit(cmd))
			}
		})
	}
}

func TestC4ErrorEscGoesBackOrQuits(t *testing.T) {
	cases := []struct {
		name       string
		phase      screen
		inst       bool
		manifest   bool
		wantScreen screen
		wantQuit   bool
	}{
		{"preparing with instance goes to versions", scPreparing, true, true, scTarget, false},
		{"self-update goes to versions", scSelfUpdate, true, true, scTarget, false},
		{"preparing without instance goes to instances", scPreparing, false, true, scHome, false},
		{"instance phase goes to instances", scHome, true, true, scHome, false},
		{"applying quits", scApplying, true, true, scError, true},
		{"loading quits", scLoading, true, true, scError, true},
		{"no manifest quits", scPreparing, true, false, scError, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := errorModel(t, c.phase)
			if !c.inst {
				m.inst = prism.Instance{}
			}
			if !c.manifest {
				m.manifest = nil
			}
			cmd := press(m, keyEsc)
			if m.screen != c.wantScreen || m.quitting != c.wantQuit || isQuit(cmd) != c.wantQuit {
				t.Errorf("esc on error: screen %d, quitting %v, quit cmd %v; want %d, %v, %v",
					m.screen, m.quitting, isQuit(cmd), c.wantScreen, c.wantQuit, c.wantQuit)
			}
		})
	}
}

func TestC4ErrorQAndEnterQuitEvenWhenGoingBackIsPossible(t *testing.T) {
	for _, k := range []tea.KeyMsg{runes("q"), keyEnter} {
		t.Run(k.String(), func(t *testing.T) {
			m := errorModel(t, scPreparing)
			cmd := press(m, k)
			if !m.quitting || !isQuit(cmd) {
				t.Errorf("%s on error: quitting %v, quit cmd %v; want true, true", k, m.quitting, isQuit(cmd))
			}
		})
	}
}

// ---- C5 list keys ----

func TestC5InstanceAShowsAllAndRebuilds(t *testing.T) {
	m := routeModel(t)
	m.insts = append(m.insts, prism.Instance{Dir: t.TempDir(), Name: "Vanilla"})
	m.showHome()
	before := len(m.list.Items())
	press(m, runes("a"))
	if !m.showAll || len(m.list.Items()) != 2 || before != 1 || m.screen != scHome {
		t.Errorf("a on instances: showAll %v, items %d -> %d, screen %d; want true, 1 -> 2, scHome", m.showAll, before, len(m.list.Items()), m.screen)
	}
}

func TestC5InstanceATwiceHidesOthersAgain(t *testing.T) {
	m := routeModel(t)
	m.insts = append(m.insts, prism.Instance{Dir: t.TempDir(), Name: "Vanilla"})
	m.showHome()
	press(m, runes("a"), runes("a"))
	if m.showAll || len(m.list.Items()) != 1 {
		t.Errorf("a twice on instances: showAll %v, items %d; want false, 1", m.showAll, len(m.list.Items()))
	}
}

func TestC5VWithNewerStartsSelfUpdate(t *testing.T) { // home spec C5: the self-update key is v
	m := routeModel(t)
	m.showTargets()
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	m.warns, m.steps, m.done, m.total = []string{"w"}, []string{"s"}, 5, 10
	cmd := press(m, runes("v"))
	if m.screen != scSelfUpdate || m.step != "Downloading gtnh-update 9.9.9" || cmd == nil ||
		m.warns != nil || m.steps != nil || m.done != 0 || m.total != 0 {
		t.Errorf("v with newer: screen %d, step %q, cmd nil %v, warns %v, steps %v, done/total %d/%d; want scSelfUpdate, the download step, a cmd, all reset",
			m.screen, m.step, cmd == nil, m.warns, m.steps, m.done, m.total)
	}
}

func TestC5UOnVersionListNoLongerSelfUpdates(t *testing.T) { // home spec C5
	m := routeModel(t)
	m.showTargets()
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	press(m, runes("u"))
	if m.screen != scTarget {
		t.Errorf("u on versions with newer: screen %d, want scTarget (%d)", m.screen, scTarget)
	}
}

func TestC5MOnTargetsAsksServerModsThenBack(t *testing.T) {
	m := routeModel(t)
	m.serverMods = "https://s/m.zip"
	m.showTargets()
	press(m, runes("m"))
	if m.screen != scServerMods || m.modsThenPrepare || m.input.Value() != "https://s/m.zip" {
		t.Errorf("m on versions: screen %d, modsThenPrepare %v, input %q; want scServerMods (%d), false, current link",
			m.screen, m.modsThenPrepare, m.input.Value(), scServerMods)
	}
}

func TestC5IOnTargetsShowsInstalled(t *testing.T) {
	m := routeModel(t)
	m.showTargets()
	press(m, runes("i"))
	if m.screen != scInstalled {
		t.Errorf("i on versions: screen %d, want scInstalled (%d)", m.screen, scInstalled)
	}
}

// ---- C6 update confirm keys ----

func TestC6ConfirmEnterOrYStartsApplyingWithFreshProgress(t *testing.T) {
	for _, k := range []tea.KeyMsg{keyEnter, runes("y")} {
		t.Run(k.String(), func(t *testing.T) {
			m := routeModel(t)
			m.target = "2.9.0-RC-1"
			m.session = &update.Session{Plan: &update.Plan{}}
			m.screen = scConfirm
			m.warns, m.steps, m.step = []string{"w"}, []string{"s"}, "old step"
			cmd := press(m, k)
			if m.screen != scApplying || m.warns != nil || m.steps != nil || m.step != "" || cmd == nil {
				t.Errorf("%s on confirm: screen %d, warns %v, steps %v, step %q, cmd nil %v; want scApplying (%d), reset, a cmd",
					k, m.screen, m.warns, m.steps, m.step, cmd == nil, scApplying)
			}
		})
	}
}

func TestC6ConfirmEscNOrQDropsSessionAndShowsVersions(t *testing.T) {
	for _, k := range []tea.KeyMsg{keyEsc, runes("n"), runes("q")} {
		t.Run(k.String(), func(t *testing.T) {
			m := routeModel(t)
			m.target = "2.9.0-RC-1"
			m.session = &update.Session{Plan: &update.Plan{}}
			m.screen = scConfirm
			press(m, k)
			if m.session != nil || m.screen != scTarget || m.quitting {
				t.Errorf("%s on confirm: session nil %v, screen %d, quitting %v; want nil, scTarget (%d), false",
					k, m.session == nil, m.screen, m.quitting, scTarget)
			}
		})
	}
}

// ---- C7 server-mods selection when picking/creating ----

func writeState(t *testing.T, dir string, st update.State) {
	t.Helper()
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, update.StateDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, update.StateDir, "state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestC7PickInstanceServerMods(t *testing.T) {
	asked := update.State{Version: "2.8.4", CustomModsURL: "https://state/m.zip", CustomModsAsked: true}
	notAsked := update.State{Version: "2.8.4", CustomModsURL: "https://state/m.zip"}
	cases := []struct {
		name      string
		cfg       string
		state     *update.State
		wantMods  string
		wantAsked bool
	}{
		{"none turns them off", "none", &asked, "", true},
		{"a URL wins over the saved one", "https://cfg/m.zip", &asked, "https://cfg/m.zip", true},
		{"empty uses the saved answer", "", &asked, "https://state/m.zip", true},
		{"empty with an unasked state asks", "", &notAsked, "", false},
		{"empty without state asks", "", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := routeModel(t)
			m.cfg.ServerMods = c.cfg
			m.serverMods, m.serverModsAsked = "https://previous/m.zip", !c.wantAsked
			in := prism.Instance{Dir: t.TempDir(), Name: "Other", GTNH: true}
			if c.state != nil {
				writeState(t, in.Dir, *c.state)
			}
			m.pickInstance(in)
			if m.serverMods != c.wantMods || m.serverModsAsked != c.wantAsked {
				t.Errorf("pickInstance with cfg %q: serverMods %q, asked %v; want %q, %v", c.cfg, m.serverMods, m.serverModsAsked, c.wantMods, c.wantAsked)
			}
		})
	}
}

func TestC7StartCreateServerMods(t *testing.T) {
	cases := []struct {
		name      string
		cfg       string
		wantMods  string
		wantAsked bool
	}{
		{"none turns them off", "none", "", true},
		{"a URL is used", "https://cfg/m.zip", "https://cfg/m.zip", true},
		{"empty asks, whatever was decided before", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := routeModel(t)
			m.cfg.ServerMods = c.cfg
			m.serverMods, m.serverModsAsked = "https://previous/m.zip", true
			m.startCreate()
			if m.serverMods != c.wantMods || m.serverModsAsked != c.wantAsked || !m.creating {
				t.Errorf("startCreate with cfg %q: serverMods %q, asked %v, creating %v; want %q, %v, true",
					c.cfg, m.serverMods, m.serverModsAsked, m.creating, c.wantMods, c.wantAsked)
			}
		})
	}
}

func TestC7StartCreateResetsInstanceAndTarget(t *testing.T) {
	m := routeModel(t)
	m.target = "2.8.4"
	m.startCreate()
	if m.inst.Dir != "" || m.target != "" || m.screen != scTarget {
		t.Errorf("startCreate: inst %q, target %q, screen %d; want empty, empty, scTarget", m.inst.Dir, m.target, m.screen)
	}
}

func TestC7PickInstanceUsesSavedVersionAndLeavesCreateMode(t *testing.T) {
	m := routeModel(t)
	m.creating, m.target = true, "2.8.1"
	in := prism.Instance{Dir: t.TempDir(), Name: "Other", GTNH: true}
	writeState(t, in.Dir, update.State{Version: "2.8.1"})
	m.pickInstance(in)
	want := update.Detection{Version: "2.8.1", Source: "updater state"}
	if m.creating || m.target != "" || m.detect != want || m.screen != scTarget || m.inst.Dir != in.Dir {
		t.Errorf("pickInstance: creating %v, target %q, detect %+v, screen %d; want false, \"\", %+v, scTarget", m.creating, m.target, m.detect, m.screen, want)
	}
}

func TestC7PickInstanceWithUnknownVersionAsksIt(t *testing.T) {
	m := routeModel(t)
	m.pickInstance(prism.Instance{Dir: t.TempDir(), Name: "Other", GTNH: true})
	if m.screen != scInstalled || m.detect.Version != "" {
		t.Errorf("pickInstance unknown version: screen %d, detect %+v; want scInstalled (%d), empty", m.screen, m.detect, scInstalled)
	}
}

func TestC7PickInstanceInstalledFlagOverridesDetection(t *testing.T) {
	m := routeModel(t)
	m.cfg.Installed = "2.8.1"
	in := prism.Instance{Dir: t.TempDir(), Name: "Other", GTNH: true}
	writeState(t, in.Dir, update.State{Version: "2.8.4"})
	m.pickInstance(in)
	want := update.Detection{Version: "2.8.1", Source: "command line"}
	if m.detect != want {
		t.Errorf("pickInstance with -installed: detect %+v, want %+v", m.detect, want)
	}
}

// ---- C8 background messages ----

func TestC8StepMsgArchivesPreviousStepAndResetsProgress(t *testing.T) {
	m := routeModel(t)
	m.steps, m.step, m.done, m.total = []string{"one"}, "two", 5, 10
	press(m, stepMsg("three"))
	if strings.Join(m.steps, "|") != "one|two" || m.step != "three" || m.done != 0 || m.total != 0 {
		t.Errorf("stepMsg: steps %v, step %q, done/total %d/%d; want [one two], three, 0/0", m.steps, m.step, m.done, m.total)
	}
}

func TestC8FirstStepMsgArchivesNothing(t *testing.T) {
	m := routeModel(t)
	press(m, stepMsg("first"))
	if len(m.steps) != 0 || m.step != "first" {
		t.Errorf("first stepMsg: steps %v, step %q; want none, first", m.steps, m.step)
	}
}

func TestC8ProgressMsgSetsCounts(t *testing.T) {
	m := routeModel(t)
	press(m, progressMsg{3, 9})
	if m.done != 3 || m.total != 9 {
		t.Errorf("progressMsg: done/total %d/%d, want 3/9", m.done, m.total)
	}
}

func TestC8WarnMsgAppends(t *testing.T) {
	m := routeModel(t)
	m.warns = []string{"a"}
	press(m, warnMsg("b"))
	if strings.Join(m.warns, "|") != "a|b" {
		t.Errorf("warnMsg: warns %v, want [a b]", m.warns)
	}
}

// chrome C6/C7: the banner sits on the line the chrome always keeps, and "v new version"
// still fits on the key bar's one row at 82 columns: 25 - 2 - 1 - 1 = 21 before and after.
func TestC8NewerMsgReappliesListSizeWithBanner(t *testing.T) {
	m := routeModel(t)
	m.showTargets()
	before := m.list.Height()
	press(m, newerMsg{&selfupdate.Release{Version: "9.9.9"}})
	if m.newer == nil || m.newer.Version != "9.9.9" || before != 21 || m.list.Height() != 21 {
		t.Errorf("newerMsg: newer %v, list height %d -> %d; want 9.9.9, 21 -> 21", m.newer, before, m.list.Height())
	}
}

func TestC8NewerMsgWithoutListOnlySetsNewer(t *testing.T) {
	m := sizedModel(t)
	press(m, newerMsg{&selfupdate.Release{Version: "9.9.9"}})
	if m.newer == nil || m.newer.Version != "9.9.9" || m.hasList {
		t.Errorf("newerMsg without list: newer %v, hasList %v; want 9.9.9, false", m.newer, m.hasList)
	}
}

// ---- C9 formatting tables ----

func TestC9Ago(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero", time.Time{}, ""},
		{"future", now.Add(time.Hour), "just now"},
		{"same day", now.Add(-time.Hour), "today"},
		{"under a day but yesterday's date", now.Add(-20 * time.Hour), "yesterday"},
		{"a day and a half", now.Add(-36 * time.Hour), "yesterday"},
		{"two days", now.Add(-2 * day), "2 days ago"},
		{"thirteen days", now.Add(-13 * day), "13 days ago"},
		{"fourteen days", now.Add(-14 * day), "2 weeks ago"},
		{"fifty-nine days", now.Add(-59 * day), "8 weeks ago"},
		{"sixty days", now.Add(-60 * day), "2 months ago"},
		{"364 days", now.Add(-364 * day), "12 months ago"},
		{"365 days", now.Add(-365 * day), "a year ago"},
		{"729 days", now.Add(-729 * day), "a year ago"},
		{"730 days", now.Add(-730 * day), "2 years ago"},
		{"1100 days", now.Add(-1100 * day), "3 years ago"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ago(c.t, now); got != c.want {
				t.Errorf("ago(%v, %v) = %q, want %q", c.t, now, got, c.want)
			}
		})
	}
}

func TestC9Eta(t *testing.T) {
	s := time.Second
	cases := []struct {
		name        string
		done, total int64
		elapsed     time.Duration
		want        string
	}{
		{"nothing done", 0, 100, 10 * s, ""},
		{"no total", 50, 0, 10 * s, ""},
		{"finished", 100, 100, 10 * s, ""},
		{"under two seconds", 50, 100, 2*s - 1, ""},
		{"two seconds left", 50, 100, 2 * s, "almost done"},
		{"nine seconds left", 50, 100, 9 * s, "almost done"},
		{"ten seconds left", 50, 100, 10 * s, "about 10 seconds left"},
		{"24 seconds rounds down", 50, 100, 24 * s, "about 20 seconds left"},
		{"25 seconds rounds up", 50, 100, 25 * s, "about 30 seconds left"},
		{"14 seconds rounds to ten", 50, 100, 14 * s, "about 10 seconds left"},
		{"54 seconds rounds to fifty", 50, 100, 54 * s, "about 50 seconds left"},
		{"55 seconds would round to 60: a minute", 50, 100, 55 * s, "about a minute left"},
		{"59 seconds would round to 60: a minute", 50, 100, 59 * s, "about a minute left"},
		{"a minute", 50, 100, 60 * s, "about a minute left"},
		{"119 seconds", 50, 100, 119 * s, "about a minute left"},
		{"two minutes", 50, 100, 120 * s, "about 2 minutes left"},
		{"two and a half minutes round up", 50, 100, 150 * s, "about 3 minutes left"},
		{"remaining scales with ratio", 25, 100, 20 * s, "about a minute left"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eta(c.done, c.total, c.elapsed); got != c.want {
				t.Errorf("eta(%d, %d, %v) = %q, want %q", c.done, c.total, c.elapsed, got, c.want)
			}
		})
	}
}

func TestC9Mb(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 MB"},
		{999_999, "0 MB"},
		{1_500_000, "1 MB"},
		{727_000_000, "727 MB"},
		{1<<30 - 1, "1073 MB"},
		{1 << 30, "1.1 GB"},
		{5_000_000_000, "5.0 GB"},
	}
	for _, c := range cases {
		if got := mb(c.n); got != c.want {
			t.Errorf("mb(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestC9Num(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{100000, "100,000"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
		// CHARACTERIZATION: suspected bug: the sign counts as a digit for grouping.
		{-123, "-,123"},
	}
	for _, c := range cases {
		if got := num(c.n); got != c.want {
			t.Errorf("num(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestC9HostOf(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://example.com/a/b.zip", "example.com"},
		{"https://example.com", "example.com"},
		{"http://example.com/x", "http:"}, // only https:// is stripped
		{"", ""},
	}
	for _, c := range cases {
		if got := hostOf(c.in); got != c.want {
			t.Errorf("hostOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestC9Plural(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{{0, "many"}, {1, "one"}, {2, "many"}, {-1, "many"}}
	for _, c := range cases {
		if got := plural(c.n, "one", "many"); got != c.want {
			t.Errorf("plural(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestC9KindOf(t *testing.T) {
	cases := []struct {
		version, title, want string
	}{
		{"2.8.4", "Stable release", "Stable release"},
		{"2.9.0-RC-1", "Stable", "Stable release"},
		{"2.9.0-RC-1", "Beta release", "Release candidate"},
		{"2.9.0-beta-3", "Beta release", "Beta"},
		{"2.7.0-pre-1", "Something", "Pre-release"},
		{"nightly-5", "Experimental", "Experimental"},
	}
	for _, c := range cases {
		if got := kindOf(manifest.Release{Version: c.version, Title: c.title}); got != c.want {
			t.Errorf("kindOf(%s, %q) = %q, want %q", c.version, c.title, got, c.want)
		}
	}
}

// ---- C10 teaReporter ----

func recordingReporter() (*teaReporter, *[]tea.Msg) {
	var got []tea.Msg
	return &teaReporter{send: func(m tea.Msg) { got = append(got, m) }}, &got
}

func TestC10ProgressThrottlesWithin80ms(t *testing.T) {
	r, got := recordingReporter()
	r.Progress(1, 10)
	r.Progress(2, 10)
	r.Progress(3, 10)
	want := []tea.Msg{progressMsg{1, 10}}
	if fmt.Sprint(*got) != fmt.Sprint(want) {
		t.Errorf("rapid Progress calls sent %v, want %v", *got, want)
	}
}

func TestC10ProgressAlwaysSendsCompletion(t *testing.T) {
	r, got := recordingReporter()
	r.Progress(1, 10)
	r.Progress(10, 10)
	want := []tea.Msg{progressMsg{1, 10}, progressMsg{10, 10}}
	if fmt.Sprint(*got) != fmt.Sprint(want) {
		t.Errorf("Progress then completion sent %v, want %v", *got, want)
	}
}

func TestC10ProgressSendsAgainAfter80ms(t *testing.T) {
	r, got := recordingReporter()
	r.Progress(1, 10)
	r.last = time.Now().Add(-81 * time.Millisecond) // as if 81ms passed
	r.Progress(2, 10)
	want := []tea.Msg{progressMsg{1, 10}, progressMsg{2, 10}}
	if fmt.Sprint(*got) != fmt.Sprint(want) {
		t.Errorf("Progress after 81ms sent %v, want %v", *got, want)
	}
}

func TestC10StepAndWarnForwardImmediately(t *testing.T) {
	r, got := recordingReporter()
	r.Progress(1, 10)
	r.Step("s1")
	r.Warn("w1")
	r.Step("s2")
	want := []tea.Msg{progressMsg{1, 10}, stepMsg("s1"), warnMsg("w1"), stepMsg("s2")}
	if fmt.Sprintf("%#v", *got) != fmt.Sprintf("%#v", want) {
		t.Errorf("Step/Warn sent %#v, want %#v", *got, want)
	}
}

func TestC10ModelReporterSendsThroughModelSend(t *testing.T) {
	var got []tea.Msg
	m := routeModel(t)
	m.send = func(msg tea.Msg) { got = append(got, msg) }
	m.reporter().Warn("hi")
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", []tea.Msg{warnMsg("hi")}) {
		t.Errorf("model reporter sent %#v, want [warnMsg(hi)]", got)
	}
}

// ---- C11 load, instance lookup and list details (paths the refactor moved) ----

// manifestTransport answers every request with the routing manifest.
type manifestTransport struct{}

func (manifestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body := `{
	  "2.8.4": {"title":"Stable release","releaseDate":"2025/12/23","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/a.zip"}},
	  "2.8.1": {"title":"Stable release","releaseDate":"2025/10/01","mmc":{"java8Url":"https://downloads.gtnewhorizons.com/c.zip"}},
	  "2.9.0-RC-1": {"title":"Beta release","releaseDate":"2026/09/24","mmc":{"java17_2XUrl":"https://downloads.gtnewhorizons.com/b.zip"}}}`
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

// prismInstance makes <dir>/instances/<folder>/instance.cfg with the given name.
func prismInstance(t *testing.T, prismDir, folder, name string) string {
	t.Helper()
	dir := filepath.Join(prismDir, "instances", folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instance.cfg"), []byte("name="+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestC11LoadWithoutPrismSaysSo(t *testing.T) {
	m := newModel(Config{})
	msg, ok := m.load().(errMsg)
	if !ok || !strings.HasPrefix(msg.err.Error(), "I couldn't find Prism Launcher on this computer.\n\n") {
		t.Errorf("load without Prism dirs = %#v, want an errMsg saying Prism wasn't found", msg)
	}
}

func TestC11LoadOfflineSaysServerUnreachable(t *testing.T) {
	m := newModel(Config{Client: &http.Client{Transport: offlineTransport{}}, PrismDirs: []string{t.TempDir()}})
	msg, ok := m.load().(errMsg)
	if !ok || !strings.HasPrefix(msg.err.Error(), "I couldn't reach the GTNH download server") || !errors.Is(msg.err, os.ErrDeadlineExceeded) {
		t.Errorf("load offline = %#v, want an errMsg about the download server wrapping the cause", msg)
	}
}

func TestC11LoadListsInstancesOfEveryPrismDir(t *testing.T) {
	prismDir := t.TempDir()
	prismInstance(t, prismDir, "one", "One")
	missing := filepath.Join(t.TempDir(), "nothing-here")
	m := newModel(Config{Client: &http.Client{Transport: manifestTransport{}}, PrismDirs: []string{missing, prismDir}})
	msg, ok := m.load().(loadedMsg)
	if !ok || len(msg.insts) != 1 || msg.insts[0].Name != "One" || len(msg.m.Releases) != 3 {
		t.Errorf("load = %#v, want loadedMsg with instance One and 3 releases", msg)
	}
}

// lookupModel has two instances in the list and -instance set to want.
func lookupModel(t *testing.T, want string) (*model, prism.Instance, prism.Instance) {
	t.Helper()
	m := sizedModel(t)
	m.cfg.ServerMods, m.cfg.Installed, m.cfg.Instance = "none", "2.8.4", want
	a := prism.Instance{Dir: t.TempDir(), Name: "Alpha", GTNH: true}
	b := prism.Instance{Dir: t.TempDir(), Name: "Beta", GTNH: true}
	m.manifest, m.insts = routeManifest(t), []prism.Instance{a, b}
	return m, a, b
}

// selectedKey is the key of the list item under the cursor ("" if none).
func selectedKey(m *model) string {
	sel, _ := m.list.SelectedItem().(item)
	return sel.key
}

func TestC11InstanceFlagFindsByName(t *testing.T) { // home spec C2: without -version it lands on home
	m, _, b := lookupModel(t, "Beta")
	m.afterLoad()
	if selectedKey(m) != b.Dir || m.screen != scHome {
		t.Errorf("-instance Beta: selected %q, screen %d; want %q, scHome (%d)", selectedKey(m), m.screen, b.Dir, scHome)
	}
}

func TestC11InstanceFlagFindsByDir(t *testing.T) { // home spec C2
	m, _, b := lookupModel(t, "")
	m.cfg.Instance = b.Dir
	m.afterLoad()
	if selectedKey(m) != b.Dir || m.screen != scHome {
		t.Errorf("-instance <Beta's dir>: selected %q, screen %d; want %q, scHome (%d)", selectedKey(m), m.screen, b.Dir, scHome)
	}
}

func TestC11InstanceFlagLoadsUnlistedInstanceFolder(t *testing.T) {
	m, _, _ := lookupModel(t, "")
	dir := prismInstance(t, t.TempDir(), "g", "Gamma")
	m.cfg.Instance = dir
	m.afterLoad()
	if len(m.insts) != 3 || m.insts[2].Dir != dir || m.insts[2].Name != "Gamma" || m.screen != scHome {
		t.Errorf("-instance <unlisted folder>: %d instances, screen %d; want Gamma added as the third, scHome (%d)", len(m.insts), m.screen, scHome)
	}
}

func TestC11InstanceFlagUnknownIsAnError(t *testing.T) {
	m, _, _ := lookupModel(t, "Nope")
	_, cmd := m.afterLoad()
	msg, ok := cmd().(errMsg)
	if !ok || msg.err.Error() != `I couldn't find an instance called "Nope".` {
		t.Errorf("-instance Nope: cmd gave %#v, want errMsg %q", msg, `I couldn't find an instance called "Nope".`)
	}
}

func TestC11HomeUPicksSelectedInstanceForUpdate(t *testing.T) { // home spec C5: u is the update path
	m, _, b := lookupModel(t, "")
	m.cfg.Instance = ""
	m.showHome()
	press(m, keyDown, runes("u"))
	if m.inst.Dir != b.Dir || m.screen != scTarget {
		t.Errorf("u on the second instance: picked %q, screen %d; want Beta, scTarget", m.inst.Name, m.screen)
	}
}

func TestC11InstanceListSaysWhenPlayed(t *testing.T) { // home spec C3: unknown version reads "update available"
	m := routeModel(t)
	m.insts[0].LastLaunch = time.Now().Add(-3 * 24 * time.Hour)
	m.inst = prism.Instance{}
	m.showHome()
	desc := m.list.Items()[0].(item).desc
	if want := "GTNH version unknown · played 3 days ago · update available"; desc != want {
		t.Errorf("instance desc = %q, want %q", desc, want)
	}
}

func TestC11BusyProgressShowsTimeLeft(t *testing.T) {
	m := routeModel(t)
	m.screen, m.target = scPreparing, "2.9.0-RC-1"
	m.step, m.stepStart, m.done, m.total = "Comparing files", time.Now().Add(-20*time.Second), 50, 100
	if body := pageBody(m); !strings.Contains(body, "50 of 100 files · about 20 seconds left") {
		t.Errorf("busy body %q lacks %q", body, "50 of 100 files · about 20 seconds left")
	}
}

func TestC11ListViewShowsNewVersionBanner(t *testing.T) {
	m := routeModel(t)
	m.showTargets()
	m.newer = &selfupdate.Release{Version: "9.9.9"}
	if v := words(m.View()); !strings.Contains(v, "A new version of this updater is out (9.9.9) — press v to get it") {
		t.Errorf("list view %q lacks the new-version banner", v)
	}
}
