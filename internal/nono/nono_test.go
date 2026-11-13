package nono

import (
	"context"
	"os"
	"reflect"
	"testing"
)

func TestParseProfilesReal(t *testing.T) {
	b, err := os.ReadFile("testdata/profile_list.json")
	if err != nil {
		t.Fatal(err)
	}
	ps, err := ParseProfiles(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 12 {
		t.Fatalf("got %d profiles, want 12", len(ps))
	}
	var found bool
	for _, p := range ps {
		if p.Name == "nolabs-ai/claude" {
			found = true
			if p.Source != "pack" || p.Description == "" {
				t.Errorf("bad default profile: %+v", p)
			}
		}
	}
	if !found {
		t.Error("nolabs-ai/claude missing")
	}
}

func TestParseProfilesBad(t *testing.T) {
	if _, err := ParseProfiles([]byte("nono profile: 12 profiles")); err == nil {
		t.Error("want error on non-JSON")
	}
}

func TestSpecString(t *testing.T) {
	s := Spec{
		Name: "fix auth", Profile: "nolabs-ai/claude",
		Allow:   []string{"/home/u/.cache/perch/usage"},
		Read:    []string{"/opt/perch bin"},
		Command: []string{"claude"},
	}
	want := `nono run --name 'fix auth' --profile nolabs-ai/claude --allow /home/u/.cache/perch/usage --read '/opt/perch bin' -- claude`
	if got := s.String(); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := (Spec{Name: "it's"}).String(); got != `nono run --name 'it'\''s' --` {
		t.Errorf("quote: %s", got)
	}
}

func TestParsePs(t *testing.T) {
	for _, in := range []string{
		`[{"name":"a","id":"1"},{"name":"b","session_id":"2"}]`,
		`{"sessions":[{"name":"a","id":"1"},{"name":"b","session_id":"2"}]}`,
	} {
		got, err := ParsePs([]byte(in))
		want := []PsEntry{{Name: "a", ID: "1"}, {Name: "b", ID: "2"}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	if _, err := ParsePs([]byte("x")); err == nil {
		t.Error("want error")
	}
}

func TestClientArgs(t *testing.T) {
	var calls [][]string
	c := Client{Run: func(_ context.Context, a ...string) ([]byte, error) {
		calls = append(calls, a)
		return []byte(`[]`), nil
	}}
	c.Stop(context.Background(), "a")
	c.Ps(context.Background())
	c.Profiles(context.Background())
	want := [][]string{{"stop", "a"}, {"ps", "--json"}, {"profile", "list", "--json"}}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v", calls)
	}
}

func TestParsePsStatusAndAttachment(t *testing.T) {
	got, err := ParsePs([]byte(`[
	 {"session_id":"c0ffee","name":"calm-gate","status":"running","attachment":"detached"},
	 {"session_id":"beef","status":"exited","attachment":{"attached":{}}},
	 {"id":"x1","attached":true}]`))
	if err != nil {
		t.Fatal(err)
	}
	want := []PsEntry{
		{Name: "calm-gate", ID: "c0ffee", Status: "running", Attachment: "detached"},
		{ID: "beef", Status: "exited", Attachment: "attached"},
		{ID: "x1", Attachment: "attached"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
	if got[1].Live() || !got[0].Live() || !got[2].Live() {
		t.Error("Live() wrong")
	}
}

func TestAgent(t *testing.T) {
	for profile, want := range map[string]string{
		"nolabs-ai/claude": "claude", "nolabs-ai/opencode": "opencode", "opencode": "opencode",
		"go-dev": "claude", "": "claude",
	} {
		if got := Agent(profile); got != want {
			t.Errorf("Agent(%q) = %q, want %q", profile, got, want)
		}
	}
}

func TestPickDefault(t *testing.T) {
	ps := []Profile{{Name: "default"}, {Name: "acme/claude"}, {Name: "mine"}}
	cases := []struct{ configured, want string }{
		{"mine", "mine"},        // configured and present
		{"gone", "acme/claude"}, // configured but absent: first claude profile
		{"", "acme/claude"},     // nothing configured
	}
	for _, c := range cases {
		if got := PickDefault(ps, c.configured); got != c.want {
			t.Errorf("PickDefault(%q) = %q, want %q", c.configured, got, c.want)
		}
	}
	if got := PickDefault([]Profile{{Name: "default"}}, ""); got != "" {
		t.Errorf("no claude profile: got %q, want empty", got)
	}
}
