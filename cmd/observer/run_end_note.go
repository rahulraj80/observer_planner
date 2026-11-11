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
// Each reader removes the note it read.
var runEndNoteReaders []func(observerDir, runID string) string

// runEndNoteReason is the first reason a reader finds for runID's end, ""
// for a clean exit or when no reader has one.
func runEndNoteReason(observerDir, runID string, exitCode int) string {
	if exitCode == 0 || observerDir == "" {
		return ""
	}
	for _, read := range runEndNoteReaders {
		if r := read(observerDir, runID); r != "" {
			return r
		}
	}
	return ""
}
