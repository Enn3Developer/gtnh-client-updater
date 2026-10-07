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

// ErrStalled is the cause of a download the watchdog gave up on.
var ErrStalled = errors.New("the connection stopped sending data")

// ErrTooBig means a body is larger than FetchOptions.MaxBytes; nothing is kept.
var ErrTooBig = errors.New("the file is too big")

// HTTPError is an answer other than 200 OK (or 304 Not Modified to a conditional fetch).
type HTTPError struct {
	URL    string
	Status int    // e.g. 404
	Text   string // e.g. "404 Not Found"
}

func (e *HTTPError) Error() string { return fmt.Sprintf("download failed: %s: HTTP %s", e.URL, e.Text) }

// FetchOptions tunes FetchContext.
type FetchOptions struct {
	Header   http.Header             // extra request headers, e.g. If-None-Match
	MaxBytes int64                   // refuse a body larger than this; 0 = no limit
	Progress func(done, total int64) // bytes so far and the total (-1 when unknown)
}

// FetchResult is what the server answered to a FetchContext that didn't fail.
type FetchResult struct {
	Status       int // http.StatusOK, or http.StatusNotModified (dest untouched)
	ContentType  string
	ETag         string
	LastModified string
}

// Download is DownloadContext without cancellation.
func Download(client *http.Client, url, dest string, progress func(done, total int64)) error {
	return DownloadContext(context.Background(), client, url, dest, progress)
}

// DownloadContext fetches url into dest (created or truncated). progress, if not nil, is
// called with bytes received so far and the total (-1 when the server sends no length).
// Cancelling parent aborts the download with an error wrapping its ctx.Err(); a partial
// dest is removed.
func DownloadContext(parent context.Context, client *http.Client, url, dest string, progress func(done, total int64)) error {
	_, err := FetchContext(parent, client, url, dest, FetchOptions{Progress: progress})
	return err
}

// FetchContext is DownloadContext with request headers, a size limit and the answer's
// validators. A conditional request (If-None-Match or If-Modified-Since in o.Header) may
// be answered 304 Not Modified, which leaves dest untouched; any other answer than 200
// is an *HTTPError. On every error a partial dest is removed.
func FetchContext(parent context.Context, client *http.Client, url, dest string, o FetchOptions) (FetchResult, error) {
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	watchdog := time.AfterFunc(StallTimeout, func() { cancel(ErrStalled) })
	defer watchdog.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return FetchResult{}, err
	}
	for k, vs := range o.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return FetchResult{}, fmt.Errorf("download failed: %w", stallCause(parent, ctx, err))
	}
	defer resp.Body.Close()
	res := FetchResult{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"),
		ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}
	conditional := o.Header.Get("If-None-Match") != "" || o.Header.Get("If-Modified-Since") != ""
	if resp.StatusCode == http.StatusNotModified && conditional {
		return res, nil
	}
	if resp.StatusCode != http.StatusOK {
		return res, &HTTPError{URL: url, Status: resp.StatusCode, Text: resp.Status}
	}
	if o.MaxBytes > 0 && resp.ContentLength > o.MaxBytes {
		return res, ErrTooBig
	}
	f, err := os.Create(dest)
	if err != nil {
		return res, err
	}
	w := &countingWriter{w: f, total: resp.ContentLength, progress: func(done, total int64) {
		watchdog.Reset(StallTimeout)
		if o.Progress != nil {
			o.Progress(done, total)
		}
	}}
	var body io.Reader = resp.Body
	if o.MaxBytes > 0 {
		body = io.LimitReader(resp.Body, o.MaxBytes+1) // one byte over tells "too big"
	}
	_, err = io.Copy(w, body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		err = fmt.Errorf("download failed: %w", stallCause(parent, ctx, err))
	case o.MaxBytes > 0 && w.done > o.MaxBytes:
		err = ErrTooBig
	case resp.ContentLength > 0 && w.done != resp.ContentLength:
		err = fmt.Errorf("download truncated: got %d of %d bytes", w.done, resp.ContentLength)
	}
	if err != nil {
		os.Remove(dest)
		return res, err
	}
	return res, nil
}

// stallCause reports why the download stopped: the caller's cancellation, or the
// watchdog's reason instead of a bare "context canceled".
func stallCause(parent, ctx context.Context, err error) error {
	if perr := parent.Err(); perr != nil {
		return perr
	}
	if cause := context.Cause(ctx); errors.Is(cause, ErrStalled) {
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
