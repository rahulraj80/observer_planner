package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/configapply"
	"github.com/marmutapp/superbased-observer/internal/db"
)

// writeLedgerApply appends one apply event directly to the briefing-CTA
// ledger next to the store's DB file (mirrors the dashboard test helper;
// the ledger lives beside the DB, so the store's dbPath locates it).
func writeLedgerApplyNextToDB(t *testing.T, dbPath string, ev configapply.LedgerEvent) {
	t.Helper()
	path := filepath.Join(filepath.Dir(dbPath), "briefing-cta-ledger.jsonl")
	line, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}

func openVirtuousCycleTestStore(t *testing.T) (*Store, string, context.Context) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vc_test.db")
	database, err := db.Open(ctx, db.Options{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	st := New(database)
	st.SetDBPath(dbPath)
	return st, dbPath, ctx
}

// TestSelectVirtuousCycle pins the Stage 3 node-side aggregate: weekly
// buckets from ALL routing applies in the ledger, with the cumulative
// realized figure (earliest apply → now, no mode/policy filter) stamped on
// the most recent week only — so the orgserver digest's row-sum stays exact.
func TestSelectVirtuousCycle(t *testing.T) {
	t.Parallel()
	st, dbPath, ctx := openVirtuousCycleTestStore(t)

	// Two applies in different weeks.
	applyTS1 := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	applyTS2 := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	writeLedgerApplyNextToDB(t, dbPath, configapply.LedgerEvent{
		Time: applyTS1, Kind: "apply", Source: "briefing-cta",
		Section: "routing", Key: "mode", From: "advise", To: "enforce",
		Detector: "routing_shadow", ProjectedUSD: 6.00,
	})
	writeLedgerApplyNextToDB(t, dbPath, configapply.LedgerEvent{
		Time: applyTS2, Kind: "apply", Source: "briefing-cta",
		Section: "routing", Key: "policy", From: "value", To: "balanced",
		Detector: "routing_shadow", ProjectedUSD: 12.00,
	})

	// Realized: only applied turns between earliest apply and now count.
	// Pre-apply and unapplied rows must be excluded.
	delta := func(v float64) *float64 { return &v }
	rows := []RouterDecisionRow{
		{SessionID: "s1", Timestamp: applyTS1.Add(2 * time.Hour), Mode: "enforce", PolicyName: "value", Applied: true, RealizedDeltaUSD: delta(4.20)},
		{SessionID: "s1", Timestamp: applyTS1.Add(-2 * time.Hour), Mode: "enforce", PolicyName: "value", Applied: true, RealizedDeltaUSD: delta(9.99)}, // pre-earliest, excluded
		{SessionID: "s1", Timestamp: applyTS2.Add(2 * time.Hour), Mode: "enforce", PolicyName: "balanced", Applied: true, RealizedDeltaUSD: delta(8.30)},
		{SessionID: "s1", Timestamp: applyTS2.Add(3 * time.Hour), Mode: "advise", PolicyName: "balanced", Applied: true, RealizedDeltaUSD: delta(3.00)}, // counts (no mode filter)
		{SessionID: "s1", Timestamp: applyTS2.Add(4 * time.Hour), Mode: "enforce", PolicyName: "value", Applied: false, RealizedDeltaUSD: delta(5.00)},  // unapplied, excluded
	}
	if err := st.InsertRouterDecisions(ctx, rows); err != nil {
		t.Fatal(err)
	}

	got, err := st.SelectVirtuousCycle(ctx, 90)
	if err != nil {
		t.Fatalf("SelectVirtuousCycle: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("weeks = %d, want 2", len(got))
	}
	if got[0].WeekStart >= got[1].WeekStart {
		t.Fatalf("weeks not chronological: %q then %q", got[0].WeekStart, got[1].WeekStart)
	}
	if got[0].Applies != 1 || !f64near(got[0].ProjectedUSD, 6.00) {
		t.Errorf("week[0] = %d applies/$%.2f, want 1/$6.00", got[0].Applies, got[0].ProjectedUSD)
	}
	if got[1].Applies != 1 || !f64near(got[1].ProjectedUSD, 12.00) {
		t.Errorf("week[1] = %d applies/$%.2f, want 1/$12.00", got[1].Applies, got[1].ProjectedUSD)
	}
	// Realized (4.20+8.30+3.00) lives on the most recent week only; the
	// earlier week carries zero so a digest sum across rows is exact.
	if !f64near(got[1].RealizedUSD, 15.50) || got[1].RealizedTurns != 3 {
		t.Errorf("week[1] realized = $%.2f/%d, want $15.50/3", got[1].RealizedUSD, got[1].RealizedTurns)
	}
	if got[0].RealizedUSD != 0 || got[0].RealizedTurns != 0 {
		t.Errorf("week[0] realized = $%.2f/%d, want 0/0 (only most recent week carries cumulative)", got[0].RealizedUSD, got[0].RealizedTurns)
	}
}

// TestSelectVirtuousCycle_EmptyLedger pins the empty case: no routing
// applies → nil, nil (not an error).
func TestSelectVirtuousCycle_EmptyLedger(t *testing.T) {
	t.Parallel()
	st, _, ctx := openVirtuousCycleTestStore(t)
	got, err := st.SelectVirtuousCycle(ctx, 90)
	if err != nil {
		t.Fatalf("SelectVirtuousCycle: %v", err)
	}
	if got != nil {
		t.Errorf("weeks = %v, want nil for empty ledger", got)
	}
}

// TestSelectVirtuousCycle_NoDBPath pins the guard: without SetDBPath the
// ledger can't be located and the call fails loudly.
func TestSelectVirtuousCycle_NoDBPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := db.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "nodbpath.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	st := New(database)
	if _, err := st.SelectVirtuousCycle(ctx, 90); err == nil {
		t.Error("SelectVirtuousCycle without dbPath should error")
	}
}

func f64near(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}
