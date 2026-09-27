//go:build !linux

package prism

// CanDetectRunning reports whether Running can actually see processes on this OS. On
// Windows and macOS it cannot, so callers must ask the player to close the game.
const CanDetectRunning = false

// Running always reports false where process inspection is not implemented.
func Running(Instance) bool { return false }
