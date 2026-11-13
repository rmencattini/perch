package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBranch(t *testing.T) {
	good := []string{"main", "feat/x", "fix-1.2", "a_b", "release/2026.11"}
	bad := []string{"", "@", "-x", "/x", "x/", "x.", "x.lock", "a..b", "a//b", "a@{b", "a b", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", `a\b`, ".hidden", "a/.b", "a\tb"}
	for _, b := range good {
		if err := ValidateBranch(b); err != nil {
			t.Errorf("%q should be valid: %v", b, err)
		}
	}
	for _, b := range bad {
		if err := ValidateBranch(b); err == nil {
			t.Errorf("%q should be invalid", b)
		}
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "myrepo")
	os.Mkdir(dir, 0o755)
	for _, a := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "commit.gpgsign=false", "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	return dir
}

func TestAdd(t *testing.T) {
	repo := initRepo(t)
	root := t.TempDir()

	if !IsRepo(repo) || IsRepo(t.TempDir()) {
		t.Fatal("IsRepo wrong")
	}
	p, err := Add(repo, root, "feat/login")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "myrepo", "feat", "login"); p != want {
		t.Errorf("path %s, want %s", p, want)
	}
	if _, err := os.Stat(filepath.Join(p, ".git")); err != nil {
		t.Error("worktree not created")
	}
	// name resolves to the same repo from inside the worktree
	if n, err := RepoName(p); err != nil || n != "myrepo" {
		t.Errorf("RepoName in worktree = %q %v", n, err)
	}
	if _, err := Add(repo, root, "feat/login"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate: %v", err)
	}
	// existing branch is reused, not recreated
	exec.Command("git", "-C", repo, "branch", "old").Run()
	if _, err := Add(repo, root, "old"); err != nil {
		t.Errorf("existing branch: %v", err)
	}
	if _, err := Add(repo, root, "bad name"); err == nil {
		t.Error("invalid branch accepted")
	}
	if _, err := Add(t.TempDir(), root, "x"); err == nil {
		t.Error("non-repo accepted")
	}
}
