package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        bool
	}{
		{"0.1.0", "0.1.1", true},
		{"0.1.0", "v0.2.0", true},
		{"0.9.9", "1.0.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.2.0", "0.1.9", false},
		{"0.1.0", "0.1.10", true},
		{"dev", "9.9.9", false},
		{"0.1.0-3-gabc123-dirty", "0.1.0", false},
		{"0.1.0", "garbage", false},
	}
	for _, c := range cases {
		if got := Newer(c.cur, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.cur, c.latest, got, c.want)
		}
	}
}

func TestApplyReplacesExecutable(t *testing.T) {
	newBin := []byte("new binary")
	sum := sha256.Sum256(newBin)
	name := AssetName(runtime.GOOS, runtime.GOARCH)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","html_url":"x","assets":[
		  {"name":%q,"browser_download_url":"%s/dl/%s"},
		  {"name":"checksums.txt","browser_download_url":"%s/dl/checksums.txt"}]}`,
			name, srv.URL, name, srv.URL)
	})
	mux.HandleFunc("/dl/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(newBin) })
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n%s  other\n", hex.EncodeToString(sum[:]), name, "00")
	})

	exe := filepath.Join(t.TempDir(), "gtnh-update")
	os.WriteFile(exe, []byte("old binary"), 0o755)
	apiBase, downloadPrefix = srv.URL, srv.URL+"/dl/"
	executable = func() (string, error) { return exe, nil }
	defer func() {
		apiBase = "https://api.github.com"
		downloadPrefix = "https://github.com/" + Repo + "/releases/download/"
		executable = os.Executable
	}()

	rel, err := Latest(srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "9.9.9" || !Newer("0.1.0", rel.Version) {
		t.Fatalf("latest %+v", rel)
	}
	if err := rel.Apply(srv.Client(), nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new binary" {
		t.Errorf("exe now %q", b)
	}
	CleanupOld()
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Error(".old left behind")
	}

	// A tampered download must not replace anything.
	mux.HandleFunc("/dl/bad", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("evil")) })
	rel.assets[name] = srv.URL + "/dl/bad"
	if err := rel.Apply(srv.Client(), nil); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	if b, _ := os.ReadFile(exe); string(b) != "new binary" {
		t.Errorf("exe changed after failed update: %q", b)
	}
	// Foreign hosts are refused.
	rel.assets[name] = "https://evil.example/x"
	if err := rel.Apply(srv.Client(), nil); err == nil {
		t.Fatal("foreign download accepted")
	}
}
