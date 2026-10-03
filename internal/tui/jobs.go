package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
)

// jobKind is what a running job does.
type jobKind int

const (
	jobUpdate jobKind = iota
	jobCreate
	jobRestore
	jobSelf
)

// job is the background work shown inline on its instance's page.
type job struct {
	kind  jobKind
	dir   string // Dir of the instance the job works on
	title string
	phase string // "prepare" | "apply"
}

// jobShown reports whether the running job belongs to the instance at dir.
func (m *model) jobShown(dir string) bool {
	return m.job != nil && m.job.dir == dir
}

// busyApplying reports whether a job is changing files right now.
func (m *model) busyApplying() bool {
	return m.job != nil && m.job.phase == "apply"
}

// jobName is the display name of the job's instance, or its dir's base name.
func (m *model) jobName() string {
	if m.job == nil {
		return ""
	}
	for _, in := range m.insts {
		if in.Dir == m.job.dir {
			return in.Name
		}
	}
	return filepath.Base(m.job.dir)
}

// notifyBusy tells the player that another job has to finish first (C3a).
func (m *model) notifyBusy() {
	m.notify("One thing at a time", "I'm still busy with "+m.jobName()+". Let it finish first.")
}

// jobRow is the row that stands in for the play rows of the job's instance (C5).
func (m *model) jobRow() row {
	return row{id: "job", lines: m.jobBlock}
}

// jobLines are the rendered progress lines of the running job.
func (m *model) jobLines(width int) []string {
	return m.jobBlock(width, false)
}

// jobBlock is the job's title line, progress bar, step log and heads-ups (C5).
func (m *model) jobBlock(width int, sel bool) []string {
	prefix, sty := "  ", lipgloss.NewStyle().Foreground(accent)
	if sel {
		prefix, sty = "▸ ", titleSty
	}
	hint := ""
	if m.busyApplying() {
		hint = "please wait"
	}
	lines := []string{styledRow(width, prefix, sty, "◐ "+m.job.title, hint)}
	if m.total > 0 {
		lines = append(lines, m.progressLine(width))
	}
	if text := m.stepText(); text != "" {
		for _, l := range wrapLines(text, width-2) {
			lines = append(lines, "  "+dimSty.Render(l))
		}
	}
	for _, w := range m.warns {
		for i, l := range wrapLines("Heads up: "+w, width-2) {
			indent := "    "
			if i == 0 {
				indent = "  "
			}
			lines = append(lines, indent+warnSty.Render(l))
		}
	}
	return lines
}

// progressLine is the bar, the percentage and how much of what is done.
func (m *model) progressLine(width int) string {
	bar := progress.New(progress.WithGradient(string(accent), string(okColor)),
		progress.WithWidth(max(min(width-14, 48), 8)), progress.WithoutPercentage())
	detail := num(m.done) + " of " + num(m.total) + " files"
	if m.total > 1_000_000 {
		detail = mb(m.done) + " of " + mb(m.total)
	}
	if e := eta(m.done, m.total, time.Since(m.stepStart)); e != "" {
		detail += " · " + e
	}
	return "  " + bar.ViewAs(float64(m.done)/float64(m.total)) +
		fmt.Sprintf("  %d%%", m.done*100/m.total) + dimSty.Render(" · "+detail)
}

// stepText is the finished steps and the current one behind a spinner frame.
func (m *model) stepText() string {
	parts := make([]string, 0, len(m.steps)+1)
	for _, s := range m.steps {
		parts = append(parts, "✓ "+s)
	}
	if m.step != "" {
		spin := m.spin
		spin.Style = lipgloss.NewStyle()
		parts = append(parts, strings.TrimSpace(spin.View())+" "+m.step)
	}
	return strings.Join(parts, " · ")
}
