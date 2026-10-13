package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Without a Prism data dir the page asks for it instead of sending the player to the
// command line.

func TestNoPrismPageAsksForTheFolder(t *testing.T) {
	m, _ := newTestModel(Config{}, 80, 24)
	m.Update(m.load())

	got := strings.Join(screen(m), "\n")

	if !m.noPrism || !strings.Contains(got, "I couldn't find Prism Launcher on this computer.") ||
		!strings.Contains(flat(got), "press enter and show me the folder") {
		t.Fatalf("screen:\n%s", got)
	}
	if strings.Contains(got, "-prism-dir") {
		t.Errorf("the page sends the player to the command line:\n%s", got)
	}
	if pairs := strings.Join(m.statusPairs(), " "); pairs != "enter show me where q quit" {
		t.Errorf("status = %q", pairs)
	}
	press(m, "enter")
	if m.dialog == nil || m.dialog.input == nil || m.dialog.title != "Where does Prism keep its data?" {
		t.Fatalf("enter opened %+v", m.dialog)
	}
}

func TestNoPrismFolderIsCheckedRememberedAndLoaded(t *testing.T) {
	data := t.TempDir()
	writeFile(t, filepath.Join(data, "prismlauncher.cfg"), "InstanceDir=instances\n")
	makeInst(t, data, fullSpec("Home"))
	m, f := newTestModel(Config{}, 80, 24)
	m.Update(m.load())
	press(m, "enter")

	press(m, "/no/such/folder", "enter")
	if m.dialog == nil || m.dialog.inputErr != msgBadPrismDir {
		t.Fatalf("a wrong folder: dialog %+v", m.dialog)
	}
	m.dialog.input.SetValue(`"` + filepath.Join(data, "instances") + `"`) // pasted with quotes, one level too deep
	cmd := press(m, "enter")

	if m.dialog != nil || m.noPrism || len(m.cfg.PrismDirs) != 1 || m.cfg.PrismDirs[0] != data {
		t.Fatalf("dialog %+v, noPrism %v, dirs %v", m.dialog, m.noPrism, m.cfg.PrismDirs)
	}
	if len(f.saved) != 1 || f.saved[0].PrismDir != data {
		t.Errorf("saved %+v, want PrismDir %s", f.saved, data)
	}
	if cmd == nil {
		t.Fatal("no load after the folder was given")
	}
	m.Update(m.load()) // what cmd does, without the network
	m.Update(manifestMsg{m: testManifest()})
	if !m.loaded || len(m.insts) != 1 || m.insts[0].Name != "Home" {
		t.Errorf("loaded %v, insts %+v", m.loaded, m.insts)
	}
}

func TestCleanPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	for in, want := range map[string]string{
		`  "C:\Program Files\Prism\prismlauncher.exe" `: `C:\Program Files\Prism\prismlauncher.exe`,
		`'/opt/prism/prismlauncher'`:                    "/opt/prism/prismlauncher",
		"~/PrismLauncher":                               filepath.Join(home, "PrismLauncher"),
		"~":                                             home,
		"/usr/bin/java":                                 "/usr/bin/java",
		`"`:                                             `"`,
	} {
		if got := cleanPath(in); got != want {
			t.Errorf("cleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}
