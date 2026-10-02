package tui

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
)

func (m *model) load() tea.Msg {
	if len(m.cfg.PrismDirs) == 0 {
		return errMsg{errors.New("I couldn't find Prism Launcher on this computer.\n\n" +
			"If it's installed somewhere unusual, start me with\n  -prism-dir <the folder that contains prismlauncher.cfg>")}
	}
	man, err := manifest.Fetch(m.cfg.Client)
	if err != nil {
		return errMsg{fmt.Errorf("I couldn't reach the GTNH download server to see which versions exist. "+
			"Check your internet connection and try again.\n\n(%w)", err)}
	}
	var insts []prism.Instance
	for _, d := range m.cfg.PrismDirs {
		if found, err := prism.ListInstances(prism.InstancesDir(d)); err == nil {
			insts = append(insts, found...)
		}
	}
	return loadedMsg{man, insts} // no instances at all: afterLoad offers to create one
}
