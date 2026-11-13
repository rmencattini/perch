// Package tmux is a thin wrapper on the tmux CLI. Windows are targeted by
// id (@N), never by name, because names can repeat or contain ':' and '.'.
package tmux

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Client talks to one tmux server. Socket "" means the user's default server;
// tests pass a private socket so they never touch it.
type Client struct {
	Socket string
	// Outer is $TMUX when perch itself runs inside tmux. A plain attach would
	// nest, and ctrl-b d would then hit the outer tmux, so attach goes through
	// a popup instead. Ignored when Socket is set.
	Outer string
}

// Window is one tmux window.
type Window struct {
	ID   string // "@3"
	Name string
	Dead bool // pane process has exited
}

func (c Client) args(a ...string) []string {
	if c.Socket != "" {
		return append([]string{"-L", c.Socket}, a...)
	}
	return a
}

func (c Client) run(a ...string) (string, error) {
	cmd := exec.Command("tmux", c.args(a...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(a, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// HasSession reports whether the session exists.
func (c Client) HasSession(session string) bool {
	_, err := c.run("has-session", "-t", "="+session)
	return err == nil
}

// NewSession creates a detached session whose first window is named name.
// command is run by the shell; "" starts the default shell.
func (c Client) NewSession(session, name, dir, command string) (string, error) {
	a := []string{"new-session", "-d", "-P", "-F", "#{window_id}", "-s", session, "-n", name, "-c", dir}
	if command != "" {
		a = append(a, command)
	}
	out, err := c.run(a...)
	return strings.TrimSpace(out), err
}

// NewWindow adds a window to an existing session and returns its id.
func (c Client) NewWindow(session, name, dir, command string) (string, error) {
	a := []string{"new-window", "-d", "-P", "-F", "#{window_id}", "-t", "=" + session + ":", "-n", name, "-c", dir}
	if command != "" {
		a = append(a, command)
	}
	out, err := c.run(a...)
	return strings.TrimSpace(out), err
}

// ListWindows lists the windows of session. A missing session is an empty list.
func (c Client) ListWindows(session string) ([]Window, error) {
	if !c.HasSession(session) {
		return nil, nil
	}
	out, err := c.run("list-windows", "-t", "="+session, "-F", "#{window_id} #{pane_dead} #{window_name}")
	if err != nil {
		return nil, err
	}
	var ws []Window
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, " ", 3)
		if len(f) != 3 {
			return nil, fmt.Errorf("tmux: bad list-windows line %q", line)
		}
		ws = append(ws, Window{ID: f[0], Dead: f[1] == "1", Name: f[2]})
	}
	return ws, nil
}

// CapturePane returns the last lines visible lines of the window's pane.
func (c Client) CapturePane(id string, lines int) (string, error) {
	if lines < 1 {
		return "", errors.New("tmux: lines must be >= 1")
	}
	return c.run("capture-pane", "-p", "-t", id, "-S", fmt.Sprintf("-%d", lines))
}

func (c Client) SelectWindow(id string) error {
	_, err := c.run("select-window", "-t", id)
	return err
}

func (c Client) KillWindow(id string) error {
	_, err := c.run("kill-window", "-t", id)
	return err
}

func (c Client) KillServer() error {
	_, err := c.run("kill-server")
	return err
}

// Prefix is the user's tmux prefix key, spelled for humans ("ctrl-a").
// It reads the live server, so a custom ~/.tmux.conf is respected.
// Without a running server it says so instead of guessing ctrl-b.
func (c Client) Prefix() string {
	out, err := c.run("show-options", "-gv", "prefix")
	if err != nil {
		return ""
	}
	return humanKey(strings.TrimSpace(out))
}

func humanKey(k string) string {
	switch {
	case k == "" || k == "None":
		return ""
	case strings.HasPrefix(k, "C-"):
		return "ctrl-" + k[2:]
	case strings.HasPrefix(k, "M-"):
		return "alt-" + k[2:]
	}
	return k
}

// AttachCmd builds the attach command, for tea.ExecProcess to run.
func (c Client) AttachCmd(session string) *exec.Cmd {
	if c.Outer == "" || c.Socket != "" {
		return exec.Command("tmux", c.args("attach", "-t", "="+session)...)
	}
	// $TMUX is "<socket path>,<pid>,<n>". The popup holds a nested client on
	// the same server; ctrl-b d closes the popup and returns to perch.
	sock, _, _ := strings.Cut(c.Outer, ",")
	inner := "env -u TMUX tmux -S " + shQuote(sock) + " attach -t " + shQuote("="+session)
	return exec.Command("tmux", "display-popup", "-E", "-w", "95%", "-h", "95%",
		"-T", " "+session+" · "+detachHint(c.Prefix())+" returns to perch ", inner)
}

// detachHint is the key sequence that detaches, e.g. "ctrl-a d".
func detachHint(prefix string) string {
	if prefix == "" {
		return "<prefix> d"
	}
	return prefix + " d"
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
