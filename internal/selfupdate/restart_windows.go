package selfupdate

import (
	"errors"
	"os"
	"os/exec"
)

// Restart runs the (updated) binary with the same arguments in this console and exits
// with its status. Windows has no exec(), so this process waits for the child; that
// also keeps a double-clicked console window open.
func Restart() error {
	exe, err := executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	if err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
