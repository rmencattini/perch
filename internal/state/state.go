// Package state persists perch's own data in ~/.config/perch/state.json.
// It holds no secrets: only session names, folders and profile names.
package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const maxRecent = 10

// Meta is what perch remembers about a session beyond what tmux knows.
type Meta struct {
	Dir     string `json:"dir"`
	Profile string `json:"profile"`
	Command string `json:"command"`
	Created int64  `json:"created"` // unix seconds
}

type State struct {
	Recent   []string        `json:"recent"` // recent folders, newest first
	Sessions map[string]Meta `json:"sessions"`
}

// DefaultPath is ~/.config/perch/state.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "perch", "state.json"), nil
}

// Load reads path. A missing file is an empty state, not an error.
func Load(path string) (*State, error) {
	s := &State{}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		s.Sessions = map[string]Meta{}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, err
	}
	if s.Sessions == nil {
		s.Sessions = map[string]Meta{}
	}
	return s, nil
}

// Save writes path atomically: temp file in the same dir, then rename.
func (s *State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// AddRecent puts dir first, dropping duplicates and old entries.
func (s *State) AddRecent(dir string) {
	out := []string{dir}
	for _, d := range s.Recent {
		if d != dir && len(out) < maxRecent {
			out = append(out, d)
		}
	}
	s.Recent = out
}

// Forget drops everything about a killed session.
func (s *State) Forget(name string) {
	delete(s.Sessions, name)
}
