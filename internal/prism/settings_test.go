package prism

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// settingsDirWithCfg returns a fresh temp dir containing instance.cfg with the given bytes.
func settingsDirWithCfg(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "instance.cfg"), []byte(content), 0o644); err != nil {
		t.Fatalf("write instance.cfg: %v", err)
	}
	return dir
}

// settingsReadCfg returns the raw bytes of <dir>/instance.cfg as a string.
func settingsReadCfg(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "instance.cfg"))
	if err != nil {
		t.Fatalf("read instance.cfg: %v", err)
	}
	return string(b)
}

func settingsDirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func settingsFull() Settings {
	return Settings{
		OverrideMemory:       true,
		MinMemMB:             1024,
		MaxMemMB:             4096,
		OverrideJavaArgs:     true,
		JvmArgs:              "-XX:+UseG1GC -Dfoo=bar",
		OverrideJavaLocation: true,
		JavaPath:             "/opt/java/bin/java",
		OverrideWindow:       true,
		WinWidth:             854,
		WinHeight:            480,
	}
}

// C1
func TestReadSettingsParsesAllKeysAcrossSections(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\n"+
		"InstanceType=OneSix\n"+
		"OverrideMemory=true\n"+
		"MinMemAlloc=1024\n"+
		"MaxMemAlloc=4096\n"+
		"OverrideJavaArgs=true\n"+
		"JvmArgs=  -XX:+UseG1GC -Dfoo=bar  \n"+
		"[Other]\n"+
		"OverrideJavaLocation=true\n"+
		"JavaPath=/opt/java/bin/java\n"+
		"OverrideWindow=true\n"+
		"MinecraftWinWidth=854\n"+
		"MinecraftWinHeight=480\n")

	got, err := ReadSettings(dir)

	if err != nil {
		t.Fatalf("ReadSettings: %v", err)
	}
	if got != settingsFull() {
		t.Fatalf("ReadSettings = %+v, want %+v", got, settingsFull())
	}
}

// C1
func TestReadSettingsHandlesCRLFLines(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\r\nOverrideMemory=true\r\nMaxMemAlloc=2048\r\nJavaPath=C:\\java\\bin\\javaw.exe\r\n")

	got, err := ReadSettings(dir)

	if err != nil {
		t.Fatalf("ReadSettings: %v", err)
	}
	want := Settings{OverrideMemory: true, MaxMemMB: 2048, JavaPath: `C:\java\bin\javaw.exe`}
	if got != want {
		t.Fatalf("ReadSettings = %+v, want %+v", got, want)
	}
}

// C1
func TestReadSettingsMissingFileIsError(t *testing.T) {
	dir := t.TempDir()

	_, err := ReadSettings(dir)

	if err == nil {
		t.Fatal("ReadSettings on dir without instance.cfg: want error, got nil")
	}
}

// C1
func TestReadSettingsMissingKeysAreZero(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\nname=GTNH\nInstanceType=OneSix\n")

	got, err := ReadSettings(dir)

	if err != nil {
		t.Fatalf("ReadSettings: %v", err)
	}
	if got != (Settings{}) {
		t.Fatalf("ReadSettings = %+v, want zero Settings", got)
	}
}

// C1, C5
func TestReadSettingsBoolsAreTrueOnlyForExactLowercaseTrue(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\n"+
		"OverrideMemory=True\n"+
		"OverrideJavaArgs=1\n"+
		"OverrideJavaLocation=yes\n"+
		"OverrideWindow=TRUE\n")

	got, err := ReadSettings(dir)

	if err != nil {
		t.Fatalf("ReadSettings: %v", err)
	}
	if got != (Settings{}) {
		t.Fatalf("ReadSettings = %+v, want all bools false", got)
	}
}

// C1
func TestReadSettingsUnparsableIntsAreZero(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\n"+
		"MinMemAlloc=abc\n"+
		"MaxMemAlloc=4096MB\n"+
		"MinecraftWinWidth=\n"+
		"MinecraftWinHeight=12.5\n")

	got, err := ReadSettings(dir)

	if err != nil {
		t.Fatalf("ReadSettings: %v", err)
	}
	if got != (Settings{}) {
		t.Fatalf("ReadSettings = %+v, want all ints 0", got)
	}
}

// C1
func TestReadSettingsNegativeIntParsesViaAtoi(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\nMinMemAlloc=-5\nMaxMemAlloc=0\n")

	got, err := ReadSettings(dir)

	if err != nil {
		t.Fatalf("ReadSettings: %v", err)
	}
	want := Settings{MinMemMB: -5}
	if got != want {
		t.Fatalf("ReadSettings = %+v, want %+v", got, want)
	}
}

// C2, C4
func TestWriteSettingsReplacesInPlaceAndKeepsOtherLines(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\n"+
		"name=GT New Horizons\n"+
		"OverrideMemory=false\n"+
		"MinMemAlloc=512\n"+
		"lastLaunchTime=1700000000000\n"+
		"MaxMemAlloc=1024\n"+
		"OverrideJavaArgs=false\n"+
		"JvmArgs=\n"+
		"SomeUnknownKey = keep me \n"+
		"OverrideJavaLocation=false\n"+
		"JavaPath=\n"+
		"InstanceType=OneSix\n"+
		"OverrideWindow=false\n"+
		"MinecraftWinWidth=0\n"+
		"MinecraftWinHeight=0\n")

	if err := WriteSettings(dir, settingsFull()); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "[General]\n" +
		"name=GT New Horizons\n" +
		"OverrideMemory=true\n" +
		"MinMemAlloc=1024\n" +
		"lastLaunchTime=1700000000000\n" +
		"MaxMemAlloc=4096\n" +
		"OverrideJavaArgs=true\n" +
		"JvmArgs=-XX:+UseG1GC -Dfoo=bar\n" +
		"SomeUnknownKey = keep me \n" +
		"OverrideJavaLocation=true\n" +
		"JavaPath=/opt/java/bin/java\n" +
		"InstanceType=OneSix\n" +
		"OverrideWindow=true\n" +
		"MinecraftWinWidth=854\n" +
		"MinecraftWinHeight=480\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2
func TestWriteSettingsInsertsMissingKeysAfterGeneralInStructOrder(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\nname=GTNH\nInstanceType=OneSix\n")

	if err := WriteSettings(dir, settingsFull()); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "[General]\n" +
		"OverrideMemory=true\n" +
		"MinMemAlloc=1024\n" +
		"MaxMemAlloc=4096\n" +
		"OverrideJavaArgs=true\n" +
		"JvmArgs=-XX:+UseG1GC -Dfoo=bar\n" +
		"OverrideJavaLocation=true\n" +
		"JavaPath=/opt/java/bin/java\n" +
		"OverrideWindow=true\n" +
		"MinecraftWinWidth=854\n" +
		"MinecraftWinHeight=480\n" +
		"name=GTNH\n" +
		"InstanceType=OneSix\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2
func TestWriteSettingsInsertsAfterGeneralEvenWhenNotFirstLine(t *testing.T) {
	dir := settingsDirWithCfg(t, "[UI]\nfoo=bar\n[General]\nname=GTNH\n")

	if err := WriteSettings(dir, Settings{}); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "[UI]\nfoo=bar\n[General]\n" +
		"OverrideMemory=false\n" +
		"MinMemAlloc=0\n" +
		"MaxMemAlloc=0\n" +
		"OverrideJavaArgs=false\n" +
		"JvmArgs=\n" +
		"OverrideJavaLocation=false\n" +
		"JavaPath=\n" +
		"OverrideWindow=false\n" +
		"MinecraftWinWidth=0\n" +
		"MinecraftWinHeight=0\n" +
		"name=GTNH\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2: present keys replaced in place, only absent ones inserted after [General].
func TestWriteSettingsMixesReplaceAndInsert(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\nname=GTNH\nMaxMemAlloc=1\n[Other]\nJavaPath=old\n")

	if err := WriteSettings(dir, settingsFull()); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "[General]\n" +
		"OverrideMemory=true\n" +
		"MinMemAlloc=1024\n" +
		"OverrideJavaArgs=true\n" +
		"JvmArgs=-XX:+UseG1GC -Dfoo=bar\n" +
		"OverrideJavaLocation=true\n" +
		"OverrideWindow=true\n" +
		"MinecraftWinWidth=854\n" +
		"MinecraftWinHeight=480\n" +
		"name=GTNH\n" +
		"MaxMemAlloc=4096\n" +
		"[Other]\n" +
		"JavaPath=/opt/java/bin/java\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2: key match trims whitespace around the key; similar-prefixed keys don't match.
func TestWriteSettingsMatchesKeyAfterTrimSpaceOnly(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\n"+
		"MaxMemAllocX=7\n"+
		"  MaxMemAlloc  = 1\n"+
		"MinMemAlloc\n"+
		"OverrideMemory=true\n"+
		"MinMemAlloc=3\n"+
		"OverrideJavaArgs=false\n"+
		"JvmArgs=a\n"+
		"OverrideJavaLocation=false\n"+
		"JavaPath=b\n"+
		"OverrideWindow=false\n"+
		"MinecraftWinWidth=1\n"+
		"MinecraftWinHeight=2\n")

	s := Settings{OverrideMemory: true, MinMemMB: 4, MaxMemMB: 2, JvmArgs: "a", JavaPath: "b", WinWidth: 1, WinHeight: 2}
	if err := WriteSettings(dir, s); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "[General]\n" +
		"MaxMemAllocX=7\n" +
		"MaxMemAlloc=2\n" +
		"MinMemAlloc\n" +
		"OverrideMemory=true\n" +
		"MinMemAlloc=4\n" +
		"OverrideJavaArgs=false\n" +
		"JvmArgs=a\n" +
		"OverrideJavaLocation=false\n" +
		"JavaPath=b\n" +
		"OverrideWindow=false\n" +
		"MinecraftWinWidth=1\n" +
		"MinecraftWinHeight=2\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2, C4: CRLF file stays CRLF; inserted lines use \r\n.
func TestWriteSettingsCRLFFileInsertsWithCRLF(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\r\nname=GTNH\r\nMaxMemAlloc=1\r\nInstanceType=OneSix\r\n")

	if err := WriteSettings(dir, settingsFull()); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "[General]\r\n" +
		"OverrideMemory=true\r\n" +
		"MinMemAlloc=1024\r\n" +
		"OverrideJavaArgs=true\r\n" +
		"JvmArgs=-XX:+UseG1GC -Dfoo=bar\r\n" +
		"OverrideJavaLocation=true\r\n" +
		"JavaPath=/opt/java/bin/java\r\n" +
		"OverrideWindow=true\r\n" +
		"MinecraftWinWidth=854\r\n" +
		"MinecraftWinHeight=480\r\n" +
		"name=GTNH\r\n" +
		"MaxMemAlloc=4096\r\n" +
		"InstanceType=OneSix\r\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2: dominant eol comes from the first line only; replaced lines keep their own ending.
func TestWriteSettingsDominantEOLIsFromFirstLine(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\nMaxMemAlloc=1\r\nname=GTNH\r\n")

	if err := WriteSettings(dir, Settings{MaxMemMB: 9}); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "[General]\n" +
		"OverrideMemory=false\n" +
		"MinMemAlloc=0\n" +
		"OverrideJavaArgs=false\n" +
		"JvmArgs=\n" +
		"OverrideJavaLocation=false\n" +
		"JavaPath=\n" +
		"OverrideWindow=false\n" +
		"MinecraftWinWidth=0\n" +
		"MinecraftWinHeight=0\n" +
		"MaxMemAlloc=9\r\n" +
		"name=GTNH\r\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2: [General] as the last line without newline gets a dominant eol first.
func TestWriteSettingsGeneralAsLastLineWithoutNewline(t *testing.T) {
	dir := settingsDirWithCfg(t, "name=GTNH\r\n[General]")

	if err := WriteSettings(dir, Settings{}); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "name=GTNH\r\n[General]\r\n" +
		"OverrideMemory=false\r\n" +
		"MinMemAlloc=0\r\n" +
		"MaxMemAlloc=0\r\n" +
		"OverrideJavaArgs=false\r\n" +
		"JvmArgs=\r\n" +
		"OverrideJavaLocation=false\r\n" +
		"JavaPath=\r\n" +
		"OverrideWindow=false\r\n" +
		"MinecraftWinWidth=0\r\n" +
		"MinecraftWinHeight=0\r\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2: no [General] and no trailing newline -> newline added, then keys appended.
func TestWriteSettingsNoGeneralAppendsAfterAddingNewline(t *testing.T) {
	dir := settingsDirWithCfg(t, "name=GTNH\nInstanceType=OneSix")

	if err := WriteSettings(dir, settingsFull()); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "name=GTNH\n" +
		"InstanceType=OneSix\n" +
		"OverrideMemory=true\n" +
		"MinMemAlloc=1024\n" +
		"MaxMemAlloc=4096\n" +
		"OverrideJavaArgs=true\n" +
		"JvmArgs=-XX:+UseG1GC -Dfoo=bar\n" +
		"OverrideJavaLocation=true\n" +
		"JavaPath=/opt/java/bin/java\n" +
		"OverrideWindow=true\n" +
		"MinecraftWinWidth=854\n" +
		"MinecraftWinHeight=480\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2: no [General] with trailing newline -> appended directly.
func TestWriteSettingsNoGeneralWithTrailingNewlineAppends(t *testing.T) {
	dir := settingsDirWithCfg(t, "name=GTNH\r\n")

	if err := WriteSettings(dir, Settings{}); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "name=GTNH\r\n" +
		"OverrideMemory=false\r\n" +
		"MinMemAlloc=0\r\n" +
		"MaxMemAlloc=0\r\n" +
		"OverrideJavaArgs=false\r\n" +
		"JvmArgs=\r\n" +
		"OverrideJavaLocation=false\r\n" +
		"JavaPath=\r\n" +
		"OverrideWindow=false\r\n" +
		"MinecraftWinWidth=0\r\n" +
		"MinecraftWinHeight=0\r\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2: empty file -> keys appended with \n and no leading blank line.
func TestWriteSettingsEmptyFileGetsAllKeys(t *testing.T) {
	dir := settingsDirWithCfg(t, "")

	if err := WriteSettings(dir, Settings{}); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "OverrideMemory=false\n" +
		"MinMemAlloc=0\n" +
		"MaxMemAlloc=0\n" +
		"OverrideJavaArgs=false\n" +
		"JvmArgs=\n" +
		"OverrideJavaLocation=false\n" +
		"JavaPath=\n" +
		"OverrideWindow=false\n" +
		"MinecraftWinWidth=0\n" +
		"MinecraftWinHeight=0\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2: a replaced key on a last line without newline stays without newline.
func TestWriteSettingsReplacedLastLineKeepsNoNewline(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\n"+
		"OverrideMemory=false\n"+
		"MinMemAlloc=0\n"+
		"MaxMemAlloc=0\n"+
		"OverrideJavaArgs=false\n"+
		"JvmArgs=\n"+
		"OverrideJavaLocation=false\n"+
		"JavaPath=\n"+
		"OverrideWindow=false\n"+
		"MinecraftWinWidth=0\n"+
		"MinecraftWinHeight=0")

	if err := WriteSettings(dir, settingsFull()); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "[General]\n" +
		"OverrideMemory=true\n" +
		"MinMemAlloc=1024\n" +
		"MaxMemAlloc=4096\n" +
		"OverrideJavaArgs=true\n" +
		"JvmArgs=-XX:+UseG1GC -Dfoo=bar\n" +
		"OverrideJavaLocation=true\n" +
		"JavaPath=/opt/java/bin/java\n" +
		"OverrideWindow=true\n" +
		"MinecraftWinWidth=854\n" +
		"MinecraftWinHeight=480"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2, C4, K1: only the first occurrence of a duplicated key is replaced.
func TestWriteSettingsReplacesOnlyFirstDuplicate(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\n"+
		"OverrideMemory=false\n"+
		"MinMemAlloc=0\n"+
		"MaxMemAlloc=1\n"+
		"OverrideJavaArgs=false\n"+
		"JvmArgs=\n"+
		"OverrideJavaLocation=false\n"+
		"JavaPath=\n"+
		"OverrideWindow=false\n"+
		"MinecraftWinWidth=0\n"+
		"MinecraftWinHeight=0\n"+
		"[Other]\n"+
		"MaxMemAlloc=1\n")

	if err := WriteSettings(dir, Settings{MaxMemMB: 2}); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "[General]\n" +
		"OverrideMemory=false\n" +
		"MinMemAlloc=0\n" +
		"MaxMemAlloc=2\n" +
		"OverrideJavaArgs=false\n" +
		"JvmArgs=\n" +
		"OverrideJavaLocation=false\n" +
		"JavaPath=\n" +
		"OverrideWindow=false\n" +
		"MinecraftWinWidth=0\n" +
		"MinecraftWinHeight=0\n" +
		"[Other]\n" +
		"MaxMemAlloc=1\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2, K2
func TestWriteSettingsOverrideWindowTrueWritesTrue(t *testing.T) {
	dir := settingsDirWithCfg(t, "OverrideWindow=false\n")

	if err := WriteSettings(dir, Settings{OverrideWindow: true}); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "OverrideWindow=true\n" +
		"OverrideMemory=false\n" +
		"MinMemAlloc=0\n" +
		"MaxMemAlloc=0\n" +
		"OverrideJavaArgs=false\n" +
		"JvmArgs=\n" +
		"OverrideJavaLocation=false\n" +
		"JavaPath=\n" +
		"MinecraftWinWidth=0\n" +
		"MinecraftWinHeight=0\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C2, K2
func TestWriteSettingsOverrideWindowFalseWritesFalse(t *testing.T) {
	dir := settingsDirWithCfg(t, "OverrideWindow=true\n")

	if err := WriteSettings(dir, Settings{OverrideMemory: true}); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "OverrideWindow=false\n" +
		"OverrideMemory=true\n" +
		"MinMemAlloc=0\n" +
		"MaxMemAlloc=0\n" +
		"OverrideJavaArgs=false\n" +
		"JvmArgs=\n" +
		"OverrideJavaLocation=false\n" +
		"JavaPath=\n" +
		"MinecraftWinWidth=0\n" +
		"MinecraftWinHeight=0\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C4, K2: each bool round-trips independently in both states.
func TestWriteSettingsRoundTripsEachBoolAlone(t *testing.T) {
	cases := map[string]Settings{
		"OverrideMemory":       {OverrideMemory: true},
		"OverrideJavaArgs":     {OverrideJavaArgs: true},
		"OverrideJavaLocation": {OverrideJavaLocation: true},
		"OverrideWindow":       {OverrideWindow: true},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			dir := settingsDirWithCfg(t, "[General]\n")
			if err := WriteSettings(dir, want); err != nil {
				t.Fatalf("WriteSettings: %v", err)
			}
			got, err := ReadSettings(dir)
			if err != nil {
				t.Fatalf("ReadSettings: %v", err)
			}
			if got != want {
				t.Fatalf("round trip = %+v, want %+v", got, want)
			}
		})
	}
}

// C2: values are written even when the matching Override* flag is false.
func TestWriteSettingsWritesValuesWhenOverridesAreFalse(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\n")
	s := Settings{MinMemMB: 512, MaxMemMB: 8192, JvmArgs: "-Xss4M", JavaPath: "java", WinWidth: 1920, WinHeight: 1080}

	if err := WriteSettings(dir, s); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	want := "[General]\n" +
		"OverrideMemory=false\n" +
		"MinMemAlloc=512\n" +
		"MaxMemAlloc=8192\n" +
		"OverrideJavaArgs=false\n" +
		"JvmArgs=-Xss4M\n" +
		"OverrideJavaLocation=false\n" +
		"JavaPath=java\n" +
		"OverrideWindow=false\n" +
		"MinecraftWinWidth=1920\n" +
		"MinecraftWinHeight=1080\n"
	if got := settingsReadCfg(t, dir); got != want {
		t.Fatalf("instance.cfg =\n%q\nwant\n%q", got, want)
	}
}

// C4
func TestWriteSettingsRoundTripAllFieldsSet(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\nname=GTNH\nInstanceType=OneSix\n")

	if err := WriteSettings(dir, settingsFull()); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}
	got, err := ReadSettings(dir)

	if err != nil {
		t.Fatalf("ReadSettings: %v", err)
	}
	if got != settingsFull() {
		t.Fatalf("round trip = %+v, want %+v", got, settingsFull())
	}
}

// C4
func TestWriteSettingsRoundTripAllZero(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\n"+
		"OverrideMemory=true\nMinMemAlloc=1\nMaxMemAlloc=2\nOverrideJavaArgs=true\nJvmArgs=x\n"+
		"OverrideJavaLocation=true\nJavaPath=y\nOverrideWindow=true\nMinecraftWinWidth=3\nMinecraftWinHeight=4\n")

	if err := WriteSettings(dir, Settings{}); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}
	got, err := ReadSettings(dir)

	if err != nil {
		t.Fatalf("ReadSettings: %v", err)
	}
	if got != (Settings{}) {
		t.Fatalf("round trip = %+v, want zero Settings", got)
	}
}

// C3
func TestWriteSettingsLeavesNoTempFile(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\nname=GTNH\n")

	if err := WriteSettings(dir, settingsFull()); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	got := settingsDirNames(t, dir)
	if len(got) != 1 || got[0] != "instance.cfg" {
		t.Fatalf("dir entries = %v, want [instance.cfg]", got)
	}
}

// C3
func TestWriteSettingsKeepsPermissionBits(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\n")
	path := filepath.Join(dir, "instance.cfg")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}

	if err := WriteSettings(dir, settingsFull()); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if after.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("perm after = %v, want %v", after.Mode().Perm(), before.Mode().Perm())
	}
}

// C3
func TestWriteSettingsMissingFileFailsAndCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write other.txt: %v", err)
	}

	err := WriteSettings(dir, settingsFull())

	if err == nil {
		t.Fatal("WriteSettings without instance.cfg: want error, got nil")
	}
	got := settingsDirNames(t, dir)
	if len(got) != 1 || got[0] != "other.txt" {
		t.Fatalf("dir entries = %v, want [other.txt]", got)
	}
}
