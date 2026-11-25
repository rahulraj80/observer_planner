//go:build !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import "testing"

// TestHostedEndNoteRoundTrip: the helper's refusal note, keyed by the
// sandbox run's own id (the only id the helper knows), reads back once as
// launch_refused with the refusal as its detail when the daemon asks with
// both ids; asked by terminal_run.run_id alone (the live gap, 2026-11-11)
// it is not found; a bad run id or a clean exit reads nothing.
func TestHostedEndNoteRoundTrip(t *testing.T) {
	obs := t.TempDir()
	writeHostedEndNote(obs, "ws-n1", "launch_refused", "tool_unavailable_in_image: claude-code is not installed in the box")
	if got := runEndNoteReason(obs, "run-n1", "ws-n1", 0); got != "" {
		t.Errorf("a clean exit read %q", got)
	}
	if got := runEndNoteReason(obs, "run-n1", "", 125); got != "" {
		t.Errorf("an unsandboxed run read the sandbox run's note: %q", got)
	}
	if got := runEndNoteReason(obs, "run-n1", "ws-n1", 125); got != "launch_refused: tool_unavailable_in_image: claude-code is not installed in the box" {
		t.Errorf("note = %q", got)
	}
	if got := runEndNoteReason(obs, "run-n1", "ws-n1", 125); got != "" {
		t.Errorf("read twice: %q", got)
	}
	writeHostedEndNote(obs, "../x", "launch_refused", "x")
	if got := readHostedEndNote(obs, "../x"); got != "" {
		t.Errorf("a bad run id read %q", got)
	}
}
