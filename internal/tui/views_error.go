package tui

import (
	"errors"

	"github.com/Enn3Developer/gtnh-client-updater/internal/update"
)

func (m *model) errorView() (body, footer string) {
	b := m.blocks()
	b.note(badSty, "Something went wrong")
	b.para(m.err.Error())
	var leftover *update.LeftoverError
	switch {
	case m.creating && (m.errPhase == scPreparing || m.errPhase == scApplying) && errors.As(m.err, &leftover):
		// The error itself says the folder is still there and what to do.
	case m.creating && m.errPhase == scPreparing:
		b.note(okSty, "Nothing was created.")
	case m.creating && m.errPhase == scApplying:
		b.note(okSty, "I removed the half-made instance, so there's nothing to clean up.")
	case m.errPhase == scPreparing:
		b.note(okSty, "Nothing was changed.")
	case m.errPhase == scApplying && errors.Is(m.err, update.ErrRolledBack):
		b.note(okSty, "Everything was put back the way it was, so your instance is exactly as before.")
	case m.errPhase == scApplying:
		b.note(badSty, "Some files may have changed. The originals are in the .gtnh-updater folder inside the instance.")
	case m.errPhase == scRestoring:
		b.raw(m.restoreErrorNote())
	}
	if m.canGoBack() {
		return b.String(), m.buttonFooter("enter", "back", "q", "quit")
	}
	return b.String(), m.buttonFooter("enter", "quit")
}
