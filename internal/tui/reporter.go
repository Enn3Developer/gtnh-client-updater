package tui

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

// reporter forwards progress to the program, throttling the chatty byte counter.
func (m *model) reporter() update.Reporter { return &teaReporter{send: m.send} }

type teaReporter struct {
	send func(tea.Msg)
	mu   sync.Mutex
	last time.Time
}

func (r *teaReporter) Step(s string) { r.send(stepMsg(s)) }
func (r *teaReporter) Warn(s string) { r.send(warnMsg(s)) }
func (r *teaReporter) Progress(done, total int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if done != total && time.Since(r.last) < 80*time.Millisecond {
		return
	}
	r.last = time.Now()
	r.send(progressMsg{done, total})
}
