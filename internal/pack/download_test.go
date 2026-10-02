package pack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// C1: cancelling before the request is sent fails with context.Canceled; no file.
func TestDownloadContextCancelledBeforeStartReturnsCanceledAndNoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("payload"))
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dest := filepath.Join(t.TempDir(), "out")
	err := DownloadContext(ctx, srv.Client(), srv.URL, dest, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DownloadContext(cancelled) = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dest exists after a cancelled download (stat err %v)", err)
	}
}

// C1: cancelling mid-body fails with context.Canceled and removes the partial file.
func TestDownloadContextCancelledMidBodyRemovesPartialFile(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dest := filepath.Join(t.TempDir(), "out")
	err := DownloadContext(ctx, srv.Client(), srv.URL, dest, func(done, _ int64) {
		if done > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DownloadContext(cancelled mid-body) = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("partial dest left behind (stat err %v)", err)
	}
}
