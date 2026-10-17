package diag

import (
	"os"
	"path/filepath"
	"strings"
)

// claudeReplayDoubleRunNote is the shared wording of the double-run finding
// (`observer doctor` here, `observer init` in cmd/observer).
const claudeReplayDoubleRunNote = "cursor-agent also runs the hooks in Claude Code's user settings (its claude-user hooks), so Observer's hooks run twice on each Cursor step. " +
	"Observer recognises the replay, answers it neutrally and records the step once, from Cursor's own hooks; the cost is one extra hook process per step."

// ClaudeReplayDoubleRunNote returns the shared double-run wording.
func ClaudeReplayDoubleRunNote() string { return claudeReplayDoubleRunNote }

// checkCursorClaudeReplay reports the T-034h double run: Observer's hooks
// registered BOTH in ~/.claude/settings.json and in ~/.cursor/hooks.json.
// cursor-agent loads the Claude file as its claude-user hooks and runs every
// one with its own payload (live-grounded on node 1, cursor-agent
// 2026.10.01, 2026-10-03). Nothing is double-counted, because the
// claude-code receiver recognises the Cursor payload and only answers it,
// but every Cursor step pays for a second hook process. StatusWarn so the
// developer knows; StatusOK when either side is absent.
//
// Named with the "cursor" substring so `observer doctor cursor` scopes to it.
func checkCursorClaudeReplay(homeDir string) Check {
	const name = "cursor.claude-replay"
	claude := fileContains(filepath.Join(homeDir, ".claude", "settings.json"), " hook claude-code ")
	cursor := fileContains(filepath.Join(homeDir, ".cursor", "hooks.json"), " hook cursor ")
	if !claude || !cursor {
		return Check{Name: name, Status: StatusOK, Message: "Observer's Claude Code hooks are not also replayed by Cursor"}
	}
	return Check{
		Name:    name,
		Status:  StatusWarn,
		Message: "Observer's hooks are registered for both Claude Code and Cursor; cursor-agent runs both sets",
		Details: []string{claudeReplayDoubleRunNote},
	}
}

func fileContains(path, needle string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), needle)
}
