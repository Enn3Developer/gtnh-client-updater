package prism

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Launcher is a resolved way to start Prism Launcher.
type Launcher struct {
	Exe  string
	Args []string
	Kind string // "flatpak" | "portable" | "native" | "custom"
}

// ErrLauncherNotFound reports that no Prism Launcher executable could be located.
var ErrLauncherNotFound = errors.New("Prism Launcher executable not found")

// wellKnownLaunchers lists install locations probed last, in search order.
// It is a variable so tests can substitute their own candidates.
var wellKnownLaunchers = func() []string {
	home, homeErr := os.UserHomeDir()
	var out []string
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			out = append(out, filepath.Join(v, "Programs", "PrismLauncher", "prismlauncher.exe"))
		}
		if v := os.Getenv("ProgramFiles"); v != "" {
			out = append(out, filepath.Join(v, "PrismLauncher", "prismlauncher.exe"))
		}
	case "darwin":
		out = append(out, "/Applications/Prism Launcher.app/Contents/MacOS/prismlauncher")
		if homeErr == nil {
			out = append(out, filepath.Join(home, "Applications", "Prism Launcher.app", "Contents", "MacOS", "prismlauncher"))
		}
	default:
		out = append(out, "/usr/bin/prismlauncher", "/usr/local/bin/prismlauncher")
		if homeErr == nil {
			out = append(out, filepath.Join(home, ".local", "bin", "prismlauncher"))
		}
	}
	return out
}

// FindLauncher locates the Prism Launcher executable for dataDir, preferring override.
func FindLauncher(dataDir, override string) (Launcher, error) {
	if override != "" {
		if isFile(override) {
			return Launcher{Exe: override, Kind: "custom"}, nil
		}
		return Launcher{}, fmt.Errorf("%w: %s is not a file", ErrLauncherNotFound, override)
	}

	// A flatpak data dir means a flatpak Prism; nothing else can own it.
	if strings.Contains(filepath.ToSlash(dataDir), "/.var/app/"+flatpakID+"/") {
		if _, err := exec.LookPath("flatpak"); err != nil {
			return Launcher{}, fmt.Errorf("%w: flatpak data dir but flatpak is not installed", ErrLauncherNotFound)
		}
		return Launcher{Exe: "flatpak", Args: []string{"run", flatpakID}, Kind: "flatpak"}, nil
	}

	for _, name := range []string{"prismlauncher", "PrismLauncher"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if p := filepath.Join(dataDir, name); isFile(p) {
			return Launcher{Exe: p, Kind: "portable"}, nil
		}
	}

	for _, name := range []string{"prismlauncher", "PrismLauncher"} {
		if p, err := exec.LookPath(name); err == nil {
			return Launcher{Exe: p, Kind: "native"}, nil
		}
	}

	for _, p := range wellKnownLaunchers() {
		if isFile(p) {
			return Launcher{Exe: p, Kind: "native"}, nil
		}
	}
	return Launcher{}, ErrLauncherNotFound
}

const flatpakID = "org.prismlauncher.PrismLauncher"

// LaunchCommand builds the command line that starts inst through l.
func LaunchCommand(l Launcher, dataDir string, inst Instance, server string) (name string, args []string) {
	args = append([]string{}, l.Args...)
	// The flatpak sandbox already knows its own data dir.
	if l.Kind != "flatpak" {
		args = append(args, "-d", dataDir)
	}
	args = append(args, "-l", filepath.Base(filepath.Clean(inst.Dir)))
	if server != "" {
		args = append(args, "-s", server)
	}
	return l.Exe, args
}

// Launch starts inst through l as a detached process and returns without waiting.
func Launch(l Launcher, dataDir string, inst Instance, server string) error {
	name, args := LaunchCommand(l, dataDir, inst, server)
	cmd := exec.Command(name, args...)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	// The launcher is already running; a failed handle release doesn't undo that.
	_ = cmd.Process.Release()
	return nil
}
