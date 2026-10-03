package tui

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// C4: golden renders of twelve workspace states at two sizes, no disk and no wall clock.

var rerecord = flag.Bool("update", false, "re-record the render goldens")

var renderNow = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

const (
	renderModsURL = "https://mods.example.org/pack/extra.zip"
	alphaCfg      = ".minecraft/config/alpha.cfg"
	betaCfg       = ".minecraft/config/beta.cfg"
	zetaCfg       = ".minecraft/config/zeta.cfg"
)

func renderRelease(version, title string, age time.Duration, java17 bool) manifest.Release {
	r := manifest.Release{Version: version, Title: title, ReleaseDate: renderNow.Add(-age),
		Java8URL: "https://downloads.gtnewhorizons.com/" + version + "-8.zip"}
	if java17 {
		r.Java17URL = "https://downloads.gtnewhorizons.com/" + version + "-17.zip"
	}
	return r
}

func renderManifest() *manifest.Manifest {
	return &manifest.Manifest{Releases: []manifest.Release{
		renderRelease("2.9.0-beta-2", "Beta release", 3*day, true),
		renderRelease("2.9.0-beta-1", "Beta release", 10*day, true),
		renderRelease("2.8.4", "Stable release", 20*day, true),
		renderRelease("2.8.3", "Stable release", 50*day, true),
		renderRelease("2.8.2", "Stable release", 80*day, true),
		renderRelease("2.8.1", "Stable release", 130*day, true),
		renderRelease("2.8.0", "Stable release", 200*day, true),
		renderRelease("2.7.4", "Stable release", 300*day, true),
		renderRelease("2.7.3", "Stable release", 400*day, false),
		renderRelease("2.7.2", "Stable release", 800*day, false),
	}}
}

var (
	renderOlder   = prism.Instance{Dir: "/x/Older", Name: "Older", GameDir: "/x/Older/.minecraft", GTNH: true}
	renderMain    = prism.Instance{Dir: "/x/Main", Name: "Main", GameDir: "/x/Main/.minecraft", GTNH: true}
	renderVanilla = prism.Instance{Dir: "/x/Vanilla", Name: "Vanilla", GameDir: "/x/Vanilla/.minecraft", GTNH: false}
)

func olderInfo() homeInfo {
	return homeInfo{gtnh: true, version: "2.8.1", rec: "2.8.4", server: "play.example.org", modsURL: renderModsURL,
		flavor: manifest.Java17,
		backup: &update.Backup{Dir: "/x/Older/.gtnh-updater/backup-20260612-110000",
			Info: update.BackupInfo{From: "2.8.0", To: "2.8.1", When: renderNow.Add(-3*day - time.Hour)}},
		settings: prism.Settings{OverrideMemory: true, MinMemMB: 4096, MaxMemMB: 8192}}
}

func mainInfo() homeInfo {
	return homeInfo{gtnh: true, version: "2.8.4", rec: "2.8.4", flavor: manifest.Java17}
}

func renderModel(w, h int) *model {
	m, _ := newTestModel(Config{PrismDirs: []string{"/x"}}, w, h)
	m.now = func() time.Time { return renderNow }
	m.manifest = renderManifest()
	m.loaded = true
	m.insts = []prism.Instance{renderOlder, renderMain, renderVanilla}
	m.home = map[string]homeInfo{
		renderOlder.Dir:   olderInfo(),
		renderMain.Dir:    mainInfo(),
		renderVanilla.Dir: {gtnh: false},
	}
	m.showAll = true
	m.sel = 0
	m.focus = focusSidebar
	m.newer = &selfupdate.Release{Version: "1.3.0"}
	return m
}

func renderConflictPlan() *update.Plan {
	return planOf(1204, 18, alphaCfg, betaCfg, zetaCfg)
}

func settingsFocus(t *testing.T, m *model) {
	t.Helper()
	m.focus = focusPage
	i := indexOf(rowIDs(m), "memory")
	if i < 0 {
		t.Fatalf("no memory row in %v", rowIDs(m))
	}
	m.row = i
}

var renderStates = []struct {
	name  string
	setup func(t *testing.T, m *model)
}{
	{"home-one", func(t *testing.T, m *model) {
		m.insts = []prism.Instance{renderMain}
		m.home = map[string]homeInfo{renderMain.Dir: mainInfo()}
		m.showAll = false
		m.newer = nil
		m.focus = focusPage
		m.row = 0
	}},
	{"home-three", func(t *testing.T, m *model) {}},
	{"page-settings-focus", settingsFocus},
	{"editing-memory", func(t *testing.T, m *model) {
		settingsFocus(t, m)
		m.editSetting("memory")
		if m.edit == nil {
			t.Fatal("editSetting(memory) didn't start editing")
		}
		m.edit.input.SetValue("6144")
	}},
	{"update-confirm", func(t *testing.T, m *model) {
		m.focus = focusPage
		m.target = "2.8.0"
		m.detect = update.Detection{Version: "2.8.1", Source: "updater state"}
		m.serverMods = renderModsURL
		m.job = &job{kind: jobUpdate, dir: "/x/Older", title: "Checking what GTNH 2.8.0 changes", phase: "prepare"}
		pl := renderConflictPlan()
		pl.Kept = []string{".minecraft/config/kept-a.cfg", ".minecraft/config/kept-b.cfg", ".minecraft/config/kept-c.cfg"}
		pl.ExtraMods = []string{"optifine.jar", "journeymap-extra.jar"}
		pl.Choose(alphaCfg, update.TakeNew)
		pl.Choose(betaCfg, update.TakeNew)
		pl.Choose(zetaCfg, update.KeepMine)
		m.session = sessionOf(pl)
		m.confirmUpdate()
	}},
	{"job-applying", func(t *testing.T, m *model) {
		m.focus = focusPage
		m.row = 0
		m.job = &job{kind: jobUpdate, dir: "/x/Older", title: "Updating to GTNH 2.9.1", phase: "apply"}
		m.steps = []string{"Backed up"}
		m.step = "Writing files"
		m.stepStart = renderNow.Add(-30 * time.Second)
		m.total = 727_000_000
		m.done = 312_610_000
		m.warns = []string{"2 config files couldn't be read, so I left them alone"}
	}},
	{"conflicts", func(t *testing.T, m *model) {
		m.focus = focusPage
		m.target = "2.8.4"
		m.detect = update.Detection{Version: "2.8.1", Source: "updater state"}
		m.serverMods = renderModsURL
		m.job = &job{kind: jobUpdate, dir: "/x/Older", title: "Checking what GTNH 2.8.4 changes", phase: "prepare"}
		pl := renderConflictPlan()
		pl.ChooseAll(update.TakeNew)
		m.session = sessionOf(pl)
		m.conflictsDialog()
	}},
	{"version-picker", func(t *testing.T, m *model) {
		m.chooseVersion()
	}},
	{"notice-updated", func(t *testing.T, m *model) {
		m.focus = focusPage
		m.row = 0
		info := olderInfo()
		info.version = "2.8.4"
		info.backup = &update.Backup{Dir: "/x/Older/.gtnh-updater/backup-20260612-110000",
			Info: update.BackupInfo{From: "2.8.1", To: "2.8.4", When: renderNow.Add(-time.Hour)}}
		m.home[renderOlder.Dir] = info
		m.notices["/x/Older"] = notice{
			text: "Updated to GTNH 2.8.4 just now · 1,204 files updated, 18 removed · 1 new config version saved as .mcnew",
			info: "2 extra mods from your server installed"}
	}},
	{"pending-create", func(t *testing.T, m *model) {
		m.target = "2.8.4"
		m.newName = "My Pack"
		m.job = &job{kind: jobCreate, dir: "/x/instances/My Pack", title: "Getting GTNH 2.8.4 ready", phase: "prepare"}
		m.home[m.job.dir] = m.pendingInfo()
		m.selectDir(m.job.dir)
		m.focus = focusPage
		m.row = 0
		m.step = "Downloading GTNH 2.8.4"
		m.stepStart = renderNow.Add(-10 * time.Second)
		m.total = 727_000_000
		m.done = 87_240_000
	}},
	{"self-update-progress", func(t *testing.T, m *model) {
		m.job = &job{kind: jobSelf, title: "Downloading GTNH Launcher 1.3.0", phase: "apply"}
		m.progressDialog("Updating the launcher")
		m.step = "Downloading GTNH Launcher 1.3.0"
		m.stepStart = renderNow.Add(-6 * time.Second)
		m.total = 12_000_000
		m.done = 7_200_000
	}},
	{"error", func(t *testing.T, m *model) {
		m.errorDialog(errors.New("I couldn't reach the GTNH download server to see what changed.\nCheck your internet connection and try again."), "Nothing was changed.")
	}},
}

func readGolden(t *testing.T, path string) ([]string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n"), true
}

func writeGolden(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// C4
func TestRenderGoldens(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		for _, st := range renderStates {
			w, h := size[0], size[1]
			name := fmt.Sprintf("%s-%dx%d", st.name, w, h)
			t.Run(name, func(t *testing.T) {
				m := renderModel(w, h)
				st.setup(t, m)

				got := trimLines(m.View())

				if len(got) != h {
					t.Errorf("view is %d lines, want %d:\n%s", len(got), h, strings.Join(got, "\n"))
				}
				for i, l := range got {
					if lw := ansi.StringWidth(l); lw > w {
						t.Errorf("line %d is %d wide, more than %d: %q", i+1, lw, w, l)
					}
				}
				path := filepath.Join("testdata", "render", name+".txt")
				want, ok := readGolden(t, path)
				if !ok || *rerecord {
					writeGolden(t, path, got)
					return
				}
				for i := range max(len(want), len(got)) {
					var wl, gl string
					if i < len(want) {
						wl = want[i]
					}
					if i < len(got) {
						gl = got[i]
					}
					if wl != gl || i >= len(want) || i >= len(got) {
						t.Fatalf("%s differs at line %d (want %d lines, got %d)\nwant: %q\ngot:  %q\n\ngot:\n%s",
							path, i+1, len(want), len(got), wl, gl, strings.Join(got, "\n"))
					}
				}
			})
		}
	}
}
