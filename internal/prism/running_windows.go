package prism

import (
	"os/exec"
	"syscall"
)

const javaCommandLines = `Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -like '*java*' } | ForEach-Object { $_.CommandLine }`

// IsRunning reports whether a JVM's command line mentions the instance path, compared
// case-insensitively because the filesystem is. It errors when the process list could
// not be read.
func IsRunning(inst Instance) (bool, error) {
	return runningByCommand(inst, powershellTimeout, true, hideWindow, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", javaCommandLines)
}

// hideWindow keeps powershell from flashing a console window over the TUI.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
