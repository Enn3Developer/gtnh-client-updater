package prism

import "os/exec"

// IsRunning reports whether a JVM's command line mentions the instance path. It errors
// when the process list could not be read.
func IsRunning(inst Instance) (bool, error) {
	return runningByCommand(inst, psTimeout, false, func(*exec.Cmd) {}, "ps", "-axww", "-o", "command=")
}
