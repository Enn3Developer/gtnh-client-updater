package update

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
)

// cancelReporter cancels its context the first time download progress arrives.
type cancelReporter struct {
	recReporter
	cancel context.CancelFunc
}

func (r *cancelReporter) Progress(d, t int64) {
	if d > 0 {
		r.cancel()
	}
	r.recReporter.Progress(d, t)
}

// stallingServer sends part of a body, then stays silent until the client goes away.
func stallingServer(t *testing.T) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		w.Write([]byte(strings.Repeat("x", 1000)))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	return srv
}

// C2
func TestPrepareCreateCancelledMidDownloadRemovesFolder(t *testing.T) {
	env := newCreateEnv(t, createPack())
	env.opts.Client = &http.Client{Transport: hostRewrite{stallingServer(t)}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env.opts.Context = ctx
	c, err := PrepareCreate(env.opts, &cancelReporter{cancel: cancel})
	if !errors.Is(err, context.Canceled) {
		if c != nil {
			c.Close()
		}
		t.Fatalf("PrepareCreate(cancelled mid-download) = %v, want context.Canceled", err)
	}
	if exists(filepath.Join(env.opts.InstancesDir, newName)) {
		t.Errorf("instance folder left after a cancelled download")
	}
}

// C2
func TestPrepareCreateWithCancelledContextRemovesFolder(t *testing.T) {
	env := newCreateEnv(t, createPack())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env.opts.Context = ctx
	_, err := PrepareCreate(env.opts, &recReporter{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PrepareCreate(cancelled) = %v, want context.Canceled", err)
	}
	if got := listDir(t, env.opts.InstancesDir); len(got) != 0 {
		t.Errorf("instances dir = %v, want empty", got)
	}
}

// C2: a nil Context means the download is never cancelled.
func TestPrepareCreateNilContextDownloads(t *testing.T) {
	env := newCreateEnv(t, createPack())
	env.opts.Context = nil
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err != nil {
		t.Fatalf("PrepareCreate(nil Context) = %v, want nil", err)
	}
	defer c.Close()
	if c.Files != 5 {
		t.Errorf("Files = %d, want 5", c.Files)
	}
}

// C3: state.json can't be saved -> the creation fails and its folder is removed.
func TestApplyStateSaveFailureRemovesFolder(t *testing.T) {
	env := newCreateEnv(t, createPack())
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(c.Dir, StateDir, "state.json.tmp")
	if err := os.MkdirAll(filepath.Join(blocker, "inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := c.Apply(&recReporter{})
	if err == nil {
		t.Fatal("Apply succeeded although state.json can't be written")
	}
	if res != nil {
		t.Errorf("Apply result = %+v, want nil on failure", res)
	}
	var le *LeftoverError
	if errors.As(err, &le) {
		t.Errorf("Apply error = %v, want no LeftoverError (the folder is removable)", err)
	}
	if exists(c.Dir) {
		t.Errorf("instance folder %s left after the state save failed", c.Dir)
	}
}

// C3: server mods failing doesn't stop instance.cfg and the state from being written.
func TestApplyServerModsFailureStillWritesCfgAndEmptyCustomMods(t *testing.T) {
	env := newCreateEnv(t, createPack())
	env.opts.CustomModsURL = "https://files.example/custom500.zip"
	env.opts.CustomModsAsked = true
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Apply(&recReporter{})
	if err != nil {
		t.Fatalf("Apply = %v, want nil", err)
	}
	if res.Mods != nil {
		t.Errorf("Mods = %+v, want nil after a failed sync", res.Mods)
	}
	want := "[General]\r\nInstanceType=OneSix\r\nname=" + newName + "\r\nJavaPath=java\r\n"
	if got := mustRead(t, filepath.Join(c.Dir, "instance.cfg")); got != want {
		t.Errorf("instance.cfg = %q, want %q", got, want)
	}
	st, err := LoadState(c.Dir)
	if err != nil || st == nil {
		t.Fatalf("LoadState = %v, %v", st, err)
	}
	if len(st.CustomMods) != 0 || st.CustomModsURL != env.opts.CustomModsURL {
		t.Errorf("state CustomMods %v URL %q, want empty and %q", st.CustomMods, st.CustomModsURL, env.opts.CustomModsURL)
	}
}

// C4
func TestLeftoverErrorMentionsDirAndUnwraps(t *testing.T) {
	inner := fs.ErrPermission
	dir := filepath.Join("instances", "Half Made")
	var err error = &LeftoverError{Dir: dir, Err: inner}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("Error() = %q, want it to mention %q", err.Error(), dir)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("errors.Is(LeftoverError, ErrPermission) = false, want true")
	}
	joined := errors.Join(errors.New("download failed"), err)
	var le *LeftoverError
	if !errors.As(joined, &le) || le.Dir != dir {
		t.Errorf("errors.As(joined) = %v, want the LeftoverError for %q", le, dir)
	}
}

// C5
func TestCloseOnZeroCreationTouchesNothing(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.WriteFile(filepath.Join(cwd, "keep.txt"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	var c Creation
	c.Close()
	if got := listDir(t, cwd); len(got) != 1 || got[0] != "keep.txt" {
		t.Errorf("cwd now holds %v, want [keep.txt]", got)
	}
}

// C5
func TestApplyAfterCloseFailsAndWritesNothing(t *testing.T) {
	env := newCreateEnv(t, createPack())
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	res, err := c.Apply(&recReporter{})
	if err == nil || res != nil {
		t.Fatalf("Apply after Close = %+v, %v, want nil and an error", res, err)
	}
	if got := listDir(t, env.opts.InstancesDir); len(got) != 0 {
		t.Errorf("instances dir = %v after Apply on a closed Creation, want empty", got)
	}
}

// C5: Apply on a zero Creation is the same "nothing to install" failure.
func TestApplyOnZeroCreationFails(t *testing.T) {
	var c Creation
	res, err := c.Apply(&recReporter{})
	if err == nil || res != nil {
		t.Errorf("zero Creation Apply = %+v, %v, want nil and an error", res, err)
	}
}

// C6
func TestPrepareCreateMakesMissingInstancesDirWhenParentExists(t *testing.T) {
	env := newCreateEnv(t, createPack())
	env.opts.InstancesDir = filepath.Join(t.TempDir(), "instances")
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err != nil {
		t.Fatalf("PrepareCreate with a missing instances dir = %v, want nil", err)
	}
	defer c.Close()
	if want := filepath.Join(env.opts.InstancesDir, newName); c.Dir != want || !exists(want) {
		t.Errorf("Dir = %q (exists %v), want existing %q", c.Dir, exists(c.Dir), want)
	}
}

// C6
func TestPrepareCreateFailsWhenInstancesDirParentIsMissing(t *testing.T) {
	env := newCreateEnv(t, createPack())
	root := t.TempDir()
	parent := filepath.Join(root, "prism")
	env.opts.InstancesDir = filepath.Join(parent, "instances")
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err == nil {
		c.Close()
		t.Fatal("PrepareCreate succeeded although the instances dir's parent is missing")
	}
	if exists(parent) {
		t.Errorf("PrepareCreate created %s, want nothing created", parent)
	}
}

// C7
func TestNewInstanceFlavor(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    manifest.Release
		want manifest.Flavor
	}{
		{"pinned java 17 url", manifest.Release{Version: "2.9.0",
			Java17URL: "https://downloads.gtnewhorizons.com/a17.zip", Java8URL: "https://downloads.gtnewhorizons.com/a8.zip"}, manifest.Java17},
		{"no java 17 url", manifest.Release{Version: "2.8.0",
			Java8URL: "https://downloads.gtnewhorizons.com/a8.zip"}, manifest.Java8},
		{"off-host java 17 url", manifest.Release{Version: "2.9.0",
			Java17URL: "https://evil.example.com/a17.zip", Java8URL: "https://downloads.gtnewhorizons.com/a8.zip"}, manifest.Java8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewInstanceFlavor(tc.r); got != tc.want {
				t.Errorf("NewInstanceFlavor(%+v) = %v, want %v", tc.r, got, tc.want)
			}
		})
	}
}

// C9
func TestCheckInstanceNameRejectsWindowsAndTrailingDotNames(t *testing.T) {
	dir := t.TempDir()
	for _, in := range []string{"CON", "nul", "Prn", "aux", "COM1", "com9", "LPT1", "LPT9.txt", "con.tar.gz", "foo.", "a\x7fb"} {
		t.Run(in, func(t *testing.T) {
			err := CheckInstanceName(dir, in)
			if err == nil || errors.Is(err, ErrInstanceExists) {
				t.Errorf("CheckInstanceName(%q) = %v, want a name error", in, err)
			}
		})
	}
}

// C9
func TestCheckInstanceNameAcceptsNamesThatOnlyLookReserved(t *testing.T) {
	dir := t.TempDir()
	for _, in := range []string{"console", "COM10", "COM0", "LPT0", "COMX", "CONX", "my CON", "nul2", "GT New Horizons 2.8.4", "a~b"} {
		t.Run(in, func(t *testing.T) {
			if err := CheckInstanceName(dir, in); err != nil {
				t.Errorf("CheckInstanceName(%q) = %v, want nil", in, err)
			}
		})
	}
}

// C2
func TestCloseReturnsNilOnZeroCreation(t *testing.T) {
	var c Creation
	if err := c.Close(); err != nil {
		t.Errorf("zero Creation Close() = %v, want nil", err)
	}
}

// C2
func TestCloseReturnsNilAndKeepsFinishedInstance(t *testing.T) {
	env := newCreateEnv(t, createPack())
	c, _ := createInstance(t, env, &recReporter{})
	if err := c.Close(); err != nil {
		t.Errorf("Close() on a finished instance = %v, want nil", err)
	}
	if !exists(filepath.Join(c.Dir, "instance.cfg")) {
		t.Errorf("Close() removed the finished instance")
	}
}

// C2
func TestCloseRemovesUnfinishedFolderAndReturnsNil(t *testing.T) {
	env := newCreateEnv(t, createPack())
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close() on an unfinished instance = %v, want nil", err)
	}
	if exists(c.Dir) {
		t.Errorf("Close() left the unfinished folder %s", c.Dir)
	}
}

// C2: a folder that can't be removed (read-only parent; Linux, not root) is reported.
func TestCloseReportsLeftoverWhenFolderCantBeRemoved(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		return // can't make a folder un-removable portably; the nil paths are covered above
	}
	env := newCreateEnv(t, createPack())
	c, err := PrepareCreate(env.opts, &recReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Dir, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(env.opts.InstancesDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(env.opts.InstancesDir, 0o755) })
	var le *LeftoverError
	if err := c.Close(); !errors.As(err, &le) || le.Dir != c.Dir {
		t.Errorf("Close() on an un-removable folder = %v, want a LeftoverError for %s", err, c.Dir)
	}
}

// C3
func TestLeftoverErrorStartsWithSentenceThenPathAndCause(t *testing.T) {
	const sentence = "I couldn't remove the half-made instance folder. Delete it yourself before trying again."
	dir := filepath.Join("instances", "Half Made")
	msg := (&LeftoverError{Dir: dir, Err: errors.New("device busy")}).Error()
	rest, ok := strings.CutPrefix(msg, sentence)
	if !ok {
		t.Fatalf("Error() = %q, want it to start with %q", msg, sentence)
	}
	i, j := strings.Index(rest, dir), strings.Index(rest, "device busy")
	if i < 0 || j < 0 || j < i {
		t.Errorf("Error() after the sentence = %q, want the path, then the cause", rest)
	}
}
