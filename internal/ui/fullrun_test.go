package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// One session through the whole program loop: create, navigate, hide the preview, kill, quit.
func TestFullRun(t *testing.T) {
	f := newFake()
	f.defDir = t.TempDir()
	tm := start(t, f)
	waitFor(t, tm, "pane of alpha")

	key(tm, "n")
	waitFor(t, tm, "New session")
	tm.Send(typ("delta"))
	tm.Send(special(tea.KeyEnter))
	waitFor(t, tm, "created delta")

	key(tm, "k") // delta is selected: step up to gamma
	waitFor(t, tm, "pane of gamma")

	key(tm, "b")
	key(tm, "j") // delta
	key(tm, "K")
	waitFor(t, tm, "Kill delta?")
	key(tm, "y")
	waitFor(t, tm, "killed delta")

	m := quit(t, tm)
	if m.showPreview {
		t.Error("preview should still be hidden")
	}
	if len(m.sessions) != 3 || len(f.created) != 1 || len(f.killed) != 1 || f.killed[0] != "delta" {
		t.Errorf("sessions=%d created=%v killed=%v", len(m.sessions), f.created, f.killed)
	}
}
