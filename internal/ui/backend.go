package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"perch/internal/nono"
	"perch/internal/session"
	"perch/internal/state"
	"perch/internal/status"
	"perch/internal/tmux"
	"perch/internal/usage"
	"perch/internal/worktree"
)

// Backend is everything the UI needs from the outside world.
// The model never touches tmux, nono or the state file directly.
type Backend interface {
	List() ([]session.Session, error)
	Preview(s session.Session) string
	// Attach reopens a closed tab, selects the window and returns the attach command.
	Attach(s session.Session) (*exec.Cmd, error)
	CloseTab(name string) error
	// ReopenLast returns the reopened name, or "" when nothing was closed.
	ReopenLast() (string, error)
	Kill(s session.Session) error

	Usage() usage.Summary
	Profiles() ([]nono.Profile, error)
	DefaultDir() string // folder the new-session dialog starts with ("" = none yet)
	Folders() []string  // recent folders, newest first
	// Command previews what Create would run: the folder and the shell command.
	Command(r NewRequest) (dir, cmd string)
	Create(r NewRequest) error
	// DetachHint is how to get back from an attached session: "ctrl-a d".
	DetachHint() string
}

const previewLines = 60

// TmuxBackend is the real Backend: tmux windows + nono + the state file.
type TmuxBackend struct {
	Tmux      tmux.Client
	Nono      nono.Client
	State     *state.State
	StatePath string
	Session   string // tmux session name, "perch"

	WorktreeRoot string // ~/git/worktrees
	UsageDir     string // ~/.cache/perch/usage: Claude's statusline writes here, so nono must allow it
	Home         string // denials under it never raise nono's review prompt (e.g. when K stops a session)
	BinDir       string // folder of the perch binary: nono must allow reading it to run `perch statusline`
	// Launch builds the shell command of a window. nil = the nono command line.
	// Tests replace it so they never start a real nono or claude.
	Launch func(nono.Spec) string
	// Adopt builds the shell command that brings a detached nono session into
	// a tab. nil = `nono attach <name>`. Tests replace it.
	Adopt func(name string) string
	// Detach builds the command that releases a session from the terminal
	// holding it. nil = `nono detach <name>`. Tests replace it.
	Detach func(name string) string
	Now    func() time.Time

	psAt time.Time
	ps   []nono.PsEntry
	hint string
}

const psEvery = 3 * time.Second

// DetachHint reads the user's real tmux prefix once it is known.
func (b *TmuxBackend) DetachHint() string {
	if b.hint == "" {
		if p := b.Tmux.Prefix(); p != "" {
			b.hint = p + " d"
		}
	}
	if b.hint == "" {
		return "<prefix> d"
	}
	return b.hint
}

// detached lists running nono sessions that have no tab: sessions started
// outside perch, or detached from it. nono ps errors are not fatal.
func (b *TmuxBackend) detached(inTabs map[string]bool) []session.Session {
	if b.psAt.IsZero() || b.now().Sub(b.psAt) >= psEvery {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if ps, err := b.Nono.Ps(ctx); err == nil {
			b.ps = ps
		}
		b.psAt = b.now()
	}
	var out []session.Session
	for _, e := range b.ps {
		name := e.Name
		if name == "" {
			name = e.ID
		}
		if !e.Live() || name == "" || inTabs[name] || inTabs[e.ID] {
			continue
		}
		st := status.Detached
		if a := strings.ToLower(e.Attachment); a != "" && !strings.Contains(a, "detach") {
			st = status.Elsewhere // attached in another terminal: perch must take it over
		}
		out = append(out, session.Session{Name: name, NonoID: e.ID, Status: st})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (b *TmuxBackend) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *TmuxBackend) Usage() usage.Summary { return usage.Load(b.UsageDir, b.now()) }

func (b *TmuxBackend) Profiles() ([]nono.Profile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return b.Nono.Profiles(ctx)
}

// DefaultDir is the last folder used. With no history it is empty, and the
// dialog opens the folder picker instead of guessing the current directory.
func (b *TmuxBackend) DefaultDir() string {
	if len(b.State.Recent) > 0 {
		return b.State.Recent[0]
	}
	return ""
}

func (b *TmuxBackend) Folders() []string { return b.State.Recent }

func (b *TmuxBackend) spec(r NewRequest) nono.Spec {
	agent := nono.Agent(r.Profile)
	s := nono.Spec{Name: r.Name, Profile: r.Profile, Command: []string{agent}}
	for _, p := range r.Extra {
		s.Allow = append(s.Allow, p) // the sandbox may use it
		if agent == "claude" {
			s.Command = append(s.Command, "--add-dir", p) // claude knows about it; opencode has no such flag
		}
	}
	if b.UsageDir != "" {
		s.Allow = append(s.Allow, b.UsageDir)
	}
	if b.BinDir != "" {
		s.Read = []string{b.BinDir}
	}
	if b.Home != "" {
		s.Quiet = []string{b.Home}
	}
	return s
}

func (b *TmuxBackend) Command(r NewRequest) (string, string) {
	dir := r.Dir
	if r.Worktree {
		if repo, err := worktree.RepoName(r.Dir); err == nil {
			dir = worktree.Path(b.WorktreeRoot, repo, r.Branch)
		}
	}
	return dir, b.spec(r).String()
}

func (b *TmuxBackend) Create(r NewRequest) error {
	ws, err := b.Tmux.ListWindows(b.Session)
	if err != nil {
		return err
	}
	for _, w := range ws {
		if w.Name == r.Name {
			return fmt.Errorf("a session named %s already exists", r.Name)
		}
	}
	dir := r.Dir
	if r.Worktree {
		if dir, err = worktree.Add(r.Dir, b.WorktreeRoot, r.Branch); err != nil {
			return err
		}
	}
	if b.UsageDir != "" {
		if err := os.MkdirAll(b.UsageDir, 0o755); err != nil {
			return err
		}
	}
	spec := b.spec(r)
	cmd := spec.String()
	if b.Launch != nil {
		cmd = b.Launch(spec)
	} else {
		// A failing nono (bad folder, sandbox error) must leave its message on
		// screen: tmux drops the window the moment the command exits.
		cmd += ` || { printf '\n[perch] exited with status %s\n' "$?"; sleep 15; }`
	}
	if len(ws) == 0 && !b.Tmux.HasSession(b.Session) {
		_, err = b.Tmux.NewSession(b.Session, r.Name, dir, cmd)
	} else {
		_, err = b.Tmux.NewWindow(b.Session, r.Name, dir, cmd)
	}
	if err != nil {
		return err
	}
	b.State.Sessions[r.Name] = state.Meta{Dir: dir, Profile: r.Profile, Command: spec.String(), Created: b.now().Unix()}
	b.State.AddRecent(r.Dir)
	return b.save()
}

func (b *TmuxBackend) save() error { return b.State.Save(b.StatePath) }

func (b *TmuxBackend) List() ([]session.Session, error) {
	ws, err := b.Tmux.ListWindows(b.Session)
	if err != nil {
		return nil, err
	}
	ss := session.Join(ws, b.State, func(id string) string {
		out, _ := b.Tmux.CapturePane(id, status.TailLines)
		return out
	})
	inTabs := map[string]bool{}
	for _, s := range ss {
		inTabs[s.Name] = true
	}
	return append(ss, b.detached(inTabs)...), nil
}

func (b *TmuxBackend) Preview(s session.Session) string {
	switch s.Status {
	case status.Detached:
		return "detached nono session\nid: " + s.NonoID + "\n\nenter brings it into a perch tab and attaches."
	case status.Elsewhere:
		return "nono session attached in another terminal tab\nid: " + s.NonoID + "\n\nenter detaches it there (nono detach), then attaches it here."
	}
	out, err := b.Tmux.CapturePane(s.WindowID, previewLines)
	if err != nil {
		return "(cannot read pane: " + err.Error() + ")"
	}
	return out
}

// adopt gives a detached nono session a tmux window running `nono attach`.
func (b *TmuxBackend) adopt(s session.Session) (string, error) {
	target := s.Name
	inner := "nono attach " + shellWord(target)
	if b.Adopt != nil {
		inner = b.Adopt(target)
	}
	if s.Status == status.Elsewhere {
		// nono attach refuses a session another terminal holds: let go of it first
		detach := "nono detach " + shellWord(target)
		if b.Detach != nil {
			detach = b.Detach(target)
		}
		inner = "{ " + detach + " >/dev/null 2>&1; sleep 0.5; }; " + inner
	}
	// A failing nono attach must leave its message in the pane long enough to be read.
	cmd := inner + ` || { printf '\n` + attachFailed + ` (exit %s)\n' "$?"; sleep 3; }`
	dir, _ := os.UserHomeDir()
	id, err := b.newWindow(s.Name, dir, cmd)
	if err != nil {
		return "", err
	}
	if err := b.checkAdopted(id); err != nil {
		return "", err
	}
	b.State.Sessions[s.Name] = state.Meta{Dir: dir, Command: "nono attach " + shellWord(target), Created: b.now().Unix()}
	return id, b.save()
}

const attachFailed = "nono attach failed"

// checkAdopted gives nono attach a moment to fail, and turns its output into the error.
func (b *TmuxBackend) checkAdopted(id string) error {
	for i := 0; i < 10; i++ {
		time.Sleep(50 * time.Millisecond)
		out, err := b.Tmux.CapturePane(id, 20)
		if err != nil || strings.Contains(out, attachFailed) { // window gone, or marker printed
			b.Tmux.KillWindow(id)
			var lines []string
			for _, l := range strings.Split(out, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					lines = append(lines, l)
				}
			}
			if len(lines) == 0 {
				return fmt.Errorf("nono attach exited immediately")
			}
			return fmt.Errorf("%s", strings.Join(lines, " · "))
		}
	}
	return nil
}

func shellWord(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (b *TmuxBackend) newWindow(name, dir, cmd string) (string, error) {
	if !b.Tmux.HasSession(b.Session) {
		return b.Tmux.NewSession(b.Session, name, dir, cmd)
	}
	return b.Tmux.NewWindow(b.Session, name, dir, cmd)
}

func (b *TmuxBackend) Attach(s session.Session) (*exec.Cmd, error) {
	if s.WindowID == "" {
		id, err := b.adopt(s)
		if err != nil {
			return nil, err
		}
		s.WindowID = id
	}
	if b.State.IsClosed(s.Name) {
		b.State.Reopen(s.Name)
		if err := b.save(); err != nil {
			return nil, err
		}
	}
	if err := b.Tmux.SelectWindow(s.WindowID); err != nil {
		return nil, err
	}
	return b.Tmux.AttachCmd(b.Session), nil
}

func (b *TmuxBackend) CloseTab(name string) error {
	b.State.CloseTab(name)
	return b.save()
}

func (b *TmuxBackend) ReopenLast() (string, error) {
	name, ok := b.State.ReopenLast()
	if !ok {
		return "", nil
	}
	return name, b.save()
}

// Kill is the only action that stops a session. nono stop runs only for
// sessions perch launched through nono; a stop failure leaves the window alone.
func (b *TmuxBackend) Kill(s session.Session) error {
	if strings.HasPrefix(s.Command, "nono ") || s.Status.External() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := b.Nono.Stop(ctx, s.Name); err != nil {
			return err
		}
	}
	if s.WindowID == "" { // detached: nono stop was the whole job
		return nil
	}
	if err := b.Tmux.KillWindow(s.WindowID); err != nil {
		// nono stop may already have ended the process and closed the window
		if ws, lerr := b.Tmux.ListWindows(b.Session); lerr != nil || hasWindow(ws, s.WindowID) {
			return err
		}
	}
	b.State.Forget(s.Name)
	return b.save()
}

func hasWindow(ws []tmux.Window, id string) bool {
	for _, w := range ws {
		if w.ID == id {
			return true
		}
	}
	return false
}
