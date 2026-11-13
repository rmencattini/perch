package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "state.json")
	s := &State{Sessions: map[string]Meta{"a": {Dir: "/x", Profile: "p", Command: "claude", Created: 5}}}
	s.CloseTab("a")
	s.AddRecent("/x")
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, s) {
		t.Errorf("got %+v, want %+v", got, s)
	}
	if ents, _ := os.ReadDir(filepath.Dir(p)); len(ents) != 1 {
		t.Errorf("temp file left behind: %v", ents)
	}
}

func TestLoadMissingAndMalformed(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(filepath.Join(dir, "none.json"))
	if err != nil || s.Sessions == nil {
		t.Fatalf("missing file: %v %+v", err, s)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{nope"), 0o644)
	if _, err := Load(bad); err == nil {
		t.Error("malformed JSON should error")
	}
}

func TestCloseReopen(t *testing.T) {
	s := &State{}
	s.CloseTab("a")
	s.CloseTab("b")
	s.CloseTab("a") // no duplicate
	if !reflect.DeepEqual(s.Closed, []string{"a", "b"}) {
		t.Fatalf("closed = %v", s.Closed)
	}
	if n, ok := s.ReopenLast(); !ok || n != "b" {
		t.Errorf("ReopenLast = %q %v", n, ok)
	}
	s.Reopen("a")
	if _, ok := s.ReopenLast(); ok || s.IsClosed("a") {
		t.Error("expected nothing closed")
	}
}

func TestRecentAndForget(t *testing.T) {
	s := &State{Sessions: map[string]Meta{"a": {}}}
	for i := 0; i < 15; i++ {
		s.AddRecent(string(rune('a' + i)))
	}
	s.AddRecent("e")
	if len(s.Recent) != maxRecent || s.Recent[0] != "e" {
		t.Errorf("recent = %v", s.Recent)
	}
	s.CloseTab("a")
	s.Forget("a")
	if s.IsClosed("a") || len(s.Sessions) != 0 {
		t.Error("Forget left traces")
	}
}
