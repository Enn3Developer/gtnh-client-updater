package pack

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildZip writes a zip with the given name -> content map and returns its bytes.
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "pack.zip")
	if err := os.WriteFile(name, buildZip(t, files), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestOpenFileStripsWrapperAndCanonicalizes(t *testing.T) {
	name := writeZip(t, map[string]string{
		"GT New Horizons 2.9.0/mmc-pack.json":           "{}",
		"GT New Horizons 2.9.0/instance.cfg":            "name=x",
		"GT New Horizons 2.9.0/minecraft/mods/a.jar":    "A",
		"GT New Horizons 2.9.0/minecraft/config/a.cfg":  "cfg",
		"GT New Horizons 2.9.0/patches/net.minecraft.j": "p",
		"stray.txt": "outside root",
	})
	p, err := OpenFile(name)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, want := range []string{"mmc-pack.json", "instance.cfg", ".minecraft/mods/a.jar", ".minecraft/config/a.cfg"} {
		if _, ok := p.Entries[want]; !ok {
			t.Errorf("missing entry %q; have %v", want, p.Paths())
		}
	}
	if _, ok := p.Entries["stray.txt"]; ok {
		t.Error("entry outside the pack root was included")
	}
	if err := p.Verify(nil); err != nil {
		t.Fatal(err)
	}
	fp := p.Entries[".minecraft/mods/a.jar"].FP
	disk := filepath.Join(t.TempDir(), "a.jar")
	os.WriteFile(disk, []byte("A"), 0o644)
	if got, _ := FingerprintFile(disk); got != fp {
		t.Errorf("disk fingerprint %v != zip fingerprint %v", got, fp)
	}
}

func TestOpenFileRejectsNonPacks(t *testing.T) {
	for _, files := range []map[string]string{
		{"x/readme.txt": "hi"},
		{"x/mmc-pack.json": "{}", "x/.minecraft/config/a": "no mods"},
		{"x/mmc-pack.json": "{}", "x/.minecraft/mods/../../../evil": "slip"},
	} {
		if p, err := OpenFile(writeZip(t, files)); err == nil {
			p.Close()
			t.Errorf("OpenFile(%v) succeeded, want error", files)
		}
	}
}

func TestRemoteMatchesLocal(t *testing.T) {
	files := map[string]string{
		"root/mmc-pack.json":         "{}",
		"root/.minecraft/mods/a.jar": string(bytes.Repeat([]byte("x"), 3*remoteBlock)),
		"root/.minecraft/config/b":   "b",
	}
	data := buildZip(t, files)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "pack.zip", fileTime, bytes.NewReader(data))
	}))
	defer srv.Close()

	rf, err := OpenRemote(srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	p, err := OpenReaderAt(rf, rf.Size())
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Entries[".minecraft/mods/a.jar"].FP.Size; got != 3*remoteBlock {
		t.Errorf("remote size %d", got)
	}
	if err := p.Verify(nil); err != nil {
		t.Fatalf("remote verify: %v", err)
	}
}

func TestDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("payload"))
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "out")
	var last int64
	if err := Download(srv.Client(), srv.URL, dest, func(d, _ int64) { last = d }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "payload" || last != 7 {
		t.Errorf("got %q, progress %d", b, last)
	}
}

var fileTime = time.Time{}

func TestDownloadStallAborts(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		select { // then go silent
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	old := StallTimeout
	StallTimeout = 200 * time.Millisecond
	defer func() { StallTimeout = old }()

	dest := filepath.Join(t.TempDir(), "out")
	err := Download(srv.Client(), srv.URL, dest, nil)
	if err == nil || !strings.Contains(err.Error(), "stopped sending data") {
		t.Fatalf("want stall error, got %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("partial file left behind")
	}
}
