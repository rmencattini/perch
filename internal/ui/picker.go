package ui

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const repoDepth = 3

type pickDoneMsg struct {
	path string // "" = cancelled
	err  error
}

// RepoFolders lists git repositories under root, up to repoDepth levels deep.
// It does not descend into a repository, so nested checkouts do not flood the list.
func RepoFolders(root string) []string {
	var out []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			out = append(out, dir)
			return
		}
		if depth >= repoDepth {
			return
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range ents {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				walk(filepath.Join(dir, e.Name()), depth+1)
			}
		}
	}
	walk(root, 0)
	sort.Strings(out)
	return out
}

// Candidates puts recent folders first, then repos, without duplicates.
// Paths use the "~/" spelling so the list stays short on screen.
func Candidates(recent, repos []string, home string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range append(append([]string{}, recent...), repos...) {
		p = filepath.Clean(p)
		if seen[p] {
			continue
		}
		seen[p] = true
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			p = "~/" + rel
		}
		out = append(out, p)
	}
	return out
}

// fzfPick runs fzf over the candidates, full screen, and reports the choice.
// fzf draws on /dev/tty, so its stdout carries only the selection.
func fzfPick(cands []string) tea.Cmd {
	path, err := exec.LookPath("fzf")
	if err != nil {
		return func() tea.Msg { return pickDoneMsg{err: errors.New("fzf not found (brew install fzf)")} }
	}
	var out bytes.Buffer
	cmd := exec.Command(path, "--reverse", "--prompt=folder> ", "--height=60%")
	cmd.Stdin = strings.NewReader(strings.Join(cands, "\n") + "\n")
	cmd.Stdout = &out
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		var ee *exec.ExitError
		if errors.As(err, &ee) && (ee.ExitCode() == 1 || ee.ExitCode() == 130) {
			return pickDoneMsg{} // no match, or esc
		}
		if err != nil {
			return pickDoneMsg{err: err}
		}
		return pickDoneMsg{path: strings.TrimSpace(out.String())}
	})
}
