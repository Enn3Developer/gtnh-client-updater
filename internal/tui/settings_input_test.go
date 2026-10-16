package tui

import (
	"path/filepath"
	"testing"
)

// The settings fields take what players type and paste: a cursor that moves, memory in
// GB, sizes with spaces, quoted paths and folders instead of programs.

func TestSettingsLeftAndRightMoveTheCursorInTheField(t *testing.T) {
	m, _ := twoGTNH(t)
	m.focus = focusPage
	m.editSetting("memory") // "8192", cursor at the end
	row, sel := m.row, m.sel

	press(m, "left", "left", "5", "right", "6")

	if m.edit == nil || m.edit.input.Value() != "815962" {
		t.Fatalf("field = %+v, want 815962", m.edit)
	}
	if m.row != row || m.sel != sel || m.focus != focusPage {
		t.Errorf("←→ moved the page: row %d→%d sel %d→%d", row, m.row, sel, m.sel)
	}
}

func TestParseMemory(t *testing.T) {
	for in, want := range map[string]int{
		"8192": 8192, "8192 MB": 8192, "8192mb": 8192, "8192M": 8192, "8 GB": 8192, "8G": 8192,
		"8gb": 8192, "8": 8192, "6.5 GB": 6656, "1": 1024, "64": 65536, "65": 65, "8 GiB": 8192,
	} {
		if got, ok := parseMemory(in); !ok || got != want {
			t.Errorf("parseMemory(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "abc", "-8", "0", "6144.5", "8 TB", "inf", "NaN"} {
		if got, ok := parseMemory(in); ok {
			t.Errorf("parseMemory(%q) = %d, want refused", in, got)
		}
	}
}

func TestSettingsSavesMemoryGivenInGB(t *testing.T) {
	m, _, in := seHome(t, "")

	seSave(t, m, "memory", "8 GB")

	if m.edit != nil {
		t.Fatalf("still editing: %+v", m.edit)
	}
	if s := seSettings(t, in.Dir); !s.OverrideMemory || s.MaxMemMB != 8192 {
		t.Errorf("settings %+v, want 8192 MB", s)
	}
}

func TestNormalSetting(t *testing.T) {
	javaHome := t.TempDir()
	java := filepath.Join(javaHome, "bin", "java")
	writeFile(t, java, "binary")
	app := filepath.Join(t.TempDir(), "Prism Launcher.app")
	prismExe := filepath.Join(app, "Contents", "MacOS", "prismlauncher")
	writeFile(t, prismExe, "binary")
	empty := t.TempDir()
	cases := []struct{ id, in, want string }{
		{"memory", "8 GB", "8192"},
		{"memory", "abc", "abc"},
		{"window", "1920 x 1080", "1920x1080"},
		{"window", "1920X1080", "1920x1080"},
		{"java", `"` + java + `"`, java},
		{"java", javaHome, java},
		{"java", empty, empty},
		{"prism", app, prismExe},
		{"prism", "'" + prismExe + "'", prismExe},
		{"server", "play.example.com", "play.example.com"},
	}
	for _, c := range cases {
		if got := normalSetting(c.id, c.in); got != c.want {
			t.Errorf("normalSetting(%s, %q) = %q, want %q", c.id, c.in, got, c.want)
		}
	}
	if msg := checkSetting("java", normalSetting("java", javaHome)); msg != "" {
		t.Errorf("Java's folder is refused: %q", msg)
	}
}
