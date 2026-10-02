package prism

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// IsRunning reports whether a JVM is using the instance: its command line mentions the
// instance path, or its working directory is inside the instance. It errors when the
// process list could not be read.
func IsRunning(inst Instance) (bool, error) {
	dir := instanceDir(inst)
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	self := strconv.Itoa(os.Getpid())
	for _, p := range procs {
		pid := p.Name()
		if pid == "" || pid[0] < '0' || pid[0] > '9' || pid == self {
			continue
		}
		// Only JVMs count: a shell sitting in the instance dir must not block updates.
		cmd, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
		if err != nil || !bytes.Contains(cmd, []byte("java")) {
			continue
		}
		if bytes.Contains(cmd, []byte(dir)) {
			return true, nil
		}
		if cwd, err := os.Readlink(filepath.Join("/proc", pid, "cwd")); err == nil &&
			(cwd == dir || strings.HasPrefix(cwd, dir+string(filepath.Separator))) {
			return true, nil
		}
	}
	return false, nil
}
