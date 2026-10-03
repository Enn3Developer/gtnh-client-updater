package tui

import (
	"fmt"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
)

func (m *model) confirmCreateView() (body, footer string) {
	c := m.creation
	b := m.blocks()
	b.title(fmt.Sprintf("Ready to create %s with GTNH %s", m.newName, m.target))
	b.bullet("It'll be a new instance in Prism, in " + c.Dir + ".")
	b.bullet(num(int64(c.Files)) + " files will be installed.")
	b.bullet("Your other instances aren't touched.")
	if m.serverMods != "" {
		b.bullet("Your server's extra mods will be installed from " + hostOf(m.serverMods) + ".")
	}
	if c.Flavor == manifest.Java8 {
		b.bullet("This version only comes as a Java 8 pack, so that's what you'll get.")
	} else {
		b.bullet("It uses the Java 17+ version of the pack.")
	}
	b.headsUp()
	return b.String(), m.buttonFooter("enter", "create it", "esc", "back")
}

func (m *model) createdView() (body, footer string) {
	r := m.created
	b := m.blocks()
	b.success(fmt.Sprintf("All done! %s is ready in Prism.", r.Instance.Name))
	b.bullet(num(int64(r.Files)) + " files installed.")
	b.raw(m.customModsSummary(r.CustomMods, r.CustomErr))
	b.bullet("If Prism is already open and doesn't show it, restart Prism.")
	b.headsUp()
	return b.String(), m.doneFooter()
}
