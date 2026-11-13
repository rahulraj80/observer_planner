package termrun

import (
	"strconv"
	"strings"
	"unicode"
)

// endreason.go is the vocabulary of why a terminal run ended (Windows 11 Pro
// run 4, N3): terminal_run.end_reason (node migration 072, an existing
// column; no migration) is recorded with the exit code, and every reason has
// one named line the CLI prints and the dashboard shows. The store is the one
// writer of the column and holds the same strings as store.EndReason*
// (pinned by TestEndReasonVocabularyMatchesStore in cmd/observer); this
// package owns the classification and the lines, so the daemon, the CLI and
// their tests share them.
const (
	// EndChildExit: the tool exited on its own (any code).
	EndChildExit = "child_exit"
	// EndConsoleClosed: the console window the run lived in was closed (a
	// CLI launch: the window's X, a log off or a shutdown; or the tool's own
	// STATUS_CONTROL_C_EXIT exit, 0xC000013A).
	EndConsoleClosed = "console_closed"
	// EndDashboardStop: the dashboard's Stop & close ended the run.
	EndDashboardStop = "dashboard_stop"
	// EndCLIStop: `observer terminals stop` ended the run.
	EndCLIStop = "cli_stop"
	// EndLaunchRefused: a sandboxed CLI launch was refused after its row
	// was written and before the tool started.
	EndLaunchRefused = "launch_refused"
	// EndStoppedOutside: something outside Observer stopped the process
	// (exit code 0xFFFFFFFF, -1: Stop-Process, taskkill /F, or a POSIX
	// signal such as kill).
	EndStoppedOutside = "stopped_outside"
	// EndProcessGone: the daemon found a sandboxed CLI launch's helper gone
	// with its row still running; nobody saw the exit, so the code is NULL.
	EndProcessGone = "process_gone"
	// EndDaemonShutdown: the Observer daemon stopped while the run was live.
	EndDaemonShutdown = "daemon_shutdown"
	// EndResumed: a newer run resumed this one.
	EndResumed = "resumed"
	// EndOrgFloorHooksDrifted: the organization's full-capture floor ended
	// the run because its hooks drifted.
	EndOrgFloorHooksDrifted = "org_floor_require_full_capture_hooks_drifted"
)

// endLines are the named lines, one per reason.
var endLines = map[string]string{
	EndChildExit:            "the tool exited",
	EndConsoleClosed:        "the console window was closed",
	EndDashboardStop:        "stopped from the dashboard (Stop & close)",
	EndCLIStop:              "stopped with observer terminals stop",
	EndLaunchRefused:        "the launch was refused before the tool started",
	EndStoppedOutside:       "stopped from outside (for example Stop-Process, taskkill /F or a kill signal)",
	EndProcessGone:          "the run's process is gone (ended outside Observer); its exit code is not known",
	EndDaemonShutdown:       "the Observer daemon stopped",
	EndResumed:              "resumed in a newer run",
	EndOrgFloorHooksDrifted: "ended by the organization's full-capture floor (the run's hooks drifted)",
}

// KnownEndReason reports whether reason is in the vocabulary (a reason
// that carries a detail, WithEndDetail, is judged by its code).
func KnownEndReason(reason string) bool {
	code, _ := SplitEndReason(reason)
	_, ok := endLines[code]
	return ok
}

// endDetailSep joins an end reason's code and its detail.
const endDetailSep = ": "

// EndDetailMax bounds an end reason's detail, in bytes. A node's refusal
// with its fix runs to about 600 bytes (SandHold's card_required, live run
// 2026-11-13, was cut at 400 so `observer terminals show` lost the fix);
// the bound only stops a runaway detail.
const EndDetailMax = 4096

// WithEndDetail is reason with a short detail saying why (live run
// 2026-11-11: a hosted launch that failed before its agent started showed
// only exit code 125, its cause only in the terminal): "launch_refused:
// claude-code is not installed in the box, ...". The detail is made one
// line without control characters and cut at EndDetailMax bytes; the code
// stays first, so every reader that compares codes splits it off
// (SplitEndReason). A reason outside the vocabulary, or an empty detail,
// is returned unchanged.
func WithEndDetail(reason, detail string) string {
	if _, ok := endLines[reason]; !ok {
		return reason
	}
	d := strings.Join(strings.FieldsFunc(detail, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if len(d) > EndDetailMax {
		d = strings.ToValidUTF8(d[:EndDetailMax], "") + "..."
	}
	if d == "" {
		return reason
	}
	return reason + endDetailSep + d
}

// SplitEndReason splits a recorded end reason into its vocabulary code and
// its detail ("" when it carries none). A value whose head is not a code
// is returned whole as the code.
func SplitEndReason(s string) (code, detail string) {
	head, tail, ok := strings.Cut(s, endDetailSep)
	if !ok {
		return s, ""
	}
	if _, known := endLines[head]; !known {
		return s, ""
	}
	return head, tail
}

// EndLine is the named line for a run's end: "the run ended: the console
// window was closed (exit code 3221225786)". A reason's detail follows its
// line. exitCode nil leaves the code out; an unknown reason says only that
// the run ended.
func EndLine(reason string, exitCode *int) string {
	s := "the run ended"
	code, detail := SplitEndReason(reason)
	if l, ok := endLines[code]; ok {
		s += ": " + l
		if detail != "" {
			s += ": " + detail
		}
	}
	if exitCode != nil {
		s += " (exit code " + strconv.Itoa(*exitCode) + ")"
	}
	return s
}

// exitShapes classifies an exit by its code alone, for an end nobody
// announced (no Stop & close, no closed window seen): the first row that
// matches wins.
var exitShapes = []struct {
	match  func(code int) bool
	reason string
}{
	// 0xC000013A STATUS_CONTROL_C_EXIT: the console was closed or Ctrl+C or
	// Ctrl+Break ended it.
	{func(c int) bool { return uint32(c) == 0xC000013A }, EndConsoleClosed}, //nolint:gosec // G115: an exit code's NTSTATUS bits
	// 0xFFFFFFFF (-1): TerminateProcess from outside (Stop-Process,
	// taskkill /F), or a POSIX process ended by a signal.
	{func(c int) bool { return c == -1 || uint32(c) == 0xFFFFFFFF }, EndStoppedOutside}, //nolint:gosec // G115: an exit code's NTSTATUS bits
}

// EndReasonForExit is the reason an exit code alone gives: console_closed,
// stopped_outside, else child_exit.
func EndReasonForExit(code int) string {
	for _, r := range exitShapes {
		if r.match(code) {
			return r.reason
		}
	}
	return EndChildExit
}
