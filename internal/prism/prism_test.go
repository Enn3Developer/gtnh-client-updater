package prism

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func mkInstance(t *testing.T, root, dir, cfg string) {
	t.Helper()
	d := filepath.Join(root, dir)
	os.MkdirAll(filepath.Join(d, ".minecraft"), 0o755)
	os.WriteFile(filepath.Join(d, "instance.cfg"), []byte(cfg), 0o644)
	os.WriteFile(filepath.Join(d, "mmc-pack.json"), []byte(`{"components":[{"uid":"net.minecraft","version":"1.7.10"}]}`), 0o644)
}

func TestListInstancesOrder(t *testing.T) {
	root := t.TempDir()
	mkInstance(t, root, "vanilla", "name=Vanilla\nlastLaunchTime=9999999999999\n")
	os.WriteFile(filepath.Join(root, "vanilla", "mmc-pack.json"), []byte(`{"components":[{"uid":"net.minecraft","version":"1.21.8"}]}`), 0o644)
	mkInstance(t, root, "old", "[General]\nname=GTNH old\nlastLaunchTime=1000\n")
	mkInstance(t, root, "new", "[General]\nname=GTNH new\nlastLaunchTime=1760000000000\n")
	mkInstance(t, root, "never", "name=GT New Horizons never played\n")

	insts, err := ListInstances(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, in := range insts {
		got = append(got, in.Name)
	}
	want := []string{"GTNH new", "GTNH old", "GT New Horizons never played", "Vanilla"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
	if !insts[0].LastLaunch.Equal(time.UnixMilli(1760000000000)) {
		t.Errorf("last launch %v", insts[0].LastLaunch)
	}
	if insts[3].GTNH {
		t.Error("a 1.21 instance is not GTNH")
	}
}

func TestInstancesDir(t *testing.T) {
	d := t.TempDir()
	abs := filepath.Join(t.TempDir(), "insts") // absolute on every OS
	os.WriteFile(filepath.Join(d, "prismlauncher.cfg"), []byte("InstanceDir="+abs+"\n"), 0o644)
	if got := InstancesDir(d); got != abs {
		t.Errorf("absolute: %s", got)
	}
	os.WriteFile(filepath.Join(d, "prismlauncher.cfg"), []byte("Foo=1\n"), 0o644)
	if got := InstancesDir(d); got != filepath.Join(d, "instances") {
		t.Errorf("default: %s", got)
	}
}
