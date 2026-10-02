package prism

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// Helper process for the Launch test (C8): when the test binary is started
// with GO_WANT_HELPER_PROCESS=1 it exits successfully before running any test.
// Done in init rather than in a TestHelperProcess function so that no test
// trivially passes against the stubs.
func init() {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Exit(0)
	}
}

const flatpakAppID = "org.prismlauncher.PrismLauncher"

// exeName returns the on-disk file name of an executable called name.
func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// writeFakeExe creates an executable regular file named name (".exe" on windows) in dir.
func writeFakeExe(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, exeName(name))
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// isolate points PATH at an empty temp dir and makes wellKnownLaunchers return nothing,
// so no real Prism install on the machine can be found. It returns the PATH dir.
func isolate(t *testing.T) string {
	t.Helper()
	pathDir := t.TempDir()
	t.Setenv("PATH", pathDir)
	setWellKnown(t, nil)
	return pathDir
}

func setWellKnown(t *testing.T, paths []string) {
	t.Helper()
	orig := wellKnownLaunchers
	t.Cleanup(func() { wellKnownLaunchers = orig })
	wellKnownLaunchers = func() []string { return paths }
}

func flatpakDataDir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), ".var", "app", flatpakAppID, "data", "PrismLauncher")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

// caseInsensitiveFS reports whether names in dir are matched case-insensitively
// (default on macOS and windows), where "prismlauncher" and "PrismLauncher" are the same file.
func caseInsensitiveFS(t *testing.T, dir string) bool {
	t.Helper()
	probeDir := filepath.Join(dir, "case-probe")
	if err := os.Mkdir(probeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(probeDir, "probe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(filepath.Join(probeDir, "PROBE"))
	return err == nil
}

func sameLauncher(a, b Launcher) bool {
	return a.Exe == b.Exe && a.Kind == b.Kind && slices.Equal(a.Args, b.Args)
}

// ---- C1 ----

func TestFindLauncherOverrideRegularFileIsCustom(t *testing.T) {
	isolate(t)
	override := writeFakeExe(t, t.TempDir(), "my-prism")
	want := Launcher{Exe: override, Kind: "custom"}

	got, err := FindLauncher(t.TempDir(), override)

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestFindLauncherOverrideWinsOverFlatpakPortableAndPath(t *testing.T) {
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "flatpak")
	writeFakeExe(t, pathDir, "prismlauncher")
	dataDir := flatpakDataDir(t)
	writeFakeExe(t, dataDir, "prismlauncher")
	override := writeFakeExe(t, t.TempDir(), "other-prism")
	want := Launcher{Exe: override, Kind: "custom"}

	got, err := FindLauncher(dataDir, override)

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestFindLauncherOverrideMissingIsNotFoundAndNamesPath(t *testing.T) {
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "prismlauncher")
	dataDir := t.TempDir()
	writeFakeExe(t, dataDir, "prismlauncher")
	override := filepath.Join(t.TempDir(), "nope", "prismlauncher")

	_, err := FindLauncher(dataDir, override)

	if !errors.Is(err, ErrLauncherNotFound) {
		t.Fatalf("err = %v, want ErrLauncherNotFound", err)
	}
	if !strings.Contains(err.Error(), override) {
		t.Fatalf("err %q does not contain override %q", err.Error(), override)
	}
}

func TestFindLauncherOverrideDirectoryIsNotFoundAndNamesPath(t *testing.T) {
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "prismlauncher")
	override := t.TempDir()

	_, err := FindLauncher(t.TempDir(), override)

	if !errors.Is(err, ErrLauncherNotFound) {
		t.Fatalf("err = %v, want ErrLauncherNotFound", err)
	}
	if !strings.Contains(err.Error(), override) {
		t.Fatalf("err %q does not contain override %q", err.Error(), override)
	}
}

func TestFindLauncherOverrideBareNameIsNotResolvedViaPath(t *testing.T) {
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "prism-override-only-in-path")
	override := exeName("prism-override-only-in-path") // not present in the cwd

	_, err := FindLauncher(t.TempDir(), override)

	if !errors.Is(err, ErrLauncherNotFound) {
		t.Fatalf("err = %v, want ErrLauncherNotFound", err)
	}
	if !strings.Contains(err.Error(), override) {
		t.Fatalf("err %q does not contain override %q", err.Error(), override)
	}
}

// ---- C2 ----

func TestFindLauncherFlatpakDataDirUsesFlatpakRun(t *testing.T) {
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "flatpak")
	want := Launcher{Exe: "flatpak", Args: []string{"run", flatpakAppID}, Kind: "flatpak"}

	got, err := FindLauncher(flatpakDataDir(t), "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestFindLauncherFlatpakDataDirWinsOverPortableNativeAndWellKnown(t *testing.T) {
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "flatpak")
	writeFakeExe(t, pathDir, "prismlauncher")
	dataDir := flatpakDataDir(t)
	writeFakeExe(t, dataDir, "prismlauncher")
	setWellKnown(t, []string{writeFakeExe(t, t.TempDir(), "prismlauncher")})
	want := Launcher{Exe: "flatpak", Args: []string{"run", flatpakAppID}, Kind: "flatpak"}

	got, err := FindLauncher(dataDir, "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestFindLauncherFlatpakDataDirWithoutFlatpakIsNotFoundEvenIfOthersExist(t *testing.T) {
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "prismlauncher")
	dataDir := flatpakDataDir(t)
	writeFakeExe(t, dataDir, "prismlauncher")
	setWellKnown(t, []string{writeFakeExe(t, t.TempDir(), "prismlauncher")})

	_, err := FindLauncher(dataDir, "")

	if !errors.Is(err, ErrLauncherNotFound) {
		t.Fatalf("err = %v, want ErrLauncherNotFound", err)
	}
}

func TestFindLauncherAppIDDirWithoutTrailingSeparatorIsNotFlatpak(t *testing.T) {
	// The marker requires a "/" after the app id; the app dir itself does not match.
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "flatpak")
	dataDir := filepath.Join(t.TempDir(), ".var", "app", flatpakAppID)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := Launcher{Exe: writeFakeExe(t, dataDir, "prismlauncher"), Kind: "portable"}

	got, err := FindLauncher(dataDir, "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

// ---- C3 ----

func TestFindLauncherPortableLowercaseInDataDir(t *testing.T) {
	isolate(t)
	dataDir := t.TempDir()
	want := Launcher{Exe: writeFakeExe(t, dataDir, "prismlauncher"), Kind: "portable"}

	got, err := FindLauncher(dataDir, "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestFindLauncherPortableCapitalizedInDataDir(t *testing.T) {
	isolate(t)
	dataDir := t.TempDir()
	writeFakeExe(t, dataDir, "PrismLauncher")
	want := Launcher{Exe: filepath.Join(dataDir, exeName("PrismLauncher")), Kind: "portable"}
	if caseInsensitiveFS(t, t.TempDir()) {
		// "prismlauncher" is checked first and names the same file here.
		want.Exe = filepath.Join(dataDir, exeName("prismlauncher"))
	}

	got, err := FindLauncher(dataDir, "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestFindLauncherPortablePrefersLowercase(t *testing.T) {
	isolate(t)
	dataDir := t.TempDir()
	writeFakeExe(t, dataDir, "PrismLauncher")
	want := Launcher{Exe: writeFakeExe(t, dataDir, "prismlauncher"), Kind: "portable"}

	got, err := FindLauncher(dataDir, "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestFindLauncherPortableWinsOverPathAndWellKnown(t *testing.T) {
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "prismlauncher")
	setWellKnown(t, []string{writeFakeExe(t, t.TempDir(), "prismlauncher")})
	dataDir := t.TempDir()
	want := Launcher{Exe: writeFakeExe(t, dataDir, "prismlauncher"), Kind: "portable"}

	got, err := FindLauncher(dataDir, "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestFindLauncherPortableDirectoryIsSkipped(t *testing.T) {
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "prismlauncher")
	dataDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dataDir, exeName("prismlauncher")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dataDir, exeName("PrismLauncher")), 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		t.Fatal(err)
	}
	inPath, err := exec.LookPath("prismlauncher")
	if err != nil {
		t.Fatal(err)
	}
	want := Launcher{Exe: inPath, Kind: "native"}

	got, err := FindLauncher(dataDir, "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

// ---- C4 ----

func TestFindLauncherNativeFromPathLowercase(t *testing.T) {
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "prismlauncher")
	inPath, err := exec.LookPath("prismlauncher")
	if err != nil {
		t.Fatal(err)
	}
	want := Launcher{Exe: inPath, Kind: "native"}

	got, err := FindLauncher(t.TempDir(), "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestFindLauncherNativeFromPathCapitalized(t *testing.T) {
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "PrismLauncher")
	lookup := "PrismLauncher"
	if caseInsensitiveFS(t, t.TempDir()) {
		// "prismlauncher" is looked up first and finds the same file here.
		lookup = "prismlauncher"
	}
	inPath, err := exec.LookPath(lookup)
	if err != nil {
		t.Fatal(err)
	}
	want := Launcher{Exe: inPath, Kind: "native"}

	got, err := FindLauncher(t.TempDir(), "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestFindLauncherNativePrefersLowercaseInPath(t *testing.T) {
	lowerDir := t.TempDir()
	upperDir := t.TempDir()
	setWellKnown(t, nil)
	// PrismLauncher comes first in PATH, but prismlauncher is looked up first.
	t.Setenv("PATH", upperDir+string(os.PathListSeparator)+lowerDir)
	writeFakeExe(t, upperDir, "PrismLauncher")
	writeFakeExe(t, lowerDir, "prismlauncher")
	inPath, err := exec.LookPath("prismlauncher")
	if err != nil {
		t.Fatal(err)
	}
	want := Launcher{Exe: inPath, Kind: "native"}

	got, err := FindLauncher(t.TempDir(), "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestFindLauncherNativePathWinsOverWellKnown(t *testing.T) {
	pathDir := isolate(t)
	writeFakeExe(t, pathDir, "prismlauncher")
	setWellKnown(t, []string{writeFakeExe(t, t.TempDir(), "prismlauncher")})
	inPath, err := exec.LookPath("prismlauncher")
	if err != nil {
		t.Fatal(err)
	}
	want := Launcher{Exe: inPath, Kind: "native"}

	got, err := FindLauncher(t.TempDir(), "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

// ---- C5 ----

func TestFindLauncherWellKnownFirstRegularFileWins(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	missing := filepath.Join(base, "missing", "prismlauncher")
	dir := filepath.Join(base, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	firstFile := writeFakeExe(t, t.TempDir(), "prismlauncher")
	secondFile := writeFakeExe(t, t.TempDir(), "prismlauncher")
	setWellKnown(t, []string{missing, dir, firstFile, secondFile})
	want := Launcher{Exe: firstFile, Kind: "native"}

	got, err := FindLauncher(t.TempDir(), "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestFindLauncherWellKnownAllMissingIsNotFound(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	dir := filepath.Join(base, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	setWellKnown(t, []string{filepath.Join(base, "a"), dir, filepath.Join(base, "b")})

	_, err := FindLauncher(t.TempDir(), "")

	if !errors.Is(err, ErrLauncherNotFound) {
		t.Fatalf("err = %v, want ErrLauncherNotFound", err)
	}
}

func TestFindLauncherWellKnownEvaluatedAtCallTime(t *testing.T) {
	isolate(t)
	candidate := filepath.Join(t.TempDir(), exeName("prismlauncher"))
	setWellKnown(t, []string{candidate})
	writeFakeExe(t, filepath.Dir(candidate), "prismlauncher")
	want := Launcher{Exe: candidate, Kind: "native"}

	got, err := FindLauncher(t.TempDir(), "")

	if err != nil || !sameLauncher(got, want) {
		t.Fatalf("FindLauncher = %+v, %v; want %+v, nil", got, err, want)
	}
}

// setLauncherEnv points the home dir and the windows install roots at fresh temp dirs.
func setLauncherEnv(t *testing.T, localAppData, programFiles string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", localAppData)
	t.Setenv("ProgramFiles", programFiles)
	return home
}

// defaultWellKnownFor is the C5 list for this OS, written out from the spec.
func defaultWellKnownFor(home, localAppData, programFiles string) []string {
	switch runtime.GOOS {
	case "windows":
		var out []string
		if localAppData != "" {
			out = append(out, filepath.Join(localAppData, "Programs", "PrismLauncher", "prismlauncher.exe"))
		}
		if programFiles != "" {
			out = append(out, filepath.Join(programFiles, "PrismLauncher", "prismlauncher.exe"))
		}
		return out
	case "darwin":
		return []string{
			"/Applications/Prism Launcher.app/Contents/MacOS/prismlauncher",
			filepath.Join(home, "Applications", "Prism Launcher.app", "Contents", "MacOS", "prismlauncher"),
		}
	default:
		return []string{
			"/usr/bin/prismlauncher",
			"/usr/local/bin/prismlauncher",
			filepath.Join(home, ".local", "bin", "prismlauncher"),
		}
	}
}

func TestWellKnownLaunchersDefaultListForThisOS(t *testing.T) {
	localAppData, programFiles := t.TempDir(), t.TempDir()
	home := setLauncherEnv(t, localAppData, programFiles)
	want := defaultWellKnownFor(home, localAppData, programFiles)

	got := wellKnownLaunchers()

	if !slices.Equal(got, want) {
		t.Fatalf("wellKnownLaunchers() = %q, want %q", got, want)
	}
}

func TestWellKnownLaunchersOmitsEmptyLocalAppData(t *testing.T) {
	programFiles := t.TempDir()
	home := setLauncherEnv(t, "", programFiles)
	want := defaultWellKnownFor(home, "", programFiles)

	got := wellKnownLaunchers()

	if !slices.Equal(got, want) {
		t.Fatalf("wellKnownLaunchers() = %q, want %q", got, want)
	}
}

func TestWellKnownLaunchersOmitsEmptyProgramFiles(t *testing.T) {
	localAppData := t.TempDir()
	home := setLauncherEnv(t, localAppData, "")
	want := defaultWellKnownFor(home, localAppData, "")

	got := wellKnownLaunchers()

	if !slices.Equal(got, want) {
		t.Fatalf("wellKnownLaunchers() = %q, want %q", got, want)
	}
}

func TestFindLauncherNothingAnywhereIsNotFound(t *testing.T) {
	isolate(t)

	_, err := FindLauncher(t.TempDir(), "")

	if !errors.Is(err, ErrLauncherNotFound) {
		t.Fatalf("err = %v, want ErrLauncherNotFound", err)
	}
}

// ---- C6 ----

func TestLaunchCommandNativeWithoutServer(t *testing.T) {
	exe := filepath.Join("opt", "prism", "prismlauncher")
	dataDir := filepath.Join("home", "u", "PrismLauncher")
	inst := Instance{Dir: filepath.Join(dataDir, "instances", "GTNH 2.7")}
	want := []string{"-d", dataDir, "-l", "GTNH 2.7"}

	name, args := LaunchCommand(Launcher{Exe: exe, Kind: "native"}, dataDir, inst, "")

	if name != exe || !slices.Equal(args, want) {
		t.Fatalf("LaunchCommand = %q %q, want %q %q", name, args, exe, want)
	}
}

func TestLaunchCommandNativeWithServer(t *testing.T) {
	exe := filepath.Join("opt", "prism", "prismlauncher")
	dataDir := filepath.Join("home", "u", "PrismLauncher")
	inst := Instance{Dir: filepath.Join(dataDir, "instances", "GTNH")}
	want := []string{"-d", dataDir, "-l", "GTNH", "-s", "mc.example.org:25565"}

	name, args := LaunchCommand(Launcher{Exe: exe, Kind: "native"}, dataDir, inst, "mc.example.org:25565")

	if name != exe || !slices.Equal(args, want) {
		t.Fatalf("LaunchCommand = %q %q, want %q %q", name, args, exe, want)
	}
}

func TestLaunchCommandCustomPrependsLauncherArgs(t *testing.T) {
	exe := filepath.Join("bin", "prism")
	dataDir := filepath.Join("data")
	inst := Instance{Dir: filepath.Join("data", "instances", "pack")}
	want := []string{"--x", "y", "-d", dataDir, "-l", "pack", "-s", "srv"}

	name, args := LaunchCommand(Launcher{Exe: exe, Args: []string{"--x", "y"}, Kind: "custom"}, dataDir, inst, "srv")

	if name != exe || !slices.Equal(args, want) {
		t.Fatalf("LaunchCommand = %q %q, want %q %q", name, args, exe, want)
	}
}

func TestLaunchCommandPortableIncludesDataDir(t *testing.T) {
	dataDir := filepath.Join("portable", "Prism")
	exe := filepath.Join(dataDir, "prismlauncher")
	inst := Instance{Dir: filepath.Join(dataDir, "instances", "pack")}
	want := []string{"-d", dataDir, "-l", "pack"}

	name, args := LaunchCommand(Launcher{Exe: exe, Kind: "portable"}, dataDir, inst, "")

	if name != exe || !slices.Equal(args, want) {
		t.Fatalf("LaunchCommand = %q %q, want %q %q", name, args, exe, want)
	}
}

func TestLaunchCommandFlatpakWithoutServerOmitsDataDir(t *testing.T) {
	dataDir := filepath.Join("home", ".var", "app", flatpakAppID, "data", "PrismLauncher")
	inst := Instance{Dir: filepath.Join(dataDir, "instances", "GTNH")}
	l := Launcher{Exe: "flatpak", Args: []string{"run", flatpakAppID}, Kind: "flatpak"}
	want := []string{"run", flatpakAppID, "-l", "GTNH"}

	name, args := LaunchCommand(l, dataDir, inst, "")

	if name != "flatpak" || !slices.Equal(args, want) {
		t.Fatalf("LaunchCommand = %q %q, want %q %q", name, args, "flatpak", want)
	}
}

func TestLaunchCommandFlatpakWithServerOmitsDataDir(t *testing.T) {
	dataDir := filepath.Join("home", ".var", "app", flatpakAppID, "data", "PrismLauncher")
	inst := Instance{Dir: filepath.Join(dataDir, "instances", "GTNH")}
	l := Launcher{Exe: "flatpak", Args: []string{"run", flatpakAppID}, Kind: "flatpak"}
	want := []string{"run", flatpakAppID, "-l", "GTNH", "-s", "play.gtnh.net"}

	name, args := LaunchCommand(l, dataDir, inst, "play.gtnh.net")

	if name != "flatpak" || !slices.Equal(args, want) {
		t.Fatalf("LaunchCommand = %q %q, want %q %q", name, args, "flatpak", want)
	}
}

func TestLaunchCommandInstanceDirTrailingSeparatorYieldsFolderName(t *testing.T) {
	dataDir := filepath.Join("d")
	inst := Instance{Dir: filepath.Join("d", "instances", "GTNH") + string(filepath.Separator)}
	want := []string{"-d", dataDir, "-l", "GTNH"}

	name, args := LaunchCommand(Launcher{Exe: "p", Kind: "native"}, dataDir, inst, "")

	if name != "p" || !slices.Equal(args, want) {
		t.Fatalf("LaunchCommand = %q %q, want %q %q", name, args, "p", want)
	}
}

func TestLaunchCommandDataDirPassedVerbatim(t *testing.T) {
	sep := string(filepath.Separator)
	dataDir := "some" + sep + ".." + sep + "data" + sep
	inst := Instance{Dir: filepath.Join("x", "inst")}
	want := []string{"-d", dataDir, "-l", "inst"}

	name, args := LaunchCommand(Launcher{Exe: "p", Kind: "custom"}, dataDir, inst, "")

	if name != "p" || !slices.Equal(args, want) {
		t.Fatalf("LaunchCommand = %q %q, want %q %q", name, args, "p", want)
	}
}

func TestLaunchCommandArgsDoNotAliasLauncherArgs(t *testing.T) {
	base := make([]string, 2, 32) // spare capacity: a plain append would share it
	base[0], base[1] = "run", flatpakAppID
	l := Launcher{Exe: "flatpak", Args: base, Kind: "flatpak"}
	inst := Instance{Dir: filepath.Join("d", "instances", "A")}

	_, first := LaunchCommand(l, "d", inst, "first-server")
	_, second := LaunchCommand(l, "d", inst, "")
	second[0] = "mutated"

	if !slices.Equal(l.Args, []string{"run", flatpakAppID}) {
		t.Errorf("l.Args = %q after mutating the returned slice, want unchanged", l.Args)
	}
	if !slices.Equal(first, []string{"run", flatpakAppID, "-l", "A", "-s", "first-server"}) {
		t.Errorf("first args = %q, clobbered by a later call", first)
	}
}

// ---- C7 ----

func TestLaunchStartsDetachedHelperAndReturnsNil(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	l := Launcher{Exe: os.Args[0], Args: []string{"-test.run=TestHelperProcess", "--"}, Kind: "custom"}
	dataDir := t.TempDir()
	inst := Instance{Dir: filepath.Join(dataDir, "instances", "GTNH")}

	start := time.Now()
	err := Launch(l, dataDir, inst, "")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Launch = %v, want nil", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Launch took %v, want it to return right after Start", elapsed)
	}
}

func TestLaunchMissingExecutableReturnsWrappedStartError(t *testing.T) {
	exe := filepath.Join(t.TempDir(), exeName("no-such-prism"))
	dataDir := t.TempDir()
	inst := Instance{Dir: filepath.Join(dataDir, "instances", "GTNH")}

	err := Launch(Launcher{Exe: exe, Kind: "custom"}, dataDir, inst, "")

	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Launch = %v, want an error wrapping fs.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), exe) {
		t.Fatalf("err %q does not contain exe %q", err.Error(), exe)
	}
}
