package prism

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The test binary doubles as the external program in these tests; nothing spawns java.
const (
	// fakeJVMEnv=1: block until stdin closes (a long-lived stand-in process whose
	// argv and cwd the test chooses).
	fakeJVMEnv = "PRISM_RUNNING_TEST_FAKE_PROCESS"
	// fakeOutputEnv: print the value to stdout and exit 0 (a fake `ps`/powershell).
	fakeOutputEnv = "PRISM_RUNNING_TEST_FAKE_OUTPUT"
	// fakeFailEnv=1: exit with status 3 and no output (a failing `ps`/powershell).
	fakeFailEnv = "PRISM_RUNNING_TEST_FAKE_FAIL"
)

func TestMain(m *testing.M) {
	switch {
	case os.Getenv(fakeJVMEnv) == "1":
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	case os.Getenv(fakeFailEnv) == "1":
		os.Exit(3)
	case os.Getenv(fakeOutputEnv) != "":
		fmt.Print(os.Getenv(fakeOutputEnv))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeLister returns the test binary path and a configure hook that makes it print
// output, the way `ps` or powershell would list command lines.
func fakeLister(t *testing.T, output string) (string, func(*exec.Cmd)) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return self, func(cmd *exec.Cmd) {
		cmd.Env = append(os.Environ(), fakeOutputEnv+"="+output)
	}
}

// noTests keeps the test binary from running the suite if the fake hook is not active.
const noTests = "-test.run=^$"

// missingInstanceDir looks like a real instance path but does not exist, so symlink
// resolution leaves it unchanged on every OS. t.TempDir embeds the test name, so test
// names must not contain "java" or the dir itself would look like a JVM.
func missingInstanceDir(t *testing.T) string {
	return filepath.Join(t.TempDir(), "instances", "GTNH 2.8.0")
}

// C7, C11: exact match of a java line naming the dir, with the real ps timeout.
func TestRunningByCommandExactFindsJVMLineWithDir(t *testing.T) {
	dir := missingInstanceDir(t)
	name, configure := fakeLister(t, "/sbin/launchd\njava -Xmx6G --gameDir "+dir+"/.minecraft\n")

	got, err := runningByCommand(Instance{Dir: dir}, psTimeout, false, configure, name, noTests)
	if err != nil || !got {
		t.Fatalf("runningByCommand = %v, %v; want true, nil", got, err)
	}
}

// C7: a non-java line naming the dir does not count.
func TestRunningByCommandExactIgnoresNonJVMLineWithDir(t *testing.T) {
	dir := missingInstanceDir(t)
	name, configure := fakeLister(t, "bash "+dir+"\n")

	got, err := runningByCommand(Instance{Dir: dir}, psTimeout, false, configure, name, noTests)
	if err != nil || got {
		t.Fatalf("runningByCommand = %v, %v; want false, nil", got, err)
	}
}

// C7, C8: without fold, the dir in another case does not match.
func TestRunningByCommandExactRejectsDirInOtherCase(t *testing.T) {
	dir := missingInstanceDir(t)
	name, configure := fakeLister(t, "JAVA --gameDir "+strings.ToUpper(dir)+"/.minecraft\n")

	got, err := runningByCommand(Instance{Dir: dir}, psTimeout, false, configure, name, noTests)
	if err != nil || got {
		t.Fatalf("runningByCommand = %v, %v; want false, nil", got, err)
	}
}

// C8, C11: with fold, the dir in another case matches, with the real powershell timeout.
func TestRunningByCommandFoldMatchesDirInOtherCase(t *testing.T) {
	dir := missingInstanceDir(t)
	name, configure := fakeLister(t, "JAVA --gameDir "+strings.ToUpper(dir)+"/.minecraft\r\n")

	got, err := runningByCommand(Instance{Dir: dir}, powershellTimeout, true, configure, name, noTests)
	if err != nil || !got {
		t.Fatalf("runningByCommand = %v, %v; want true, nil", got, err)
	}
}

// C8: fold still requires java on the line.
func TestRunningByCommandFoldIgnoresNonJVMLineWithDir(t *testing.T) {
	dir := missingInstanceDir(t)
	name, configure := fakeLister(t, "EXPLORER.EXE "+strings.ToUpper(dir)+"\n")

	got, err := runningByCommand(Instance{Dir: dir}, powershellTimeout, true, configure, name, noTests)
	if err != nil || got {
		t.Fatalf("runningByCommand = %v, %v; want false, nil", got, err)
	}
}

// C9: a lister that exits non-zero is an error.
func TestRunningByCommandNonZeroExitIsError(t *testing.T) {
	dir := missingInstanceDir(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	fail := func(cmd *exec.Cmd) { cmd.Env = append(os.Environ(), fakeFailEnv+"=1") }

	got, err := runningByCommand(Instance{Dir: dir}, psTimeout, false, fail, self, noTests)
	if err == nil || got {
		t.Fatalf("runningByCommand = %v, %v; want false, non-nil error", got, err)
	}
}

// C9: a lister that cannot be started is an error.
func TestRunningByCommandMissingProgramIsError(t *testing.T) {
	dir := missingInstanceDir(t)
	missingProgram := filepath.Join(t.TempDir(), "no-such-lister")

	got, err := runningByCommand(Instance{Dir: dir}, psTimeout, false, func(*exec.Cmd) {}, missingProgram)
	if err == nil || got {
		t.Fatalf("runningByCommand = %v, %v; want false, non-nil error", got, err)
	}
}

// C10: configure runs before the command starts; here it is the only way the fake
// lister learns what to print, so a skipped or late configure finds nothing.
func TestRunningByCommandAppliesConfigureBeforeStart(t *testing.T) {
	dir := missingInstanceDir(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	configured := false
	configure := func(cmd *exec.Cmd) {
		configured = cmd.Process == nil
		cmd.Env = append(os.Environ(), fakeOutputEnv+"=java --gameDir "+dir+"\n")
	}

	got, err := runningByCommand(Instance{Dir: dir}, psTimeout, false, configure, self, noTests)
	if err != nil || !got {
		t.Fatalf("runningByCommand = %v, %v; want true, nil (configure's env must reach the process)", got, err)
	}
	if !configured {
		t.Error("configure was called after the process started, want before")
	}
}

// C12: a path that cannot be resolved is returned as given.
func TestInstanceDirReturnsMissingPathUnchanged(t *testing.T) {
	dir := missingInstanceDir(t)

	got := instanceDir(Instance{Dir: dir})
	if got != dir {
		t.Errorf("instanceDir(%q) = %q, want it unchanged", dir, got)
	}
}

// C1, C6: matchesInstance requires a JVM command line that contains the exact
// instance dir.
func TestMatchesInstanceRequiresJavaAndExactDir(t *testing.T) {
	const dir = "/home/p/.local/share/PrismLauncher/instances/GTNH 2.8.0"
	const otherDir = "/home/p/.local/share/PrismLauncher/instances/GTNH 2.7.4"

	cases := []struct {
		name     string
		cmdlines []string
		dir      string
		want     bool
	}{
		{
			name:     "java with dir matches",
			cmdlines: []string{"/usr/lib/jvm/java-21-openjdk/bin/java -Xmx6G -Djava.library.path=" + dir + "/natives net.minecraft.client.main.Main"},
			dir:      dir,
			want:     true,
		},
		{
			name:     "java with dir inside longer gameDir argument matches",
			cmdlines: []string{"java -Xmx6G net.minecraft.client.main.Main --gameDir " + dir + "/.minecraft --username p"},
			dir:      dir,
			want:     true,
		},
		{
			name:     "uppercase JAVA with dir matches",
			cmdlines: []string{"C:\\Program Files\\JAVA\\bin\\JAVAW.EXE --gameDir " + dir + "/.minecraft"},
			dir:      dir,
			want:     true,
		},
		{
			name:     "matching line among unrelated lines matches",
			cmdlines: []string{"/usr/bin/bash", "java --gameDir " + otherDir + "/.minecraft", "java --gameDir " + dir + "/.minecraft"},
			dir:      dir,
			want:     true,
		},
		{
			name:     "dir without java does not match",
			cmdlines: []string{"/usr/bin/bash", "vim " + dir + "/config/forge.cfg", "cd " + dir},
			dir:      dir,
			want:     false,
		},
		{
			name:     "java with another dir does not match",
			cmdlines: []string{"java -Xmx6G net.minecraft.client.main.Main --gameDir " + otherDir + "/.minecraft"},
			dir:      dir,
			want:     false,
		},
		{
			name:     "dir and java on separate lines does not match",
			cmdlines: []string{"vim " + dir + "/options.txt", "java --gameDir " + otherDir + "/.minecraft"},
			dir:      dir,
			want:     false,
		},
		{
			name:     "empty dir does not match java lines",
			cmdlines: []string{"java -Xmx6G net.minecraft.client.main.Main", "/usr/bin/java -jar server.jar"},
			dir:      "",
			want:     false,
		},
		{
			name:     "empty lines do not match",
			cmdlines: []string{},
			dir:      dir,
			want:     false,
		},
		{
			name:     "nil lines do not match",
			cmdlines: nil,
			dir:      dir,
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchesInstance(tc.cmdlines, tc.dir)
			if got != tc.want {
				t.Errorf("matchesInstance(%q, %q) = %v, want %v", tc.cmdlines, tc.dir, got, tc.want)
			}
		})
	}
}

// C5: Running on an instance no JVM uses is false and agrees with IsRunning.
func TestRunningIsFalseForUnusedInstanceAndAgreesWithIsRunning(t *testing.T) {
	inst := Instance{Dir: t.TempDir()}

	running := Running(inst)
	if running {
		t.Errorf("Running(%q) = true, want false for a fresh temp dir", inst.Dir)
	}

	isRunning, err := IsRunning(inst)
	if err == nil && isRunning != running {
		t.Errorf("IsRunning(%q) = %v, nil; Running = %v; want them equal", inst.Dir, isRunning, running)
	}

	if !CanDetectRunning {
		t.Error("CanDetectRunning = false, want true on every supported OS")
	}
}
