package pack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// StallTimeout aborts a download that receives no data for this long, so a dead
// connection fails with a message instead of hanging forever.
var StallTimeout = 60 * time.Second

var errStalled = errors.New("the connection stopped sending data")

// Download fetches url into dest (created or truncated). progress, if not nil, is called
// with bytes received so far and the total (-1 when the server sends no length).
func Download(client *http.Client, url, dest string, progress func(done, total int64)) error {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	watchdog := time.AfterFunc(StallTimeout, func() { cancel(errStalled) })
	defer watchdog.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", stallCause(ctx, err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s: HTTP %s", url, resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	w := &countingWriter{w: f, total: resp.ContentLength, progress: func(done, total int64) {
		watchdog.Reset(StallTimeout)
		if progress != nil {
			progress(done, total)
		}
	}}
	_, err = io.Copy(w, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dest)
		return fmt.Errorf("download failed: %w", stallCause(ctx, err))
	}
	if resp.ContentLength > 0 && w.done != resp.ContentLength {
		os.Remove(dest)
		return fmt.Errorf("download truncated: got %d of %d bytes", w.done, resp.ContentLength)
	}
	return nil
}

// stallCause reports the watchdog's reason instead of a bare "context canceled".
func stallCause(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); errors.Is(cause, errStalled) {
		return fmt.Errorf("%w for %s -- check your internet connection and try again", cause, StallTimeout)
	}
	return err
}

type countingWriter struct {
	w        io.Writer
	done     int64
	total    int64
	progress func(done, total int64)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.done += int64(n)
	if c.progress != nil {
		c.progress(c.done, c.total)
	}
	return n, err
}
