//go:build !sbo_omit_terminals

package main

// run_end_note.go: why a run ended, read from what the run left behind,
// for an end nobody marked (live run 2026-11-11: a hosted launch whose
// in-box tool was missing showed only exit code 125 in `observer launch`
// and `observer terminals show`; the reason was only in the terminal). A
// driver whose helper can fail before the agent starts registers a reader;
// the daemon's terminal service asks them when a run ends with a non-zero
// code (termsvc.Options.ExitReason), and the reason, its short detail
// included (termrun.WithEndDetail), is recorded on terminal_run.end_reason.

// runEndNoteReaders read a run's end note: the end reason, "" for none.
// id is the id the run's helper knows the run by (runEndNoteID). Each
// reader removes the note it read.
var runEndNoteReaders []func(observerDir, id string) string

// runEndNoteID is the id a run's end note is keyed by: the sandbox run's
// own id when the run went through the Sandboxer (the helper is a separate
// process the daemon starts with a plan naming only that id; it never
// learns terminal_run.run_id), else the run id. Live run 2026-11-11: the
// daemon asked by terminal_run.run_id, the hosted helper had written its
// note under the workspace id, and every refusal was recorded as a bare
// exit code.
func runEndNoteID(runID, sandboxRunID string) string {
	if sandboxRunID != "" {
		return sandboxRunID
	}
	return runID
}

// runEndNoteReason is the first reason a reader finds for the run's end,
// "" for a clean exit or when no reader has one.
func runEndNoteReason(observerDir, runID, sandboxRunID string, exitCode int) string {
	if exitCode == 0 || observerDir == "" {
		return ""
	}
	id := runEndNoteID(runID, sandboxRunID)
	for _, read := range runEndNoteReaders {
		if r := read(observerDir, id); r != "" {
			return r
		}
	}
	return ""
}

// runEndNoteHook is termsvc.Options.ExitReason for a daemon whose state
// lives in observerDir (buildTerminalStack).
func runEndNoteHook(observerDir string) func(runID, sandboxRunID string, exitCode int) string {
	return func(runID, sandboxRunID string, exitCode int) string {
		return runEndNoteReason(observerDir, runID, sandboxRunID, exitCode)
	}
}
