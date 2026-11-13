package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingFileIsDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.json"), "/h")
	if err != nil {
		t.Fatal(err)
	}
	if c.GitRoot != "/h/git" || c.WorktreeRoot != "/h/git/worktrees" || c.DefaultProfile != "" {
		t.Errorf("defaults wrong: %+v", c)
	}
	if c.DeepSeek.Enabled || c.DeepSeek.KeychainService != "perch" || c.DeepSeek.KeychainAccount != "deepseek_api_key" {
		t.Errorf("deepseek defaults wrong: %+v", c.DeepSeek)
	}
}

func TestLoadMergesKeyByKey(t *testing.T) {
	p := write(t, `{"default_profile":"p/claude","git_root":"~/src","deepseek":{"enabled":true},"unknown":1}`)
	c, err := Load(p, "/h")
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultProfile != "p/claude" || c.GitRoot != "/h/src" || c.WorktreeRoot != "/h/src/worktrees" {
		t.Errorf("merge wrong: %+v", c)
	}
	if !c.DeepSeek.Enabled || c.DeepSeek.KeychainService != "perch" {
		t.Errorf("nested keys must merge, not replace: %+v", c.DeepSeek)
	}
}

func TestLoadWorktreeRootExplicit(t *testing.T) {
	c, err := Load(write(t, `{"worktree_root":"~/wt"}`), "/h")
	if err != nil || c.WorktreeRoot != "/h/wt" {
		t.Errorf("got %+v, %v", c, err)
	}
}

func TestLoadBadJSON(t *testing.T) {
	if _, err := Load(write(t, `{nope`), "/h"); err == nil {
		t.Error("want error on bad JSON")
	}
}

func TestDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x")
	if d, _ := Dir(); d != "/x/perch" {
		t.Errorf("Dir = %q", d)
	}
}
