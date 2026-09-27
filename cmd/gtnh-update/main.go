// Command gtnh-update updates a GT: New Horizons Prism Launcher instance in place to any
// version from the official GTNH manifest, keeping worlds and the player's settings, and
// optionally syncs a server's extra mods.
//
// Run it without arguments for the interactive TUI. For scripting:
//
//	gtnh-update -list
//	gtnh-update -instance <dir|name> -version <ver|latest|latest-stable> -yes
package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
	"github.com/Enn3Developer/gtnh-client-updater/internal/tui"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

var version = "dev" // set by the release build: -ldflags "-X main.version=…"

// packaged names the package manager that installed this binary (e.g. "AUR"), set by
// distro builds with -ldflags "-X main.packaged=…". Such builds never self-update: the
// binary is owned by the package manager, which is also how it gets updated.
var packaged = ""

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		prismDir   = flag.String("prism-dir", "", "Prism data folder (the one with prismlauncher.cfg); default: find it automatically")
		instance   = flag.String("instance", "", "instance to update: its folder or its name in Prism")
		target     = flag.String("version", "", "GTNH version to install: e.g. 2.8.4, \"latest\" or \"latest-stable\"")
		installed  = flag.String("installed", "", "the GTNH version the instance is on now, if it's detected wrong")
		serverMods = flag.String("server-mods", "", "link to your server's extra mods archive (https://…), or \"none\"; default: what the instance remembers")
		yes        = flag.Bool("yes", false, "don't ask anything, just update (needs -instance and -version)")
		list       = flag.Bool("list", false, "show your instances and the available GTNH versions")
		selfUpd    = flag.Bool("self-update", false, "update gtnh-update itself to the newest release")
		noCheck    = flag.Bool("no-update-check", false, "don't check GitHub for a newer gtnh-update")
		showVer    = flag.Bool("V", false, "print the gtnh-update version")
	)
	flag.Parse()
	selfupdate.CleanupOld()
	if *showVer {
		if packaged != "" {
			fmt.Printf("gtnh-update %s (%s)\n", version, packaged)
		} else {
			fmt.Println("gtnh-update", version)
		}
		return nil
	}
	updateCheck := !*noCheck && packaged == ""
	if *serverMods != "" && *serverMods != "none" {
		if err := update.CheckCustomModsURL(*serverMods); err != nil {
			return err
		}
	}
	dirs := prism.DataDirs()
	if *prismDir != "" {
		dirs = []string{*prismDir}
	}
	client := &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}}

	switch {
	case *selfUpd:
		return selfUpdate(client)
	case *list:
		return listAll(client, dirs)
	case *yes:
		if *instance == "" || *target == "" {
			return errors.New("-yes needs -instance and -version")
		}
		if updateCheck {
			noticeNewer(client)
		}
		return headless(client, dirs, *instance, *installed, *target, *serverMods)
	}
	out, err := tui.Run(tui.Config{
		Client: client, AppVersion: version, PrismDirs: dirs, Instance: *instance,
		Installed: *installed, Target: *target, ServerMods: *serverMods, UpdateCheck: updateCheck,
	})
	if err != nil {
		return err
	}
	if out.Restart {
		return selfupdate.Restart()
	}
	return nil
}

func selfUpdate(client *http.Client) error {
	if packaged != "" {
		return fmt.Errorf("this gtnh-update was installed with %s -- update it with your package manager instead", packaged)
	}
	rel, err := selfupdate.Latest(client)
	if err != nil {
		return fmt.Errorf("couldn't check for a new version: %w", err)
	}
	if !selfupdate.Newer(version, rel.Version) {
		fmt.Printf("gtnh-update %s is the newest version.\n", version)
		return nil
	}
	fmt.Printf("Updating gtnh-update %s -> %s…\n", version, rel.Version)
	if err := rel.Apply(client, nil); err != nil {
		if errors.Is(err, selfupdate.ErrNotWritable) {
			return fmt.Errorf("%w -- download it from %s", err, rel.Page)
		}
		return err
	}
	fmt.Println("Done. The next run uses the new version.")
	return nil
}

// noticeNewer prints a one-line hint when a newer release exists; failures are silent.
func noticeNewer(client *http.Client) {
	c := *client
	c.Timeout = 5 * time.Second
	if rel, err := selfupdate.Latest(&c); err == nil && selfupdate.Newer(version, rel.Version) {
		fmt.Printf("note: gtnh-update %s is available (you have %s) -- run with -self-update to get it\n", rel.Version, version)
	}
}

func resolveTarget(m *manifest.Manifest, t string) (string, error) {
	switch t {
	case "latest":
		return m.Releases[0].Version, nil
	case "latest-stable":
		for _, r := range m.Releases {
			if r.Stable() {
				return r.Version, nil
			}
		}
		return "", errors.New("the GTNH manifest has no stable release")
	}
	if _, ok := m.Find(t); !ok {
		return "", fmt.Errorf("GTNH has no version called %q (see -list)", t)
	}
	return t, nil
}

func findInstance(dirs []string, want string) (prism.Instance, error) {
	if in, err := prism.LoadInstance(want); err == nil {
		return in, nil
	}
	for _, d := range dirs {
		insts, _ := prism.ListInstances(prism.InstancesDir(d))
		for _, in := range insts {
			if in.Name == want {
				return in, nil
			}
		}
	}
	return prism.Instance{}, fmt.Errorf("no instance called %q (see -list)", want)
}

func listAll(client *http.Client, dirs []string) error {
	m, err := manifest.Fetch(client)
	if err != nil {
		return err
	}
	if len(dirs) == 0 {
		fmt.Println("Prism Launcher not found -- pass -prism-dir.")
	}
	fmt.Println("Your instances:")
	for _, d := range dirs {
		insts, _ := prism.ListInstances(prism.InstancesDir(d))
		for _, in := range insts {
			st, _ := update.LoadState(in.Dir)
			det := update.DetectVersion(in, st, m)
			what := "not GTNH"
			if in.GTNH {
				what = "GTNH " + orDefault(det.Version, "(version unknown)")
			}
			fmt.Printf("  %-42s %s\n    %s\n", in.Name, what, in.Dir)
		}
	}
	fmt.Println("\nGTNH versions (newest first):")
	for _, r := range m.Releases {
		date := "unknown date"
		if !r.ReleaseDate.IsZero() {
			date = r.ReleaseDate.Format("2006-01-02")
		}
		fmt.Printf("  %-22s %s  %s\n", r.Version, date, r.Title)
	}
	return nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// textReporter prints progress for non-interactive runs.
type textReporter struct {
	last    time.Time
	lastPct int
}

func (r *textReporter) Step(s string) { fmt.Println("•", s); r.lastPct = -1 }
func (r *textReporter) Warn(s string) { fmt.Println("  heads up:", s) }
func (r *textReporter) Progress(done, total int64) {
	if total <= 0 {
		return
	}
	pct := int(done * 100 / total)
	if pct == r.lastPct || (pct%25 != 0 && time.Since(r.last) < 5*time.Second) {
		return
	}
	r.last, r.lastPct = time.Now(), pct
	fmt.Printf("    %d%%\n", pct)
}

func headless(client *http.Client, dirs []string, instName, installed, target, serverMods string) error {
	m, err := manifest.Fetch(client)
	if err != nil {
		return err
	}
	inst, err := findInstance(dirs, instName)
	if err != nil {
		return err
	}
	if prism.Running(inst) {
		return fmt.Errorf("%s is running -- close Minecraft first", inst.Name)
	}
	if target, err = resolveTarget(m, target); err != nil {
		return err
	}
	st, err := update.LoadState(inst.Dir)
	if err != nil {
		return err
	}
	if installed == "" {
		det := update.DetectVersion(inst, st, m)
		if det.Version == "" {
			return errors.New("can't tell which GTNH version the instance is on -- pass -installed")
		}
		installed = det.Version
	}
	switch {
	case serverMods == "none":
		serverMods = ""
	case serverMods == "" && st != nil:
		serverMods = st.CustomModsURL
	}
	fmt.Printf("Updating %s from %s to %s\n", inst.Name, installed, target)
	if manifest.CompareVersions(target, installed) < 0 {
		fmt.Printf("  heads up: %s is OLDER than %s -- worlds played on the newer version may break\n", target, installed)
	}
	rep := &textReporter{}
	s, err := update.Prepare(update.Options{
		Client: client, Manifest: m, Instance: inst, Installed: installed, Target: target, CustomModsURL: serverMods,
	}, rep)
	if err != nil {
		return err
	}
	pl := s.Plan
	if pl.BaselineMatch < 0.8 {
		s.Close()
		return fmt.Errorf("only %.0f%% of the mods that come with %s are in this instance, so it's probably on another version -- pass it with -installed",
			pl.BaselineMatch*100, installed)
	}
	res, err := s.Apply(rep)
	if err != nil {
		return err
	}
	fmt.Printf("\nDone: %s is now on GTNH %s.\n", inst.Name, res.To)
	fmt.Printf("  %d files updated, %d removed\n", pl.Count(update.Install), pl.Count(update.Remove))
	if res.Renamed != "" {
		fmt.Printf("  renamed in Prism to %q\n", res.Renamed)
	}
	if cm := res.CustomMods; cm != nil {
		fmt.Printf("  server extra mods: %d installed (%d new or updated, %d removed)\n", len(cm.Installed), len(cm.Added), len(cm.Removed))
		if len(cm.Skipped) > 0 {
			fmt.Println("  skipped (GTNH ships a mod with that name):", strings.Join(cm.Skipped, ", "))
		}
	}
	for _, c := range pl.Conflicts() {
		fmt.Println("  kept your version, new one saved as .mcnew:", c)
	}
	if len(pl.ExtraMods) > 0 {
		fmt.Println("  mods you added yourself, left in place:", strings.Join(pl.ExtraMods, ", "))
	}
	if res.BackupDir != "" {
		fmt.Println("  old files backed up in", res.BackupDir)
	}
	return nil
}
