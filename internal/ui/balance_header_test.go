package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/rmencattini/perch/internal/balance"
)

func TestHeaderBalance(t *testing.T) {
	ok := balance.Info{Available: true, Currency: "CNY", Total: "110.00"}
	tests := []struct {
		name string
		st   balance.State
		want string // "" = no DeepSeek text at all
	}{
		{"not configured", balance.State{}, ""},
		{"no key found", balance.State{Configured: true, Missing: true}, "DeepSeek: no key"},
		{"loading", balance.State{Configured: true}, "DeepSeek …"},
		{"ok", balance.State{Configured: true, Loaded: true, Info: ok}, "DeepSeek 110.00 CNY"},
		{"402", balance.State{Configured: true, Loaded: true, Err: balance.ErrExhausted}, "DeepSeek empty ✗"},
		{"unavailable", balance.State{Configured: true, Loaded: true, Info: balance.Info{Currency: "CNY", Total: "0.00"}}, "DeepSeek empty ✗"},
		{"401", balance.State{Configured: true, Loaded: true, Err: errors.New("401")}, "DeepSeek ?"},
	}
	for _, tc := range tests {
		f := newFake()
		f.balance = tc.st
		m := drive(New(f), refreshMsg{sessions: f.sessions, selected: "alpha", balance: tc.st})
		first := strings.SplitN(m.View(), "\n", 2)[0]
		if tc.want == "" {
			if strings.Contains(first, "DeepSeek") {
				t.Errorf("%s: header %q should not mention DeepSeek", tc.name, first)
			}
			continue
		}
		if !strings.Contains(first, tc.want) || !strings.Contains(first, "5h") {
			t.Errorf("%s: header %q missing %q or the usage bars", tc.name, first, tc.want)
		}
	}
}
