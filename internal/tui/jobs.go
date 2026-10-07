package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"

	"github.com/Enn3Developer/gtnh-client-updater/internal/manifest"
	"github.com/Enn3Developer/gtnh-client-updater/internal/prism"
	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// jobKind is what a running job does.
type jobKind int

const (
	jobUpdate jobKind = iota
	jobCreate
	jobRestore
	jobSelf
	jobMods // a sync of the server's mods on its own
)

// job is the background work shown inline on its instance's page.
type job struct {
	kind   jobKind
	dir    string // Dir of the instance the job works on
	title  string
	phase  string // "prepare" | "apply"
	backup string // backup dir of a jobRestore; "" otherwise
}

// pending is the instance being created: a synthetic GTNH instance while a jobCreate runs.
func (m *model) pending() (prism.Instance, bool) {
	if m.job == nil || m.job.kind != jobCreate {
		return prism.Instance{}, false
	}
	return prism.Instance{Dir: m.job.dir, Name: m.newName, GTNH: true}, true
}

// jobOutcome is what a failed job means for its instance, per kind and phase (C4, C9,
// C13); "" when the error already says it.
func (m *model) jobOutcome(err error) string {
	switch m.job.kind {
	case jobRestore:
		if errors.Is(err, update.ErrGameRunning) {
			return "Nothing was changed."
		}
		return "Some files may have changed. The backup folder is still there, so you can try again: " + m.job.backup
	case jobCreate:
		var left *update.LeftoverError
		switch {
		case errors.As(err, &left):
			return ""
		case m.job.phase == "prepare":
			return "Nothing was created."
		}
		return "I removed the half-made instance, so there's nothing to clean up."
	case jobSelf:
		return "The launcher wasn't changed."
	}
	return m.updateOutcome(err)
}

// pendingInfo is the page info of the pending instance, read without touching the disk.
func (m *model) pendingInfo() homeInfo {
	flavor := manifest.Java17
	if r, ok := m.release(m.target); ok {
		flavor = update.NewInstanceFlavor(r)
	}
	return homeInfo{gtnh: true, version: m.target, rec: m.target, flavor: flavor}
}

// pendingEntries is the page of the pending instance: the hero and the job block.
func (m *model) pendingEntries() []entry {
	es := textEntries(titleSty.Render(m.newName), dimSty.Render("GTNH "+m.target+" · being created"), "")
	return append(es, rowEntries([]row{m.jobRow()})...)
}

// jobShown reports whether the running job belongs to the instance at dir.
func (m *model) jobShown(dir string) bool {
	return m.job != nil && m.job.dir == dir
}

// busyApplying reports whether a job is changing files right now.
func (m *model) busyApplying() bool {
	return m.job != nil && m.job.phase == "apply"
}

// jobName is what the job works on: the launcher update, the new instance's name, or
// the display name of the job's instance (its dir's base name when unlisted).
func (m *model) jobName() string {
	if m.job == nil {
		return ""
	}
	if m.job.kind == jobSelf {
		return "the launcher update"
	}
	if p, ok := m.pending(); ok {
		return p.Name
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
		for _, l := range m.progressLines(width-2, min(width-12, 40)) {
			lines = append(lines, "  "+l)
		}
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

// progressLines are the bar line (bar of barWidth, percentage) and the dim detail
// line(s) of how much of what is done, wrapped to width (C2).
func (m *model) progressLines(width, barWidth int) []string {
	barWidth = max(barWidth, 1) // a tiny page asks for a negative bar
	bar := progress.New(progress.WithGradient(string(accent), string(okColor)),
		progress.WithWidth(barWidth), progress.WithoutPercentage())
	lines := []string{bar.ViewAs(float64(m.done)/float64(m.total)) + fmt.Sprintf("  %d%%", m.done*100/m.total)}
	detail := num(m.done) + " of " + num(m.total) + " files"
	if m.total > 1_000_000 {
		detail = mb(m.done) + " of " + mb(m.total)
	}
	if e := eta(m.done, m.total, m.now().Sub(m.stepStart)); e != "" {
		detail += " · " + e
	}
	for _, l := range wrapLines(detail, width) {
		lines = append(lines, dimSty.Render(l))
	}
	return lines
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
