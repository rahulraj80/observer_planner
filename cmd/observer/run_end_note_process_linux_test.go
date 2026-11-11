//go:build linux && !sbo_omit_terminals_sandbox_e2b && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/sandbox"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	"github.com/marmutapp/superbased-observer/internal/store"
	"github.com/marmutapp/superbased-observer/internal/termrun"
	"github.com/marmutapp/superbased-observer/internal/termsvc"
)

// endNoteSandboxer stands in for the hosted driver's Create+Attach: it
// mints the sandbox run's own id the way Create does (newWorkspaceID),
// writes the helper's plan under that id, and returns the helper argv the
// driver returns (this test binary as the observer binary).
type endNoteSandboxer struct {
	obs, root, exe string
	wsID           string
}

func (s *endNoteSandboxer) Prepare(_ context.Context, req termsvc.PrepareRequest) (termsvc.PrepareResult, error) {
	id, err := newWorkspaceID()
	if err != nil {
		return termsvc.PrepareResult{}, err
	}
	s.wsID = id
	v, _ := sandboxremote.VendorFor(sandboxremote.VendorE2B)
	p, err := writeHostedPlan(s.obs, hostedPlanFile{
		Version: hostedPlanVersion, RunID: id, Provider: v.ID, Endpoint: v.DefaultEndpoint, Region: v.Regions.Default,
		Tier: "internet", Custody: "in_box_api_key", TimeoutMinutes: 30, Root: s.root, ObserverDir: s.obs, Tool: req.Tool,
	})
	if err != nil {
		return termsvc.PrepareResult{}, err
	}
	return termsvc.PrepareResult{
		RunID: id, Dir: s.root,
		WrapArgv:  []string{s.exe, hostedHostVerb, "--plan", p, "--", "observer", sandbox.HostVerb, "--", hostedInnerMarker},
		Isolation: termrun.Isolation{Class: "remote"},
	}, nil
}

// endNoteLauncher spawns the PTY child as a real process (no PTY) and
// reports its exit through EndRunByHandle, as the daemon's Manager does.
type endNoteLauncher struct {
	env    []string
	svc    *termsvc.Service
	ready  chan struct{}
	ended  chan struct{}
	mu     sync.Mutex
	stderr strings.Builder
}

func (l *endNoteLauncher) Spawn(lr termsvc.LaunchRequest) (string, error) {
	argv := append(append([]string(nil), lr.WrapArgv...), "observer", "claude")
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // the test binary, argv built above
	cmd.Env, cmd.Dir = l.env, lr.Dir
	var errb strings.Builder
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go func() {
		err := cmd.Wait()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		l.mu.Lock()
		l.stderr.WriteString(errb.String())
		l.mu.Unlock()
		<-l.ready
		l.svc.EndRunByHandle(context.Background(), "H1", code)
		close(l.ended)
	}()
	return "H1", nil
}

// TestHostedRefusalReachesTheRunRecordAcrossTheHelperProcess (live run
// 2026-11-11 on sb, all four isolations: a refused hosted launch was
// recorded as a bare exit 125): the daemon's terminal service launches a
// sandboxed run whose hosted helper is a separate process (this binary as
// `observer sandbox-hosted-host`, reading a real plan file); the helper
// refuses before any box (no E2B credential in its environment) and
// leaves its end note under the sandbox run's own id; the daemon's real
// exit hook (runEndNoteHook, as buildTerminalStack wires it) finds it and
// terminal_run.end_reason records launch_refused with the refusal.
func TestHostedRefusalReachesTheRunRecordAcrossTheHelperProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	obs, root, home := t.TempDir(), t.TempDir(), t.TempDir()
	database, err := db.Open(ctx, db.Options{Path: filepath.Join(obs, "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	validated, err := termsvc.ValidateProjectRoot(root, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	sb := &endNoteSandboxer{obs: obs, root: validated, exe: exe}
	// The helper's environment holds no vendor key, so it refuses at the
	// credential, before any box (and never reaches a keychain: no
	// connected marker under obs).
	l := &endNoteLauncher{env: []string{"PATH=/usr/bin:/bin", "HOME=" + home, "DBUS_SESSION_BUS_ADDRESS=disabled:"}, ready: make(chan struct{}), ended: make(chan struct{})}
	svc := termsvc.New(termsvc.Options{
		Policy:     termsvc.Policy{AllowFresh: true, AllowedTools: []string{"claude-code"}, AllowedProjectRoots: []string{root}},
		Recorder:   termRunRecorder{st: store.New(database)},
		Launcher:   l,
		Sandboxer:  sb,
		ExitReason: runEndNoteHook(obs),
	})
	l.svc = svc
	res, err := svc.LaunchFresh(ctx, termsvc.FreshRequest{Tool: "claude-code", Subcommand: "claude", Sandbox: true, ProjectRoot: root, WorkspaceSource: "live"})
	close(l.ready)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-l.ended:
	case <-time.After(60 * time.Second):
		t.Fatal("the helper did not exit")
	}
	if res.RunID == sb.wsID {
		t.Fatalf("the run id and the sandbox run id are the same (%q): the test would not tell the ids apart", res.RunID)
	}
	l.mu.Lock()
	stderr := l.stderr.String()
	l.mu.Unlock()
	var reason string
	var code int
	if err := database.QueryRowContext(ctx, `SELECT end_reason, exit_code FROM terminal_run WHERE run_id = ?`, res.RunID).Scan(&reason, &code); err != nil {
		t.Fatal(err)
	}
	if code != sandboxHelperFailCode || !strings.HasPrefix(reason, termrun.EndLaunchRefused+": ") || !strings.Contains(reason, "credential") {
		t.Fatalf("recorded exit %d, end_reason %q; helper said %q", code, reason, stderr)
	}
	if got := svc.EndReasonForHandle("H1"); got != reason {
		t.Errorf("live end reason %q, recorded %q", got, reason)
	}
	if _, err := os.Lstat(hostedEndNotePath(obs, sb.wsID)); !os.IsNotExist(err) {
		t.Errorf("the end note outlived its read: %v", err)
	}
}
