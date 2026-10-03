// Package prism finds Prism Launcher installs and the instances inside them.
package prism

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DataDirs returns the Prism data directories that exist on this machine: the standard
// per-OS location, the Linux Flatpak location, and a portable install next to the
// running executable.
func DataDirs() []string {
	var cands []string
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			cands = append(cands, filepath.Join(appdata, "PrismLauncher"))
		}
	case "darwin":
		cands = append(cands, filepath.Join(home, "Library", "Application Support", "PrismLauncher"))
	default:
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		cands = append(cands,
			filepath.Join(data, "PrismLauncher"),
			filepath.Join(home, ".var", "app", "org.prismlauncher.PrismLauncher", "data", "PrismLauncher"))
	}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Dir(exe))
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range cands {
		if seen[c] {
			continue
		}
		seen[c] = true
		if isFile(filepath.Join(c, "prismlauncher.cfg")) {
			out = append(out, c)
		}
	}
	return out
}

// InstancesDir resolves the instances directory of a Prism data dir from the
// InstanceDir setting in prismlauncher.cfg (relative to the data dir, or absolute).
func InstancesDir(dataDir string) string {
	dir := "instances"
	if v, ok := readKey(filepath.Join(dataDir, "prismlauncher.cfg"), "InstanceDir"); ok && v != "" {
		dir = v
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(dataDir, dir)
	}
	return dir
}

// DataDirOf returns the data dir whose instances dir strictly contains inst.Dir,
// falling back to dataDirs[0] ("" when dataDirs is empty).
func DataDirOf(dataDirs []string, inst Instance) string {
	for _, d := range dataDirs {
		rel, err := filepath.Rel(InstancesDir(d), inst.Dir)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return d
		}
	}
	if len(dataDirs) == 0 {
		return ""
	}
	return dataDirs[0]
}

// Instance is one Prism instance directory.
type Instance struct {
	Dir     string // instance root (holds instance.cfg)
	Name    string // display name from instance.cfg
	GameDir string // absolute game dir (".minecraft" or "minecraft")
	GTNH    bool   // looks like a GTNH instance
	// LastLaunch is when Prism last started the instance; zero if never.
	LastLaunch time.Time
}

// ListInstances returns the instances under an instances dir, GTNH ones first.
func ListInstances(instancesDir string) ([]Instance, error) {
	ents, err := os.ReadDir(instancesDir)
	if err != nil {
		return nil, err
	}
	var out []Instance
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		inst, err := LoadInstance(filepath.Join(instancesDir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, inst)
	}
	// GTNH first, then most recently played, then by name.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.GTNH != b.GTNH {
			return a.GTNH
		}
		if !a.LastLaunch.Equal(b.LastLaunch) {
			return a.LastLaunch.After(b.LastLaunch)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out, nil
}

// LoadInstance reads one instance directory.
func LoadInstance(dir string) (Instance, error) {
	cfg := filepath.Join(dir, "instance.cfg")
	if !isFile(cfg) {
		return Instance{}, errors.New("not a Prism instance (no instance.cfg): " + dir)
	}
	name, _ := readKey(cfg, "name")
	if name == "" {
		name = filepath.Base(dir)
	}
	game := filepath.Join(dir, ".minecraft")
	if !isDir(game) && isDir(filepath.Join(dir, "minecraft")) {
		game = filepath.Join(dir, "minecraft")
	}
	inst := Instance{Dir: dir, Name: name, GameDir: game}
	if v, ok := readKey(cfg, "lastLaunchTime"); ok {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil && ms > 0 {
			inst.LastLaunch = time.UnixMilli(ms)
		}
	}
	low := strings.ToLower(name + " " + filepath.Base(dir))
	inst.GTNH = isMC1710(dir) && (strings.Contains(low, "horizons") || strings.Contains(low, "gtnh") ||
		isDir(filepath.Join(dir, ".gtnh-updater")))
	return inst, nil
}

// Components returns the component uids listed in mmc-pack.json.
func Components(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "mmc-pack.json"))
	if err != nil {
		return nil
	}
	var mp struct {
		Components []struct {
			UID     string `json:"uid"`
			Version string `json:"version"`
		} `json:"components"`
	}
	if json.Unmarshal(data, &mp) != nil {
		return nil
	}
	var out []string
	for _, c := range mp.Components {
		out = append(out, c.UID)
	}
	return out
}

func isMC1710(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "mmc-pack.json"))
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte(`"1.7.10"`))
}

// UsesLWJGL3 reports whether the instance runs the Java 17+ (lwjgl3ify) flavor.
func UsesLWJGL3(dir string) bool {
	for _, c := range Components(dir) {
		if c == "org.lwjgl3" || strings.HasPrefix(c, "me.eigenraven.lwjgl3ify") {
			return true
		}
	}
	return false
}

// readKey reads key=value from an INI-style Prism config (any section).
func readKey(name, key string) (string, bool) {
	f, err := os.Open(name)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

// RenameVersion swaps oldVer for newVer in the instance display name, keeping any
// custom text around it (like the server updater does for the MOTD). It is a no-op if
// the name does not contain oldVer. Only the name= line is rewritten; every other
// setting (Java path, memory, JVM args) is preserved byte-for-byte.
func RenameVersion(dir, oldVer, newVer string) (string, bool, error) {
	if oldVer == "" || oldVer == newVer {
		return "", false, nil
	}
	cfg := filepath.Join(dir, "instance.cfg")
	data, err := os.ReadFile(cfg)
	if err != nil {
		return "", false, err
	}
	lines := strings.SplitAfter(string(data), "\n")
	newName := ""
	for i, line := range lines {
		body := strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(body, "name=") || !strings.Contains(body, oldVer) {
			continue
		}
		eol := line[len(body):]
		body = strings.ReplaceAll(body, oldVer, newVer)
		lines[i] = body + eol
		newName = strings.TrimPrefix(body, "name=")
		break
	}
	if newName == "" {
		return "", false, nil
	}
	info, err := os.Stat(cfg)
	if err != nil {
		return "", false, err
	}
	tmp := cfg + ".gtnh-tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "")), info.Mode().Perm()); err != nil {
		return "", false, err
	}
	if err := os.Rename(tmp, cfg); err != nil {
		os.Remove(tmp)
		return "", false, err
	}
	return newName, true, nil
}

// SetName returns cfg (an instance.cfg) with its first name= line set to name, keeping
// line endings and every other line. Without a name= line, one is added after a leading
// [General] line, else at the end.
func SetName(cfg, name string) string {
	lines := strings.SplitAfter(cfg, "\n")
	for i, l := range lines {
		body := strings.TrimRight(l, "\r\n")
		if strings.HasPrefix(body, "name=") {
			lines[i] = "name=" + name + l[len(body):]
			return strings.Join(lines, "")
		}
	}
	line := "name=" + name + "\n"
	if strings.TrimRight(lines[0], "\r\n") == "[General]" {
		if !strings.HasSuffix(lines[0], "\n") {
			lines[0] += "\n"
		}
		return lines[0] + line + strings.Join(lines[1:], "")
	}
	if cfg != "" && !strings.HasSuffix(cfg, "\n") {
		cfg += "\n"
	}
	return cfg + line
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
