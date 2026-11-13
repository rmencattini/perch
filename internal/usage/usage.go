// Package usage reads the rate-limit snapshots that `perch statusline` writes
// to ~/.cache/perch/usage/*.json.
package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// File is one snapshot, written by one Claude session.
type File struct {
	Updated  int64   `json:"updated"` // unix seconds
	FiveHour *Window `json:"five_hour,omitempty"`
	SevenDay *Window `json:"seven_day,omitempty"`
}

type Window struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"` // unix seconds, 0 = unknown
}

// Reading is what the header shows for one window.
type Reading struct {
	Known    bool
	Percent  float64
	ResetsAt time.Time
	Age      time.Duration // since the snapshot was written
}

type Summary struct {
	FiveHour, SevenDay Reading
}

// DefaultDir is ~/.cache/perch/usage ($XDG_CACHE_HOME/perch/usage when set).
func DefaultDir() (string, error) {
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "perch", "usage"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "perch", "usage"), nil
}

// Load reads every snapshot in dir. For each window the freshest snapshot that
// has it wins. A window that is missing, or whose reset time is already past
// (stale), stays unknown. It is never shown as 0%. Unreadable files are skipped.
func Load(dir string, now time.Time) Summary {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var s Summary
	var fiveAt, sevenAt int64 = -1, -1
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f File
		if json.Unmarshal(b, &f) != nil {
			continue
		}
		if r, ok := reading(f.FiveHour, f.Updated, now); ok && f.Updated > fiveAt {
			s.FiveHour, fiveAt = r, f.Updated
		}
		if r, ok := reading(f.SevenDay, f.Updated, now); ok && f.Updated > sevenAt {
			s.SevenDay, sevenAt = r, f.Updated
		}
	}
	return s
}

func reading(w *Window, updated int64, now time.Time) (Reading, bool) {
	if w == nil || w.UsedPercentage < 0 {
		return Reading{}, false
	}
	r := Reading{Known: true, Percent: w.UsedPercentage, Age: now.Sub(time.Unix(updated, 0))}
	if w.ResetsAt > 0 {
		r.ResetsAt = time.Unix(w.ResetsAt, 0)
		if !r.ResetsAt.After(now) {
			return Reading{}, false // stale: the window has reset since this was written
		}
	}
	return r, true
}
