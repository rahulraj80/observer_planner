package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestDocAssistedWaveHeadlessPredicates pins the attach gate's
// incompatible-mode predicate for each 2026-10-03 doc-assisted launcher: a
// one-shot / server / help invocation takes the bare path, an interactive one
// may attach. One case per row (CLAUDE.md #5).
func TestDocAssistedWaveHeadlessPredicates(t *testing.T) {
	poolIncompatible := func(args []string) bool {
		return argsLeadWithSubcommand(args, poolHeadlessSubcommands) ||
			argsContainHeadlessFlag(args, "-h", "--help", "--version")
	}
	crushIncompatible := func(args []string) bool {
		return argsLeadWithSubcommand(args, crushHeadlessSubcommands) ||
			argsContainHeadlessFlag(args, "-h", "--help", "-v", "--version")
	}
	cases := []struct {
		name string
		pred func([]string) bool
		args []string
		want bool
	}{
		{"junie bare TUI", junieHeadless, nil, false},
		{"junie model + project values are not positionals", junieHeadless, []string{"--model", "gpt-5", "-p", "/repo"}, false},
		{"junie --prompt seed stays interactive", junieHeadless, []string{"--prompt", "hello there"}, false},
		{"junie --resume stays interactive", junieHeadless, []string{"--resume", "--session-id", "session-1"}, false},
		{"junie positional task is headless", junieHeadless, []string{"fix the bug"}, true},
		{"junie positional after a value flag is headless", junieHeadless, []string{"--model", "x", "fix it"}, true},
		{"junie --task is headless", junieHeadless, []string{"--task", "fix it"}, true},
		{"junie --task= is headless", junieHeadless, []string{"--task=fix it"}, true},
		{"junie --acp is a server", junieHeadless, []string{"--acp"}, true},
		{"junie --output-format is scripted", junieHeadless, []string{"--output-format", "json"}, true},
		{"aider bare REPL", aiderHeadless, []string{"--model", "gpt-4o"}, false},
		{"aider --message exits", aiderHeadless, []string{"--message", "hi"}, true},
		{"aider -m exits", aiderHeadless, []string{"-m", "hi"}, true},
		{"aider --message-file exits", aiderHeadless, []string{"--message-file=x.txt"}, true},
		{"pool bare TUI", poolIncompatible, nil, false},
		{"pool exec is a one-shot", poolIncompatible, []string{"exec", "-p", "hi"}, true},
		{"pool acp is a server", poolIncompatible, []string{"acp"}, true},
		{"pool login", poolIncompatible, []string{"login"}, true},
		{"pool --resume stays interactive", poolIncompatible, []string{"--resume", "abc"}, false},
		{"crush bare TUI", crushIncompatible, []string{"-c", "/repo"}, false},
		{"crush run is a one-shot", crushIncompatible, []string{"run", "hi"}, true},
		{"crush --version", crushIncompatible, []string{"--version"}, true},
	}
	for _, tc := range cases {
		if got := tc.pred(tc.args); got != tc.want {
			t.Errorf("%s: predicate(%q) = %v, want %v", tc.name, tc.args, got, tc.want)
		}
	}
}

// TestArgsHaveUndeclaredPositional pins the positional scan junieHeadless
// rides on, including the bare `--` and the undeclared-flag safe direction.
func TestArgsHaveUndeclaredPositional(t *testing.T) {
	vf := map[string]bool{"--model": true}
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"--model", "x"}, false},
		{[]string{"--model=x"}, false},
		{[]string{"--flag"}, false},
		{[]string{"task"}, true},
		{[]string{"--flag", "value"}, true}, // undeclared flag: value reads as a positional (safe direction)
		{[]string{"--"}, false},
		{[]string{"--", "task"}, true},
		{[]string{"-"}, true},
	}
	for _, tc := range cases {
		if got := argsHaveUndeclaredPositional(tc.args, vf); got != tc.want {
			t.Errorf("argsHaveUndeclaredPositional(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

// TestDocAssistedContinueNoopWithoutContinueFrom pins that a fresh launch (no
// --continue-from, every New Terminal launch) never touches the handoff
// service and inherits the caller's cwd.
func TestDocAssistedContinueNoopWithoutContinueFrom(t *testing.T) {
	var stderr bytes.Buffer
	dir, err := docAssistedContinue(context.Background(), &stderr, "", "junie", "junie", docAssistedFlags{})
	if err != nil || dir != "" || stderr.Len() != 0 {
		t.Fatalf("docAssistedContinue(no continue-from) = (%q, %v), stderr %q; want (\"\", nil) and silence", dir, err, stderr.String())
	}
}

// TestDocAssistedWaveRegistryShape pins the registry rows the four launchers
// rely on: doc-assisted Launch whose verb is the wired command, an Attach row
// with the same verb, and no native resume (none is live-verified).
func TestDocAssistedWaveRegistryShape(t *testing.T) {
	for tool, verb := range map[string]string{"aider": "aider", "crush": "crush", "junie": "junie", "poolside": "pool"} {
		c, ok := integration.For(tool)
		if !ok || c.Handoff.Launch == nil {
			t.Fatalf("%s: no Launch row", tool)
		}
		if c.Handoff.Launch.Mode != integration.LaunchDocAssisted || c.Handoff.Launch.Subcommand != verb {
			t.Errorf("%s: Launch = %+v, want doc-assisted verb %q", tool, *c.Handoff.Launch, verb)
		}
		if c.Attach == nil || c.Attach.Subcommand != verb {
			t.Errorf("%s: Attach = %+v, want verb %q", tool, c.Attach, verb)
		}
		if c.Resume.Kind == integration.ResumeNative {
			t.Errorf("%s: declares native resume, but no resume argv was live-verified", tool)
		}
		if !integration.TerminalLaunchable(c) {
			t.Errorf("%s: not TerminalLaunchable, so the New Terminal picker would hide it", tool)
		}
	}
}
