//go:build !windows

package prism

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in its own session so it outlives the updater.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
