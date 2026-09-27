package prism

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CanDetectRunning reports whether Running can actually see processes on this OS.
const CanDetectRunning = true

// Running reports whether a JVM is using the instance: its command line mentions the
// instance path, or its working directory is inside the instance.
func Running(inst Instance) bool {
	dir, err := filepath.EvalSymlinks(inst.Dir)
	if err != nil {
		dir = inst.Dir
	}
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	self := os.Getpid()
	for _, p := range procs {
		pid := p.Name()
		if pid == "" || pid[0] < '0' || pid[0] > '9' || pid == strconv.Itoa(self) {
			continue
		}
		// Only JVMs count: a shell sitting in the instance dir must not block updates.
		cmd, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
		if err != nil || !bytes.Contains(cmd, []byte("java")) {
			continue
		}
		if bytes.Contains(cmd, []byte(dir)) {
			return true
		}
		if cwd, err := os.Readlink(filepath.Join("/proc", pid, "cwd")); err == nil &&
			(cwd == dir || strings.HasPrefix(cwd, dir+string(filepath.Separator))) {
			return true
		}
	}
	return false
}
