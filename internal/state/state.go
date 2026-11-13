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
	Closed   []string        `json:"closed"` // tab-closed sessions, oldest first
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

func (s *State) IsClosed(name string) bool {
	for _, n := range s.Closed {
		if n == name {
			return true
		}
	}
	return false
}

// CloseTab marks name as tab-closed. The session itself keeps running.
func (s *State) CloseTab(name string) {
	if !s.IsClosed(name) {
		s.Closed = append(s.Closed, name)
	}
}

// Reopen removes name from Closed.
func (s *State) Reopen(name string) {
	out := s.Closed[:0]
	for _, n := range s.Closed {
		if n != name {
			out = append(out, n)
		}
	}
	s.Closed = out
}

// ReopenLast reopens the most recently closed tab.
func (s *State) ReopenLast() (string, bool) {
	if len(s.Closed) == 0 {
		return "", false
	}
	name := s.Closed[len(s.Closed)-1]
	s.Closed = s.Closed[:len(s.Closed)-1]
	return name, true
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
	s.Reopen(name)
	delete(s.Sessions, name)
}
