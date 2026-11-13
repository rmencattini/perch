// Package statusline is `perch statusline`: Claude Code pipes its status JSON
// to stdin; perch saves the rate-limit part for the hub and prints a status line.
package statusline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rmencattini/perch/internal/usage"
)

type Options struct {
	Dir  string    // snapshot directory, ~/.cache/perch/usage
	Now  time.Time // zero = time.Now()
	Wrap string    // optional existing statusLine command to run on the same input
}

type input struct {
	SessionID  string `json:"session_id"`
	RateLimits struct {
		FiveHour json.RawMessage `json:"five_hour"`
		SevenDay json.RawMessage `json:"seven_day"`
	} `json:"rate_limits"`
}

// Run never fails the status line for bad input: unparseable JSON just means
// nothing is saved. The returned error is only for the caller to log.
func Run(in io.Reader, out io.Writer, o Options) error {
	raw, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	snap, id, ok := parse(raw, o.Now)

	var saveErr error
	if ok {
		saveErr = write(o.Dir, id, snap)
	}
	if o.Wrap != "" {
		cmd := exec.Command("sh", "-c", o.Wrap)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(raw), out, os.Stderr
		cmd.Run() // the wrapped command's own failure is its business
		return saveErr
	}
	fmt.Fprintln(out, summary(snap))
	return saveErr
}

func parse(raw []byte, now time.Time) (usage.File, string, bool) {
	var in input
	if json.Unmarshal(raw, &in) != nil {
		return usage.File{}, "", false
	}
	f := usage.File{Updated: now.Unix(), FiveHour: window(in.RateLimits.FiveHour), SevenDay: window(in.RateLimits.SevenDay)}
	return f, in.SessionID, f.FiveHour != nil || f.SevenDay != nil
}

// window reads {"used_percentage": n, "resets_at": unix-seconds | RFC3339}.
func window(raw json.RawMessage) *usage.Window {
	if len(raw) == 0 {
		return nil
	}
	var w struct {
		Used     *float64        `json:"used_percentage"`
		ResetsAt json.RawMessage `json:"resets_at"`
	}
	if json.Unmarshal(raw, &w) != nil || w.Used == nil {
		return nil
	}
	out := &usage.Window{UsedPercentage: *w.Used}
	var secs float64
	var str string
	if json.Unmarshal(w.ResetsAt, &secs) == nil {
		out.ResetsAt = int64(secs)
	} else if json.Unmarshal(w.ResetsAt, &str) == nil {
		if t, err := time.Parse(time.RFC3339, str); err == nil {
			out.ResetsAt = t.Unix()
		}
	}
	return out
}

func summary(f usage.File) string {
	var parts []string
	if f.FiveHour != nil {
		parts = append(parts, fmt.Sprintf("5h %.0f%%", f.FiveHour.UsedPercentage))
	}
	if f.SevenDay != nil {
		parts = append(parts, fmt.Sprintf("7d %.0f%%", f.SevenDay.UsedPercentage))
	}
	return strings.Join(parts, " · ")
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func write(dir, id string, f usage.File) error {
	name := unsafeName.ReplaceAllString(id, "_")
	if name == "" || strings.Trim(name, ".") == "" {
		name = "unknown"
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".snap-*.tmp") // not *.json: readers never see a half file
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name+".json"))
}
