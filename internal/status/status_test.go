package status

import (
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		tail string
		want Status
	}{
		{"empty", "", Idle},
		{"blank lines", "\n\n  \n", Idle},
		{"idle prompt", "● Done.\n\n╭──────╮\n│ >    │\n╰──────╯\n  ? for shortcuts", Idle},
		{"running", "✻ Thinking… (12s · esc to interrupt)\n", Running},
		{"permission prompt", " Bash command\n   rm -rf build\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n", NeedsInput},
		{"edit prompt", " Do you want to make this edit to main.go?\n ❯ 1. Yes\n", NeedsInput},
		{"y/n", "Overwrite file? (y/n) ", NeedsInput},
		{"[Y/n]", "Continue? [Y/n]", NeedsInput},
		{"prompt beats spinner", "✻ Working (esc to interrupt)\nDo you want to proceed?\n❯ 1. Yes", NeedsInput},
		{"old prompt scrolled away", "Do you want to proceed?\n" + strings.Repeat("output\n", 40) + "✻ Working (esc to interrupt)", Running},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(tc.tail); got != tc.want {
				t.Errorf("Detect = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSymbols(t *testing.T) {
	want := map[Status]string{Running: "● running", NeedsInput: "! needs input", Idle: "○ idle", Detached: "◌ detached", Elsewhere: "◐ other tab"}
	for s, w := range want {
		if got := s.Symbol() + " " + s.String(); got != w {
			t.Errorf("got %q, want %q", got, w)
		}
	}
}
