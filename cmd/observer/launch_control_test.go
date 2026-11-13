//go:build !sbo_omit_terminals

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/agentlaunch"
	"github.com/marmutapp/superbased-observer/internal/attachsock"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/store"
	"github.com/marmutapp/superbased-observer/internal/termcore"
	"github.com/marmutapp/superbased-observer/internal/termrun"
	"github.com/marmutapp/superbased-observer/internal/termsvc"
)

// fakeTermsvc is the launch control's fake terminal service: it records
// each fresh launch and keeps a PTY snapshot.
type fakeTermsvc struct {
	specs    []dashapi.FreshLaunchSpec
	err      error
	gate     bool
	snap     []dashapi.LaunchInfo
	sessions map[string]string
	history  []store.TerminalRunSummary
	stopped  map[string]string
	probe    dashapi.SandboxAvailability
	now      time.Time
	// exitCode, when set, makes a launched run exit at once with it.
	exitCode *int
	// endReason is the reason the terminal service recorded for an ended
	// run ("" defers to the exit code).
	endReason string
}

func newFakeTermsvc() *fakeTermsvc {
	return &fakeTermsvc{sessions: map[string]string{}, stopped: map[string]string{}, now: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
}

func (f *fakeTermsvc) control(withProbe bool) *launchControl {
	c := &launchControl{
		fresh: func(spec dashapi.FreshLaunchSpec) (termsvc.LaunchResult, bool, error) {
			if f.err != nil {
				return termsvc.LaunchResult{}, f.gate, f.err
			}
			f.specs = append(f.specs, spec)
			res := termsvc.LaunchResult{Handle: "h-new", RunID: "run-new-0001"}
			info := dashapi.LaunchInfo{
				ID: res.Handle, RunID: res.RunID, Tool: spec.Tool, Subcommand: spec.Subcommand,
				Kind: string(termrun.KindFresh), CreatedAt: f.now, Sandboxed: spec.Sandbox,
				Initiator: spec.Initiator, ParentSessionID: spec.ParentSessionID,
			}
			if spec.Sandbox {
				info.Isolation = &dashapi.LaunchIsolation{Class: "namespace", Backend: "bwrap", CaptureLevel: "full"}
			}
			if f.exitCode != nil {
				info.Exited, info.ExitCode = true, *f.exitCode
			}
			f.snap = append(f.snap, info)
			return res, false, nil
		},
		snapshot:   func() []dashapi.LaunchInfo { return append([]dashapi.LaunchInfo(nil), f.snap...) },
		sessionFor: func(runID string) (string, bool) { s, ok := f.sessions[runID]; return s, ok },
		endReason:  func(string) string { return f.endReason },
		// A stop ends the process and records its reason, as the terminal
		// service does (StopRun waits for the record to say so).
		stop: func(handle, reason string) {
			f.stopped[handle] = reason
			f.endReason = reason
			for i := range f.snap {
				if f.snap[i].ID == handle {
					f.snap[i].Exited = true
				}
			}
		},
		history: func(context.Context, int) ([]store.TerminalRunSummary, error) {
			return f.history, nil
		},
		now: func() time.Time { return f.now },
	}
	if withProbe {
		c.probe = func(context.Context) dashapi.SandboxAvailability { return f.probe }
	}
	return c
}

func liveProbe() dashapi.SandboxAvailability {
	return dashapi.SandboxAvailability{
		Available: true,
		Backends: []dashapi.SandboxBackendAvail{
			{ID: "bwrap", Maturity: "shipped", Available: true, Verification: "live"},
			{ID: "oci-runc", Maturity: "preview", Available: true, Verification: "doc", Beta: true},
		},
		Sources: []dashapi.SandboxSourceAvail{{ID: "live", Available: true}, {ID: "clone-local", Available: true}},
		Tools:   map[string]dashapi.SandboxToolAvail{"claude-code": {Available: true}},
	}
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ce *attachsock.ControlError
	if !errors.As(err, &ce) {
		t.Fatalf("not a ControlError: %v", err)
	}
	return ce.Code
}

// TestLaunchControlLaunch covers the launch rows: validation, the backend
// choice against the probe, the spec handed to the one launch path, and
// the refusal of a sandbox that cannot be built (no launch happens).
func TestLaunchControlLaunch(t *testing.T) {
	cases := []struct {
		name      string
		spec      attachsock.LaunchSpec
		probe     func(*dashapi.SandboxAvailability)
		noProber  bool
		wantCode  string
		wantSpec  dashapi.FreshLaunchSpec
		wantState string
	}{
		{
			name: "unsandboxed verb", spec: attachsock.LaunchSpec{Tool: "claude", Project: "/home/dev/r"},
			wantSpec: dashapi.FreshLaunchSpec{Tool: "claude-code", Subcommand: "claude", ProjectRoot: "/home/dev/r", WorkspaceSource: "live"},
		},
		{
			name: "auto takes the daemon's pick", spec: attachsock.LaunchSpec{Tool: "claude-code", Project: "/r", Sandbox: "auto", Workspace: "clone-local", Branch: "main"},
			wantSpec: dashapi.FreshLaunchSpec{Tool: "claude-code", Subcommand: "claude", ProjectRoot: "/r", Sandbox: true, WorkspaceSource: "clone-local", WorkspaceBranch: "main"},
		},
		{
			name: "explicit beta row passes the backend", spec: attachsock.LaunchSpec{Tool: "claude", Project: "/r", Sandbox: "oci-runc", Model: "opus"},
			wantSpec: dashapi.FreshLaunchSpec{Tool: "claude-code", Subcommand: "claude", ProjectRoot: "/r", Sandbox: true, WorkspaceSource: "live", Backend: "oci-runc", Model: "opus"},
		},
		{name: "invalid request", spec: attachsock.LaunchSpec{Tool: "claude", Project: "relative"}, wantCode: agentlaunch.CodeInvalid},
		{name: "unknown tool", spec: attachsock.LaunchSpec{Tool: "nope", Project: "/r"}, wantCode: agentlaunch.CodeNotLaunchable},
		{name: "auto on a beta-only host refuses", spec: attachsock.LaunchSpec{Tool: "claude", Project: "/r", Sandbox: "auto"}, probe: func(p *dashapi.SandboxAvailability) {
			p.Backends[0].Available = false
		}, wantCode: agentlaunch.CodeSandboxUnavailable},
		{name: "unavailable named backend refuses", spec: attachsock.LaunchSpec{Tool: "claude", Project: "/r", Sandbox: "gvisor"}, wantCode: agentlaunch.CodeSandboxUnavailable},
		{name: "no sandbox support refuses a sandboxed launch", spec: attachsock.LaunchSpec{Tool: "claude", Project: "/r", Sandbox: "bwrap"}, noProber: true, wantCode: agentlaunch.CodeSandboxUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeTermsvc()
			f.probe = liveProbe()
			if tc.probe != nil {
				tc.probe(&f.probe)
			}
			ri, err := f.control(!tc.noProber).LaunchDetached(context.Background(), tc.spec)
			if tc.wantCode != "" {
				if err == nil {
					t.Fatalf("want refusal %s, got %+v", tc.wantCode, ri)
				}
				if got := codeOf(t, err); got != tc.wantCode {
					t.Fatalf("code = %s, want %s (%v)", got, tc.wantCode, err)
				}
				if len(f.specs) != 0 {
					t.Fatalf("a refused launch reached the terminal service: %+v", f.specs)
				}
				return
			}
			if err != nil {
				t.Fatalf("launch: %v", err)
			}
			if len(f.specs) != 1 || f.specs[0] != tc.wantSpec {
				t.Fatalf("spec = %+v, want %+v", f.specs, tc.wantSpec)
			}
			if ri.RunID != "run-new-0001" || ri.Handle != "h-new" || ri.State != attachsock.StateRunning || ri.Sandboxed != tc.wantSpec.Sandbox {
				t.Fatalf("run = %+v", ri)
			}
			if tc.wantSpec.Sandbox && (ri.Backend != "bwrap" || ri.Capture != "full" || ri.Isolation != "namespace") {
				t.Fatalf("isolation not reported: %+v", ri)
			}
		})
	}
}

// TestFreshErrorCodes pins the refusal table of a failed fresh launch.
func TestFreshErrorCodes(t *testing.T) {
	cases := []struct {
		err  error
		gate bool
		want string
	}{
		{dashapi.ErrLaunchToolNotAllowed, false, agentlaunch.CodeRefused},
		{dashapi.ErrLaunchFreshDisabled, false, agentlaunch.CodeRefused},
		{dashapi.ErrLaunchProjectRootDenied, false, agentlaunch.CodeRefused},
		{dashapi.ErrLaunchSandboxRequired, false, agentlaunch.CodeRefused},
		{termsvc.ErrTerminalNotAllowed, false, agentlaunch.CodeRefused},
		{dashapi.ErrLaunchTooMany, false, agentlaunch.CodeTooMany},
		{termcore.ErrSandboxUnavailable, false, agentlaunch.CodeSandboxUnavailable},
		{termcore.ErrWorkspacePrepFailed, false, agentlaunch.CodeSandboxUnavailable},
		{errors.New("org disallows codex"), true, agentlaunch.CodeRefused},
		{errors.New("pty: fork failed"), false, agentlaunch.CodeFailed},
	}
	for _, tc := range cases {
		if got := freshErrorCode(tc.err, tc.gate); got != tc.want {
			t.Errorf("freshErrorCode(%v, %v) = %s, want %s", tc.err, tc.gate, got, tc.want)
		}
		f := newFakeTermsvc()
		f.err, f.gate = tc.err, tc.gate
		_, err := f.control(true).LaunchDetached(context.Background(), attachsock.LaunchSpec{Tool: "claude", Project: "/r"})
		if got := codeOf(t, err); got != tc.want {
			t.Errorf("relayed code for %v = %s, want %s", tc.err, got, tc.want)
		}
	}
}

// TestLaunchControlRuns: live terminals first, then history this daemon
// holds no PTY for; setup sessions are not runs; an ancient unended row is
// abandoned; the correlated session is reported.
func TestLaunchControlRuns(t *testing.T) {
	f := newFakeTermsvc()
	ended := f.now.Add(-time.Hour)
	code := 0
	f.snap = []dashapi.LaunchInfo{
		{ID: "h-old", RunID: "run-live-old", Tool: "codex", Kind: "fresh", CreatedAt: f.now.Add(-2 * time.Minute)},
		{ID: "h-new", RunID: "run-live-new", Tool: "claude-code", Kind: "attach", CreatedAt: f.now.Add(-time.Minute), Exited: true, ExitCode: 2},
		{ID: "h-setup", RunID: "run-setup", Kind: "fresh", Setup: true},
		{ID: "h-kindless", RunID: "run-x"},
	}
	f.sessions["run-live-old"] = "sess-codex"
	f.history = []store.TerminalRunSummary{
		{RunID: "run-live-old", Tool: "codex", Kind: "fresh", LaunchedAt: f.now.Add(-2 * time.Minute)},
		{RunID: "run-ended", Tool: "claude-code", Kind: "fresh", LaunchedAt: f.now.Add(-2 * time.Hour), EndedAt: &ended, ExitCode: &code, EndReason: "child_exit", BestSessionID: "sess-1"},
		{
			RunID: "run-orphan", Tool: "claude-code", Kind: "attach", LaunchedAt: f.now.Add(-72 * time.Hour),
			IsolationClass: "process", Stack: "bwrap,oci-runc", EgressTier: "internet", CaptureLevel: "full",
		},
	}
	runs, err := f.control(true).Runs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids, states []string
	for _, r := range runs {
		ids = append(ids, r.RunID)
		states = append(states, r.State)
	}
	wantIDs := []string{"run-live-new", "run-live-old", "run-ended", "run-orphan"}
	wantStates := []string{attachsock.StateExited, attachsock.StateRunning, attachsock.StateEnded, attachsock.StateAbandoned}
	if len(ids) != len(wantIDs) {
		t.Fatalf("runs = %v, want %v", ids, wantIDs)
	}
	for i := range wantIDs {
		if ids[i] != wantIDs[i] || states[i] != wantStates[i] {
			t.Fatalf("runs = %v %v, want %v %v", ids, states, wantIDs, wantStates)
		}
	}
	if runs[0].ExitCode == nil || *runs[0].ExitCode != 2 || runs[0].EndReason != termrun.EndReasonForExit(2) {
		t.Fatalf("exited run = %+v", runs[0])
	}
	if runs[1].SessionID != "sess-codex" || runs[2].SessionID != "sess-1" || runs[2].EndedAt == "" {
		t.Fatalf("sessions/ended not reported: %+v %+v", runs[1], runs[2])
	}
	// History rows carry their recorded sandbox facts (L7); a row that
	// recorded none stays unsandboxed.
	if o := runs[3]; !o.Sandboxed || o.Backend != "bwrap" || o.Isolation != "process" || o.Capture != "full" {
		t.Fatalf("history isolation = %+v", o)
	}
	if runs[2].Sandboxed || runs[2].Backend != "" {
		t.Fatalf("unrecorded history isolation = %+v", runs[2])
	}
}

// TestLaunchControlRunRefs: run id, handle, a unique prefix, and the
// misses (too short, ambiguous, unknown).
func TestLaunchControlRunRefs(t *testing.T) {
	f := newFakeTermsvc()
	f.snap = []dashapi.LaunchInfo{
		{ID: "h-a", RunID: "abcdef-1111", Tool: "codex", Kind: "fresh"},
		{ID: "h-b", RunID: "abcdef-2222", Tool: "codex", Kind: "fresh"},
	}
	c := f.control(true)
	for ref, want := range map[string]string{"abcdef-1111": "abcdef-1111", "h-b": "abcdef-2222", "abcdef-2": "abcdef-2222"} {
		ri, err := c.Run(context.Background(), ref)
		if err != nil || ri.RunID != want {
			t.Errorf("Run(%q) = %+v, %v; want %s", ref, ri, err, want)
		}
	}
	for _, ref := range []string{"", "abc", "zzzzzz-0"} {
		if _, err := c.Run(context.Background(), ref); !errors.Is(err, attachsock.ErrRunNotFound) {
			t.Errorf("Run(%q) err = %v, want not found", ref, err)
		}
	}
	// A prefix that matches two runs says so and names them (audit L5).
	_, err := c.Run(context.Background(), "abcdef")
	if codeOf(t, err) != attachsock.CodeAmbiguousRun || !strings.Contains(err.Error(), "abcdef-1111") || !strings.Contains(err.Error(), "abcdef-2222") {
		t.Errorf("ambiguous prefix err = %v", err)
	}
	if launchExitCodes[attachsock.CodeAmbiguousRun] != launchExitUsage {
		t.Error("an ambiguous reference is not a usage error")
	}
}

// TestLaunchControlStop: a live owned terminal is stopped with cli_stop; a
// history-only run is not stoppable here; an exited one is left alone.
func TestLaunchControlStop(t *testing.T) {
	f := newFakeTermsvc()
	f.snap = []dashapi.LaunchInfo{
		{ID: "h-live", RunID: "run-live-01", Tool: "codex", Kind: "fresh"},
		{ID: "h-done", RunID: "run-done-01", Tool: "codex", Kind: "fresh", Exited: true},
	}
	f.history = []store.TerminalRunSummary{{RunID: "run-cli-sandbox", Tool: "codex", Kind: "fresh", LaunchedAt: f.now}}
	c := f.control(true)
	// The process goes a few polls after the stop (a hosted helper deletes
	// its box first): the stop answers with the record as it then reads,
	// the same answer `ls` and `show` give, never an ended it made up.
	polls := 0
	c.stop = func(handle, reason string) {
		f.stopped[handle] = reason
		f.endReason = reason
	}
	c.snapshot = func() []dashapi.LaunchInfo {
		out := append([]dashapi.LaunchInfo(nil), f.snap...)
		if f.stopped["h-live"] != "" {
			if polls++; polls > 3 {
				out[0].Exited = true
			}
		}
		return out
	}
	defer func(w, p time.Duration) { stopSettleWait, stopSettlePoll = w, p }(stopSettleWait, stopSettlePoll)
	stopSettlePoll = time.Millisecond
	ri, err := c.StopRun(context.Background(), "run-live-01")
	if err != nil || ri.State != attachsock.StateExited || ri.EndReason != termrun.EndCLIStop || polls <= 3 {
		t.Fatalf("stop live = %+v, %v after %d polls", ri, err, polls)
	}
	if show, _ := c.Run(context.Background(), "run-live-01"); show.State != ri.State {
		t.Fatalf("stop said %s, show says %s", ri.State, show.State)
	}
	if f.stopped["h-live"] != termrun.EndCLIStop {
		t.Fatalf("stop marked %v", f.stopped)
	}
	if _, err := c.StopRun(context.Background(), "run-cli-sandbox"); codeOf(t, err) != attachsock.CodeNotStoppable {
		t.Fatalf("history-only stop = %v", err)
	}
	ri, err = c.StopRun(context.Background(), "h-done")
	if err != nil || ri.State != attachsock.StateExited || f.stopped["h-done"] != "" {
		t.Fatalf("exited stop = %+v, %v (stopped %v)", ri, err, f.stopped)
	}
	if _, err := c.StopRun(context.Background(), "missing-run"); !errors.Is(err, attachsock.ErrRunNotFound) {
		t.Fatalf("unknown stop = %v", err)
	}
}

// TestLaunchControlStopNeverClaimsEnded: a run whose process outlives the
// stop's wait is reported running, as its record reads.
func TestLaunchControlStopNeverClaimsEnded(t *testing.T) {
	f := newFakeTermsvc()
	f.snap = []dashapi.LaunchInfo{{ID: "h-slow", RunID: "run-slow-01", Tool: "codex", Kind: "fresh"}}
	c := f.control(true)
	c.stop = func(handle, reason string) { f.stopped[handle] = reason } // the process does not go
	defer func(w, p time.Duration) { stopSettleWait, stopSettlePoll = w, p }(stopSettleWait, stopSettlePoll)
	stopSettleWait, stopSettlePoll = 20*time.Millisecond, time.Millisecond
	ri, err := c.StopRun(context.Background(), "run-slow-01")
	if err != nil || ri.State != attachsock.StateRunning || ri.EndReason != "" || f.stopped["h-slow"] != termrun.EndCLIStop {
		t.Fatalf("slow stop = %+v, %v (stopped %v)", ri, err, f.stopped)
	}
}

// TestAttachHostWithoutControl answers unsupported, never panics.
func TestAttachHostWithoutControl(t *testing.T) {
	h := newAttachHost(nil, nil, nil)
	if _, err := h.Runs(context.Background()); codeOf(t, err) != attachsock.CodeUnsupported {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.LaunchDetached(context.Background(), attachsock.LaunchSpec{}); codeOf(t, err) != attachsock.CodeUnsupported {
		t.Fatalf("err = %v", err)
	}
}

// TestClassifyControlErrUnverifiedServer: an endpoint served by another
// user is its own code and exit 3, and says nothing was sent.
func TestClassifyControlErrUnverifiedServer(t *testing.T) {
	err := fmt.Errorf("dial: %w", attachsock.ErrServerUnverified)
	code, msg := classifyControlErr(err)
	if code != codeDaemonUnverified || !strings.Contains(msg, "nothing was sent") || launchExitCodes[code] != launchExitUnavailable {
		t.Fatalf("code %q msg %q", code, msg)
	}
}

// TestLaunchControlRunLiveSkipsHistory: a live terminal named by its run id
// or handle is answered from the snapshot, without a history read (the
// path a wait takes on every poll).
func TestLaunchControlRunLiveSkipsHistory(t *testing.T) {
	f := newFakeTermsvc()
	f.snap = []dashapi.LaunchInfo{{ID: "h-1", RunID: "run-live-01", Tool: "codex", Kind: "fresh"}}
	c := f.control(true)
	reads := 0
	c.history = func(context.Context, int) ([]store.TerminalRunSummary, error) { reads++; return nil, nil }
	for _, ref := range []string{"run-live-01", "h-1"} {
		if _, err := c.Run(context.Background(), ref); err != nil {
			t.Fatal(err)
		}
	}
	if reads != 0 {
		t.Fatalf("history read %d times for a live run", reads)
	}
	if _, err := c.Run(context.Background(), "run-gone-01"); err == nil || reads != 1 {
		t.Fatalf("a run not live reads the history once (reads %d, err %v)", reads, err)
	}
}

// TestLaunchControlModelOffered: a model the dashboard's picker does not
// offer is refused as invalid_request before anything launches; an
// offered one reaches the terminal service.
func TestLaunchControlModelOffered(t *testing.T) {
	f := newFakeTermsvc()
	c := f.control(true)
	c.modelOffered = func(_ context.Context, tool, model string) bool { return tool == "claude-code" && model == "opus" }
	_, err := c.LaunchDetached(context.Background(), attachsock.LaunchSpec{Tool: "claude", Project: "/r", Model: "gpt-nope"})
	if got := codeOf(t, err); got != agentlaunch.CodeInvalid || len(f.specs) != 0 {
		t.Fatalf("unoffered model: code %s, specs %+v", got, f.specs)
	}
	if _, err := c.LaunchDetached(context.Background(), attachsock.LaunchSpec{Tool: "claude", Project: "/r", Model: "opus"}); err != nil || len(f.specs) != 1 || f.specs[0].Model != "opus" {
		t.Fatalf("offered model: %v %+v", err, f.specs)
	}
}

// TestLaunchControlNodeBoxWords (demo screenshots 2026-11-12): a run on a
// row whose node decides the box's isolation reports what the node said it
// made, live and from the history, never the row's recorded floor; while
// the node has not said, the isolation is empty and pending.
func TestLaunchControlNodeBoxWords(t *testing.T) {
	f := newFakeTermsvc()
	f.snap = []dashapi.LaunchInfo{
		{
			ID: "h-sh", RunID: "run-sh-01", Tool: "claude-code", Kind: "fresh", Sandboxed: true,
			Isolation: &dashapi.LaunchIsolation{Class: "process", Backend: "sandhold", EgressTier: "internet", CaptureLevel: "partial"},
		},
		{
			ID: "h-new", RunID: "run-new-01", Tool: "claude-code", Kind: "fresh", Sandboxed: true,
			Isolation: &dashapi.LaunchIsolation{Class: "process", Backend: "sandhold", EgressTier: "internet"},
		},
		{
			ID: "h-bw", RunID: "run-bw-01", Tool: "codex", Kind: "fresh", Sandboxed: true,
			Isolation: &dashapi.LaunchIsolation{Class: "process", Backend: "bwrap"},
		},
	}
	ended := f.now
	f.history = []store.TerminalRunSummary{{RunID: "run-old-01", Tool: "claude-code", Kind: "fresh", LaunchedAt: f.now, EndedAt: &ended, IsolationClass: "process", Stack: "sandhold"}}
	c := f.control(true)
	c.boxWords = func(runID, backend string, running bool) (dashapi.BoxWords, bool) {
		if backend != "sandhold" {
			return dashapi.BoxWords{}, false
		}
		switch runID {
		case "run-sh-01", "run-old-01":
			return dashapi.BoxWords{Isolation: "shielded", Network: "limited", RunsOn: "runc"}, true
		}
		return dashapi.BoxWords{Pending: running}, true
	}
	runs, err := c.Runs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]attachsock.RunInfo{}
	for _, r := range runs {
		by[r.RunID] = r
	}
	for _, id := range []string{"run-sh-01", "run-old-01"} {
		if r := by[id]; r.Isolation != "shielded" || r.Network != "limited" || r.RunsOn != "runc" || r.IsolationPending {
			t.Errorf("%s = %+v", id, r)
		}
	}
	if r := by["run-new-01"]; r.Isolation != "" || !r.IsolationPending {
		t.Errorf("not answered yet = %+v", r)
	}
	if r := by["run-bw-01"]; r.Isolation != "process" || r.Network != "" || r.IsolationPending {
		t.Errorf("a local row = %+v", r)
	}
	if got := describeIsolation(by["run-sh-01"]); got != "Shielded" {
		t.Errorf("isolation line %q", got)
	}
	if got := describeNetwork(by["run-sh-01"].Network); got != "allow list (limited)" {
		t.Errorf("network line %q", got)
	}
	if got := describeIsolation(by["run-new-01"]); !strings.Contains(got, "not reported yet") {
		t.Errorf("pending line %q", got)
	}
}
