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

// C8
func TestSetName(t *testing.T) {
	for _, tc := range []struct{ name, cfg, want string }{
		{"replaces existing name line",
			"[General]\nname=Old\nJavaPath=java\n",
			"[General]\nname=New\nJavaPath=java\n"},
		{"replaces only the first name line",
			"name=A\nname=B\n",
			"name=New\nname=B\n"},
		{"keeps CRLF on the replaced line",
			"[General]\r\nname=Old\r\nJavaPath=java\r\n",
			"[General]\r\nname=New\r\nJavaPath=java\r\n"},
		{"name line without trailing newline",
			"[General]\nname=Old",
			"[General]\nname=New"},
		{"does not match a key that merely contains name=",
			"[General]\nxname=Old\n",
			"[General]\nname=New\nxname=Old\n"},
		{"inserts after leading General",
			"[General]\nJavaPath=java\n",
			"[General]\nname=New\nJavaPath=java\n"},
		{"General alone without newline",
			"[General]",
			"[General]\nname=New\n"},
		{"appends when no General header",
			"JavaPath=java\n",
			"JavaPath=java\nname=New\n"},
		{"appends with newline when last line unterminated",
			"JavaPath=java",
			"JavaPath=java\nname=New\n"},
		{"General not first line appends at end",
			"x=1\n[General]\n",
			"x=1\n[General]\nname=New\n"},
		{"empty cfg", "", "name=New\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SetName(tc.cfg, "New"); got != tc.want {
				t.Errorf("SetName(%q) = %q, want %q", tc.cfg, got, tc.want)
			}
		})
	}
}
