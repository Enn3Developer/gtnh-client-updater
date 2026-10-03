package prism

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirOfPicksDirHoldingTheInstance(t *testing.T) { // C1
	d1, d2 := t.TempDir(), t.TempDir()
	inst := Instance{Dir: filepath.Join(d2, "instances", "X"), Name: "X"}
	if got := DataDirOf([]string{d1, d2}, inst); got != d2 {
		t.Errorf("DataDirOf(instance under the second dir) = %q, want %q", got, d2)
	}
}

func TestDataDirOfFallsBackToFirstDir(t *testing.T) { // C1
	d1, d2 := t.TempDir(), t.TempDir()
	inst := Instance{Dir: filepath.Join(t.TempDir(), "instances", "X"), Name: "X"}
	if got := DataDirOf([]string{d1, d2}, inst); got != d1 {
		t.Errorf("DataDirOf(instance elsewhere) = %q, want %q", got, d1)
	}
}

func TestDataDirOfIgnoresSiblingWithSamePrefix(t *testing.T) { // C1
	d1, d2 := t.TempDir(), t.TempDir()
	inst := Instance{Dir: filepath.Join(d2, "instances-old", "X"), Name: "X"}
	if got := DataDirOf([]string{d1, d2}, inst); got != d1 {
		t.Errorf("DataDirOf(instance under instances-old) = %q, want %q", got, d1)
	}
}

func TestDataDirOfEmptyListReturnsEmpty(t *testing.T) { // C1
	inst := Instance{Dir: filepath.Join(t.TempDir(), "instances", "X"), Name: "X"}
	if got := DataDirOf(nil, inst); got != "" {
		t.Errorf("DataDirOf(nil) = %q, want \"\"", got)
	}
}

func TestDataDirOfInstancesDirItselfIsNotInside(t *testing.T) { // C1, kill: rel != "." dropped
	a, b := t.TempDir(), t.TempDir()
	inst := Instance{Dir: filepath.Join(b, "instances"), Name: "instances"}
	if got := DataDirOf([]string{a, b}, inst); got != a {
		t.Errorf("DataDirOf(inst.Dir == InstancesDir(b)) = %q, want the fallback %q", got, a)
	}
}

func TestDataDirOfParentOfInstancesDirIsNotInside(t *testing.T) { // C1: rel == ".."
	a, b := t.TempDir(), t.TempDir()
	inst := Instance{Dir: b, Name: "b"}
	if got := DataDirOf([]string{a, b}, inst); got != a {
		t.Errorf("DataDirOf(inst.Dir == b) = %q, want the fallback %q", got, a)
	}
}

func TestDataDirOfChildNamedWithDoubleDotPrefixIsInside(t *testing.T) { // C1: rel "..X" does not start with ".."+separator
	a, b := t.TempDir(), t.TempDir()
	inst := Instance{Dir: filepath.Join(b, "instances", "..X"), Name: "..X"}
	if got := DataDirOf([]string{a, b}, inst); got != b {
		t.Errorf("DataDirOf(instance named ..X under b) = %q, want %q", got, b)
	}
}

func TestDataDirOfHonoursInstanceDirSetting(t *testing.T) { // C1: uses InstancesDir, not d/instances
	a, b := t.TempDir(), t.TempDir()
	custom := filepath.Join(t.TempDir(), "insts")
	if err := os.WriteFile(filepath.Join(b, "prismlauncher.cfg"), []byte("InstanceDir="+custom+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst := Instance{Dir: filepath.Join(custom, "X"), Name: "X"}
	if got := DataDirOf([]string{a, b}, inst); got != b {
		t.Errorf("DataDirOf(instance under b's custom InstanceDir) = %q, want %q", got, b)
	}
}

func TestDataDirOfFirstMatchingDirWins(t *testing.T) { // C1
	a, b := t.TempDir(), t.TempDir()
	shared := filepath.Join(t.TempDir(), "insts")
	for _, d := range []string{a, b} {
		if err := os.WriteFile(filepath.Join(d, "prismlauncher.cfg"), []byte("InstanceDir="+shared+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inst := Instance{Dir: filepath.Join(shared, "X"), Name: "X"}
	if got := DataDirOf([]string{a, b}, inst); got != a {
		t.Errorf("DataDirOf(both dirs share the instances dir) = %q, want the first %q", got, a)
	}
}
