package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"perch/internal/nono"
	"perch/internal/session"
	"perch/internal/state"
	"perch/internal/status"
	"perch/internal/tmux"
)

func TestTmuxBackendCreate(t *testing.T) {
	tc := tmux.Client{Socket: fmt.Sprintf("perch-ui-create-%d", time.Now().UnixNano())}
	t.Cleanup(func() {
		tc.KillServer()
		os.Remove(fmt.Sprintf("/private/tmp/tmux-%d/%s", os.Getuid(), tc.Socket))
	})
	tmp := t.TempDir()
	repo := initGitRepo(t)
	exec := func(args ...string) { // commit so worktrees have something to check out
		t.Helper()
		if out, err := runGit(repo, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	exec("-c", "commit.gpgsign=false", "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "i")

	usageDir := filepath.Join(tmp, "usage")
	st := &state.State{Sessions: map[string]state.Meta{}}
	var stopped []string
	b := &TmuxBackend{
		Tmux: tc, State: st, StatePath: filepath.Join(tmp, "state.json"), Session: "perch",
		WorktreeRoot: filepath.Join(tmp, "wt"), UsageDir: usageDir, BinDir: "/opt/perch-bin",
		Nono: nono.Client{Run: func(_ context.Context, a ...string) ([]byte, error) {
			if a[0] == "stop" { // ps polling is not what these tests count
				stopped = append(stopped, strings.Join(a, " "))
			}
			return []byte("[]"), nil
		}},
		// never start real nono or claude
		Launch: func(nono.Spec) string { return `printf 'cwd=%s\n' "$(basename "$PWD")"; sleep 30` },
		Now:    func() time.Time { return time.Unix(1_800_000_000, 0) },
	}

	// preview and launch share one builder
	dir, cmd := b.Command(NewRequest{Name: "one", Dir: repo, Profile: "nolabs-ai/claude", Worktree: true, Branch: "feat/x"})
	wantCmd := fmt.Sprintf("nono run --name one --profile nolabs-ai/claude --allow %s --read /opt/perch-bin -- claude", usageDir)
	if cmd != wantCmd {
		t.Errorf("cmd = %s\nwant  %s", cmd, wantCmd)
	}
	if want := filepath.Join(tmp, "wt", filepath.Base(repo), "feat", "x"); dir != want {
		t.Errorf("preview dir = %s, want %s", dir, want)
	}

	// 1st create starts the tmux session, in a plain folder
	plain := t.TempDir()
	if err := b.Create(NewRequest{Name: "one", Dir: plain, Profile: "p"}); err != nil {
		t.Fatal(err)
	}
	// 2nd adds a window, in a new worktree
	if err := b.Create(NewRequest{Name: "two", Dir: repo, Profile: "p", Worktree: true, Branch: "feat/x"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Create(NewRequest{Name: "two", Dir: plain}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate: %v", err)
	}
	if _, err := os.Stat(usageDir); err != nil {
		t.Error("usage dir must exist before nono --allow uses it")
	}

	ss, err := b.List()
	if err != nil || len(ss) != 2 {
		t.Fatalf("sessions: %v %v", ss, err)
	}
	for _, c := range []struct{ name, want string }{{"one", "cwd=" + filepath.Base(plain)}, {"two", "cwd=x"}} {
		found := false
		for _, s := range ss {
			if s.Name != c.name {
				continue
			}
			for i := 0; i < 50 && !found; i++ {
				out := b.Preview(s)
				found = strings.Contains(out, c.want)
				if !found {
					time.Sleep(50 * time.Millisecond)
				}
			}
		}
		if !found {
			t.Errorf("%s did not start in the right folder (want %q)", c.name, c.want)
		}
	}

	m := st.Sessions["two"]
	if !strings.HasPrefix(m.Command, "nono run --name two") || m.Profile != "p" || m.Created != 1_800_000_000 || !strings.HasSuffix(m.Dir, "feat/x") {
		t.Errorf("meta = %+v", m)
	}
	if st.Recent[0] != repo {
		t.Errorf("recent = %v", st.Recent)
	}
	if loaded, _ := state.Load(b.StatePath); len(loaded.Sessions) != 2 {
		t.Error("state not saved")
	}

	// kill calls nono stop because the saved command is a nono command
	for _, s := range ss {
		if s.Name == "two" {
			if err := b.Kill(s); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(stopped) != 1 || stopped[0] != "stop two" {
		t.Errorf("stopped = %v", stopped)
	}
}

func TestTmuxBackendDetachedSessions(t *testing.T) {
	tc := tmux.Client{Socket: fmt.Sprintf("perch-ui-detached-%d", time.Now().UnixNano())}
	t.Cleanup(func() {
		tc.KillServer()
		os.Remove(fmt.Sprintf("/private/tmp/tmux-%d/%s", os.Getuid(), tc.Socket))
	})
	tmp := t.TempDir()
	if _, err := tc.NewSession("perch", "inside", tmp, "sleep 30"); err != nil {
		t.Fatal(err)
	}
	var calls []string
	ps := `[{"name":"old-one","session_id":"aa11","status":"running","attachment":"detached"},
	        {"name":"inside","session_id":"bb22","status":"running"},
	        {"name":"gone","session_id":"cc33","status":"exited"},
	        {"session_id":"dd44","status":"running","attachment":"detached"}]`
	st := &state.State{Sessions: map[string]state.Meta{}}
	b := &TmuxBackend{
		Tmux: tc, State: st, StatePath: filepath.Join(tmp, "state.json"), Session: "perch",
		Nono: nono.Client{Run: func(_ context.Context, a ...string) ([]byte, error) {
			calls = append(calls, strings.Join(a, " "))
			return []byte(ps), nil
		}},
		Adopt: func(string) string { return "printf 'adopted\\n'; sleep 30" }, // never run real nono
		Now:   func() time.Time { return time.Unix(1_800_000_000, 0) },
	}

	ss, err := b.List()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range ss {
		names = append(names, s.Name+":"+s.Status.String())
	}
	// own window first; exited and already-in-a-tab sessions are not repeated; unnamed ones show by id
	if got := strings.Join(names, " "); !strings.HasPrefix(got, "inside:") || !strings.Contains(got, "dd44:detached old-one:detached") || strings.Contains(got, "gone") || strings.Count(got, "inside") != 1 {
		t.Fatalf("sessions = %s", got)
	}
	if p := b.Preview(ss[len(ss)-1]); !strings.Contains(p, "enter brings it into a perch tab") {
		t.Errorf("preview = %q", p)
	}

	// adopt: enter on a detached row gives it a tab
	var old session.Session
	for _, s := range ss {
		if s.Name == "old-one" {
			old = s
		}
	}
	cmd, err := b.Attach(old)
	if err != nil || cmd == nil {
		t.Fatalf("attach: %v", err)
	}
	ws, _ := tc.ListWindows("perch")
	if len(ws) != 2 || ws[1].Name != "old-one" {
		t.Fatalf("windows = %v", ws)
	}
	if m := st.Sessions["old-one"]; !strings.HasPrefix(m.Command, "nono attach 'old-one'") {
		t.Errorf("meta = %+v", m)
	}
	ss, _ = b.List()
	for _, s := range ss {
		if s.Name == "old-one" && (s.Status == status.Detached || s.WindowID == "") {
			t.Errorf("old-one should be a tab now: %+v", s)
		}
	}

	// killing a detached session with no tab is just nono stop
	calls = nil
	var other session.Session
	for _, s := range ss {
		if s.Name == "dd44" {
			other = s
		}
	}
	if err := b.Kill(other); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "stop dd44" {
		t.Errorf("calls = %v", calls)
	}
}

func TestAdoptFailureShowsNonoError(t *testing.T) {
	tc := tmux.Client{Socket: fmt.Sprintf("perch-ui-adoptfail-%d", time.Now().UnixNano())}
	t.Cleanup(func() {
		tc.KillServer()
		os.Remove(fmt.Sprintf("/private/tmp/tmux-%d/%s", os.Getuid(), tc.Socket))
	})
	tmp := t.TempDir()
	st := &state.State{Sessions: map[string]state.Meta{}}
	b := &TmuxBackend{
		Tmux: tc, State: st, StatePath: filepath.Join(tmp, "state.json"), Session: "perch",
		Nono:  nono.Client{Run: func(context.Context, ...string) ([]byte, error) { return []byte("[]"), nil }},
		Adopt: func(string) string { return `sh -c 'echo "session is attached elsewhere" >&2; exit 1'` },
		Now:   func() time.Time { return time.Unix(1_800_000_000, 0) },
	}
	cmd, err := b.Attach(session.Session{Name: "old-one", Status: status.Detached})
	if err == nil || cmd != nil {
		t.Fatalf("want an error, got cmd=%v err=%v", cmd, err)
	}
	if !strings.Contains(err.Error(), "session is attached elsewhere") || !strings.Contains(err.Error(), "nono attach failed (exit 1)") {
		t.Errorf("error does not carry nono's message: %v", err)
	}
	if _, ok := st.Sessions["old-one"]; ok {
		t.Error("a failed adoption must not be saved")
	}
	if tc.HasSession("perch") {
		ws, _ := tc.ListWindows("perch")
		if len(ws) != 0 {
			t.Errorf("dead tab left behind: %v", ws)
		}
	}
}

func TestAttachedElsewhereIsDetachedFirst(t *testing.T) {
	tc := tmux.Client{Socket: fmt.Sprintf("perch-ui-else-%d", time.Now().UnixNano())}
	t.Cleanup(func() {
		tc.KillServer()
		os.Remove(fmt.Sprintf("/private/tmp/tmux-%d/%s", os.Getuid(), tc.Socket))
	})
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "order")
	ps := `[{"name":"busy","session_id":"11","status":"running","attachment":"attached"},
	        {"name":"free","session_id":"22","status":"running","attachment":"detached"}]`
	b := &TmuxBackend{
		Tmux: tc, State: &state.State{Sessions: map[string]state.Meta{}}, StatePath: filepath.Join(tmp, "state.json"), Session: "perch",
		Nono:   nono.Client{Run: func(context.Context, ...string) ([]byte, error) { return []byte(ps), nil }},
		Detach: func(n string) string { return "echo detach-" + n + " >> " + marker },
		Adopt:  func(n string) string { return "echo attach-" + n + " >> " + marker + "; sleep 30" },
		Now:    func() time.Time { return time.Unix(1_800_000_000, 0) },
	}
	ss, err := b.List()
	if err != nil || len(ss) != 2 {
		t.Fatalf("%v %v", ss, err)
	}
	by := map[string]session.Session{ss[0].Name: ss[0], ss[1].Name: ss[1]}
	if by["busy"].Status != status.Elsewhere || by["free"].Status != status.Detached {
		t.Fatalf("statuses: busy=%v free=%v", by["busy"].Status, by["free"].Status)
	}
	if p := b.Preview(by["busy"]); !strings.Contains(p, "detaches it there") {
		t.Errorf("preview = %q", p)
	}

	if _, err := b.Attach(by["free"]); err != nil { // already free: no detach step
		t.Fatal(err)
	}
	if _, err := b.Attach(by["busy"]); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for i := 0; i < 40; i++ {
		got, _ = os.ReadFile(marker)
		if strings.Count(string(got), "\n") >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if string(got) != "attach-free\ndetach-busy\nattach-busy\n" {
		t.Errorf("order = %q (busy must be detached before it is attached; free must not be detached)", got)
	}
}
