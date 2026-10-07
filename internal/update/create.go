package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/pack"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// CreateOptions describes one new instance to create from a GTNH pack.
type CreateOptions struct {
	Context         context.Context // cancels the download; nil = never
	Client          *http.Client
	Manifest        *manifest.Manifest
	InstancesDir    string // Prism instances dir (prism.InstancesDir(dataDir))
	Name            string // display name; also the folder name under InstancesDir
	Target          string // manifest version key
	CustomModsURL   string // "" = none
	CustomModsAsked bool   // saved verbatim into State.CustomModsAsked
	ServerAddress   string // saved verbatim into State.ServerAddress; "" = none
}

// Creation is a prepared new instance: pack downloaded and verified into the new
// folder. Call Apply to write it out, or Close to throw it away.
type Creation struct {
	Dir      string         // <InstancesDir>/<Name>
	Instance prism.Instance // Dir, Name, GameDir = Dir/.minecraft, GTNH = true
	Flavor   manifest.Flavor
	Files    int // pack entries that will be written (every entry, instance.cfg included)
	// ModsPlan is what installing the server's mods will do (nil: no link, or the
	// archive couldn't be fetched: ModsErr says why).
	ModsPlan *ModsPlan
	ModsErr  error

	opts    CreateOptions
	pack    *pack.Pack
	zipPath string
	mods    *ModsArchive
}

// CreateResult summarizes a finished creation.
type CreateResult struct {
	Instance prism.Instance
	Files    int       // files written (same as Creation.Files)
	Mods     *ModsPlan // what installing the server's mods did; nil when none ran or it failed
	ModsErr  error     // why the server's mods couldn't be fetched or installed
}

// ErrInstanceExists means the chosen name is already taken in the instances dir.
var ErrInstanceExists = errors.New("an instance with that name already exists")

// LeftoverError means a failed creation couldn't remove its half-made folder; the player
// has to delete it before the name can be used again.
type LeftoverError struct {
	Dir string
	Err error
}

func (e *LeftoverError) Error() string {
	return fmt.Sprintf("I couldn't remove the half-made instance folder. Delete it yourself before trying again. It's here: %s (%v)", e.Dir, e.Err)
}

func (e *LeftoverError) Unwrap() error { return e.Err }

// removeHalfMade removes the folder of a creation that failed with err, adding a
// LeftoverError to err if the folder can't be removed.
func removeHalfMade(dir string, err error) error {
	if rmErr := os.RemoveAll(dir); rmErr != nil {
		return errors.Join(err, &LeftoverError{Dir: dir, Err: rmErr})
	}
	return err
}

// NewInstanceFlavor is the pack flavor a new instance of r gets: the Java 17+ pack when
// r has a usable one, else the Java 8 pack.
func NewInstanceFlavor(r manifest.Release) manifest.Flavor {
	if _, err := r.URL(manifest.Java17); err == nil {
		return manifest.Java17
	}
	return manifest.Java8
}

// DefaultInstanceName is the name offered for a new instance of version.
func DefaultInstanceName(version string) string {
	return "GT New Horizons " + version
}

// badNameChars can't appear in a folder name on at least one of the supported OSes.
const badNameChars = `/\:*?"<>|`

// existsError is a player-facing "name taken" message that still matches
// ErrInstanceExists with errors.Is.
type existsError struct{ name string }

func (e existsError) Error() string {
	return fmt.Sprintf("There's already a folder called %q in your Prism instances. Pick another name.", e.name)
}

func (existsError) Unwrap() error { return ErrInstanceExists }

// CheckInstanceName reports, as a player-facing sentence, why name can't be used as a
// new instance folder in instancesDir; nil if it can.
func CheckInstanceName(instancesDir, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("The name can't be empty.")
	}
	if strings.ContainsAny(name, badNameChars) {
		return errors.New(`A name can't contain / \ : * ? " < > |`)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7F {
			return errors.New("A name can't contain tabs or other invisible characters.")
		}
	}
	// Prism and ListInstances hide folders starting with "." or "_" ("." and ".." too).
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return errors.New(`A name can't start with "." or "_" (Prism would hide it).`)
	}
	// Windows drops a trailing dot (trailing spaces are already trimmed).
	if strings.HasSuffix(name, ".") {
		return errors.New("A name can't end with a dot.")
	}
	if windowsReserved(name) {
		return errors.New("Windows doesn't allow that name.")
	}
	if _, err := os.Lstat(filepath.Join(instancesDir, name)); err == nil {
		return existsError{name}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("I couldn't check your Prism instances folder: %w", err)
	}
	return nil
}

// windowsReserved reports whether name is a Windows device name (CON, PRN, AUX, NUL,
// COM1-COM9, LPT1-LPT9), with or without an extension, in any case.
func windowsReserved(name string) bool {
	base, _, _ := strings.Cut(name, ".")
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	return len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) &&
		base[3] >= '1' && base[3] <= '9'
}

// PrepareCreate checks the name, creates the instance folder and downloads and verifies
// the pack into it. On error nothing is left on disk (or the error includes a
// LeftoverError). Only InstancesDir itself and <InstancesDir>/<Name> are ever created.
func PrepareCreate(opts CreateOptions, rep Reporter) (_ *Creation, err error) {
	opts.Name = strings.TrimSpace(opts.Name)
	if err := CheckInstanceName(opts.InstancesDir, opts.Name); err != nil {
		return nil, err
	}
	target, ok := opts.Manifest.Find(opts.Target)
	if !ok {
		return nil, fmt.Errorf("I don't know a GTNH version called %q.", opts.Target)
	}
	flavor := NewInstanceFlavor(target)
	url, err := target.URL(flavor)
	if err != nil {
		return nil, fmt.Errorf("I couldn't download GTNH %s: %w", target.Version, err)
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	// Mkdir, not MkdirAll: a missing parent means a wrong Prism setting, not something
	// to build.
	if err := os.Mkdir(opts.InstancesDir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("I couldn't create the instance folder: %w", err)
	}
	dir := filepath.Join(opts.InstancesDir, opts.Name)
	// Mkdir, not MkdirAll: a folder that appeared since the check must never be adopted
	// (and later removed) as ours.
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, existsError{opts.Name}
		}
		return nil, fmt.Errorf("I couldn't create the instance folder: %w", err)
	}
	cr := &Creation{
		Dir:      dir,
		Instance: prism.Instance{Dir: dir, Name: opts.Name, GameDir: filepath.Join(dir, ".minecraft"), GTNH: true},
		Flavor:   flavor,
		opts:     opts,
	}
	defer func() {
		if err != nil {
			cr.release()
			err = removeHalfMade(dir, err)
		}
	}()
	stateDir := filepath.Join(dir, StateDir)
	if err := os.Mkdir(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("I couldn't create the instance folder: %w", err)
	}
	rep.Step("Downloading GTNH " + target.Version)
	cr.zipPath = filepath.Join(stateDir, "download.zip")
	if err := pack.DownloadContext(ctx, opts.Client, url, cr.zipPath, rep.Progress); err != nil {
		return nil, fmt.Errorf("I couldn't download GTNH %s: %w", target.Version, err)
	}
	if cr.pack, err = pack.OpenFile(cr.zipPath); err != nil {
		return nil, fmt.Errorf("The download of GTNH %s is damaged: %w", target.Version, err)
	}
	rep.Step("Checking the download")
	if err := cr.pack.Verify(itemProgress(rep)); err != nil {
		return nil, fmt.Errorf("The download of GTNH %s is damaged: %w", target.Version, err)
	}
	cr.Files = len(cr.pack.Entries)
	if opts.CustomModsURL != "" {
		// Fetched here, where the player can still cancel, so Apply never waits on the
		// network. A failure is not fatal: the instance just starts without them.
		rep.Step("Getting your server's mods")
		cr.mods, cr.ModsErr = fetchMods(ctx, opts.Client, opts.CustomModsURL, stateDir, rep.Progress)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("I couldn't download your server's mods: %w", ctx.Err())
		}
		if cr.ModsErr != nil {
			rep.Warn("I couldn't get your server's mods: " + cr.ModsErr.Error() + ".")
		} else {
			cr.ModsPlan = planMods(cr.modsInput(false))
		}
	}
	return cr, nil
}

// modsInput is what installing the server's mods into the new instance looks at: once
// the pack's files are written (ids true), or as they will be (a preview).
func (c *Creation) modsInput(ids bool) modsInput {
	fps := c.pack.Fingerprints()
	in := modsInputFor(c.mods, c.Instance, nil, fps, ids, true)
	if !ids {
		var files []string
		for p := range fps {
			if name, ok := strings.CutPrefix(p, pack.GameDir+"mods/"); ok && !strings.Contains(name, "/") {
				files = append(files, name)
			}
		}
		sort.Strings(files)
		in.files, in.pack = files, packJars(fps, files)
		in.fp = func(name string) (pack.Fingerprint, bool) {
			fp, ok := fps[pack.GameDir+"mods/"+name]
			return fp, ok
		}
	}
	return in
}

// Apply writes the pack into the new instance, installs the server's extra mods, saves
// the state and writes instance.cfg last, so its presence means the instance is
// finished. On error the instance folder is removed. It always releases the download.
func (c *Creation) Apply(rep Reporter) (res *CreateResult, err error) {
	if c.pack == nil {
		return nil, errors.New("There's nothing to install: this instance was already created or thrown away.")
	}
	defer func() {
		c.release()
		if err != nil {
			res = nil
			err = removeHalfMade(c.Dir, err)
		}
	}()
	cfg, err := c.instanceCfg()
	if err != nil {
		return nil, fmt.Errorf("The download of GTNH %s is damaged: %w", c.opts.Target, err)
	}
	rep.Step("Installing files")
	done := 0
	for _, p := range c.pack.Paths() {
		if p == "instance.cfg" {
			continue // written last: its presence marks the instance as finished
		}
		if err := writeNew(DiskPath(c.Instance, p), c.pack.Entries[p].Open); err != nil {
			return nil, fmt.Errorf("I couldn't write %s: %w", p, err)
		}
		done++
		rep.Progress(int64(done), int64(c.Files))
	}
	if _, ok := c.pack.Entries["instance.cfg"]; ok {
		done++ // counted here so the progress bar ends with the files step
		rep.Progress(int64(done), int64(c.Files))
	}

	fps := c.pack.Fingerprints()
	res = &CreateResult{Instance: c.Instance, Files: c.Files, ModsErr: c.ModsErr}
	st := &State{Version: c.opts.Target, Baseline: fps,
		CustomModsURL: c.opts.CustomModsURL, CustomModsAsked: c.opts.CustomModsAsked,
		ServerAddress: c.opts.ServerAddress}
	if c.mods != nil {
		rep.Step("Installing your server's mods")
		mp := planMods(c.modsInput(true))
		if err := applyModsPlan(mp, c.mods, c.Instance); err != nil {
			res.ModsErr = err // shown in the summary; not fatal
		} else {
			res.Mods = mp
			setManaged(st, mp.Managed)
			st.CustomModsSynced = time.Now()
		}
	}
	if err := SaveState(c.Dir, st); err != nil {
		return nil, fmt.Errorf("I couldn't save my notes about the new instance: %w", err)
	}
	open := func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(cfg)), nil }
	if err := writeNew(filepath.Join(c.Dir, "instance.cfg"), open); err != nil {
		return nil, fmt.Errorf("I couldn't write instance.cfg: %w", err)
	}
	return res, nil
}

// instanceCfg is the pack's instance.cfg carrying the player's chosen name.
func (c *Creation) instanceCfg() ([]byte, error) {
	e, ok := c.pack.Entries["instance.cfg"]
	if !ok {
		return []byte("[General]\nInstanceType=OneSix\nname=" + c.Instance.Name + "\n"), nil
	}
	rc, err := e.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var b strings.Builder
	if _, err := io.Copy(&b, rc); err != nil {
		return nil, err
	}
	return []byte(prism.SetName(b.String(), c.Instance.Name)), nil
}

// writeNew writes open's content to disk via a temp file and rename.
func writeNew(disk string, open func() (io.ReadCloser, error)) error {
	tmp, err := writeTemp(disk, open)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, disk); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// release closes and deletes the download and closes the server's archive. Safe to call
// more than once.
func (c *Creation) release() {
	if c.pack != nil {
		c.pack.Close()
		c.pack = nil
	}
	if c.zipPath != "" {
		os.Remove(c.zipPath)
		c.zipPath = ""
	}
	c.mods.Close()
}

// Close releases the download and removes the instance folder unless Apply finished
// (instance.cfg is written last). A folder it couldn't remove is a *LeftoverError; nil
// otherwise. Safe to call more than once and on a zero Creation.
func (c *Creation) Close() error {
	if c.Dir == "" {
		return nil
	}
	c.release()
	if _, err := os.Stat(filepath.Join(c.Dir, "instance.cfg")); errors.Is(err, os.ErrNotExist) {
		if err := os.RemoveAll(c.Dir); err != nil {
			return &LeftoverError{Dir: c.Dir, Err: err}
		}
	}
	return nil
}
