package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	accent    = lipgloss.Color("#7FB4CA")
	titleSty  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	okColor   = lipgloss.Color("#98BB6C")
	okSty     = lipgloss.NewStyle().Foreground(okColor)
	warnColor = lipgloss.Color("#E6C384")
	warnSty   = lipgloss.NewStyle().Foreground(warnColor)
	badSty    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E46876")).Bold(true)
	dimSty    = lipgloss.NewStyle().Foreground(lipgloss.Color("#727169"))
	barBg     = lipgloss.Color("#2A2A37")
	chipBg    = lipgloss.Color("#363646")
	keySty    = lipgloss.NewStyle().Bold(true).Foreground(accent).Background(chipBg) // no padding: a hint is as wide as its text
	inkColor  = lipgloss.Color("#1F1F28")
	btnSelSty = lipgloss.NewStyle().Background(accent).Foreground(inkColor).Bold(true)
	bannerSty = lipgloss.NewStyle().Foreground(inkColor).Background(warnColor).Padding(0, 1)
)

// titleBar is the full-width bar on top: left text, right text, exactly width columns.
// The right side gives way first, then the left.
func titleBar(left, right string, width int) string {
	if width < 10 {
		return ansi.Truncate(left, width, "…")
	}
	if 1+ansi.StringWidth(left)+1 > width {
		left, right = ansi.Truncate(left, width-2, "…"), ""
	}
	lw := ansi.StringWidth(left)
	if room := width - lw - 3; ansi.StringWidth(right) > room {
		right = ansi.Truncate(right, room, "…")
	}
	gap := width - 2 - lw - ansi.StringWidth(right)
	bar := lipgloss.NewStyle().Background(barBg)
	return bar.Render(" ") + titleSty.Background(barBg).Render(left) + bar.Render(strings.Repeat(" ", gap)) +
		dimSty.Background(barBg).Render(right) + bar.Render(" ")
}

// bodyWindow picks the rows of body that fit on screen, scrolled to scroll (clamped).
// When body overflows, the first/last visible row is replaced by a "more" marker.
func bodyWindow(body []string, rows, scroll int) (visible []string, offset int) {
	rows = max(rows, 1)
	if len(body) <= rows {
		return body, 0
	}
	maxOff := len(body) - rows
	offset = max(min(scroll, maxOff), 0)
	visible = append([]string{}, body[offset:offset+rows]...)
	if offset < maxOff {
		visible[rows-1] = dimSty.Render(fmt.Sprintf("↓ %d more", maxOff-offset))
	}
	if offset > 0 {
		visible[0] = dimSty.Render(fmt.Sprintf("↑ %d more", offset))
	}
	return visible, offset
}

// overlay splices box into the full view base (height lines), centred horizontally and
// vertically between the blank line under the title bar and the status bar (C8). The
// workspace lines under it are dimmed (C1).
func overlay(base, box string, width, height int) string {
	lines := strings.Split(base, "\n")
	for n := 2; n <= height-2 && n < len(lines); n++ {
		lines[n] = backdrop(lines[n])
	}
	boxLines := strings.Split(box, "\n")
	bw := 0
	for _, l := range boxLines {
		bw = max(bw, ansi.StringWidth(l))
	}
	x := max((width-bw)/2, 0)
	top, last := 2, height-3 // 0-based lines 3..height-2: below the title and blank line, above the status bar
	y := top + max((last-top+1-len(boxLines))/2, 0)
	for i, bl := range boxLines {
		n := y + i
		if n > last || n >= len(lines) {
			break
		}
		left := ansi.Truncate(lines[n], x, "")
		if ansi.StringWidth(lines[n]) < x {
			left = padTo(left, x)
		}
		lines[n] = ansi.Truncate(left+bl+ansi.TruncateLeft(lines[n], x+bw, ""), width, "")
	}
	return strings.Join(lines, "\n")
}

// backdrop is one workspace line dimmed for showing under a dialog (C1).
func backdrop(line string) string {
	return dimSty.Render(ansi.Strip(line))
}

func wrap(s string, width int) string {
	if width < 20 {
		return s
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}
