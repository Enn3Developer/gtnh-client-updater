package tui

import "github.com/charmbracelet/bubbles/textinput"

// serverModsIntro explains the server extra mods link wherever the player is asked for it.
const serverModsIntro = "Some servers add a few mods on top of GTNH. If the server owner gave you a link for " +
	"them, paste it here — I'll install them now and keep them in sync every time you update."

// msgBadModsLink is shown when a server extra mods link is rejected.
const msgBadModsLink = "That doesn't look like a download link — it should start with https://"

func (m *model) serverModsView() (body, footer string) {
	return m.inputView("Does your server have its own extra mods?", serverModsIntro,
		m.input, m.inputEr, "No link? Leave it empty — you can add one later with m on the version list.")
}

func (m *model) nameView() (body, footer string) {
	return m.inputView("What should the new instance be called?",
		"That's the name you'll see in Prism; it's also the folder name.",
		m.nameIn, m.nameEr, "It'll be created in "+m.instancesDir())
}

// inputView is a text-input screen: title, intro, the input, its error (if any) and a
// dim footnote.
func (m *model) inputView(title, intro string, input textinput.Model, errText, footnote string) (body, footer string) {
	b := m.blocks()
	b.title(title)
	b.para(intro)
	b.raw(input.View() + "\n")
	if errText != "" {
		b.raw(badSty.Render(wrap(errText, m.bodyWidth())) + "\n")
	}
	b.raw("\n" + dimSty.Render(wrap(footnote, m.bodyWidth())))
	return b.String(), hint("enter", "continue", "esc", "back")
}
