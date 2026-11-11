package termsvc

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/termrun"
)

// TestEndRunRecordsWhyTheRunEnded pins N3 (Windows 11 Pro run 4) for the
// daemon's runs: each way a run ends records its own reason with the exit
// code, the dashboard can read it back for the exit line, and the daemon
// logs one line at every end.
func TestEndRunRecordsWhyTheRunEnded(t *testing.T) {
	cases := []struct {
		name   string
		mark   string // reason marked before the end ("" for none)
		code   int
		reason string
	}{
		{"the tool exited", "", 7, termrun.EndChildExit},
		{"stopped from the dashboard", termrun.EndDashboardStop, 1, termrun.EndDashboardStop},
		{"the console window was closed", "", 3221225786, termrun.EndConsoleClosed},
		{"stopped from outside", "", 4294967295, termrun.EndStoppedOutside},
		{"the daemon stopped", termrun.EndDaemonShutdown, -1, termrun.EndDaemonShutdown},
		{"an org floor ended it", termrun.EndOrgFloorHooksDrifted, 1, termrun.EndOrgFloorHooksDrifted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := newFakeRecorder()
			var logs bytes.Buffer
			svc := New(Options{
				Policy: Policy{AllowFresh: true, AllowedTools: []string{"claude-code"}}, Recorder: rec,
				Launcher: &fakeLauncher{handle: "H"}, Logger: slog.New(slog.NewTextHandler(&logs, nil)),
			})
			res, err := svc.LaunchFresh(context.Background(), FreshRequest{Tool: "claude-code", Subcommand: "claude"})
			if err != nil {
				t.Fatal(err)
			}
			if c.mark != "" && !svc.MarkEnding("H", c.mark) {
				t.Fatal("MarkEnding on a live run")
			}
			svc.EndRunByHandle(context.Background(), "H", c.code)
			if rec.endReasons[res.RunID] != c.reason || rec.ended[res.RunID] != c.code {
				t.Fatalf("recorded %q / %d, want %q / %d", rec.endReasons[res.RunID], rec.ended[res.RunID], c.reason, c.code)
			}
			if got := svc.EndReasonForHandle("H"); got != c.reason {
				t.Fatalf("EndReasonForHandle = %q, want %q", got, c.reason)
			}
			line := logs.String()
			if strings.Count(line, "terminal: run ended") != 1 || !strings.Contains(line, "end_reason="+c.reason) || !strings.Contains(line, "run="+res.RunID) {
				t.Fatalf("daemon log %q", line)
			}
		})
	}
}

// TestMarkEndingKeepsTheFirstReason: a second mark, or one on an ended or
// unknown handle, changes nothing; MarkAllEnding marks only the unmarked.
func TestMarkEndingKeepsTheFirstReason(t *testing.T) {
	rec := newFakeRecorder()
	l := &seqLauncher{}
	svc := newService(t, Policy{AllowFresh: true, AllowedTools: []string{"claude-code"}}, rec, l, nil)
	a, _ := svc.LaunchFresh(context.Background(), FreshRequest{Tool: "claude-code", Subcommand: "claude"})
	b, _ := svc.LaunchFresh(context.Background(), FreshRequest{Tool: "claude-code", Subcommand: "claude"})
	if !svc.MarkEnding(a.Handle, termrun.EndDashboardStop) || svc.MarkEnding(a.Handle, termrun.EndConsoleClosed) {
		t.Fatal("the first mark wins")
	}
	if svc.MarkEnding("nope", termrun.EndDashboardStop) || svc.MarkEnding(b.Handle, "") {
		t.Fatal("unknown handle or empty reason")
	}
	if n := svc.MarkAllEnding(termrun.EndDaemonShutdown); n != 1 {
		t.Fatalf("MarkAllEnding marked %d, want 1", n)
	}
	svc.EndRunByHandle(context.Background(), a.Handle, 1)
	svc.EndRunByHandle(context.Background(), b.Handle, -1)
	if rec.endReasons[a.RunID] != termrun.EndDashboardStop || rec.endReasons[b.RunID] != termrun.EndDaemonShutdown {
		t.Fatalf("reasons %v", rec.endReasons)
	}
	if svc.MarkEnding(a.Handle, termrun.EndConsoleClosed) {
		t.Fatal("an ended run takes no mark")
	}
}

// TestExitReasonNamesAnUnmarkedEnd (live run 2026-11-11): a run nobody
// marked takes the reason its exit hook reads from what the run left (a
// hosted launch refused before its agent started), detail included; a
// marked reason still wins, and an empty answer defers to the exit code.
func TestExitReasonNamesAnUnmarkedEnd(t *testing.T) {
	refused := termrun.WithEndDetail(termrun.EndLaunchRefused, "claude-code is not installed in the box")
	for _, c := range []struct {
		name, mark, hook, want string
	}{
		{"the hook names it", "", refused, refused},
		{"a mark wins", termrun.EndCLIStop, refused, termrun.EndCLIStop},
		{"an empty answer defers to the code", "", "", termrun.EndChildExit},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := newFakeRecorder()
			calls := 0
			svc := New(Options{
				Policy: Policy{AllowFresh: true, AllowedTools: []string{"claude-code"}}, Recorder: rec,
				Launcher: &fakeLauncher{handle: "H"},
				ExitReason: func(runID string, code int) string {
					calls++
					if code != 125 || runID == "" {
						t.Errorf("hook got %q / %d", runID, code)
					}
					return c.hook
				},
			})
			res, err := svc.LaunchFresh(context.Background(), FreshRequest{Tool: "claude-code", Subcommand: "claude"})
			if err != nil {
				t.Fatal(err)
			}
			if c.mark != "" {
				svc.MarkEnding("H", c.mark)
			}
			svc.EndRunByHandle(context.Background(), "H", 125)
			svc.EndRunByHandle(context.Background(), "H", 125) // idempotent: no second read
			if rec.endReasons[res.RunID] != c.want || svc.EndReasonForHandle("H") != c.want {
				t.Fatalf("recorded %q, handle %q, want %q", rec.endReasons[res.RunID], svc.EndReasonForHandle("H"), c.want)
			}
			if wantCalls := map[bool]int{true: 0, false: 1}[c.mark != ""]; calls != wantCalls {
				t.Errorf("hook called %d times, want %d", calls, wantCalls)
			}
		})
	}
}
