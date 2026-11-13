package statusline

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"perch/internal/usage"
)

var now = time.Unix(1_800_000_000, 0)

const sample = `{"session_id":"abc-1","model":{"id":"x"},"rate_limits":{"five_hour":{"used_percentage":42.4,"resets_at":1800003600},"seven_day":{"used_percentage":10,"resets_at":"2027-01-15T09:00:00Z"}}}`

func run(t *testing.T, in, wrap string) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "usage")
	var out bytes.Buffer
	if err := Run(strings.NewReader(in), &out, Options{Dir: dir, Now: now, Wrap: wrap}); err != nil {
		t.Fatal(err)
	}
	return out.String(), dir
}

func TestWritesSnapshot(t *testing.T) {
	out, dir := run(t, sample, "")
	if out != "5h 42% · 7d 10%\n" {
		t.Errorf("out = %q", out)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != "abc-1.json" {
		t.Fatalf("dir = %v (temp file left behind?)", ents)
	}
	s := usage.Load(dir, now)
	if !s.FiveHour.Known || s.FiveHour.Percent != 42.4 || !s.SevenDay.Known {
		t.Errorf("hub reads %+v", s)
	}
	if !s.SevenDay.ResetsAt.After(now) {
		t.Errorf("RFC3339 resets_at not parsed: %v", s.SevenDay.ResetsAt)
	}
}

func TestBadInputIsHarmless(t *testing.T) {
	for _, in := range []string{"", "not json", `{}`, `{"session_id":"s","rate_limits":{}}`, `{"rate_limits":{"five_hour":{"resets_at":5}}}`} {
		out, dir := run(t, in, "")
		if strings.TrimSpace(out) != "" {
			t.Errorf("%q: out = %q", in, out)
		}
		if ents, _ := os.ReadDir(dir); len(ents) != 0 {
			t.Errorf("%q: wrote %v", in, ents)
		}
	}
}

func TestOneWindowAndUnsafeID(t *testing.T) {
	out, dir := run(t, `{"session_id":"../../etc/x","rate_limits":{"seven_day":{"used_percentage":3}}}`, "")
	if out != "7d 3%\n" {
		t.Errorf("out = %q", out)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || strings.Contains(ents[0].Name(), "/") || ents[0].Name() != ".._.._etc_x.json" {
		t.Errorf("dir = %v", ents)
	}
	s := usage.Load(dir, now)
	if s.FiveHour.Known || !s.SevenDay.Known {
		t.Errorf("got %+v", s)
	}
}

func TestWrapPassesInputThrough(t *testing.T) {
	out, dir := run(t, sample, "cat")
	if out != sample {
		t.Errorf("wrapped cat did not get the raw input: %q", out)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Error("snapshot must still be written when wrapping")
	}
	out, _ = run(t, sample, "tr a-z A-Z | head -c 12")
	if out != strings.ToUpper(sample[:12]) {
		t.Errorf("out = %q", out)
	}
	out, _ = run(t, sample, "exit 3") // a failing wrapped command must not break us
	if out != "" {
		t.Errorf("out = %q", out)
	}
}

func TestOverwritesSameSession(t *testing.T) {
	dir := t.TempDir()
	for _, pct := range []string{"10", "20"} {
		in := `{"session_id":"s","rate_limits":{"five_hour":{"used_percentage":` + pct + `,"resets_at":1800003600}}}`
		if err := Run(strings.NewReader(in), &bytes.Buffer{}, Options{Dir: dir, Now: now}); err != nil {
			t.Fatal(err)
		}
	}
	if s := usage.Load(dir, now); s.FiveHour.Percent != 20 {
		t.Errorf("got %v", s.FiveHour.Percent)
	}
}
