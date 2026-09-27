package pack

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
)

// RemoteFile is an io.ReaderAt over an HTTP resource that supports range requests. It
// reads in aligned blocks and caches them, because archive/zip issues many small reads
// while parsing the central directory. It lets the updater fingerprint an old pack from
// its central directory (a few MB) instead of downloading the whole archive.
type RemoteFile struct {
	client *http.Client
	url    string
	size   int64

	mu    sync.Mutex
	cache map[int64][]byte
	order []int64
}

const (
	remoteBlock    = 1 << 20
	remoteMaxCache = 16
)

// ErrNoRanges means the server ignored the Range header; callers should fall back to a
// full download.
var ErrNoRanges = errors.New("server does not support range requests")

// OpenRemote probes url with a HEAD request and returns a ReaderAt over it.
func OpenRemote(client *http.Client, url string) (*RemoteFile, error) {
	resp, err := client.Head(url)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HEAD %s: %s", url, resp.Status)
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		return nil, ErrNoRanges
	}
	size, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("HEAD %s: missing Content-Length", url)
	}
	return &RemoteFile{client: client, url: url, size: size, cache: map[int64][]byte{}}, nil
}

// Size is the resource length in bytes.
func (r *RemoteFile) Size() int64 { return r.size }

// ReadAt implements io.ReaderAt.
func (r *RemoteFile) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	n := 0
	for n < len(p) {
		pos := off + int64(n)
		if pos >= r.size {
			return n, io.EOF
		}
		start := pos - pos%remoteBlock
		blk, err := r.block(start)
		if err != nil {
			return n, err
		}
		c := copy(p[n:], blk[pos-start:])
		if c == 0 {
			return n, io.ErrUnexpectedEOF
		}
		n += c
	}
	return n, nil
}

func (r *RemoteFile) block(start int64) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.cache[start]; ok {
		return b, nil
	}
	end := start + remoteBlock - 1
	if end >= r.size {
		end = r.size - 1
	}
	req, err := http.NewRequest(http.MethodGet, r.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return nil, ErrNoRanges
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, end-start+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) != end-start+1 {
		return nil, io.ErrUnexpectedEOF
	}
	if len(r.order) >= remoteMaxCache {
		delete(r.cache, r.order[0])
		r.order = r.order[1:]
	}
	r.cache[start] = b
	r.order = append(r.order, start)
	return b, nil
}
