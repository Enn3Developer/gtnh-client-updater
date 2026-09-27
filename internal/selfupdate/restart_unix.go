//go:build !windows

package selfupdate

import (
	"os"
	"syscall"
)

// Restart replaces the current process with the (updated) binary, same arguments.
func Restart() error {
	exe, err := executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
