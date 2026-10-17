//go:build !sbo_omit_terminals

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/termsvc"
)

// TestTerminalColorEnvTable is one case per terminalColorEnvRules row, per
// PTY column, plus the pass-through and ordering guarantees.
func TestTerminalColorEnvTable(t *testing.T) {
	cases := []struct {
		name    string
		windows bool
		in      []string
		want    []string
	}{
		{
			name: "posix: agent-harness env (TERM=dumb, NO_COLOR, FORCE_COLOR=0) is replaced",
			in:   []string{"HOME=/h", "TERM=dumb", "NO_COLOR=1", "FORCE_COLOR=0", "PATH=/bin"},
			want: []string{"HOME=/h", "PATH=/bin", "TERM=xterm-256color", "COLORTERM=truecolor"},
		},
		{
			name: "posix: no TERM at all (supervisor unit) gains TERM + COLORTERM",
			in:   []string{"HOME=/h"},
			want: []string{"HOME=/h", "TERM=xterm-256color", "COLORTERM=truecolor"},
		},
		{
			name: "posix: an inherited 256-color TERM without COLORTERM is upgraded to truecolor",
			in:   []string{"TERM=xterm-256color", "LANG=C.UTF-8"},
			want: []string{"LANG=C.UTF-8", "TERM=xterm-256color", "COLORTERM=truecolor"},
		},
		{
			name: "posix: keys are case-sensitive (a lowercase lookalike is left alone)",
			in:   []string{"no_color=1", "Term=x"},
			want: []string{"no_color=1", "Term=x", "TERM=xterm-256color", "COLORTERM=truecolor"},
		},
		{
			name:    "windows: TERM is never invented (ConPTY has no terminfo)",
			windows: true,
			in:      []string{"Path=C:\\w"},
			want:    []string{"Path=C:\\w", "COLORTERM=truecolor"},
		},
		{
			name:    "windows: an inherited TERM=dumb is dropped",
			windows: true,
			in:      []string{"TERM=dumb", "Path=C:\\w"},
			want:    []string{"Path=C:\\w", "COLORTERM=truecolor"},
		},
		{
			name:    "windows: a real inherited TERM is kept",
			windows: true,
			in:      []string{"TERM=xterm-256color"},
			want:    []string{"TERM=xterm-256color", "COLORTERM=truecolor"},
		},
		{
			name:    "windows: suppressors match case-insensitively",
			windows: true,
			in:      []string{"No_Color=1", "force_color=0", "colorterm=", "Path=C:\\w"},
			want:    []string{"Path=C:\\w", "COLORTERM=truecolor"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := terminalColorEnv(tc.in, tc.windows)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("terminalColorEnv(%q, windows=%v)\n got  %q\n want %q", tc.in, tc.windows, got, tc.want)
			}
		})
	}
}

// TestTerminalColorEnvEveryRuleCovered keeps the table and its test in step: a
// new row must be exercised by an input above.
func TestTerminalColorEnvEveryRuleCovered(t *testing.T) {
	for _, r := range terminalColorEnvRules {
		if r.key == "" {
			t.Fatalf("rule with empty key: %+v", r)
		}
		if (r.posix == colorEnvSet || r.windows == colorEnvSet) && r.value == "" {
			t.Fatalf("set rule %s has no value", r.key)
		}
	}
}

// TestLaunchChildEnvAdvertisesColor pins the wiring: whatever the daemon was
// started with, the launched child sees a color-capable terminal, and a
// caller's deliberate ExtraEnv value still wins over the default.
func TestLaunchChildEnvAdvertisesColor(t *testing.T) {
	t.Setenv("TERM", "dumb")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "0")

	out := launchChildEnv(termsvc.LaunchRequest{Tool: "claude-code", RunID: "r"}, "auth")
	last := lastEnv(out)
	if v, ok := last["NO_COLOR"]; ok {
		t.Fatalf("inherited NO_COLOR=%q leaked into the child", v)
	}
	if v, ok := last["FORCE_COLOR"]; ok {
		t.Fatalf("inherited FORCE_COLOR=%q leaked into the child", v)
	}
	if last["COLORTERM"] != "truecolor" {
		t.Fatalf("COLORTERM = %q, want truecolor", last["COLORTERM"])
	}
	if term := last["TERM"]; term == "dumb" {
		t.Fatalf("TERM is still dumb")
	}

	out = launchChildEnv(termsvc.LaunchRequest{
		Tool: "claude-code", RunID: "r", ExtraEnv: []string{"NO_COLOR=1"},
	}, "auth")
	if lastEnv(out)["NO_COLOR"] != "1" {
		t.Fatalf("caller ExtraEnv NO_COLOR=1 must still win: %q", out)
	}
}

// lastEnv maps each key to its LAST value (the one a POSIX child honours).
func lastEnv(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}
