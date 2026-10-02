package prism

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Settings are the per-instance launch overrides Prism stores in instance.cfg.
// Each Override* flag gates whether Prism honours the values that follow it.
type Settings struct {
	OverrideMemory bool
	MinMemMB       int // MinMemAlloc, in megabytes
	MaxMemMB       int // MaxMemAlloc, in megabytes

	OverrideJavaArgs bool
	JvmArgs          string

	OverrideJavaLocation bool
	JavaPath             string

	OverrideWindow bool
	WinWidth       int // MinecraftWinWidth
	WinHeight      int // MinecraftWinHeight
}

// ReadSettings loads the override settings from <dir>/instance.cfg. A missing file
// is an error; missing or malformed keys read as zero values. Booleans are true only
// for the exact lowercase "true" Prism writes.
func ReadSettings(dir string) (Settings, error) {
	cfg := filepath.Join(dir, "instance.cfg")
	// readKey cannot tell a missing file from a missing key, so check the file first.
	if _, err := os.Stat(cfg); err != nil {
		return Settings{}, err
	}
	str := func(key string) string {
		v, _ := readKey(cfg, key)
		return v
	}
	num := func(key string) int {
		n, err := strconv.Atoi(str(key))
		if err != nil {
			return 0
		}
		return n
	}
	flag := func(key string) bool { return str(key) == "true" }
	return Settings{
		OverrideMemory:       flag("OverrideMemory"),
		MinMemMB:             num("MinMemAlloc"),
		MaxMemMB:             num("MaxMemAlloc"),
		OverrideJavaArgs:     flag("OverrideJavaArgs"),
		JvmArgs:              str("JvmArgs"),
		OverrideJavaLocation: flag("OverrideJavaLocation"),
		JavaPath:             str("JavaPath"),
		OverrideWindow:       flag("OverrideWindow"),
		WinWidth:             num("MinecraftWinWidth"),
		WinHeight:            num("MinecraftWinHeight"),
	}, nil
}

// WriteSettings atomically rewrites the override keys in <dir>/instance.cfg in place,
// keeping every other line and the file's line endings byte-for-byte. Keys absent from
// the file are inserted under [General] (or appended when there is none). It fails
// without creating anything if instance.cfg does not exist.
func WriteSettings(dir string, s Settings) error {
	cfg := filepath.Join(dir, "instance.cfg")
	info, err := os.Stat(cfg)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		return err
	}
	tmp := cfg + ".gtnh-tmp"
	if err := os.WriteFile(tmp, []byte(applySettings(string(data), s)), info.Mode().Perm()); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, cfg); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

type cfgEntry struct{ key, value string }

// settingsEntries lists the instance.cfg keys for s in the order missing keys are inserted.
func settingsEntries(s Settings) []cfgEntry {
	b, n := strconv.FormatBool, strconv.Itoa
	return []cfgEntry{
		{"OverrideMemory", b(s.OverrideMemory)},
		{"MinMemAlloc", n(s.MinMemMB)},
		{"MaxMemAlloc", n(s.MaxMemMB)},
		{"OverrideJavaArgs", b(s.OverrideJavaArgs)},
		{"JvmArgs", s.JvmArgs},
		{"OverrideJavaLocation", b(s.OverrideJavaLocation)},
		{"JavaPath", s.JavaPath},
		{"OverrideWindow", b(s.OverrideWindow)},
		{"MinecraftWinWidth", n(s.WinWidth)},
		{"MinecraftWinHeight", n(s.WinHeight)},
	}
}

// applySettings returns cfg with each settings key replaced on its first line, and the
// absent ones inserted after [General] (or appended) using the first line's ending.
func applySettings(cfg string, s Settings) string {
	lines := strings.SplitAfter(cfg, "\n")
	eol := "\n"
	if strings.HasSuffix(lines[0], "\r\n") {
		eol = "\r\n"
	}
	insert := ""
	for _, e := range settingsEntries(s) {
		if i := firstKeyLine(lines, e.key); i >= 0 {
			body := lineBody(lines[i])
			lines[i] = e.key + "=" + e.value + lines[i][len(body):]
			continue
		}
		insert += e.key + "=" + e.value + eol
	}
	if insert == "" {
		return strings.Join(lines, "")
	}
	for i, l := range lines {
		if lineBody(l) != "[General]" {
			continue
		}
		if !strings.HasSuffix(l, "\n") {
			l += eol
		}
		lines[i] = l + insert
		return strings.Join(lines, "")
	}
	out := strings.Join(lines, "")
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += eol
	}
	return out + insert
}

// firstKeyLine returns the index of the first line (any section) whose key is key, or -1.
func firstKeyLine(lines []string, key string) int {
	for i, l := range lines {
		if k, _, ok := strings.Cut(lineBody(l), "="); ok && strings.TrimSpace(k) == key {
			return i
		}
	}
	return -1
}

// lineBody strips a single trailing "\r\n" or "\n" from line.
func lineBody(line string) string {
	if b, ok := strings.CutSuffix(line, "\r\n"); ok {
		return b
	}
	return strings.TrimSuffix(line, "\n")
}
