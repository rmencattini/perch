// Package session joins live tmux windows with saved state into one model.
package session

import (
	"sort"

	"perch/internal/state"
	"perch/internal/status"
	"perch/internal/tmux"
)

type Session struct {
	Name     string
	Dir      string
	Profile  string
	Command  string
	Created  int64
	WindowID string
	Status   status.Status
	NonoID   string // set for detached nono sessions that have no tmux window yet
}

// Tail returns the pane tail for a window id.
type Tail func(windowID string) string

// Join builds the session list. Order: creation time, then name.
// Windows with a dead pane are skipped: the process is gone, nothing to manage.
// A closed tab keeps its window and shows TabClosed instead of a live status.
func Join(windows []tmux.Window, st *state.State, tail Tail) []Session {
	var out []Session
	for _, w := range windows {
		if w.Dead {
			continue
		}
		m := st.Sessions[w.Name]
		s := Session{
			Name: w.Name, Dir: m.Dir, Profile: m.Profile, Command: m.Command,
			Created: m.Created, WindowID: w.ID,
		}
		if st.IsClosed(w.Name) {
			s.Status = status.TabClosed
		} else {
			s.Status = status.Detect(tail(w.ID))
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created < out[j].Created
		}
		return out[i].Name < out[j].Name
	})
	return out
}
