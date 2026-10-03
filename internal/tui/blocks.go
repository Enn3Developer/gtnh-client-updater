package tui

// Message blocks: the titles, bullets and notes the decision and result screens are
// built from, and the texts more than one of those screens shows.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// worldsStayBullet promises the player's own data is left alone.
const worldsStayBullet = "Your worlds, screenshots, maps and game settings stay exactly as they are."

// downgradeWarning warns that going back to an older pack can break worlds played on ver.
func downgradeWarning(ver string) string {
	return fmt.Sprintf("This goes BACK to an older version. Worlds you played on %s may lose "+
		"blocks and items or not load at all. Copy your saves folder somewhere safe first.", ver)
}

// renamedBullet reports that the instance got a new name in Prism.
func renamedBullet(name string) string {
	return "Renamed the instance in Prism to \"" + name + "\"."
}

// blocks builds a screen body out of message blocks wrapped to the body width.
type blocks struct {
	m *model
	b strings.Builder
}

func (m *model) blocks() *blocks { return &blocks{m: m} }

func (b *blocks) String() string { return b.b.String() }

func (b *blocks) title(s string) { b.note(titleSty, s) }

func (b *blocks) success(s string) { b.note(okSty.Bold(true), s) }

// danger is a red warning; s comes without the "! " it gets.
func (b *blocks) danger(s string) { b.note(badSty, "! "+s) }

func (b *blocks) note(sty lipgloss.Style, s string) {
	b.b.WriteString(sty.Render(wrap(s, b.m.bodyWidth())) + "\n\n")
}

func (b *blocks) para(s string) { b.b.WriteString(wrap(s, b.m.bodyWidth()) + "\n\n") }

func (b *blocks) bullet(s string) { b.b.WriteString(b.m.bullet(s)) }

// warnBullet is a bullet in the warning colour.
func (b *blocks) warnBullet(s string) {
	lines := strings.Split(strings.TrimSuffix(b.m.bullet(s), "\n"), "\n")
	for i, l := range lines {
		lines[i] = warnSty.Render(l) // per line, so the bullet and wrapped lines keep the colour
	}
	b.b.WriteString(strings.Join(lines, "\n") + "\n")
}

func (b *blocks) headsUp() { b.b.WriteString(b.m.headsUp()) }

func (b *blocks) closeMinecraftNote() { b.b.WriteString(b.m.closeMinecraftNote()) }

func (b *blocks) blank() { b.b.WriteString("\n") }

// raw writes s as is, for lines whose spacing no other block has.
func (b *blocks) raw(s string) { b.b.WriteString(s) }

// headsUp lists the warnings of the current run, set off by blank lines; "" without any.
func (m *model) headsUp() string {
	if len(m.warns) == 0 {
		return ""
	}
	blocks := make([]string, len(m.warns))
	for i, w := range m.warns {
		blocks[i] = warnSty.Render(indentWrap("Heads up: "+w, m.bodyWidth(), 10))
	}
	return "\n" + strings.Join(blocks, "\n") + "\n"
}

// closeMinecraftNote asks the player to close the game when pickInstance couldn't tell
// whether it runs.
func (m *model) closeMinecraftNote() string {
	if !m.runUnknown {
		return ""
	}
	return "\n" + warnSty.Render("  Make sure Minecraft is closed before you continue.") + "\n"
}

// bullet renders "  • text" with wrapped lines indented under the text.
func (m *model) bullet(s string) string {
	lines := strings.Split(wrap(s, m.bodyWidth()-4), "\n")
	for i, l := range lines {
		prefix := "    "
		if i == 0 {
			prefix = "  • "
		}
		lines[i] = prefix + strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

// indentWrap wraps s to width and indents the continuation lines by indent spaces.
func indentWrap(s string, width, indent int) string {
	return strings.ReplaceAll(strings.TrimRight(wrap(s, width), " "), "\n", "\n"+strings.Repeat(" ", indent))
}

func wrap(s string, width int) string {
	if width < 20 {
		return s
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}
