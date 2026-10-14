package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/guardposture"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestFailOpenRecordedOncePerInvocation (T-049): a gate hook that fails
// open on a config error and then again on a nil guard (the receiver keeps
// going after a fail-open failure) spools ONE fail_open record, the first
// kind; a later watchdog fire after the reply records nothing.
func TestFailOpenRecordedOncePerInvocation(t *testing.T) {
	cp := filepath.Join(t.TempDir(), "config.toml")
	writeFileT(t, cp, "")
	writePosture(t, cp, guardposture.Posture{Enforced: false, Source: guardposture.SourceLocal})
	plan := resolveHookFailurePlan("claude-code", "pre-tool", cp)
	if plan.FailClosed || !plan.Gated {
		t.Fatalf("plan = %+v, want a gated fail-open plan", plan)
	}
	h := newFailureHarness(t, plan)
	if hookFailClosed(guardposture.KindConfig, []byte(`{"session_id":"s9"}`)) {
		t.Fatal("a fail-open plan reported handled")
	}
	if hookFailClosed(guardposture.KindGuardInit, []byte(`{"session_id":"s9"}`)) {
		t.Fatal("a fail-open plan reported handled")
	}
	markHookReplied()
	hookWatchdogFire(time.Millisecond, h.exit, &h.stderr)
	b, err := guardposture.Drain(guardposture.SpoolPath(cp), 0)
	if err != nil {
		t.Fatal(err)
	}
	if b == nil || len(b.Records) != 1 {
		t.Fatalf("spool = %+v, want one record", b)
	}
	if r := b.Records[0]; r.Kind != guardposture.KindConfig || r.Outcome != guardposture.OutcomeFailOpen || r.SessionID != "s9" {
		t.Errorf("record = %+v", r)
	}
}

// TestFailOpenNotRecordedForLifecycle: a lifecycle hook that fails open
// never spools (only wired gate hooks are recorded).
func TestFailOpenNotRecordedForLifecycle(t *testing.T) {
	cp := filepath.Join(t.TempDir(), "config.toml")
	writeFileT(t, cp, "")
	for _, tc := range []struct{ tool, event string }{
		{"claude-code", "session-start"},
	} {
		plan := resolveHookFailurePlan(tc.tool, tc.event, cp)
		if plan.Gated {
			t.Fatalf("%s %s: plan is gated", tc.tool, tc.event)
		}
		h := newFailureHarness(t, plan)
		hookWatchdogFire(time.Millisecond, h.exit, &h.stderr)
		if b, err := guardposture.Drain(guardposture.SpoolPath(cp), 0); err != nil || b != nil {
			t.Fatalf("%s %s spooled %+v (%v)", tc.tool, tc.event, b, err)
		}
	}
}

// TestDrainPersistsFailOpenAsFlag: a fail_open record lands as a
// non-enforced guard_error FLAG, a fail_closed (and a legacy, outcome-less)
// record as an enforced deny.
func TestDrainPersistsFailOpenAsFlag(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(dir, "o.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	st := store.New(database)
	spool := filepath.Join(dir, guardposture.SpoolFilename)
	for _, r := range []guardposture.Record{
		{Tool: "cursor", Event: "beforeShellExecution", Cap: "tool_gate", Kind: guardposture.KindTimeout, Outcome: guardposture.OutcomeFailOpen},
		{Tool: "claude-code", Event: "pre-tool", Cap: "tool_gate", Kind: guardposture.KindCrash, Outcome: guardposture.OutcomeFailClosed},
		{Tool: "qoder", Event: "UserPromptSubmit", Cap: "prompt_gate", Kind: guardposture.KindConfig},
	} {
		if err := guardposture.Append(spool, r); err != nil {
			t.Fatal(err)
		}
	}
	if n := drainHookFailures(ctx, spool, st, newLogger("error")); n != 3 {
		t.Fatalf("drained %d, want 3", n)
	}
	rows, err := st.GuardEventsAfter(ctx, 0, 10)
	if err != nil || len(rows) != 3 {
		t.Fatalf("guard_events = %d (%v)", len(rows), err)
	}
	want := []struct {
		decision string
		enforced bool
	}{{"flag", false}, {"deny", true}, {"deny", true}}
	for i, r := range rows {
		if r.RuleID != guard.GuardErrorRuleID || r.Decision != want[i].decision || r.Enforced != want[i].enforced {
			t.Errorf("row %d = rule %s decision %s enforced %v, want %s/%v", i, r.RuleID, r.Decision, r.Enforced, want[i].decision, want[i].enforced)
		}
	}
}
