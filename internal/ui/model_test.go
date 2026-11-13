package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"perch/internal/nono"
	"perch/internal/session"
	"perch/internal/state"
	"perch/internal/status"
	"perch/internal/tmux"
	"perch/internal/usage"
)

type fakeBackend struct {
	mu        sync.Mutex
	usage     usage.Summary
	profiles  []nono.Profile
	defDir    string
	created   []NewRequest
	createErr error
	sessions  []session.Session
	killed    []string
	attached  []string
}

func (f *fakeBackend) List() ([]session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]session.Session(nil), f.sessions...)
	return out, nil
}

func (f *fakeBackend) Preview(s session.Session) string { return "pane of " + s.Name }

func (f *fakeBackend) Diff(s session.Session) string { return "diff of " + s.Name }

func (f *fakeBackend) Attach(s session.Session) (*exec.Cmd, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attached = append(f.attached, s.Name)
	return exec.Command("true"), nil
}

func (f *fakeBackend) Kill(s session.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killed = append(f.killed, s.Name)
	for i, x := range f.sessions {
		if x.Name == s.Name {
			f.sessions = append(f.sessions[:i], f.sessions[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeBackend) Usage() usage.Summary { return f.usage }

func (f *fakeBackend) Profiles() ([]nono.Profile, error) { return f.profiles, nil }

func (f *fakeBackend) DefaultDir() string { return f.defDir }

func (f *fakeBackend) Folders() []string { return nil }

func (f *fakeBackend) DetachHint() string { return "ctrl-a d" }

func (f *fakeBackend) Command(r NewRequest) (string, string) {
	return r.Dir, fmt.Sprintf("nono run --name %s --profile %s -- claude", r.Name, r.Profile)
}

func (f *fakeBackend) Create(r NewRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, r)
	f.sessions = append(f.sessions, session.Session{Name: r.Name, WindowID: "@9", Dir: r.Dir, Profile: r.Profile})
	return nil
}

func newFake() *fakeBackend {
	return &fakeBackend{
		profiles: []nono.Profile{
			{Name: "default", Description: "Default conservative base profile"},
			{Name: nono.DefaultProfile, Description: "Anthropic Claude Code CLI agent"},
			{Name: "go-dev", Description: "Go SDK"},
		},
		sessions: []session.Session{
			{Name: "alpha", WindowID: "@1", Status: status.Running, Dir: "/a"},
			{Name: "beta", WindowID: "@2", Status: status.NeedsInput},
			{Name: "gamma", WindowID: "@3", Status: status.Idle},
		}}
}

func start(t *testing.T, f *fakeBackend) *teatest.TestModel {
	t.Helper()
	return teatest.NewTestModel(t, New(f), teatest.WithInitialTermSize(100, 24))
}

func key(tm *teatest.TestModel, s string) {
	switch s {
	case "enter":
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	case "esc":
		tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	default:
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	}
}

func waitFor(t *testing.T, tm *teatest.TestModel, substr string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return bytes.Contains(b, []byte(substr)) },
		teatest.WithDuration(3*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func quit(t *testing.T, tm *teatest.TestModel) Model {
	t.Helper()
	key(tm, "q")
	return tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(Model)
}

func TestNavigation(t *testing.T) {
	tm := start(t, newFake())
	waitFor(t, tm, "pane of alpha")
	key(tm, "j")
	key(tm, "j")
	key(tm, "j") // past the end: stays on gamma
	waitFor(t, tm, "diff of gamma")
	key(tm, "k")
	waitFor(t, tm, "pane of beta")
	m := quit(t, tm)
	if s, _ := m.selected(); s.Name != "beta" {
		t.Errorf("selected %s, want beta", s.Name)
	}
}

func TestHeaderAndSymbols(t *testing.T) {
	tm := start(t, newFake())
	waitFor(t, tm, "○ gamma")
	v := quit(t, tm).View()
	for _, want := range []string{"! 1 need input", "● alpha", "! beta", "○ gamma", "3 sessions"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestTogglePreview(t *testing.T) {
	tm := start(t, newFake())
	waitFor(t, tm, "pane of alpha")
	key(tm, "b")
	m := quit(t, tm)
	if m.showPreview {
		t.Error("preview should be hidden")
	}
	if strings.Contains(m.View(), "pane of alpha") {
		t.Error("hidden preview still rendered")
	}

	tm = start(t, newFake())
	waitFor(t, tm, "pane of alpha")
	key(tm, "b")
	key(tm, "b")
	if m := quit(t, tm); !m.showPreview {
		t.Error("preview should be back")
	}
}

func TestEnterAttaches(t *testing.T) {
	f := newFake()
	tm := start(t, f)
	waitFor(t, tm, "pane of alpha")
	key(tm, "j")
	key(tm, "enter")
	waitFor(t, tm, "detached from beta")
	quit(t, tm)
	if len(f.attached) != 1 || f.attached[0] != "beta" {
		t.Errorf("attached = %v", f.attached)
	}
}

func TestKillConfirm(t *testing.T) {
	f := newFake()
	tm := start(t, f)
	waitFor(t, tm, "pane of alpha")

	key(tm, "K")
	waitFor(t, tm, "Kill alpha?")
	key(tm, "esc")
	waitFor(t, tm, "kill cancelled")
	if len(f.killed) != 0 {
		t.Fatalf("esc must not kill: %v", f.killed)
	}

	key(tm, "K")
	waitFor(t, tm, "Kill alpha?")
	key(tm, "y")
	waitFor(t, tm, "killed alpha")
	m := quit(t, tm)
	if len(f.killed) != 1 || len(m.sessions) != 2 {
		t.Errorf("killed %v, %d sessions left", f.killed, len(m.sessions))
	}
}

func TestMouseClickSelects(t *testing.T) {
	tm := start(t, newFake())
	waitFor(t, tm, "pane of alpha")
	tm.Send(tea.MouseMsg{X: 3, Y: headerLines + listTop + 2*rowHeight, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	waitFor(t, tm, "diff of gamma")
	tm.Send(tea.MouseMsg{X: 90, Y: headerLines, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}) // preview area: ignored
	m := quit(t, tm)
	if s, _ := m.selected(); s.Name != "gamma" {
		t.Errorf("selected %s", s.Name)
	}
}

func TestEmpty(t *testing.T) {
	tm := start(t, &fakeBackend{})
	waitFor(t, tm, "no sessions yet")
	key(tm, "K") // nothing to kill: no modal
	key(tm, "x")
	m := quit(t, tm)
	if m.confirmKill {
		t.Error("kill modal opened on empty list")
	}
}

// Real backend on a private tmux socket, with a fake nono runner.
func TestTmuxBackend(t *testing.T) {
	tc := tmux.Client{Socket: fmt.Sprintf("perch-ui-test-%d", time.Now().UnixNano())}
	t.Cleanup(func() {
		tc.KillServer()
		os.Remove(fmt.Sprintf("/private/tmp/tmux-%d/%s", os.Getuid(), tc.Socket))
	})
	dir := t.TempDir()
	if _, err := tc.NewSession("perch", "plain", dir, "printf 'do you want? (y/n) '; sleep 30"); err != nil {
		t.Fatal(err)
	}
	if _, err := tc.NewWindow("perch", "boxed", dir, "sleep 30"); err != nil {
		t.Fatal(err)
	}
	var stopped []string
	st := &state.State{Sessions: map[string]state.Meta{"boxed": {Command: "nono run --name boxed -- claude"}}}
	b := &TmuxBackend{
		Tmux: tc, State: st, StatePath: filepath.Join(dir, "state.json"), Session: "perch",
		Nono: nono.Client{Run: func(_ context.Context, a ...string) ([]byte, error) {
			if a[0] == "stop" { // ps polling is not what these tests count
				stopped = append(stopped, strings.Join(a, " "))
			}
			return []byte("[]"), nil
		}},
	}

	var ss []session.Session
	for i := 0; i < 50; i++ {
		ss, _ = b.List()
		if len(ss) == 2 && ss[0].Status == status.NeedsInput || len(ss) == 2 && ss[1].Status == status.NeedsInput {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	byName := map[string]session.Session{}
	for _, s := range ss {
		byName[s.Name] = s
	}
	if byName["plain"].Status != status.NeedsInput {
		t.Fatalf("plain status = %v", byName["plain"].Status)
	}

	if cmd, err := b.Attach(byName["plain"]); err != nil || cmd == nil {
		t.Errorf("attach: %v", err)
	}

	if err := b.Kill(byName["boxed"]); err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0] != "stop boxed" {
		t.Errorf("nono calls = %v", stopped)
	}
	if err := b.Kill(byName["plain"]); err != nil { // no nono command: tmux only
		t.Fatal(err)
	}
	if len(stopped) != 1 {
		t.Errorf("non-nono session must not call nono stop: %v", stopped)
	}
	if ss, _ = b.List(); len(ss) != 0 {
		t.Errorf("windows left: %v", ss)
	}
}

func tea_size(w, h int) tea.Msg { return tea.WindowSizeMsg{Width: w, Height: h} }

func TestStaleRefreshDoesNotMoveCursor(t *testing.T) {
	f := newFake()
	m := drive(New(f), refreshMsg{sessions: f.sessions, selected: "alpha", preview: "pane of alpha"})
	m = drive(m, typ("j"), typ("j")) // user is on gamma
	// a refresh started while the cursor was still on alpha lands now
	m = drive(m, refreshMsg{sessions: f.sessions, selected: "alpha", preview: "pane of alpha"})
	if s, _ := m.selected(); s.Name != "gamma" {
		t.Fatalf("cursor jumped back to %s", s.Name)
	}
	if m.preview == "pane of alpha" && m.cursor == 2 {
		// stale preview must not be shown as gamma's; a refresh is requested instead
		_, cmd := step(m, refreshMsg{sessions: f.sessions, selected: "alpha", preview: "pane of alpha"})
		if cmd == nil {
			t.Error("stale preview should trigger a fresh refresh")
		}
	}
}

// An idle session has nothing new in its pane, so the panel opens on the diff;
// a running one stays on the pane. tab flips either.
func TestDiffTab(t *testing.T) {
	f := newFake()
	m := drive(New(f), refreshMsg{sessions: f.sessions, selected: "alpha", preview: "pane of alpha"})
	if m.diffTab(f.sessions[0]) || !m.diffTab(f.sessions[2]) {
		t.Fatal("running opens on the pane, idle on the diff")
	}
	m = drive(m, special(tea.KeyTab))
	if !m.diffTab(f.sessions[0]) {
		t.Error("tab on a running session should show its diff")
	}
	m = drive(m, typ("j"), typ("j"), special(tea.KeyTab))
	if m.diffTab(f.sessions[2]) {
		t.Error("tab on an idle session should show its pane")
	}
}
