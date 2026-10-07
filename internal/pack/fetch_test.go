package pack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// fetchServer serves body with an ETag and Last-Modified, answering 304 to a matching
// If-None-Match. chunked drops the Content-Length.
func fetchServer(t *testing.T, body string, chunked bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Mon, 05 Oct 2026 10:00:00 GMT")
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if !chunked {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
		w.Write([]byte(body))
		if chunked {
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchContextReturnsTheValidators(t *testing.T) {
	srv := fetchServer(t, "payload", false)
	dest := filepath.Join(t.TempDir(), "out")
	res, err := FetchContext(context.Background(), srv.Client(), srv.URL, dest, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusOK || res.ETag != `"v1"` || res.LastModified != "Mon, 05 Oct 2026 10:00:00 GMT" {
		t.Errorf("result = %+v, want 200 with the ETag and Last-Modified", res)
	}
	if b, _ := os.ReadFile(dest); string(b) != "payload" {
		t.Errorf("dest = %q, want payload", b)
	}
}

func TestFetchContextNotModifiedLeavesDestAlone(t *testing.T) {
	srv := fetchServer(t, "payload", false)
	dest := filepath.Join(t.TempDir(), "out")
	os.WriteFile(dest, []byte("cached"), 0o644)
	h := http.Header{}
	h.Set("If-None-Match", `"v1"`)
	res, err := FetchContext(context.Background(), srv.Client(), srv.URL, dest, FetchOptions{Header: h})
	if err != nil || res.Status != http.StatusNotModified {
		t.Fatalf("FetchContext = %+v, %v; want 304 and no error", res, err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "cached" {
		t.Errorf("dest = %q, want the cached content untouched", b)
	}
}

// A 304 nobody asked for is an error, like any other answer than 200.
func TestFetchContextUnaskedNotModifiedIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()
	_, err := FetchContext(context.Background(), srv.Client(), srv.URL, filepath.Join(t.TempDir(), "out"), FetchOptions{})
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusNotModified {
		t.Errorf("FetchContext = %v, want an *HTTPError with status 304", err)
	}
}

func TestFetchContextHTTPErrorKeepsTheOldMessage(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "out")
	err := Download(srv.Client(), srv.URL+"/x.zip", dest, nil)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusNotFound {
		t.Fatalf("Download = %v, want an *HTTPError with status 404", err)
	}
	if want := "download failed: " + srv.URL + "/x.zip: HTTP 404 Not Found"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dest created for an error answer (stat err %v)", err)
	}
}

func TestFetchContextRefusesTooBigBodies(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		t.Run("chunked="+strconv.FormatBool(chunked), func(t *testing.T) {
			srv := fetchServer(t, "0123456789", chunked)
			dest := filepath.Join(t.TempDir(), "out")
			_, err := FetchContext(context.Background(), srv.Client(), srv.URL, dest, FetchOptions{MaxBytes: 9})
			if !errors.Is(err, ErrTooBig) {
				t.Fatalf("FetchContext = %v, want ErrTooBig", err)
			}
			if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a too big body was kept (stat err %v)", err)
			}
			if _, err := FetchContext(context.Background(), srv.Client(), srv.URL, dest, FetchOptions{MaxBytes: 10}); err != nil {
				t.Errorf("a body of exactly MaxBytes failed: %v", err)
			}
		})
	}
}
