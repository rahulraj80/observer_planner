package store

import (
	"context"
	"testing"
	"time"
)

// TestSetTerminalRunNodeIsolation (demo round 2026-11-12): a SandHold run
// is recorded with the row's floor (process) and the launch's tier; once
// the node answers, the stored class and tier are the box the node made,
// the digest is resealed over them, the org activity probe moves so the
// wire row re-ships, and a local run is never rewritten by a node's answer.
func TestSetTerminalRunNodeIsolation(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	hosted := TerminalRunIsolation{Class: "process", Stack: []string{"sandhold"}, EgressTier: "internet", CaptureLevel: "full", CaptureOrigin: "remote_sandbox", EvidenceDigest: "d0"}
	local := TerminalRunIsolation{Class: "process", Stack: []string{"bwrap"}, EgressTier: "internet", CaptureLevel: "full", CaptureOrigin: "sandboxed", EvidenceDigest: "l0"}
	for id, iso := range map[string]TerminalRunIsolation{"run-h": hosted, "run-l": local} {
		if err := s.InsertTerminalRun(ctx, TerminalRun{RunID: id, Tool: "claude-code", Kind: "fresh", LaunchedAt: time.Now().UTC(), Isolation: iso}); err != nil {
			t.Fatal(err)
		}
	}
	var sealedOver TerminalRunIsolation
	reseal := func(_ string, iso TerminalRunIsolation) string { sealedOver = iso; return "d1" }
	read := func(id string) (class, tier, digest string) {
		t.Helper()
		if err := s.db.QueryRowContext(ctx, `SELECT isolation_class, egress_tier, evidence_digest FROM terminal_run WHERE run_id = ?`, id).Scan(&class, &tier, &digest); err != nil {
			t.Fatal(err)
		}
		return
	}
	before, err := s.probeTerminalActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}

	changed, err := s.SetTerminalRunNodeIsolation(ctx, "run-h", "sandhold", "container", "proxy_only", reseal)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v, want the hosted row recorded", changed, err)
	}
	if c, tr, d := read("run-h"); c != "container" || tr != "proxy_only" || d != "d1" {
		t.Fatalf("hosted row = %s / %s / %s, want container / proxy_only / d1", c, tr, d)
	}
	if sealedOver.Class != "container" || sealedOver.EgressTier != "proxy_only" || len(sealedOver.Stack) != 1 || sealedOver.Stack[0] != "sandhold" || sealedOver.CaptureLevel != "full" {
		t.Fatalf("the digest was resealed over %+v, want the node's class and tier with the row's other facts", sealedOver)
	}
	after, err := s.probeTerminalActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatalf("the node's class did not move the org activity probe: %q", after)
	}

	// The same answer again changes nothing; an answer with no network
	// keeps the recorded tier.
	if changed, err := s.SetTerminalRunNodeIsolation(ctx, "run-h", "sandhold", "container", "proxy_only", reseal); err != nil || changed {
		t.Fatalf("a repeated answer: changed=%v err=%v, want no change", changed, err)
	}
	if changed, err := s.SetTerminalRunNodeIsolation(ctx, "run-h", "sandhold", "hw_vm", "", reseal); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if c, tr, _ := read("run-h"); c != "hw_vm" || tr != "proxy_only" {
		t.Fatalf("hosted row = %s / %s, want hw_vm / proxy_only", c, tr)
	}

	// A local run and a missing run are untouched.
	for _, tc := range []struct{ id, backend string }{{"run-l", "sandhold"}, {"run-x", "sandhold"}} {
		if changed, err := s.SetTerminalRunNodeIsolation(ctx, tc.id, tc.backend, "hw_vm", "none", reseal); err != nil || changed {
			t.Fatalf("%s as %s: changed=%v err=%v, want untouched", tc.id, tc.backend, changed, err)
		}
	}
	if c, tr, d := read("run-l"); c != "process" || tr != "internet" || d != "l0" {
		t.Fatalf("local row = %s / %s / %s, want it as launched", c, tr, d)
	}
}
