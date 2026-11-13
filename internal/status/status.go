// Package status turns the tail of a tmux pane into a session status.
package status

import (
	"strings"

	"github.com/rmencattini/perch/internal/patterns"
)

type Status int

const (
	Idle Status = iota
	Running
	NeedsInput
	Elsewhere // a nono session attached in another terminal tab, with no perch tab
)

// TailLines is how many pane lines Detect looks at.
const TailLines = 25

// External reports a nono session that has no perch tab yet.
func (s Status) External() bool { return s == Elsewhere }

// Symbol is readable without color.
func (s Status) Symbol() string {
	switch s {
	case Running:
		return "●"
	case NeedsInput:
		return "!"
	case Elsewhere:
		return "◐"
	default:
		return "○"
	}
}

func (s Status) String() string {
	switch s {
	case Running:
		return "running"
	case NeedsInput:
		return "needs input"
	case Elsewhere:
		return "other tab"
	default:
		return "idle"
	}
}

// Detect classifies a pane tail. A prompt beats a spinner: if both show,
// the user is the one being waited on.
func Detect(tail string) Status {
	tail = lastLines(tail, TailLines)
	switch {
	case patterns.Any(patterns.NeedsInput, tail):
		return NeedsInput
	case patterns.Any(patterns.Running, tail):
		return Running
	default:
		return Idle
	}
}

func lastLines(s string, n int) string {
	s = strings.TrimRight(s, "\n ")
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
