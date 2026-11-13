// Package worktree creates git worktrees under <root>/<repo>/<branch>.
package worktree

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ValidateBranch applies the core rules of git check-ref-format.
func ValidateBranch(b string) error {
	switch {
	case b == "":
		return errors.New("branch name is empty")
	case b == "@":
		return errors.New("branch name cannot be @")
	case strings.HasPrefix(b, "-"):
		return errors.New("branch name cannot start with -")
	case strings.HasPrefix(b, "/") || strings.HasSuffix(b, "/"):
		return errors.New("branch name cannot start or end with /")
	case strings.HasSuffix(b, ".") || strings.HasSuffix(b, ".lock"):
		return errors.New("branch name cannot end with . or .lock")
	case strings.Contains(b, "..") || strings.Contains(b, "//") || strings.Contains(b, "@{"):
		return errors.New("branch name cannot contain .. // or @{")
	}
	for _, r := range b {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return fmt.Errorf("branch name cannot contain %q", r)
		}
	}
	for _, part := range strings.Split(b, "/") {
		if strings.HasPrefix(part, ".") {
			return errors.New("a branch name part cannot start with .")
		}
	}
	return nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// IsRepo reports whether dir is inside a git repository.
func IsRepo(dir string) bool {
	_, err := git(dir, "rev-parse", "--git-dir")
	return err == nil
}

// RepoName is the name of the repository dir belongs to, also from inside a worktree.
func RepoName(dir string) (string, error) {
	common, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if filepath.Base(common) == ".git" {
		return filepath.Base(filepath.Dir(common)), nil
	}
	return strings.TrimSuffix(filepath.Base(common), ".git"), nil
}

// Path is where Add puts the worktree.
func Path(root, repo, branch string) string {
	return filepath.Join(root, repo, filepath.FromSlash(branch))
}

// Add creates the worktree for branch (created if missing) and returns its path.
func Add(repoDir, root, branch string) (string, error) {
	if err := ValidateBranch(branch); err != nil {
		return "", err
	}
	repo, err := RepoName(repoDir)
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", repoDir)
	}
	path := Path(root, repo, branch)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if _, err := git(repoDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		_, err = git(repoDir, "worktree", "add", path, branch)
		return path, err
	}
	_, err = git(repoDir, "worktree", "add", "-b", branch, path)
	return path, err
}
