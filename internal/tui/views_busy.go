package tui

import (
	"fmt"
	"strings"
	"time"
)

func (m *model) busyView() (body, footer string) {
	var title string
	switch {
	case m.screen == scSelfUpdate:
		title = "Updating the launcher"
	case m.screen == scRestoring:
		title = fmt.Sprintf("Putting %s back on GTNH %s", m.inst.Name, m.backup.Info.From)
	case m.creating && m.screen == scApplying:
		title = "Creating " + m.newName
	case m.creating:
		title = "Getting GTNH " + m.target + " ready"
	case m.screen == scApplying:
		title = fmt.Sprintf("Updating %s to GTNH %s", m.inst.Name, m.target)
	default:
		title = fmt.Sprintf("Getting GTNH %s ready for %s", m.target, m.inst.Name)
	}
	var steps []string
	for _, s := range m.steps {
		steps = append(steps, okSty.Render("✓ ")+dimSty.Render(indentWrap(s, m.bodyWidth()-6, 2)))
	}
	if m.step != "" {
		steps = append(steps, m.spin.View()+indentWrap(m.step, m.bodyWidth()-6, 2))
	}
	if m.total > 0 {
		pct := float64(m.done) / float64(m.total)
		steps = append(steps, "", m.bar.ViewAs(pct), dimSty.Render(m.progressDetail()))
	}
	body = titleSty.Render(wrap(title, m.bodyWidth())) + "\n\n" + panel("", strings.Join(steps, "\n"), m.bodyWidth()) + "\n" + m.headsUp()
	switch {
	case m.screen != scPreparing:
		footer = warnSty.Render(indentWrap("Please don't close this window until I'm done.", m.bodyWidth(), 0))
	case m.quitAfterCancel:
		footer = dimSty.Render(wrap("Stopping and cleaning up…", m.bodyWidth()))
	default:
		footer = hint("ctrl+c", "cancel")
	}
	return body, footer
}

func (m *model) progressDetail() string {
	var s string
	if m.total > 1_000_000 {
		s = fmt.Sprintf("%s of %s", mb(m.done), mb(m.total))
	} else {
		s = fmt.Sprintf("%s of %s files", num(m.done), num(m.total))
	}
	if left := eta(m.done, m.total, time.Since(m.stepStart)); left != "" {
		s += " · " + left
	}
	return s
}
