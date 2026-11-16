// Package nono wraps the nono CLI: profile list, the launch command, stop, ps.
package nono

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

type Profile struct {
	Name        string   `json:"name"`
	Source      string   `json:"source"` // built-in, pack, user
	Description string   `json:"description"`
	Extends     []string `json:"extends"`
}

// ParseProfiles reads the output of `nono profile list --json`. User-made
// profiles come first, then the rest in nono's order.
func ParseProfiles(b []byte) ([]Profile, error) {
	var ps []Profile
	if err := json.Unmarshal(b, &ps); err != nil {
		return nil, fmt.Errorf("nono: parse profile list: %w", err)
	}
	sort.SliceStable(ps, func(i, j int) bool {
		return ps[i].Source == "user" && ps[j].Source != "user"
	})
	return ps, nil
}

// PsEntry is one row of `nono ps --json`. nono ps cannot run inside the dev
// sandbox, so the field names (session_id, name, status, attachment) come from
// the nono binary, not from a captured sample: every field is optional.
type PsEntry struct {
	Name       string
	ID         string
	Status     string // running, paused, exited
	Attachment string // attached, detached
}

// Live reports whether the session is still running.
func (e PsEntry) Live() bool {
	return !strings.EqualFold(e.Status, "exited")
}

// ParsePs reads `nono ps --json`: an array of objects (or {"sessions":[...]}).
func ParsePs(b []byte) ([]PsEntry, error) {
	var rows []map[string]any
	if err := json.Unmarshal(b, &rows); err != nil {
		var wrapped struct {
			Sessions []map[string]any `json:"sessions"`
		}
		if err2 := json.Unmarshal(b, &wrapped); err2 != nil {
			return nil, fmt.Errorf("nono: parse ps: %w", err)
		}
		rows = wrapped.Sessions
	}
	var out []PsEntry
	for _, r := range rows {
		e := PsEntry{
			Name:       first(r, "name", "session_name"),
			ID:         first(r, "id", "session_id"),
			Status:     first(r, "status", "state"),
			Attachment: first(r, "attachment", "attached"),
		}
		out = append(out, e)
	}
	return out, nil
}

// first returns the first present key as text. Enums that serialize as
// {"detached": ...} give their variant name.
func first(r map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := r[k].(type) {
		case string:
			return v
		case bool:
			if v {
				return "attached"
			}
			return "detached"
		case map[string]any:
			for name := range v {
				return name
			}
		}
	}
	return ""
}

// Runner executes the nono binary. Tests replace it.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// Exec runs the real nono.
func Exec(ctx context.Context, args ...string) ([]byte, error) {
	// stdout only: nono's progress or warning lines on stderr would corrupt --json output.
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "nono", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(string(out))
		}
		return out, fmt.Errorf("nono %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return out, nil
}

type Client struct{ Run Runner }

func New() Client { return Client{Run: Exec} }

func (c Client) Profiles(ctx context.Context) ([]Profile, error) {
	out, err := c.Run(ctx, "profile", "list", "--json")
	if err != nil {
		return nil, err
	}
	return ParseProfiles(out)
}

func (c Client) Stop(ctx context.Context, name string) error {
	_, err := c.Run(ctx, "stop", name)
	return err
}

func (c Client) Ps(ctx context.Context) ([]PsEntry, error) {
	out, err := c.Run(ctx, "ps", "--json")
	if err != nil {
		return nil, err
	}
	return ParsePs(out)
}

// Agent is the program a profile runs: the last part of its name
// (nolabs-ai/opencode -> opencode) when that is a known agent, else claude.
func Agent(profile string) string {
	name := profile[strings.LastIndex(profile, "/")+1:]
	switch name {
	case "opencode", "claude":
		return name
	}
	return "claude"
}

// Spec describes one launch.
type Spec struct {
	Name    string
	Profile string
	Allow   []string // --allow paths (read+write)
	Read    []string // --read paths
	Quiet   []string // --suppress-save-prompt paths: no "review denied paths" prompt, no access granted
	Command []string // what to run inside the sandbox, e.g. ["claude"]
}

// Args is the nono argument list after the binary name.
func (s Spec) Args() []string {
	a := []string{"run", "--name", s.Name}
	if s.Profile != "" {
		a = append(a, "--profile", s.Profile)
	}
	for _, p := range s.Allow {
		a = append(a, "--allow", p)
	}
	for _, p := range s.Read {
		a = append(a, "--read", p)
	}
	for _, p := range s.Quiet {
		a = append(a, "--suppress-save-prompt", p)
	}
	a = append(a, "--")
	return append(a, s.Command...)
}

// String is the shell command line. One builder feeds both the preview and
// the tmux launch, so what the user sees is what runs.
func (s Spec) String() string {
	parts := []string{"nono"}
	for _, a := range s.Args() {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ")
}

func quote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=@%+,~", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// PickDefault is the profile preselected in the new-session dialog: the
// configured one when it exists, else the first whose name contains "claude",
// else "" (the dialog keeps its first row).
func PickDefault(profiles []Profile, configured string) string {
	for _, p := range profiles {
		if configured != "" && p.Name == configured {
			return p.Name
		}
	}
	for _, p := range profiles {
		if strings.Contains(p.Name, "claude") {
			return p.Name
		}
	}
	return ""
}
