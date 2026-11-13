package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/rmencattini/perch/internal/session"
	"github.com/rmencattini/perch/internal/status"
	"github.com/rmencattini/perch/internal/usage"
)

func TestBar(t *testing.T) {
	tests := []struct {
		name string
		r    usage.Reading
		want string
	}{
		{"unknown", usage.Reading{}, "5h ░░░░░░░░ unknown"},
		{"half", usage.Reading{Known: true, Percent: 50}, "5h ████░░░░ 50%"},
		{"zero but known", usage.Reading{Known: true, Percent: 0}, "5h ░░░░░░░░ 0%"},
		{"over 100 clamps", usage.Reading{Known: true, Percent: 140}, "5h ████████ 140%"},
		{"old data", usage.Reading{Known: true, Percent: 10, Age: 12 * time.Minute}, "5h █░░░░░░░ 10% 12m old"},
	}
	for _, tc := range tests {
		if got := bar("5h", tc.r); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestHeaderShowsUsage(t *testing.T) {
	f := newFake()
	m := drive(New(f), refreshMsg{sessions: f.sessions, selected: "alpha", usage: usage.Summary{
		FiveHour: usage.Reading{Known: true, Percent: 42},
	}})
	first := strings.SplitN(m.View(), "\n", 2)[0]
	for _, want := range []string{"5h ███░░░░░ 42%", "7d ░░░░░░░░ unknown", "3 sessions"} {
		if !strings.Contains(first, want) {
			t.Errorf("header %q missing %q", first, want)
		}
	}
	m = drive(m, refreshMsg{sessions: f.sessions, selected: "alpha"}) // no data at all
	if first := strings.SplitN(m.View(), "\n", 2)[0]; strings.Count(first, "unknown") != 2 {
		t.Errorf("both windows should be unknown: %q", first)
	}
}

func TestNarrowHeaderDropsBars(t *testing.T) {
	f := newFake()
	m := drive(New(f), tea_size(30, 20), refreshMsg{sessions: f.sessions, selected: "alpha"})
	first := strings.SplitN(m.View(), "\n", 2)[0]
	if strings.Contains(first, "5h") || !strings.Contains(first, "perch") {
		t.Errorf("narrow header = %q", first)
	}
}

func TestPreviewKeepsNewestLines(t *testing.T) {
	f := newFake()
	long := strings.Repeat("x", 300) // would wrap into many rows and push the tail out
	m := drive(New(f), tea_size(100, 12), refreshMsg{
		sessions: f.sessions, selected: "alpha",
		preview: long + "\n" + long + "\n" + long + "\nLAST LINE\n",
	})
	if !strings.Contains(m.View(), "LAST LINE") {
		t.Errorf("newest line cropped:\n%s", m.View())
	}
}

func TestDetachedRowAndHint(t *testing.T) {
	f := newFake()
	ss := append(f.sessions, session.Session{Name: "old-one", Status: status.Detached})
	m := drive(New(f), refreshMsg{sessions: ss, selected: "old-one", hint: "ctrl-a d"})
	v := m.View()
	for _, want := range []string{"◌ old-one", "detached", "enter attach (ctrl-a d back)"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}
