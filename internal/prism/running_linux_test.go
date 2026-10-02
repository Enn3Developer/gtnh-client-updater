package prism

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// startFakeProcess starts the test binary with the given argv and cwd and stops it
// when the test ends. exec.Cmd.Start returns after execve, so /proc already shows argv.
func startFakeProcess(t *testing.T, cwd string, argv ...string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := &exec.Cmd{Path: self, Args: argv, Dir: cwd, Env: append(os.Environ(), fakeJVMEnv+"=1")}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake process: %v", err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
}

// newInstanceDir returns a fresh instance-looking dir (with a space) under t.TempDir().
func newInstanceDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "instances", name)
	if err := os.MkdirAll(filepath.Join(dir, ".minecraft"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir
}

// C5 + linux IsRunning: a java process whose command line names the instance dir.
func TestIsRunningLinuxTrueForJVMWithInstanceDirInCmdline(t *testing.T) {
	dir := newInstanceDir(t, "GTNH 2.8.0")
	startFakeProcess(t, t.TempDir(), "java", "--gameDir", dir+"/.minecraft")

	got, err := IsRunning(Instance{Dir: dir})
	if err != nil || !got {
		t.Fatalf("IsRunning = %v, %v; want true, nil", got, err)
	}
	if !Running(Instance{Dir: dir}) {
		t.Error("Running = false, want true when IsRunning is true without error")
	}
}

// linux IsRunning: the instance dir in a command line without java does not count.
func TestIsRunningLinuxFalseForNonJVMWithInstanceDirInCmdline(t *testing.T) {
	dir := newInstanceDir(t, "GTNH 2.8.0")
	startFakeProcess(t, dir, "bash", "--gameDir", dir+"/.minecraft")

	got, err := IsRunning(Instance{Dir: dir})
	if err != nil || got {
		t.Fatalf("IsRunning = %v, %v; want false, nil", got, err)
	}
}

// linux IsRunning: a java process whose working directory is inside the instance.
func TestIsRunningLinuxTrueForJVMWithCwdInsideInstance(t *testing.T) {
	dir := newInstanceDir(t, "GTNH 2.8.0")
	startFakeProcess(t, filepath.Join(dir, ".minecraft"), "java", "-Xmx6G")

	got, err := IsRunning(Instance{Dir: dir})
	if err != nil || !got {
		t.Fatalf("IsRunning = %v, %v; want true, nil", got, err)
	}
}

// linux IsRunning: a java process whose working directory is the instance itself.
func TestIsRunningLinuxTrueForJVMWithCwdEqualToInstance(t *testing.T) {
	dir := newInstanceDir(t, "GTNH 2.8.0")
	startFakeProcess(t, dir, "java", "-Xmx6G")

	got, err := IsRunning(Instance{Dir: dir})
	if err != nil || !got {
		t.Fatalf("IsRunning = %v, %v; want true, nil", got, err)
	}
}

// linux IsRunning: a sibling folder sharing the instance name as a prefix is not inside it.
func TestIsRunningLinuxFalseForJVMWithCwdInSiblingWithSamePrefix(t *testing.T) {
	dir := newInstanceDir(t, "GTNH 2.8.0")
	sibling := dir + " backup"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	startFakeProcess(t, sibling, "java", "-Xmx6G")

	got, err := IsRunning(Instance{Dir: dir})
	if err != nil || got {
		t.Fatalf("IsRunning = %v, %v; want false, nil", got, err)
	}
}

// linux IsRunning: an instance reached through a symlink is matched by the real
// working directory the kernel reports.
func TestIsRunningLinuxTrueForJVMInInstanceReachedThroughSymlink(t *testing.T) {
	real := newInstanceDir(t, "GTNH 2.8.0")
	link := filepath.Join(t.TempDir(), "GTNH link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	startFakeProcess(t, filepath.Join(real, ".minecraft"), "java", "-Xmx6G")

	got, err := IsRunning(Instance{Dir: link})
	if err != nil || !got {
		t.Fatalf("IsRunning(symlink) = %v, %v; want true, nil", got, err)
	}
}

// C12: a symlink to a real instance dir resolves to the real dir.
func TestInstanceDirResolvesSymlinkToRealDir(t *testing.T) {
	real := newInstanceDir(t, "GTNH 2.8.0")
	link := filepath.Join(t.TempDir(), "GTNH link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got := instanceDir(Instance{Dir: link})
	if got != real {
		t.Errorf("instanceDir(%q) = %q, want %q", link, got, real)
	}
}

// linux IsRunning: a missing instance dir is not used by an unrelated java process.
func TestIsRunningLinuxFalseForMissingDirWithOtherJVMRunning(t *testing.T) {
	other := newInstanceDir(t, "GTNH 2.7.4")
	missing := filepath.Join(t.TempDir(), "instances", "GTNH 2.8.0")
	startFakeProcess(t, other, "java", "--gameDir", other+"/.minecraft")

	got, err := IsRunning(Instance{Dir: missing})
	if err != nil || got {
		t.Fatalf("IsRunning(missing) = %v, %v; want false, nil", got, err)
	}
}
