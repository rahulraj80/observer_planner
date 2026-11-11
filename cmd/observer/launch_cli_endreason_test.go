//go:build !sbo_omit_terminals && !windows

package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/store"
	"github.com/marmutapp/superbased-observer/internal/termrun"
)

// TestLaunchCLIPrintsWhyALaunchWasRefused (live run 2026-11-11): a hosted
// launch refused before its agent started showed only exit code 125; the
// run's end reason now carries the refusal, and `observer launch` (text
// and --json, without --detach) and `observer terminals show` (a live run
// and a recorded one) print it.
func TestLaunchCLIPrintsWhyALaunchWasRefused(t *testing.T) {
	requireTerminalsActive(t)
	why := "tool_unavailable_in_image: claude-code is not installed in the box, and the box cannot install it: it has no npm (for npm install -g @anthropic-ai/claude-code)"
	reason := termrun.WithEndDetail(termrun.EndLaunchRefused, why)
	code := 125
	f := newFakeTermsvc()
	f.exitCode, f.endReason = &code, reason
	serveFakeDaemon(t, f)

	out, errb, got := runVerb(t, "launch", "claude", "--project", "/r", "--json")
	var run map[string]any
	if got != launchExitRunFailed || json.Unmarshal([]byte(out), &run) != nil || run["end_reason"] != reason || run["exit_code"] != float64(125) {
		t.Fatalf("launch --json: exit %d, stdout %q, stderr %q", got, out, errb)
	}
	out, _, got = runVerb(t, "launch", "claude", "--project", "/r")
	if got != launchExitRunFailed || !strings.Contains(out, "end reason") || !strings.Contains(out, "launch_refused: "+why) {
		t.Fatalf("launch text: exit %d, stdout %q", got, out)
	}
	out, _, got = runVerb(t, "terminals", "show", "run-new-0001", "--json")
	if got != 0 || json.Unmarshal([]byte(out), &run) != nil || run["end_reason"] != reason {
		t.Fatalf("terminals show (live): %d %q", got, out)
	}

	// The recorded run (terminal_run.end_reason), after the live handle is gone.
	f2 := newFakeTermsvc()
	ended := f2.now.Add(time.Second)
	f2.history = []store.TerminalRunSummary{{RunID: "run-old-0002", Tool: "claude-code", Kind: "fresh", ExitCode: &code, EndReason: reason, LaunchedAt: f2.now, EndedAt: &ended}}
	f2.snap = []dashapi.LaunchInfo{}
	serveFakeDaemon(t, f2)
	out, _, got = runVerb(t, "terminals", "show", "run-old-0002")
	if got != 0 || !strings.Contains(out, "launch_refused: "+why) || !strings.Contains(out, "125") {
		t.Fatalf("terminals show (recorded): %d %q", got, out)
	}
}
