// Package config reads perch's settings: the defaults shipped in
// config.default.json, then the user's ~/.config/perch/config.json on top,
// key by key.
package config

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed config.default.json
var defaults []byte

type DeepSeek struct {
	Enabled         bool   `json:"enabled"`
	KeychainService string `json:"keychain_service"`
	KeychainAccount string `json:"keychain_account"`
}

type Config struct {
	DefaultProfile string   `json:"default_profile"` // "" = pick from `nono profile list`
	GitRoot        string   `json:"git_root"`        // repos the folder picker lists
	WorktreeRoot   string   `json:"worktree_root"`   // "" = <git_root>/worktrees
	DeepSeek       DeepSeek `json:"deepseek"`
}

// Dir is perch's config folder: $XDG_CONFIG_HOME/perch, else ~/.config/perch.
func Dir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "perch"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "perch"), nil
}

// DefaultPath is <Dir>/config.json.
func DefaultPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load merges the user file at path over the shipped defaults. A missing file
// is not an error. Unknown keys are ignored; bad JSON is an error.
func Load(path, home string) (Config, error) {
	var c Config
	if err := json.Unmarshal(defaults, &c); err != nil {
		return c, fmt.Errorf("config.default.json: %w", err)
	}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return c, err
	default:
		if err := json.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("%s: %w", path, err)
		}
	}
	c.GitRoot = expand(c.GitRoot, home)
	if c.WorktreeRoot == "" {
		c.WorktreeRoot = filepath.Join(c.GitRoot, "worktrees")
	}
	c.WorktreeRoot = expand(c.WorktreeRoot, home)
	return c, nil
}

func expand(p, home string) string {
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	}
	return p
}
