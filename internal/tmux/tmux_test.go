package tmux

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// newClient returns a Client on a private socket, killed at test end.
// Tests never touch the user's tmux.
func newClient(t *testing.T) Client {
	t.Helper()
	c := Client{Socket: fmt.Sprintf("perch-test-%d-%d", os.Getpid(), time.Now().UnixNano())}
	t.Cleanup(func() {
		c.KillServer()
		os.Remove(fmt.Sprintf("/private/tmp/tmux-%d/%s", os.Getuid(), c.Socket)) // killed servers leave the socket file
	})
	return c
}

func waitFor(t *testing.T, c Client, id, substr string) string {
	t.Helper()
	var out string
	for i := 0; i < 50; i++ {
		out, _ = c.CapturePane(id, 20)
		if strings.Contains(out, substr) {
			return out
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%q never appeared in pane; last: %q", substr, out)
	return ""
}

func TestSessionLifecycle(t *testing.T) {
	c := newClient(t)
	if c.HasSession("perch") {
		t.Fatal("session should not exist yet")
	}
	if ws, err := c.ListWindows("perch"); err != nil || ws != nil {
		t.Fatalf("missing session: %v %v", ws, err)
	}
	dir := t.TempDir()
	id1, err := c.NewSession("perch", "one", dir, "printf 'hello one\\n'; sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := c.NewWindow("perch", "two.with:odd", dir, "sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id1, "@") || id1 == id2 {
		t.Fatalf("ids: %q %q", id1, id2)
	}
	ws, err := c.ListWindows("perch")
	if err != nil || len(ws) != 2 {
		t.Fatalf("windows: %v %v", ws, err)
	}
	if ws[1].Name != "two.with:odd" || ws[1].ID != id2 || ws[1].Dead {
		t.Errorf("window 2 = %+v", ws[1])
	}
	waitFor(t, c, id1, "hello one")
	if err := c.SelectWindow(id2); err != nil {
		t.Error(err)
	}
	if err := c.KillWindow(id2); err != nil {
		t.Fatal(err)
	}
	if ws, _ := c.ListWindows("perch"); len(ws) != 1 || ws[0].ID != id1 {
		t.Errorf("after kill: %v", ws)
	}
}

func TestDeadPane(t *testing.T) {
	c := newClient(t)
	if _, err := c.NewSession("perch", "keep", t.TempDir(), "sleep 30"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.run("set-option", "-g", "remain-on-exit", "on"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NewWindow("perch", "dies", t.TempDir(), "true"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		ws, _ := c.ListWindows("perch")
		for _, w := range ws {
			if w.Name == "dies" && w.Dead {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("dead pane never reported")
}

func TestCapturePaneBadLines(t *testing.T) {
	if _, err := (Client{}).CapturePane("@1", 0); err == nil {
		t.Error("want error")
	}
}

func TestAttachCmd(t *testing.T) {
	got := Client{Socket: "s"}.AttachCmd("perch").Args
	want := []string{"tmux", "-L", "s", "attach", "-t", "=perch"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v", got)
	}
}

func TestAttachCmdInsideTmux(t *testing.T) {
	c := Client{Outer: "/tmp/tmux-501/default,123,0"}
	got := c.AttachCmd("perch").Args
	if got[1] != "display-popup" || got[2] != "-E" {
		t.Fatalf("want a popup, got %v", got)
	}
	inner := got[len(got)-1]
	if !strings.Contains(got[len(got)-2], "returns to perch") {
		t.Errorf("title = %q", got[len(got)-2])
	}
	if inner != "env -u TMUX tmux -S '/tmp/tmux-501/default' attach -t '=perch'" {
		t.Errorf("inner = %s", inner)
	}
	// a private test socket never goes through the popup
	if a := (Client{Socket: "s", Outer: "x,1,0"}).AttachCmd("perch").Args; a[1] != "-L" {
		t.Errorf("socket client used a popup: %v", a)
	}
}

func TestPrefixAndHumanKey(t *testing.T) {
	for in, want := range map[string]string{"C-a": "ctrl-a", "C-Space": "ctrl-Space", "M-b": "alt-b", "None": "", "": "", "F1": "F1"} {
		if got := humanKey(in); got != want {
			t.Errorf("humanKey(%q) = %q, want %q", in, got, want)
		}
	}
	c := newClient(t)
	if c.Prefix() != "" {
		t.Error("no server: prefix must be unknown, not guessed")
	}
	if _, err := c.NewSession("perch", "w", t.TempDir(), "sleep 30"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.run("set-option", "-g", "prefix", "C-a"); err != nil {
		t.Fatal(err)
	}
	if got := c.Prefix(); got != "ctrl-a" {
		t.Errorf("Prefix = %q", got)
	}
	if detachHint("ctrl-a") != "ctrl-a d" || detachHint("") != "<prefix> d" {
		t.Error("detachHint")
	}
}
