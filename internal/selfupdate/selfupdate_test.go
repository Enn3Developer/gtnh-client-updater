package selfupdate

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	sums := fmt.Sprintf("%s  %s\n%s  other\n", hex.EncodeToString(sum[:]), name, "00")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sig := SignChecksums(priv, []byte(sums))
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","html_url":"x","assets":[
		  {"name":%q,"browser_download_url":"%s/dl/%s"},
		  {"name":"checksums.txt","browser_download_url":"%s/dl/checksums.txt"},
		  {"name":"checksums.txt.sig","browser_download_url":"%s/dl/checksums.txt.sig"}]}`,
			name, srv.URL, name, srv.URL, srv.URL)
	})
	mux.HandleFunc("/dl/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(newBin) })
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sums) })
	mux.HandleFunc("/dl/checksums.txt.sig", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sig) })

	exe := filepath.Join(t.TempDir(), "gtnh-update")
	os.WriteFile(exe, []byte("old binary"), 0o755)
	apiBase, downloadPrefix = srv.URL, srv.URL+"/dl/"
	executable = func() (string, error) { return exe, nil }
	oldKey := publicKey
	publicKey = pub
	defer func() {
		publicKey = oldKey
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

	// Signature problems must stop the update before anything is downloaded or replaced.
	os.WriteFile(exe, []byte("new binary"), 0o755)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	publicKey = otherPub
	if err := rel.Apply(srv.Client(), nil); err == nil {
		t.Fatal("release signed by another key accepted")
	}
	publicKey = nil
	if err := rel.Apply(srv.Client(), nil); err == nil {
		t.Fatal("a build without a key accepted an update")
	}
	publicKey = pub
	realSums := sums
	sums = strings.Replace(sums, "00  other", "11  other", 1) // tampered after signing
	if err := rel.Apply(srv.Client(), nil); err == nil {
		t.Fatal("tampered checksums.txt accepted")
	}
	sums = realSums
	sigURL := rel.assets["checksums.txt.sig"]
	delete(rel.assets, "checksums.txt.sig")
	if err := rel.Apply(srv.Client(), nil); err == nil || !strings.Contains(err.Error(), "not signed") {
		t.Fatalf("unsigned release accepted: %v", err)
	}
	rel.assets["checksums.txt.sig"] = sigURL

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

func TestKeyEncodingRoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	p2, err := ParsePrivateKey(EncodePrivateKey(priv))
	if err != nil || !p2.Equal(priv) {
		t.Fatalf("private key round trip: %v", err)
	}
	k2, err := ParsePublicKey(EncodePublicKey(pub))
	if err != nil || !k2.Equal(pub) {
		t.Fatalf("public key round trip: %v", err)
	}
	if _, err := ParsePublicKey(""); err == nil {
		t.Error("empty key accepted")
	}
}
