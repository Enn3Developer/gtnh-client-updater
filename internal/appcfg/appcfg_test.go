package appcfg

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("arrange: write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// C1
func TestLoadFromMissingFileReturnsZeroConfigAndNilError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	got, err := LoadFrom(path)

	if err != nil {
		t.Fatalf("LoadFrom(missing) error = %v, want nil", err)
	}
	if got != (Config{}) {
		t.Fatalf("LoadFrom(missing) = %+v, want zero Config", got)
	}
}

// C1
func TestLoadFromIgnoresUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"prismExe":"/opt/prism/prismlauncher","afterPlay":"quit","theme":"dark"}`)

	got, err := LoadFrom(path)

	if err != nil {
		t.Fatalf("LoadFrom error = %v, want nil", err)
	}
	want := Config{PrismExe: "/opt/prism/prismlauncher", AfterPlay: "quit"}
	if got != want {
		t.Fatalf("LoadFrom = %+v, want %+v", got, want)
	}
}

// C1
func TestLoadFromInvalidJSONReturnsZeroConfigAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"prismExe":"/opt/prism/prismlauncher",`)

	got, err := LoadFrom(path)

	if err == nil {
		t.Errorf("LoadFrom(invalid JSON) error = nil, want non-nil")
	}
	if got != (Config{}) {
		t.Errorf("LoadFrom(invalid JSON) = %+v, want zero Config", got)
	}
}

// C1: a directory is an unreadable file on every OS.
func TestLoadFromUnreadablePathReturnsZeroConfigAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("arrange: %v", err)
	}

	got, err := LoadFrom(path)

	if err == nil {
		t.Errorf("LoadFrom(directory) error = nil, want non-nil")
	}
	if got != (Config{}) {
		t.Errorf("LoadFrom(directory) = %+v, want zero Config", got)
	}
}

// C2
func TestSaveToZeroConfigWritesEmptyObjectAndNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	if err := SaveTo(path, Config{}); err != nil {
		t.Fatalf("SaveTo error = %v", err)
	}

	if got := readFile(t, path); got != "{}\n" {
		t.Fatalf("file content = %q, want %q", got, "{}\n")
	}
}

// C2
func TestSaveToWritesIndentedJSONWithTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Config{PrismExe: "/opt/prism/prismlauncher", AfterPlay: "quit"}

	if err := SaveTo(path, c); err != nil {
		t.Fatalf("SaveTo error = %v", err)
	}

	want := "{\n  \"prismExe\": \"/opt/prism/prismlauncher\",\n  \"afterPlay\": \"quit\"\n}\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}

// C2
func TestSaveToCreatesMissingParentDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "config.json")

	if err := SaveTo(path, Config{AfterPlay: "stay"}); err != nil {
		t.Fatalf("SaveTo error = %v", err)
	}

	want := "{\n  \"afterPlay\": \"stay\"\n}\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}

// C2
func TestSaveToLeavesNoTmpFileAfterSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	if err := SaveTo(path, Config{PrismExe: "/opt/prism/prismlauncher"}); err != nil {
		t.Fatalf("SaveTo error = %v", err)
	}

	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat(path.tmp) error = %v, want not-exist", err)
	}
}

// C2: rename replaces an existing file.
func TestSaveToOverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"prismExe":"/old"}`)

	if err := SaveTo(path, Config{}); err != nil {
		t.Fatalf("SaveTo error = %v", err)
	}

	if got := readFile(t, path); got != "{}\n" {
		t.Fatalf("file content = %q, want %q", got, "{}\n")
	}
}

// C2
func TestSaveToFailingWriteReturnsErrorAndLeavesNoTmp(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "notadir")
	writeFile(t, parentFile, "x")
	path := filepath.Join(parentFile, "config.json")

	err := SaveTo(path, Config{PrismExe: "/opt/prism/prismlauncher"})

	if err == nil {
		t.Errorf("SaveTo under a regular file error = nil, want non-nil")
	}
	if _, statErr := os.Stat(path + ".tmp"); statErr == nil {
		t.Errorf("path.tmp exists after failed SaveTo, want absent")
	}
}

// C2: the rename step failing (path is a non-empty directory) is a failing write too.
func TestSaveToFailingRenameReturnsErrorAndLeavesNoTmp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("arrange: %v", err)
	}
	writeFile(t, filepath.Join(path, "keep"), "x")

	err := SaveTo(path, Config{PrismExe: "/opt/prism/prismlauncher"})

	if err == nil {
		t.Errorf("SaveTo over a directory error = nil, want non-nil")
	}
	if _, statErr := os.Stat(path + ".tmp"); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("Stat(path.tmp) error = %v, want not-exist after failed SaveTo", statErr)
	}
}

// C3
func TestSaveToThenLoadFromRoundTripsAllFieldsSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := Config{PrismExe: `C:\Program Files\Prism\prismlauncher.exe`, AfterPlay: AfterPlayQuit}

	if err := SaveTo(path, want); err != nil {
		t.Fatalf("SaveTo error = %v", err)
	}
	got, err := LoadFrom(path)

	if err != nil {
		t.Fatalf("LoadFrom error = %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

// C3
func TestSaveToThenLoadFromRoundTripsZeroConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	if err := SaveTo(path, Config{}); err != nil {
		t.Fatalf("SaveTo error = %v", err)
	}
	got, err := LoadFrom(path)

	if err != nil {
		t.Fatalf("LoadFrom error = %v", err)
	}
	if got != (Config{}) {
		t.Fatalf("round trip = %+v, want zero Config", got)
	}
}

// C4
func TestStaysOpen(t *testing.T) {
	cases := []struct {
		afterPlay string
		want      bool
	}{
		{"", true},
		{"stay", true},
		{"quit", false},
		{"QUIT", true},
		{"whatever", true},
	}
	for _, tc := range cases {
		t.Run("afterPlay="+tc.afterPlay, func(t *testing.T) {
			got := Config{AfterPlay: tc.afterPlay}.StaysOpen()
			if got != tc.want {
				t.Fatalf("Config{AfterPlay: %q}.StaysOpen() = %v, want %v", tc.afterPlay, got, tc.want)
			}
		})
	}
}

// C5: Path mirrors os.UserConfigDir (its error untouched, or <dir>/gtnh-update/config.json) and creates nothing.
func TestPathIsUnderUserConfigDirAndCreatesNothing(t *testing.T) {
	base, baseErr := os.UserConfigDir()
	appDir := filepath.Join(base, "gtnh-update")
	_, statErr := os.Stat(appDir)
	existedBefore := statErr == nil

	got, err := Path()

	if baseErr != nil {
		if err == nil || err.Error() != baseErr.Error() {
			t.Fatalf("Path() error = %v, want %v", err, baseErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("Path() error = %v, want nil", err)
	}
	want := filepath.Join(base, "gtnh-update", "config.json")
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, filepath.Join("gtnh-update", "config.json")) {
		t.Errorf("Path() = %q, want suffix gtnh-update/config.json", got)
	}
	if _, err := os.Stat(appDir); !existedBefore && err == nil {
		t.Errorf("Path() created %s", appDir)
	}
}
