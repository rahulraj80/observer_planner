package rollup

import (
	"context"
	"testing"
)

// TestSelectVirtuousCycle pins the org-server side of the Stage 3 exec-memo
// rollup: pushed virtuous_cycle rows are read back in chronological week
// order, filtered to the window, and untouched (the digest sums them).
func TestSelectVirtuousCycle(t *testing.T) {
	d := newDB(t)
	ctx := context.Background()

	ins := func(weekStart string, applies int64, projected, realized float64, turns int64) {
		t.Helper()
		if _, err := d.ExecContext(ctx,
			`INSERT INTO virtuous_cycle
			   (org_id, user_email, week_start, applies, projected_usd, realized_usd, realized_turns, updated_at, pushed_by_user_id)
			 VALUES ('org1', 'node@x', ?, ?, ?, ?, ?, '2026-05-26T06:00:00Z', 'node')`,
			weekStart, applies, projected, realized, turns); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// Three weeks, two of them inside the 90-day window of fixedNow.
	ins("2026-02-16", 2, 10.00, 0, 0)    // out-of-window (before 90d) → excluded
	ins("2026-05-18", 3, 15.00, 8.50, 5) // in-window
	ins("2026-05-25", 1, 6.00, 8.50, 5)  // in-window, most recent carries cumulative

	rows, err := SelectVirtuousCycle(ctx, d, 90, fixedNow)
	if err != nil {
		t.Fatalf("SelectVirtuousCycle: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (out-of-window week excluded)", len(rows))
	}
	if rows[0].WeekStart != "2026-05-18" || rows[1].WeekStart != "2026-05-25" {
		t.Fatalf("weeks = %q, %q, want 2026-05-18 then 2026-05-25", rows[0].WeekStart, rows[1].WeekStart)
	}
	if rows[0].Applies != 3 || !near(rows[0].ProjectedUSD, 15.00) {
		t.Errorf("week[0] = %d applies/$%.2f, want 3/$15.00", rows[0].Applies, rows[0].ProjectedUSD)
	}
	if rows[1].Applies != 1 || !near(rows[1].RealizedUSD, 8.50) || rows[1].RealizedTurns != 5 {
		t.Errorf("week[1] = %d applies realized $%.2f/%d, want 1/$8.50/5", rows[1].Applies, rows[1].RealizedUSD, rows[1].RealizedTurns)
	}
}

// TestSelectVirtuousCycle_Empty pins the empty case: no pushed rows → empty
// slice, no error.
func TestSelectVirtuousCycle_Empty(t *testing.T) {
	d := newDB(t)
	rows, err := SelectVirtuousCycle(context.Background(), d, 90, fixedNow)
	if err != nil {
		t.Fatalf("SelectVirtuousCycle: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("rows = %d, want 0", len(rows))
	}
}
