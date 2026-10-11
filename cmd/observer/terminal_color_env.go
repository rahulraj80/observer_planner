package main

import "strings"

// terminal_color_env.go makes every daemon-spawned terminal child see the
// terminal it is ACTUALLY attached to: the dashboard's xterm.js emulator, which
// renders 16-color, 256-color and 24-bit truecolor SGR.
//
// Before this, the child inherited the daemon's own environment verbatim, and
// that environment describes whatever process STARTED the daemon, not the
// emulator. A daemon started from an AI agent's shell tool, a supervisor unit,
// or a CI-style runner carries TERM=dumb / no TERM, NO_COLOR=1 or FORCE_COLOR=0
// (agent harnesses set these so THEIR captured output is plain text). Every
// program that auto-detects color (Claude Code, codex, git, ls, grep, any
// chalk / supports-color / Node getColorDepth consumer) then printed
// monochrome in the embedded terminal, in BOTH the light and the dark terminal
// theme, because no palette can color output that was never emitted.
//
// The rules below are the facts about the emulator, so the daemon owns them:
//   - TERM / COLORTERM name the emulator's capabilities (xterm-compatible,
//     truecolor). They replace the inherited values on a POSIX PTY.
//   - NO_COLOR / FORCE_COLOR inherited from the daemon's launcher are dropped:
//     they described the launcher's captured pipe. A developer who wants
//     NO_COLOR in embedded terminals sets it in their shell rc (shell launches
//     source it) or the tool's own config; a caller-supplied ExtraEnv entry is
//     layered AFTER this and still wins.
//
// Windows ConPTY has no terminfo and translates VT itself, so there TERM is
// never invented; only an inherited TERM=dumb (which would make a POSIX-style
// tool such as git-for-windows refuse color) is removed. That is a PLATFORM
// CAPABILITY input (how the PTY layer works), not a tool or caller identity.
//
// Pure: no os.Environ, no runtime.GOOS here; the callers pass both in.

// colorEnvAction is what a rule does to one environment key.
type colorEnvAction int

const (
	// colorEnvSet drops every inherited entry for the key and sets value.
	colorEnvSet colorEnvAction = iota
	// colorEnvDrop drops every inherited entry for the key.
	colorEnvDrop
	// colorEnvDropIfDumb drops inherited entries whose value is "dumb".
	colorEnvDropIfDumb
)

// colorEnvRule is one row of the terminal color environment table.
type colorEnvRule struct {
	key     string
	value   string
	posix   colorEnvAction
	windows colorEnvAction
}

// terminalColorEnvRules is walked top-down; one test case per row.
var terminalColorEnvRules = []colorEnvRule{
	{key: "TERM", value: "xterm-256color", posix: colorEnvSet, windows: colorEnvDropIfDumb},
	{key: "COLORTERM", value: "truecolor", posix: colorEnvSet, windows: colorEnvSet},
	{key: "NO_COLOR", posix: colorEnvDrop, windows: colorEnvDrop},
	{key: "FORCE_COLOR", posix: colorEnvDrop, windows: colorEnvDrop},
}

// terminalColorEnv rewrites an INHERITED environment so the child advertises
// the embedded emulator's color capabilities (see the file comment). Order of
// untouched entries is preserved; set values are appended at the end, so a
// later layer (ExtraEnv) can still override them. windows selects the ConPTY
// column of the table and case-insensitive key matching.
func terminalColorEnv(env []string, windows bool) []string {
	out := make([]string, 0, len(env)+len(terminalColorEnvRules))
	for _, kv := range env {
		key, val, _ := strings.Cut(kv, "=")
		if rule, ok := colorEnvRuleFor(key, windows); ok {
			switch actionFor(rule, windows) {
			case colorEnvSet, colorEnvDrop:
				continue
			case colorEnvDropIfDumb:
				if val == "dumb" {
					continue
				}
			}
		}
		out = append(out, kv)
	}
	for _, rule := range terminalColorEnvRules {
		if actionFor(rule, windows) == colorEnvSet {
			out = append(out, rule.key+"="+rule.value)
		}
	}
	return out
}

// colorEnvRuleFor finds the rule governing key, matching case-insensitively
// on Windows (environment keys are case-insensitive there).
func colorEnvRuleFor(key string, windows bool) (colorEnvRule, bool) {
	for _, r := range terminalColorEnvRules {
		if key == r.key || (windows && strings.EqualFold(key, r.key)) {
			return r, true
		}
	}
	return colorEnvRule{}, false
}

// actionFor picks the rule's column for the PTY layer in use.
func actionFor(r colorEnvRule, windows bool) colorEnvAction {
	if windows {
		return r.windows
	}
	return r.posix
}
