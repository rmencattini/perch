package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// drive feeds messages straight to the model, no program loop. Commands are
// dropped, so the tests below stay deterministic.
func drive(m Model, msgs ...tea.Msg) Model {
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func typ(s string) tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func special(t tea.KeyType) tea.Msg { return tea.KeyMsg{Type: t} }

func dialogModel(t *testing.T, dir string) (Model, *fakeBackend) {
	t.Helper()
	f := newFake()
	f.defDir = dir
	m := New(f)
	m = drive(m, refreshMsg{sessions: f.sessions, selected: "alpha"})
	m = drive(m, profilesMsg{profiles: f.profiles})
	if m.dlg == nil {
		t.Fatal("dialog did not open")
	}
	return m, f
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	return dir
}

func TestDialogInlineErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	os.WriteFile(file, nil, 0o644)

	tests := []struct {
		name  string
		setup func(m Model) Model
		want  string
	}{
		{"name required on submit", func(m Model) Model { return m }, "name is required"},
		{"bad characters", func(m Model) Model { return drive(m, typ("bad name")) }, "use letters, digits"},
		{"leading dash", func(m Model) Model { return drive(m, typ("-x")) }, "use letters, digits"},
		{"duplicate", func(m Model) Model { return drive(m, typ("beta")) }, "a session named beta already exists"},
		{"too long", func(m Model) Model { return drive(m, typ(strings.Repeat("a", 41))) }, "longer than 40"},
		{"folder missing", func(m Model) Model {
			return drive(m, typ("ok"), special(tea.KeyTab), special(tea.KeyCtrlU), typ("/no/such/dir"))
		}, "folder does not exist"},
		{"folder relative", func(m Model) Model {
			return drive(m, typ("ok"), special(tea.KeyTab), special(tea.KeyCtrlU), typ("rel/dir"))
		}, "absolute path or start with ~"},
		{"folder is a file", func(m Model) Model {
			return drive(m, typ("ok"), special(tea.KeyTab), special(tea.KeyCtrlU), typ(file))
		}, "not a folder"},
		{"folder empty on submit", func(m Model) Model {
			return drive(m, typ("ok"), special(tea.KeyTab), special(tea.KeyCtrlU))
		}, "folder is required"},
		{"worktree needs a repo", func(m Model) Model {
			return drive(m, typ("ok"), special(tea.KeyTab), special(tea.KeyTab), special(tea.KeyTab), special(tea.KeyTab), typ(" "))
		}, "not a git repository"},
		{"branch required", func(m Model) Model {
			return drive(m, typ("ok"), special(tea.KeyTab), special(tea.KeyTab), special(tea.KeyTab), special(tea.KeyTab), typ(" "))
		}, "branch name is required"},
		{"branch invalid", func(m Model) Model {
			return drive(m, typ("ok"), special(tea.KeyTab), special(tea.KeyTab), special(tea.KeyTab), special(tea.KeyTab), typ(" "), special(tea.KeyTab), typ("a..b"))
		}, "cannot contain .."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, f := dialogModel(t, dir)
			m = tc.setup(m)
			m = drive(m, special(tea.KeyEnter))
			if m.dlg == nil {
				t.Fatal("dialog closed on invalid input")
			}
			if v := m.View(); !strings.Contains(v, tc.want) {
				t.Errorf("view missing %q:\n%s", tc.want, v)
			}
			if len(f.created) != 0 {
				t.Error("Create called on invalid input")
			}
		})
	}
}

func TestDialogUntouchedFormIsNotRed(t *testing.T) {
	m, _ := dialogModel(t, t.TempDir())
	if v := m.View(); strings.Contains(v, "✗") {
		t.Errorf("errors before any input:\n%s", v)
	}
}

func TestDialogProfileDefaultAndCycle(t *testing.T) {
	m, _ := dialogModel(t, t.TempDir())
	if got := m.dlg.profileName(); got != "nolabs-ai/claude" {
		t.Fatalf("default profile = %s", got)
	}
	m = drive(m, special(tea.KeyTab), special(tea.KeyTab), special(tea.KeyTab), special(tea.KeyRight))
	if got := m.dlg.profileName(); got != "go-dev" {
		t.Errorf("after right: %s", got)
	}
	m = drive(m, special(tea.KeyRight)) // wraps
	if got := m.dlg.profileName(); got != "default" {
		t.Errorf("wrap: %s", got)
	}
	if v := m.View(); !strings.Contains(v, "Default conservative base profile") {
		t.Error("profile description not shown")
	}
}

func TestDialogNoProfiles(t *testing.T) {
	f := newFake()
	f.defDir = t.TempDir()
	m := drive(New(f), profilesMsg{err: errors.New("boom")})
	m = drive(m, typ("ok"), special(tea.KeyEnter))
	if v := m.View(); !strings.Contains(v, "nono profile list failed: boom") || m.dlg == nil {
		t.Errorf("want blocking error:\n%s", v)
	}
}

func TestDialogTabAcceptsFolderSuggestion(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "projects", "perch"), 0o755)
	f := newFake()
	m := New(f)
	m.home = home
	m = drive(m, profilesMsg{profiles: f.profiles})
	m = drive(m, special(tea.KeyTab), typ("~/pro"))
	m = drive(m, special(tea.KeyTab))
	if got := m.dlg.dir.Value(); got != "~/projects/" {
		t.Fatalf("after tab: %q", got)
	}
	if m.dlg.focus != fDir {
		t.Error("tab should have stayed on folder while completing")
	}
	m = drive(m, typ("pe"), special(tea.KeyTab))
	if got := m.dlg.dir.Value(); got != "~/projects/perch/" {
		t.Errorf("second completion: %q", got)
	}
	m = drive(m, special(tea.KeyTab)) // nothing left to complete: moves on
	if m.dlg.focus != fExtra {
		t.Errorf("focus = %v", m.dlg.focus)
	}
}

func TestCompletions(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"git/perch", "git/other", ".hidden", "gamma"} {
		os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	os.WriteFile(filepath.Join(home, "gfile"), nil, 0o644)
	os.Symlink(filepath.Join(home, "git"), filepath.Join(home, "glink"))

	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"~/g", []string{"~/gamma/", "~/git/", "~/glink/"}},
		{"~/git/", []string{"~/git/other/", "~/git/perch/"}},
		{"~/git/p", []string{"~/git/perch/"}},
		{"~/.", []string{"~/.hidden/"}},
		{"~/", []string{"~/gamma/", "~/git/", "~/glink/"}},
		{"~/nope/", nil},
		{"relative", nil},
		{home + "/ga", []string{home + "/gamma/"}},
	}
	for _, tc := range cases {
		got := Completions(tc.in, home)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("Completions(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestDialogSubmitWorktree(t *testing.T) {
	repo := initGitRepo(t)
	m, f := dialogModel(t, repo)
	m = drive(m, typ("wt1"), special(tea.KeyTab), special(tea.KeyTab), special(tea.KeyTab), special(tea.KeyTab), typ(" "), special(tea.KeyTab), typ("feat/x"))
	m, cmd := step(m, special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("valid form should submit")
	}
	m = drive(m, cmd())
	if m.dlg != nil {
		t.Error("dialog should close on success")
	}
	want := NewRequest{Name: "wt1", Dir: repo, Profile: "nolabs-ai/claude", Worktree: true, Branch: "feat/x"}
	if len(f.created) != 1 || !reflect.DeepEqual(f.created[0], want) {
		t.Errorf("created = %+v, want %+v", f.created, want)
	}
}

func step(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func TestDialogCreateErrorStaysOpen(t *testing.T) {
	m, f := dialogModel(t, t.TempDir())
	f.createErr = errors.New("tmux exploded")
	m = drive(m, typ("fresh"))
	m, cmd := step(m, special(tea.KeyEnter))
	if !m.dlg.busy {
		t.Error("dialog should be busy while creating")
	}
	m = drive(m, cmd())
	if m.dlg == nil || m.dlg.busy || !strings.Contains(m.View(), "tmux exploded") {
		t.Errorf("error should show in the dialog:\n%s", m.View())
	}
}

// Full flow through the real program loop.
func TestNewSessionFlow(t *testing.T) {
	dir := t.TempDir()
	f := newFake()
	f.defDir = dir
	tm := start(t, f)
	waitFor(t, tm, "pane of alpha")

	key(tm, "n")
	waitFor(t, tm, "New session")
	tm.Send(typ("mine"))
	waitFor(t, tm, "--name mine --profile nolabs-ai/claude -- claude") // live command preview
	key(tm, "esc")
	waitFor(t, tm, "pane of alpha") // back on the list, nothing created
	if len(f.created) != 0 {
		t.Fatal("esc must not create")
	}

	key(tm, "n")
	waitFor(t, tm, "New session")
	tm.Send(typ("mine"))
	tm.Send(special(tea.KeyEnter))
	waitFor(t, tm, "created mine")
	m := quit(t, tm)
	if len(f.created) != 1 || f.created[0].Dir != dir {
		t.Errorf("created = %+v", f.created)
	}
	if s, _ := m.selected(); s.Name != "mine" {
		t.Errorf("new session should be selected, got %q", s.Name)
	}
}

func TestDialogCapturesKeys(t *testing.T) {
	// 'q', 'K' and 'j' typed into the name must not quit, kill or move.
	m, _ := dialogModel(t, t.TempDir())
	m = drive(m, typ("qKj"))
	if m.dlg == nil || m.confirmKill || m.dlg.name.Value() != "qKj" {
		t.Errorf("dialog leaked keys: %q", m.dlg.name.Value())
	}
}

func runGit(dir string, args ...string) ([]byte, error) {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
}

func TestDialogExtraFolders(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	m, f := dialogModel(t, t.TempDir())
	m = drive(m, typ("multi"), special(tea.KeyTab), special(tea.KeyTab)) // on Also allow
	m = drive(m, typ(a), special(tea.KeyEnter))
	if m.dlg.extra.Value() != "" || len(m.dlg.extras) != 1 {
		t.Fatalf("enter should add the folder: extras=%v input=%q", m.dlg.extras, m.dlg.extra.Value())
	}
	m = drive(m, typ(b), special(tea.KeyEnter), typ(a), special(tea.KeyEnter))
	if len(m.dlg.extras) != 2 {
		t.Errorf("duplicate must be refused: %v", m.dlg.extras)
	}
	if m.dlg.errors()[fExtra] == "" {
		t.Error("duplicate should show an error")
	}
	m.dlg.extra.SetValue("")
	m = drive(m, special(tea.KeyBackspace))
	if len(m.dlg.extras) != 1 {
		t.Errorf("backspace on empty should drop the last folder: %v", m.dlg.extras)
	}
	m, cmd := step(m, special(tea.KeyEnter)) // empty text: submit
	if cmd == nil {
		t.Fatal("empty extra field + enter should submit")
	}
	drive(m, cmd())
	if len(f.created) != 1 || !reflect.DeepEqual(f.created[0].Extra, []string{a}) {
		t.Errorf("created = %+v, want Extra [%s]", f.created, a)
	}
}
