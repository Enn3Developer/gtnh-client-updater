package update

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
)

const modsLink = "https://mods.example/custom_mods.zip"

// modsHost serves files on any host (through hostRewrite) with an ETag, so a conditional
// request gets 304, and counts the full answers per path.
type modsHost struct {
	mu    sync.Mutex
	files map[string][]byte
	full  map[string]int
	srv   *httptest.Server
}

func newModsHost(t *testing.T, files map[string][]byte) *modsHost {
	t.Helper()
	h := &modsHost{files: files, full: map[string]int{}}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		b, ok := h.files[r.URL.Path]
		h.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		etag := fmt.Sprintf(`"%08x"`, crc32.ChecksumIEEE(b))
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h.mu.Lock()
		h.full[r.URL.Path]++
		h.mu.Unlock()
		w.Write(b)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *modsHost) client() *http.Client { return &http.Client{Transport: hostRewrite{h.srv}} }

func (h *modsHost) set(path string, b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if b == nil {
		delete(h.files, path)
	} else {
		h.files[path] = b
	}
}

func (h *modsHost) fullAnswers(path string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.full[path]
}

// modsZipOf is a server archive of name -> content, the names as given.
func modsZipOf(t *testing.T, files map[string]string) []byte {
	return zipBytes(t, "", files)
}

func fetchFrom(t *testing.T, h *modsHost) (*ModsArchive, error) {
	t.Helper()
	a, err := fetchMods(context.Background(), h.client(), modsLink, t.TempDir(), nil)
	if a != nil {
		t.Cleanup(func() { a.Close() })
	}
	return a, err
}

// P3: the server owner zipped the folder, not its contents.
func TestFetchModsTakesTheJarsOutOfTheOneFolder(t *testing.T) {
	data := zipBytes(t, "custom_mods/", map[string]string{"a.jar": "a", "b.jar": "b", "readme.txt": "hi"})
	data = withEntries(t, data, map[string]string{"__MACOSX/custom_mods/._a.jar": "junk"})
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": data})
	a, err := fetchFrom(t, h)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(a.Names()); got != "[a.jar b.jar]" || len(a.ignored) != 0 {
		t.Errorf("jars %s ignored %v, want [a.jar b.jar] and nothing ignored", got, a.ignored)
	}
}

// withEntries appends entries to a zip.
func withEntries(t *testing.T, data []byte, extra map[string]string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if err := zw.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range extra {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func TestFetchModsRefusesJarsOnlyInFolders(t *testing.T) {
	data := modsZipOf(t, map[string]string{"one/a.jar": "a", "two/b.jar": "b"})
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": data})
	if _, err := fetchFrom(t, h); !errors.Is(err, ErrModsInFolders) || err.Error() != ErrModsInFolders.Error() {
		t.Errorf("fetch = %v, want ErrModsInFolders", err)
	}
}

func TestFetchModsLeavesOutOddNames(t *testing.T) {
	data := modsZipOf(t, map[string]string{
		"ok.jar": "ok", "con.jar": "c", "a?b.jar": "q", ".hidden.jar": "h", "Foo.jar": "1", "foo.jar": "2",
		"sub/nested.jar": "n", "notes.txt": "x",
	})
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": data})
	a, err := fetchFrom(t, h)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(a.Names()); got != "[Foo.jar ok.jar]" {
		t.Errorf("jars = %s, want [Foo.jar ok.jar]", got)
	}
	if got := fmt.Sprint(a.ignored); got != "[.hidden.jar a?b.jar con.jar foo.jar sub/nested.jar]" {
		t.Errorf("ignored = %s", got)
	}
}

func TestFetchModsErrorsSayWhatsWrong(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{"not found", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }, ErrModsNotFound},
		{"web page", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<!DOCTYPE html><html>Download</html>"))
		}, ErrModsWebPage},
		{"web page without a type", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte("<html><body>virus scan warning</body></html>"))
		}, ErrModsWebPage},
		{"not a zip", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte{0, 1, 2, 3}) }, ErrModsNotZip},
		{"plain http redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://mods.example/custom_mods.zip", http.StatusFound)
		}, ErrModsInsecure},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(c.handler)
			defer srv.Close()
			dir := t.TempDir()
			_, err := fetchMods(context.Background(), &http.Client{Transport: hostRewrite{srv}}, modsLink, dir, nil)
			if !errors.Is(err, c.want) || err.Error() != c.want.Error() {
				t.Errorf("fetch = %v, want %v", err, c.want)
			}
			if fileExists(filepath.Join(dir, modsZip)) || fileExists(filepath.Join(dir, modsZip+".part")) {
				t.Error("a refused download was kept")
			}
		})
	}
}

// P5: an https link may redirect to another https address, never to plain http.
func TestFetchModsFollowsHTTPSRedirectsOnly(t *testing.T) {
	data := modsZipOf(t, map[string]string{"a.jar": "a"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/custom_mods.zip" {
			http.Redirect(w, r, "/releases/latest/custom_mods.zip", http.StatusFound) // stays https
			return
		}
		w.Write(data)
	}))
	defer srv.Close()
	a, err := fetchMods(context.Background(), &http.Client{Transport: hostRewrite{srv}}, modsLink, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("fetch through an https redirect = %v", err)
	}
	a.Close()
}

// P7: a server that stops sending fails the fetch instead of hanging it.
func TestFetchModsGivesUpOnAStalledDownload(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte("PK"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	old := pack.StallTimeout
	pack.StallTimeout = 200 * time.Millisecond
	defer func() { pack.StallTimeout = old }()
	start := time.Now()
	_, err := fetchMods(context.Background(), &http.Client{Transport: hostRewrite{srv}}, modsLink, t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "the download stopped") || !errors.Is(err, pack.ErrStalled) {
		t.Errorf("fetch = %v, want the stall reason", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("fetch took %v to give up", d)
	}
}

func TestFetchModsSizeLimits(t *testing.T) {
	data := modsZipOf(t, map[string]string{"a.jar": strings.Repeat("a", 4000), "b.jar": strings.Repeat("b", 4000)})
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": data})
	oldDown, oldUnpacked := maxModsDownload, maxModsUnpacked
	defer func() { maxModsDownload, maxModsUnpacked = oldDown, oldUnpacked }()

	maxModsDownload = int64(len(data) - 1)
	if _, err := fetchFrom(t, h); !errors.Is(err, ErrModsTooBig) {
		t.Errorf("fetch of a too big file = %v, want ErrModsTooBig", err)
	}
	maxModsDownload, maxModsUnpacked = oldDown, 7999
	if _, err := fetchFrom(t, h); !errors.Is(err, ErrModsUnpacked) {
		t.Errorf("fetch of a zip bomb = %v, want ErrModsUnpacked", err)
	}
}

// The copy is kept: the next fetch asks whether it changed, a changed archive replaces
// it, and a broken download never does.
func TestFetchModsKeepsACopyAndAsksWhetherItChanged(t *testing.T) {
	v1 := modsZipOf(t, map[string]string{"a.jar": "a1"})
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": v1})
	dir := t.TempDir()
	// every archive is closed before the next fetch: Windows can't rename over an open file
	fetch := func() (*ModsArchive, error) {
		return fetchMods(context.Background(), h.client(), modsLink, dir, nil)
	}
	a, err := fetch()
	if err != nil {
		t.Fatal(err)
	}
	a.Close()
	a, err = fetch()
	if err != nil || h.fullAnswers("/custom_mods.zip") != 1 || fmt.Sprint(a.Names()) != "[a.jar]" {
		t.Fatalf("second fetch = %v (%d full downloads), want the copy after a 304", err, h.fullAnswers("/custom_mods.zip"))
	}
	a.Close()

	h.set("/custom_mods.zip", modsZipOf(t, map[string]string{"b.jar": "b"}))
	if a, err := fetch(); err != nil || fmt.Sprint(a.Names()) != "[b.jar]" {
		t.Fatalf("fetch of a changed archive = %v, %v", a.Names(), err)
	} else {
		a.Close()
	}

	h.set("/custom_mods.zip", []byte("<html>oops</html>"))
	if _, err := fetch(); !errors.Is(err, ErrModsWebPage) {
		t.Fatalf("fetch of a web page = %v, want ErrModsWebPage", err)
	}
	if c := cachedMods(modsLink, dir); c == nil || fmt.Sprint(c.Names()) != "[b.jar]" {
		t.Errorf("the copy after a broken download = %v, want the last good one", c.Names())
	} else {
		c.Close()
	}
	if c := cachedMods("https://other.example/x.zip", dir); c != nil {
		c.Close()
		t.Error("the copy of one link was handed out for another")
	}
}

// A copy that doesn't open any more is downloaded again, even when the server says
// it didn't change.
func TestFetchModsReplacesADamagedCopy(t *testing.T) {
	h := newModsHost(t, map[string][]byte{"/custom_mods.zip": modsZipOf(t, map[string]string{"a.jar": "a"})})
	dir := t.TempDir()
	a, err := fetchMods(context.Background(), h.client(), modsLink, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Close()
	os.WriteFile(filepath.Join(dir, modsZip), []byte("broken"), 0o644)
	a, err = fetchMods(context.Background(), h.client(), modsLink, dir, nil)
	if err != nil || fmt.Sprint(a.Names()) != "[a.jar]" || h.fullAnswers("/custom_mods.zip") != 2 {
		t.Fatalf("fetch over a damaged copy = %v (%d full downloads)", err, h.fullAnswers("/custom_mods.zip"))
	}
	a.Close()
}
