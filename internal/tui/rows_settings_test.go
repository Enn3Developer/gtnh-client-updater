package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// C4
func TestC4SettingValueOfEachKey(t *testing.T) {
	overridden := homeInfo{
		server: "mc.example.net:25565", modsURL: "https://dl.example.com/pack/extra.zip",
		settings: prism.Settings{
			OverrideMemory: true, MinMemMB: 2048, MaxMemMB: 6144,
			OverrideJavaArgs: true, JvmArgs: "-XX:+UseZGC",
			OverrideJavaLocation: true, JavaPath: "/usr/lib/jvm/java-21/bin/java",
			OverrideWindow: true, WinWidth: 1280, WinHeight: 720,
		},
	}
	// Values without the Override flags are what Prism ignores.
	ignored := homeInfo{settings: prism.Settings{
		MinMemMB: 2048, MaxMemMB: 6144, JvmArgs: "-XX:+UseZGC", JavaPath: "/usr/bin/java", WinWidth: 1280, WinHeight: 720,
	}}
	cases := []struct {
		key  string
		info homeInfo
		want string
	}{
		{"memory", overridden, "6144 MB (at least 2048 MB)"},
		{"memory", ignored, prismDefault},
		{"jvm", overridden, "-XX:+UseZGC"},
		{"jvm", ignored, prismDefault},
		{"java", overridden, "/usr/lib/jvm/java-21/bin/java"},
		{"java", ignored, prismDefault},
		{"window", overridden, "1280×720"},
		{"window", ignored, prismDefault},
		{"server", overridden, "mc.example.net:25565"},
		{"server", ignored, "none"},
		{"mods", overridden, "dl.example.com"},
		{"mods", ignored, "none"},
	}
	for _, c := range cases {
		t.Run(c.key+"="+c.want, func(t *testing.T) {
			if got := settingValue(c.key, c.info); got != c.want {
				t.Errorf("settingValue(%s) = %q, want %q", c.key, got, c.want)
			}
		})
	}
}

// C4
func TestC4SettingsRowsShowLabelsAndValues(t *testing.T) {
	root := t.TempDir()
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4",
		cfg: "OverrideWindow=true\nMinecraftWinWidth=1920\nMinecraftWinHeight=1080\n" +
			"OverrideJavaLocation=true\nJavaPath=/opt/java/bin/java\n"})
	m, _ := loadedModel(root, 80, 24, in)
	m.focus = focusPage

	got := pageLines(m, 78)

	for _, want := range []string{
		"  Memory         Prism's default",
		"  Java           /opt/java/bin/java",
		"  Window         1920×1080",
		"  Server         none",
		"  Server mods    none",
	} {
		if !containsLine(got, want) {
			t.Errorf("no line %q in page:\n%s", want, strings.Join(got, "\n"))
		}
	}
}

// C4
func TestC4LongJavaArgumentsAreTruncatedToFit(t *testing.T) {
	root := t.TempDir()
	args := "-XX:+UseG1GC -XX:+ParallelRefProcEnabled -XX:MaxGCPauseMillis=200 -XX:+UnlockExperimentalVMOptions -XX:+DisableExplicitGC"
	in := makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4",
		cfg: "OverrideJavaArgs=true\nJvmArgs=" + args + "\n"})
	m, _ := loadedModel(root, 80, 24, in)
	m.focus = focusPage
	jvm := m.rows()[indexOf(rowIDs(m), "jvm")]

	line := ansi.Strip(strings.Join(jvm.lines(60, false), "\n"))

	if !strings.HasPrefix(line, "  Java arguments -XX:+UseG1GC") || !strings.HasSuffix(strings.TrimRight(line, " "), "…") {
		t.Errorf("jvm row = %q, want truncated args", line)
	}
	if w := ansi.StringWidth(line); w > 60 || strings.Contains(line, "\n") {
		t.Errorf("jvm row is %d columns / multiline, want one line ≤ 60", w)
	}
}

// C2
func TestC2SettingHelpTexts(t *testing.T) {
	cases := []struct{ id, want string }{
		{"memory", "How much memory the game may use, in MB — GTNH runs well with 6144 to 8192. Empty means Prism's default."},
		{"jvm", "Extra arguments for Java. Only change this if someone told you what to put here. Empty means Prism's default."},
		{"java", "Full path of the java program Prism should use for this instance. Empty means Prism's default."},
		{"window", "Width and height of the game window, like 1920x1080. Empty means Prism's default."},
		{"server", "The server you usually play on, like play.example.com or play.example.com:25565. Empty means none."},
		{"mods", "The link your server owner gave you for its extra mods. Empty means none."},
		{"prism", "Where Prism Launcher is on this computer — the full path of its program. Empty lets me find it myself."},
		{"after", ""},
		{"play", ""},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			if got := settingHelp(c.id); got != c.want {
				t.Errorf("settingHelp(%s) = %q, want %q", c.id, got, c.want)
			}
		})
	}
}

// C2: the edited row is the field line and the help line.
func TestC2TheEditedRowIsTheFieldAndItsHelp(t *testing.T) {
	m, _ := oneFull(t)
	m.editSetting("memory")
	row := m.rows()[indexOf(rowIDs(m), "memory")]

	lines := row.lines(200, true)

	if len(lines) != 2 {
		t.Fatalf("edited row has %d lines, want 2: %q", len(lines), lines)
	}
	if first := ansi.Strip(lines[0]); !strings.HasPrefix(first, "▸ Memory         8192") || strings.Contains(first, ">") {
		t.Errorf("field line = %q, want ▸, the label padded to 15 and the value without a prompt", first)
	}
	if help := ansi.Strip(lines[1]); help != "  How much memory the game may use, in MB — GTNH runs well with 6144 to 8192. Empty means Prism's default." {
		t.Errorf("help line = %q", help)
	}
}

// C2: a refused value adds the reason as a third line.
func TestC2TheEditedRowShowsTheReasonOfARefusedValue(t *testing.T) {
	m, _ := oneFull(t)
	m.editSetting("window")
	m.edit.input.SetValue("big")
	m.saveEdit()
	row := m.rows()[indexOf(rowIDs(m), "window")]

	lines := row.lines(200, true)

	if len(lines) != 3 || ansi.Strip(lines[2]) != "  Give me width and height like 1920x1080." {
		t.Errorf("edited row = %q, want field, help and the window message", lines)
	}
}

// C2: the field line is cut to the width.
func TestC2TheFieldLineFitsTheWidth(t *testing.T) {
	m, _ := oneFull(t)
	m.editSetting("server")
	row := m.rows()[indexOf(rowIDs(m), "server")]

	first := row.lines(24, true)[0]

	if w := ansi.StringWidth(first); w > 24 || !strings.HasPrefix(ansi.Strip(first), "▸ Server") {
		t.Errorf("field line %q is %d columns, want ≤ 24 starting with ▸ Server", ansi.Strip(first), w)
	}
}

// C2: rows other than the edited one render as before.
func TestC2OtherRowsAreUnchangedWhileEditing(t *testing.T) {
	m, _ := oneFull(t)
	m.editSetting("memory")

	got := pageLines(m, 78)

	for _, want := range []string{
		"  Java arguments Prism's default",
		"  Server         play.example.org",
		"  Server mods    mods.example.org",
	} {
		if !containsLine(got, want) {
			t.Errorf("no line %q while editing memory:\n%s", want, strings.Join(got, "\n"))
		}
	}
}

// C2: after the "after" toggle its row carries the saved marker.
func TestC2TheToggledAfterRowIsMarkedSaved(t *testing.T) {
	m, _ := oneFull(t)
	m.app.AfterPlay = "stay"

	m.editSetting("after")
	got := pageLines(m, 78)

	if after := lineWith(got, "After I start the game"); !strings.HasSuffix(after, " quit ✓ saved") {
		t.Errorf("no saved after row:\n%s", strings.Join(got, "\n"))
	}
}

// C4
func TestC4UnreadableSettingsShowOneInfoLine(t *testing.T) {
	root := t.TempDir()
	m, _ := loadedModel(root, 80, 24, makeInst(t, root, instSpec{name: "Home", gtnh: true, version: "2.8.4", noCfg: true}))
	m.focus = focusPage

	got := pageLines(m, 78)

	found := false
	for _, l := range got {
		if strings.TrimSpace(l) == cantReadCfg {
			found = true
		}
	}
	if !found || containsLine(got, "Memory") {
		t.Errorf("want the %q line and no settings rows:\n%s", cantReadCfg, strings.Join(got, "\n"))
	}
	if ids := strings.Join(rowIDs(m), ","); ids != "play,versions,after,prism" {
		t.Errorf("rows = %s", ids)
	}
}
