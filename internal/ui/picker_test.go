package ui

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRepoFolders(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{
		"a/.git", "b/c/.git", "b/c/inner/.git", "plain/x", ".hidden/.git", "d/e/f/g/.git",
	} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	os.MkdirAll(filepath.Join(root, "wt/r/br"), 0o755)
	os.WriteFile(filepath.Join(root, "wt/r/br/.git"), []byte("gitdir: x"), 0o644) // a worktree has a .git *file*
	var rel []string
	for _, p := range RepoFolders(root) {
		r, _ := filepath.Rel(root, p)
		rel = append(rel, r)
	}
	want := []string{"a", "b/c", "wt/r/br"} // not inner (inside a repo), not hidden, not deeper than 3
	if !reflect.DeepEqual(rel, want) {
		t.Errorf("got %v, want %v", rel, want)
	}
	if got := RepoFolders(filepath.Join(root, "missing")); len(got) != 0 {
		t.Errorf("missing root: %v", got)
	}
}

func TestCandidates(t *testing.T) {
	got := Candidates([]string{"/h/git/b", "/other/x"}, []string{"/h/git/a", "/h/git/b/"}, "/h")
	want := []string{"~/git/b", "/other/x", "~/git/a"} // recent first, deduped, ~ form under home
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func pickerModel(t *testing.T, defDir string) (Model, *[][]string) {
	t.Helper()
	f := newFake()
	f.defDir = defDir
	var asked [][]string
	m := New(f)
	m.home = t.TempDir()
	f.gitRoot = filepath.Join(m.home, "git")
	m.pick = func(c []string) tea.Cmd { asked = append(asked, c); return nil }
	m = drive(m, profilesMsg{profiles: f.profiles})
	return m, &asked
}

func TestCtrlFOpensPickerAndFillsFolder(t *testing.T) {
	m, asked := pickerModel(t, "")
	m = drive(m, typ("n1"), tea.KeyMsg{Type: tea.KeyCtrlF})
	if len(*asked) != 1 || m.dlg.focus != fDir {
		t.Fatalf("picker calls=%d focus=%v", len(*asked), m.dlg.focus)
	}
	dir := t.TempDir()
	m = drive(m, pickDoneMsg{path: dir})
	if got := m.dlg.dir.Value(); got != dir {
		t.Errorf("folder = %q", got)
	}
	if errs := m.dlg.errors(); errs[fDir] != "" {
		t.Errorf("picked folder rejected: %v", errs)
	}
	m = drive(m, pickDoneMsg{}) // esc in fzf keeps the folder
	if m.dlg.dir.Value() != dir {
		t.Error("cancel must not clear the folder")
	}
}

func TestTabIntoEmptyFolderOpensPicker(t *testing.T) {
	m, asked := pickerModel(t, "")
	m = drive(m, typ("n1"), special(tea.KeyTab))
	if len(*asked) != 1 {
		t.Fatalf("picker calls = %d", len(*asked))
	}
	m = drive(m, special(tea.KeyTab)) // folder still empty, but tab leaving it must not reopen
	if len(*asked) != 1 || m.dlg.focus != fExtra {
		t.Errorf("calls=%d focus=%v", len(*asked), m.dlg.focus)
	}
	// with a folder already set, arriving does not open it
	m2, asked2 := pickerModel(t, t.TempDir())
	drive(m2, typ("n1"), special(tea.KeyTab))
	if len(*asked2) != 0 {
		t.Error("picker opened although a folder was set")
	}
}

func TestPickerCandidatesComeFromRecentAndRepos(t *testing.T) {
	m, asked := pickerModel(t, "")
	os.MkdirAll(filepath.Join(m.home, "git", "proj", ".git"), 0o755)
	m = drive(m, special(tea.KeyCtrlF))
	if got := (*asked)[0]; !reflect.DeepEqual(got, []string{"~/git/proj"}) {
		t.Errorf("candidates = %v", got)
	}
}

func TestPickerError(t *testing.T) {
	m, _ := pickerModel(t, "")
	m = drive(m, pickDoneMsg{err: errors.New("fzf not found (brew install fzf)")})
	if !strings.Contains(m.View(), "fzf not found") {
		t.Error("error not shown in the dialog")
	}
	if cmd := fzfPick(nil); cmd == nil {
		t.Error("fzfPick must always return a command")
	}
}

func TestFolderHintVisibleOnFolderField(t *testing.T) {
	m, _ := pickerModel(t, t.TempDir())
	m = drive(m, special(tea.KeyTab))
	if !strings.Contains(m.View(), "ctrl+f: pick with fzf") {
		t.Error("hint missing")
	}
}
