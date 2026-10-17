package guard

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/policy"
)

// TestP341FailedRefreshKeepsGoodSnapshot pins review P3-41: an advisory
// (non-admission) lookup that fails after the TTL keeps the session's good
// snapshot (bounded by budgetKeepGoodMax) instead of evicting it, which left
// every captured action of the session unmeasured for a whole TTL. The retry
// clock still moves, so a failing read is not retried on every action. An
// admission still re-reads and fails closed on the failure.
func TestP341FailedRefreshKeepsGoodSnapshot(t *testing.T) {
	t.Parallel()
	clock := newFakeBudgetClock(time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC))
	g := newTestGuard(t, budgetCfg("observe", false), nil)
	g.budgetClock = clock.Now
	var calls atomic.Int64
	ok := atomic.Bool{}
	ok.Store(true)
	g.SetBudgetLookup(func(string) (BudgetSnapshot, bool) {
		calls.Add(1)
		return BudgetSnapshot{SessionUSD: 6}, ok.Load()
	})
	stamp := func(kind policy.EventKind) policy.Event {
		ev := policy.Event{Kind: kind, SessionID: "s1", Now: clock.Now()}
		g.stampBudgetWithFreshness(&ev, false)
		return ev
	}
	if ev := stamp(policy.KindShellExec); ev.SessionCostUSD != 6 {
		t.Fatalf("first stamp = %+v", ev)
	}
	ok.Store(false)
	clock.Advance(budgetCacheTTL + time.Second)
	if ev := stamp(policy.KindShellExec); ev.SessionCostUSD != 6 {
		t.Fatalf("a failed refresh evicted the good snapshot: cost=%v", ev.SessionCostUSD)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("lookups = %d, want 2", got)
	}
	// Within the TTL after the failed refresh nothing is re-read.
	clock.Advance(time.Second)
	_ = stamp(policy.KindShellExec)
	if got := calls.Load(); got != 2 {
		t.Fatalf("a failed refresh is retried on every action: lookups = %d", got)
	}
	// An admission does not take the kept snapshot: it re-reads and fails closed.
	if ev := stamp(policy.KindAPIRequest); !ev.USDUnavailable.Session || calls.Load() != 3 {
		t.Fatalf("an admission used a kept snapshot: %+v lookups=%d", ev.USDUnavailable, calls.Load())
	}
	// Past budgetKeepGoodMax the old number is no longer presented as current.
	ok.Store(true)
	clock.Advance(budgetCacheTTL + time.Second)
	if ev := stamp(policy.KindShellExec); ev.SessionCostUSD != 6 {
		t.Fatalf("recovered read = %v", ev.SessionCostUSD)
	}
	ok.Store(false)
	clock.Advance(5*time.Minute + budgetCacheTTL) // past budgetKeepGoodMax; a literal so the red run compiles on the base
	if ev := stamp(policy.KindShellExec); ev.SessionCostUSD != 0 {
		t.Fatalf("a snapshot older than budgetKeepGoodMax is still stamped: cost=%v", ev.SessionCostUSD)
	}
}

// TestP311SnapshotAgeSharesOneRead pins review P3-11's cached read: a fresh
// caller passing MaxSnapshotAge reuses a good snapshot read within it (the
// hook-path publisher's node-wide and per-tool checks in one tick share one
// read), and zero keeps the strict fresh read.
func TestP311SnapshotAgeSharesOneRead(t *testing.T) {
	t.Parallel()
	clock := newFakeBudgetClock(time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC))
	cfg := guardCfg()
	cfg.Mode = "enforce"
	g := newTestGuard(t, cfg, nil)
	g.budgetClock = clock.Now
	if err := g.ApplyOrgBudget(config.GuardBudgetConfig{DailyUSD: 0.35, Hard: true}, nil, false,
		policy.BudgetProtection{DailyUSD: true}, ""); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	lookupOK := atomic.Bool{}
	lookupOK.Store(true)
	g.SetBudgetLookup(func(string) (BudgetSnapshot, bool) {
		calls.Add(1)
		return BudgetSnapshot{DailyUSD: 0.1}, lookupOK.Load()
	})
	in := InterventionBudgetInput{SourceReady: true, Now: clock.Now(), MaxSnapshotAge: 15 * time.Second}
	for i := 0; i < 4; i++ {
		if d := g.CheckInterventionBudget(in); d.Deny || d.AccountingUnavailable {
			t.Fatalf("check %d = %+v", i, d)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("four checks within the snapshot age = %d reads, want 1", got)
	}
	clock.Advance(16 * time.Second)
	_ = g.CheckInterventionBudget(in)
	if got := calls.Load(); got != 2 {
		t.Fatalf("a check past the snapshot age = %d reads, want 2", got)
	}
	in.MaxSnapshotAge = 0
	_ = g.CheckInterventionBudget(in)
	if got := calls.Load(); got != 3 {
		t.Fatalf("a strict fresh check reused a snapshot: %d reads", got)
	}
	// A read that fails is flagged as the transient condition, and a
	// structural one is not.
	lookupOK.Store(false)
	d := g.CheckInterventionBudget(in)
	if !d.Deny || !d.AccountingUnavailable {
		t.Fatalf("a failed read = %+v, want a deny flagged AccountingUnavailable", d)
	}
	in.SourceReady = false
	if d := g.CheckInterventionBudget(in); !d.Deny || d.AccountingUnavailable {
		t.Fatalf("a broken capture source = %+v, want a deny NOT flagged transient", d)
	}
}
