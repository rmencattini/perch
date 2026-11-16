package session

import (
	"testing"

	"github.com/rmencattini/perch/internal/state"
	"github.com/rmencattini/perch/internal/status"
	"github.com/rmencattini/perch/internal/tmux"
)

func TestJoin(t *testing.T) {
	st := &state.State{Sessions: map[string]state.Meta{
		"b": {Dir: "/b", Profile: "p", Command: "nono run", Created: 2},
		"a": {Dir: "/a", Command: "nono run", Created: 1},
		"c": {Command: "nono run", Created: 3},
	}}
	ws := []tmux.Window{
		{ID: "@1", Name: "b"}, {ID: "@2", Name: "a"}, {ID: "@3", Name: "c"},
		{ID: "@4", Name: "dead", Dead: true}, {ID: "@5", Name: "stranger"}, // plain tmux tab: not listed
	}
	tails := map[string]string{"@1": "Do you want to proceed?", "@2": "esc to interrupt", "@3": "esc to interrupt"}
	got := Join(ws, st, func(id string) string { return tails[id] })

	want := []struct {
		name string
		st   status.Status
	}{
		{"a", status.Running},
		{"b", status.NeedsInput},
		{"c", status.Running},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d sessions: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Name != w.name || got[i].Status != w.st {
			t.Errorf("[%d] = %s/%v, want %s/%v", i, got[i].Name, got[i].Status, w.name, w.st)
		}
	}
	if got[1].Dir != "/b" || got[1].Profile != "p" || got[1].WindowID != "@1" {
		t.Errorf("metadata not joined: %+v", got[1])
	}
}
