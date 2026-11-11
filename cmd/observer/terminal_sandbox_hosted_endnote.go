//go:build !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/marmutapp/superbased-observer/internal/sandboxrelay"
	"github.com/marmutapp/superbased-observer/internal/termrun"
)

// A hosted run's end note (live run 2026-11-11): when the host helper
// refuses the launch before the agent starts (the tool missing from the
// box, a box that cannot install it, a credential gone, a create the node
// refused), it leaves the refusal as a short note under the hosted state
// directory; the daemon reads it when the run ends (run_end_note.go) and
// records launch_refused with the refusal as its detail, which `observer
// launch` and `observer terminals show` print. The note holds Observer's
// own words, the credential redacted (hostedRun.fail), never a box's
// output past one bounded line.

// hostedEndNote is one run's end note.
type hostedEndNote struct {
	V      int    `json:"v"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// hostedEndNotePath is runID's end note.
func hostedEndNotePath(observerDir, runID string) string {
	return filepath.Join(hostedStateDir(observerDir), "ended", runID+".json")
}

// writeHostedEndNote leaves runID's end note (best effort: the run's exit
// code is recorded either way).
func writeHostedEndNote(observerDir, runID, reason, detail string) {
	if observerDir == "" || !sandboxrelay.ValidRunID(runID) || !termrun.KnownEndReason(reason) {
		return
	}
	_ = hostedWriteOwned(hostedEndNotePath(observerDir, runID), hostedEndNote{V: 1, Reason: reason, Detail: detail})
}

// readHostedEndNote reads and removes runID's end note: the reason with its
// detail, "" when there is none or it does not read.
func readHostedEndNote(observerDir, runID string) string {
	if !sandboxrelay.ValidRunID(runID) {
		return ""
	}
	p := hostedEndNotePath(observerDir, runID)
	b, err := hostedReadOwned(p)
	_ = os.Remove(p) //nolint:gosec // G703: the note path built above from a shape-checked run id
	if err != nil {
		return ""
	}
	var n hostedEndNote
	if json.Unmarshal(b, &n) != nil || n.V != 1 || !termrun.KnownEndReason(n.Reason) {
		return ""
	}
	return termrun.WithEndDetail(n.Reason, n.Detail)
}

func init() { runEndNoteReaders = append(runEndNoteReaders, readHostedEndNote) }
