package tui

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

// load lists the instances, which needs no network: the manifest comes on its own
// (fetchManifest), so Play and the settings work offline and on a slow connection.
func (m *model) load() tea.Msg {
	if len(m.cfg.PrismDirs) == 0 {
		return noPrismMsg{}
	}
	return loadedMsg{insts: listInstances(m.cfg.PrismDirs)} // no instances at all: the page offers to create one
}

// noPrismText is the page when no Prism data dir was found; enter asks for it.
const noPrismText = "I start your GTNH instances through Prism Launcher (prismlauncher.org), so it needs to be installed. " +
	"If it is, press enter and show me the folder where Prism keeps its data — the one with prismlauncher.cfg in it."

// noPrismView is the page when no Prism data dir was found: what's wrong in red, then
// what to do, wrapped to width.
func noPrismView(width int) string {
	return badSty.Render("I couldn't find Prism Launcher on this computer.") + "\n\n" + wrap(noPrismText, width)
}

// askPrismDir asks for Prism's data folder, remembers it in the launcher settings and
// loads from it.
func (m *model) askPrismDir() {
	m.openInput("Where does Prism keep its data?", "Paste the folder's path here. For a portable Prism, it's the folder Prism itself is in.",
		"", prismDirExample(), "", []string{"Continue"},
		func(m *model, label, value string) tea.Cmd {
			if label == "" {
				m.closeDialog()
				return nil
			}
			dir, ok := prism.DataDirAt(cleanPath(value))
			if !ok {
				m.dialog.inputErr = msgBadPrismDir
				return nil
			}
			m.closeDialog()
			m.app.PrismDir = dir
			if err := m.saveApp(m.app); err != nil {
				m.notify(couldntSaveSetting, "I'll use that folder now, but I couldn't remember it for next time: "+err.Error())
			}
			m.cfg.PrismDirs, m.noPrism = []string{dir}, false
			return tea.Batch(m.load, m.fetchManifest)
		})
}

const msgBadPrismDir = "I can't find Prism's files there — it's the folder with prismlauncher.cfg in it."

// prismDirExample is where Prism keeps its data on this OS, as the field's placeholder.
func prismDirExample() string {
	switch runtime.GOOS {
	case "windows":
		return `C:\Users\you\AppData\Roaming\PrismLauncher`
	case "darwin":
		return "~/Library/Application Support/PrismLauncher"
	}
	return "~/.local/share/PrismLauncher"
}

// fetchManifest gets the list of GTNH versions and sends manifestMsg.
func (m *model) fetchManifest() tea.Msg {
	man, err := manifest.Fetch(m.cfg.Client)
	return manifestMsg{man, err}
}

// retryManifest fetches the manifest again after a failed fetch.
func (m *model) retryManifest() tea.Cmd {
	if m.manifest != nil || m.manifestErr == nil {
		return nil
	}
	m.manifestErr = nil
	return m.fetchManifest
}

// onManifest takes the fetched manifest (or why it couldn't be fetched), refreshes the
// page and runs what the command line asked for once it's known.
func (m *model) onManifest(msg manifestMsg) tea.Cmd {
	if msg.err != nil {
		m.manifestErr = msg.err
	} else {
		m.manifest, m.manifestErr = msg.m, nil
	}
	if !m.loaded {
		return nil
	}
	m.refresh()
	if !m.startPending {
		return nil
	}
	m.startPending = false
	if m.manifest == nil {
		m.offlineNotice()
		return nil
	}
	return m.startVersionTask()
}

// offlineNotice tells the player that something needs the GTNH server, which can't be
// reached right now (or is still being asked).
func (m *model) offlineNotice() {
	if m.manifestErr == nil {
		m.notify("Just a moment", "I'm still asking the GTNH download server which versions exist. Try again in a few seconds.")
		return
	}
	m.notify(cantReachGTNH, "I need the GTNH download server for that, and I can't reach it right now. "+
		"Check your internet connection, then press r to try again.\n\n("+netReason(m.manifestErr)+")")
}

// netReason is err without the request it came from: what went wrong on the network.
func netReason(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

// cantReachGTNH heads what's said when the GTNH manifest couldn't be fetched.
const cantReachGTNH = "I couldn't reach the GTNH download server"

// listInstances lists the instances of every Prism data dir; unreadable dirs are skipped.
func listInstances(dirs []string) []prism.Instance {
	var insts []prism.Instance
	for _, d := range dirs {
		if found, err := prism.ListInstances(prism.InstancesDir(d)); err == nil {
			insts = append(insts, found...)
		}
	}
	return insts
}

// afterLoad marks the workspace loaded, preselects Config.Instance (by name or folder),
// refreshes and starts the creation Config.Create asks for, else the update Config.Target
// asks for (both wait for the manifest), else the game when Config.Play asks to (its mods
// synced from Config.ServerMods when given).
func (m *model) afterLoad() tea.Cmd {
	m.loaded = true
	m.focus = focusSidebar
	if m.cfg.Instance != "" {
		in, err := m.cfgInstance()
		if err != nil {
			m.refresh()
			return errCmd(err)
		}
		if !in.GTNH {
			m.showAll = true
		}
		m.selectDir(in.Dir)
	}
	m.refresh()
	if _, ok := m.current(); m.cfg.Create || ok && m.cfg.Target != "" {
		switch {
		case m.manifest != nil:
			return m.startVersionTask()
		case m.manifestErr != nil:
			m.offlineNotice()
		default:
			m.startPending = true // once the manifest is here
		}
		return nil
	}
	if m.cfg.Play {
		m.modsOverride = m.cfg.ServerMods
		return m.playCmd(false)
	}
	return nil
}

// startVersionTask starts the creation Config.Create asks for, else the update to
// Config.Target; both need the manifest.
func (m *model) startVersionTask() tea.Cmd {
	if m.cfg.Create {
		return m.newInstance()
	}
	v, err := manifest.Resolve(m.manifest, m.cfg.Target)
	if err != nil {
		m.notify("No such version", fmt.Sprintf("GTNH has no version called %q.", m.cfg.Target))
		return nil
	}
	return m.startUpdate(v)
}

// cfgInstance finds the instance named on the command line, by name or folder; a folder
// outside the scanned Prism dirs is loaded and added to m.insts.
func (m *model) cfgInstance() (prism.Instance, error) {
	for _, in := range m.insts {
		if in.Name == m.cfg.Instance || sameDir(in.Dir, m.cfg.Instance) {
			return in, nil
		}
	}
	if in, err := prism.LoadInstance(m.cfg.Instance); err == nil {
		m.insts = append(m.insts, in)
		return in, nil
	}
	return prism.Instance{}, fmt.Errorf("I couldn't find an instance called %q.", m.cfg.Instance)
}

// cleanPath is a path as a player pastes it, made usable: spaces around it and the
// quotes Windows' "Copy as path" adds go, and a leading ~ is the home folder.
func cleanPath(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	if v == "~" || strings.HasPrefix(v, "~/") || strings.HasPrefix(v, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			v = filepath.Join(home, v[1:])
		}
	}
	return v
}

func sameDir(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
}

// reload lists the instances again (the manifest is kept) and sends reloadedMsg.
func (m *model) reload() tea.Cmd {
	dirs := m.cfg.PrismDirs
	return func() tea.Msg { return reloadedMsg{listInstances(dirs)} }
}

// onReloaded takes the new instance list, keeping sel on the same Dir (else 0), and
// refreshes.
func (m *model) onReloaded(msg reloadedMsg) {
	dir := ""
	if in, ok := m.current(); ok {
		dir = in.Dir
	}
	m.insts = msg.insts
	m.selectDir(dir)
	m.refresh()
}
