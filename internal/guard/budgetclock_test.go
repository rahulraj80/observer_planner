package guard

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/policy"
)

// fakeBudgetClock is a settable clock for the budget cache.
type fakeBudgetClock struct{ now atomic.Int64 }

func newFakeBudgetClock(t time.Time) *fakeBudgetClock {
	c := &fakeBudgetClock{}
	c.now.Store(t.UnixNano())
	return c
}

func (c *fakeBudgetClock) Now() time.Time          { return time.Unix(0, c.now.Load()).UTC() }
func (c *fakeBudgetClock) Advance(d time.Duration) { c.now.Add(int64(d)) }

// backlogInputs is a replayed transcript: n actions of one session whose
// own timestamps are ten minutes apart (a backlog the watcher fell behind
// on), all evaluated in one batch moments after the fact.
func backlogInputs(session string, n int, start time.Time) []ActionInput {
	in := make([]ActionInput, 0, n)
	for i := 0; i < n; i++ {
		in = append(in, budgetIn(session, fmt.Sprintf("echo %d", i), start.Add(time.Duration(i)*10*time.Minute)))
	}
	return in
}

// TestBudgetCacheAgesOnGuardClockNotEventTime pins T-413: the spend read is
// relative to wall-clock now, so a batch of historical actions is one cache
// window, not one lookup per action. Before the fix every action of a
// replayed backlog more than 30 s (of transcript time) after the previous
// one paid a full node-wide spend read.
func TestBudgetCacheAgesOnGuardClockNotEventTime(t *testing.T) {
	t.Parallel()
	clock := newFakeBudgetClock(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
	g := newTestGuard(t, budgetCfg("observe", false), nil)
	g.budgetClock = clock.Now
	var calls atomic.Int64
	g.SetBudgetLookup(func(string) (BudgetSnapshot, bool) { calls.Add(1); return BudgetSnapshot{SessionUSD: 1}, true })

	// Transcript time three days ago, stepping forward ten minutes a row.
	g.EvaluateActions(backlogInputs("s1", 50, clock.Now().Add(-72*time.Hour)))
	if got := calls.Load(); got != 1 {
		t.Fatalf("lookups for a 50-action backlog batch = %d, want 1", got)
	}
	// Event timestamps running BACKWARDS (a second file of the same session
	// replayed after the first) are not a reason to re-read either.
	g.EvaluateActions(backlogInputs("s1", 10, clock.Now().Add(-96*time.Hour)))
	if got := calls.Load(); got != 1 {
		t.Fatalf("lookups after an older batch = %d, want 1", got)
	}
	// The guard's own clock moving past the TTL refreshes.
	clock.Advance(budgetCacheTTL + time.Second)
	g.EvaluateActions(backlogInputs("s1", 5, clock.Now().Add(-72*time.Hour)))
	if got := calls.Load(); got != 2 {
		t.Fatalf("lookups after the guard clock passed the TTL = %d, want 2", got)
	}
	// A guard clock that steps backwards (NTP correction) refreshes too.
	clock.Advance(-time.Hour)
	g.EvaluateActions(backlogInputs("s1", 1, clock.Now()))
	if got := calls.Load(); got != 3 {
		t.Fatalf("lookups after the guard clock stepped back = %d, want 3", got)
	}
}

// TestBudgetFailedLookupCachedForTTL pins the negative half of T-413: a
// lookup that fails (its caller's timeout, a locked database) is stamped as
// unavailable for the TTL instead of being retried on every action of the
// batch, and the next window retries it.
func TestBudgetFailedLookupCachedForTTL(t *testing.T) {
	t.Parallel()
	clock := newFakeBudgetClock(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
	g := newTestGuard(t, budgetCfg("observe", false), nil)
	g.budgetClock = clock.Now
	var calls atomic.Int64
	ok := atomic.Bool{}
	g.SetBudgetLookup(func(string) (BudgetSnapshot, bool) {
		calls.Add(1)
		return BudgetSnapshot{SessionUSD: 6}, ok.Load()
	})

	if out := g.EvaluateActions(backlogInputs("s1", 20, clock.Now())); len(out) != 0 {
		t.Fatalf("a failed lookup produced verdicts: %+v", out)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("lookups for a batch whose first lookup failed = %d, want 1", got)
	}
	// The failure is not sticky past the TTL: the read recovers and the
	// breach is seen.
	ok.Store(true)
	clock.Advance(budgetCacheTTL + time.Second)
	out := g.EvaluateActions(backlogInputs("s1", 1, clock.Now()))
	if got := calls.Load(); got != 2 {
		t.Fatalf("lookups after the TTL = %d, want 2", got)
	}
	if len(out) != 1 || out[0].Verdict.RuleID != "B-601" {
		t.Fatalf("recovered lookup did not flag the breach: %+v", out)
	}
}

// TestBudgetFreshFailureKeepsCachedAnswer pins that a fresh caller's failed
// read (hard admission, intervention) never replaces a cached answer that
// the advisory lanes are still entitled to.
func TestBudgetFreshFailureKeepsCachedAnswer(t *testing.T) {
	t.Parallel()
	clock := newFakeBudgetClock(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
	g := newTestGuard(t, budgetCfg("observe", false), nil)
	g.budgetClock = clock.Now
	ok := atomic.Bool{}
	ok.Store(true)
	var calls atomic.Int64
	g.SetBudgetLookup(func(string) (BudgetSnapshot, bool) {
		calls.Add(1)
		return BudgetSnapshot{SessionUSD: 6}, ok.Load()
	})
	if out := g.EvaluateActions(backlogInputs("s1", 1, clock.Now())); len(out) != 1 {
		t.Fatalf("first breach = %+v", out)
	}
	ok.Store(false)
	// A fresh stamp on s1 fails and must not poison the advisory entry.
	ev := policy.Event{Kind: policy.KindAPIRequest, SessionID: "s1", Now: clock.Now()}
	g.stampBudgetWithFreshness(&ev, true)
	if !ev.USDUnavailable.Session {
		t.Fatalf("a failed fresh read was not stamped unavailable: %+v", ev.USDUnavailable)
	}
	g.budgetMu.Lock()
	e := g.budgetCache["s1"]
	g.budgetMu.Unlock()
	if !e.ok || e.snap.SessionUSD != 6 {
		t.Fatalf("fresh failure replaced the cached answer: %+v", e)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("lookups = %d, want 2 (one advisory, one fresh)", got)
	}
}

// BenchmarkEvaluateActionsBacklogReplay measures the lookups a 500-action
// replayed backlog batch costs. lookups/op is the number that matters: each
// lookup is a node-wide spend read (about 1.2 s on the 2026-10-05 node).
func BenchmarkEvaluateActionsBacklogReplay(b *testing.B) {
	g, err := New(Options{
		Config:            budgetCfg("observe", false),
		Home:              "/home/u",
		KnownProjectRoots: []string{"/home/u/proj"},
		ReadFile:          fsMap(nil),
	})
	if err != nil {
		b.Fatalf("guard.New: %v", err)
	}
	var calls atomic.Int64
	g.SetBudgetLookup(func(string) (BudgetSnapshot, bool) { calls.Add(1); return BudgetSnapshot{SessionUSD: 1}, true })
	start := time.Now().Add(-72 * time.Hour)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.budgetMu.Lock()
		g.budgetCache = nil
		g.budgetMu.Unlock()
		g.EvaluateActions(backlogInputs(fmt.Sprintf("s%d", i), 500, start))
	}
	b.ReportMetric(float64(calls.Load())/float64(b.N), "lookups/op")
}
