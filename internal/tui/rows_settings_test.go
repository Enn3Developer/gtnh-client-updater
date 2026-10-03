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
