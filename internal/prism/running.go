package prism

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CanDetectRunning reports that Running and IsRunning can see processes on every
// supported OS.
const CanDetectRunning = true

// psTimeout bounds the darwin `ps` call.
const psTimeout = 10 * time.Second

// powershellTimeout bounds the windows powershell call, which starts much slower.
const powershellTimeout = 15 * time.Second

// Running reports IsRunning's answer, treating an unreadable process list as "not
// running" so existing callers keep a plain bool.
func Running(inst Instance) bool {
	running, err := IsRunning(inst)
	return err == nil && running
}

// instanceDir resolves symlinks so it matches the paths JVMs report, falling back to
// the path as given when it cannot be resolved.
func instanceDir(inst Instance) string {
	dir, err := filepath.EvalSymlinks(inst.Dir)
	if err != nil {
		return inst.Dir
	}
	return dir
}

// runningByCommand lists command lines with an external program and matches them
// against the instance. fold lower-cases both sides for case-insensitive filesystems.
func runningByCommand(inst Instance, timeout time.Duration, fold bool, configure func(*exec.Cmd), name string, args ...string) (bool, error) {
	dir := instanceDir(inst)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	configure(cmd)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(out), "\n")
	if fold {
		dir = strings.ToLower(dir)
		for i, line := range lines {
			lines[i] = strings.ToLower(line)
		}
	}
	return matchesInstance(lines, dir), nil
}

// matchesInstance reports whether any command line belongs to a JVM using dir. Only
// JVMs count: a shell or editor sitting in the instance folder must not block updates.
func matchesInstance(cmdlines []string, dir string) bool {
	if dir == "" {
		return false
	}
	for _, line := range cmdlines {
		if strings.Contains(strings.ToLower(line), "java") && strings.Contains(line, dir) {
			return true
		}
	}
	return false
}
