//go:build unix

package prism

import (
	"os"
	"path/filepath"
	"testing"
)

// C3: a failing rename (read-only dir, tmp file pre-existing and writable) must be
// reported as an error and must leave instance.cfg untouched. Not meaningful as root.
func TestWriteSettingsReportsRenameFailureAndKeepsOriginal(t *testing.T) {
	dir := settingsDirWithCfg(t, "[General]\nname=GTNH\n")
	tmp := filepath.Join(dir, "instance.cfg.gtnh-tmp")
	if err := os.WriteFile(tmp, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale tmp: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := WriteSettings(dir, settingsFull())

	if err == nil {
		t.Fatal("WriteSettings in read-only dir: want error, got nil")
	}
	if got := settingsReadCfg(t, dir); got != "[General]\nname=GTNH\n" {
		t.Fatalf("instance.cfg changed after failed write: %q", got)
	}
}
