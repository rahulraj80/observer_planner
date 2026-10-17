package store

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"
	"time"
)

// guardBudgetFloorRow is one usage row a T-422 case writes, in insertion
// order. stamp is either a canonical time (valid) or raw malformed text.
type guardBudgetFloorRow struct {
	table string // "token_usage" or "api_turns"
	at    time.Time
	raw   string // when non-empty, the row's timestamp is replaced by this text
}

func insertGuardBudgetFloorRow(t *testing.T, db *sql.DB, i int, row guardBudgetFloorRow) {
	t.Helper()
	ctx := context.Background()
	at := row.at
	if at.IsZero() {
		at = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	}
	var res sql.Result
	var err error
	switch row.table {
	case "api_turns":
		res, err = db.ExecContext(ctx, `
			INSERT INTO api_turns(session_id, timestamp, provider, model, input_tokens, output_tokens, cost_usd,
			                      cache_read_tokens, cache_creation_tokens, cache_creation_1h_tokens, web_search_requests)
			VALUES ('floor', ?, 'anthropic', 'fixture-model', 100, 0, 0, 0, 0, 0, 0)`, timestamp(at))
	default:
		res, err = db.ExecContext(ctx, `
			INSERT INTO token_usage(session_id, timestamp, tool, model, input_tokens, output_tokens,
			                        estimated_cost_usd, source, reliability, source_event_id,
			                        cache_read_tokens, cache_creation_tokens, cache_creation_1h_tokens,
			                        reasoning_tokens, web_search_requests)
			VALUES ('floor', ?, 'claude-code', 'fixture-model', 100, 0, 0, 'jsonl', 'approximate', ?, 0, 0, 0, 0, 0)`,
			timestamp(at), "floor-"+string(rune('a'+i)))
	}
	if err != nil {
		t.Fatalf("insert row %d: %v", i, err)
	}
	if row.raw == "" {
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	table := "token_usage"
	if row.table == "api_turns" {
		table = "api_turns"
	}
	//nolint:gosec // G202: table is one of two closed test constants.
	if _, err := db.ExecContext(ctx, `UPDATE `+table+` SET timestamp = ? WHERE id = ?`, row.raw, id); err != nil {
		t.Fatalf("malform row %d: %v", i, err)
	}
}

// TestManagedBudgetMalformedTimestampCheckIsBoundedByInsertionOrder pins
// T-422. The managed read used to validate every row's timestamp across the
// node's whole history: on a large node that took longer than the guard's
// 3 s lookup deadline, and a malformed row written months before the current
// budget window closed every window forever. The check is now bounded by
// insertion order (AUTOINCREMENT id): every row written since the window
// opened is validated, whatever its timestamp text, and a row written before
// that is outside every window. Each case is one ordered insertion history.
func TestManagedBudgetMalformedTimestampCheckIsBoundedByInsertionOrder(t *testing.T) {
	t.Parallel()
	preWindow := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) // before month start (earliest)
	inWindow := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)  // in the day window
	cases := []struct {
		name            string
		rows            []guardBudgetFloorRow
		wantUnavailable bool
	}{
		{
			name: "malformed row written before the window opened is outside it",
			rows: []guardBudgetFloorRow{
				{raw: "2026-08-15 10:00:00"},
				{at: preWindow},
				{at: inWindow},
			},
			wantUnavailable: false,
		},
		{
			name: "malformed proxy turn written before the window opened is outside it",
			rows: []guardBudgetFloorRow{
				{table: "api_turns", raw: "100"},
				{table: "api_turns", at: preWindow},
				{table: "api_turns", at: inWindow},
			},
			wantUnavailable: false,
		},
		{
			name: "malformed row written after in-window rows still closes the windows",
			rows: []guardBudgetFloorRow{
				{at: preWindow},
				{at: inWindow},
				{raw: "100"},
			},
			wantUnavailable: true,
		},
		{
			name: "malformed proxy turn written after in-window rows still closes the windows",
			rows: []guardBudgetFloorRow{
				{table: "api_turns", at: preWindow},
				{table: "api_turns", at: inWindow},
				{table: "api_turns", raw: "2000-01-01 00:00:00"},
			},
			wantUnavailable: true,
		},
		{
			name: "malformed row after the last pre-window row, before any in-window row",
			rows: []guardBudgetFloorRow{
				{at: preWindow},
				{raw: "2000-01-01"},
				{at: inWindow},
			},
			wantUnavailable: true,
		},
		{
			name: "a later backfill of an old row does not hide a malformed row written in the window",
			rows: []guardBudgetFloorRow{
				{at: inWindow},
				{raw: "2000-01-01"},
				{at: preWindow},
			},
			wantUnavailable: true,
		},
		{
			// Review P3-38: the backfilled row is dated AFTER the last live
			// pre-window row, so it became "the newest row dated before the
			// window" and its post-window id raised the floor past the
			// malformed row written live in the window.
			name: "a backfill dated after the last live pre-window row does not hide an in-window malformed row",
			rows: []guardBudgetFloorRow{
				{at: preWindow},
				{raw: "2000-01-01"},
				{at: inWindow},
				{at: preWindow.Add(5 * 24 * time.Hour)},
			},
			wantUnavailable: true,
		},
		{
			name: "malformed text that sorts into the window is still read by the window itself",
			rows: []guardBudgetFloorRow{
				{raw: "not-a-timestamp"},
				{at: preWindow},
				{at: inWindow},
			},
			wantUnavailable: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st, database := newTestStore(t)
			guardBudgetTestSession(t, st, "floor")
			for i, row := range tc.rows {
				insertGuardBudgetFloorRow(t, database, i, row)
			}
			day, week, month := guardBudgetTestWindows()
			want := GuardBudgetUnavailableWindows{Daily: tc.wantUnavailable, Weekly: tc.wantUnavailable, Monthly: tc.wantUnavailable}

			tokens, err := st.GuardBudgetTokens(context.Background(), "", day, week, month, GuardBudgetReadOptions{Managed: true})
			if err != nil {
				t.Fatalf("GuardBudgetTokens: %v", err)
			}
			if tokens.Unavailable != want {
				t.Fatalf("token availability = %+v, want %+v", tokens.Unavailable, want)
			}
			if !tc.wantUnavailable && tokens.DailyTokens != 100 {
				t.Fatalf("daily tokens = %d, want the one in-window row's 100", tokens.DailyTokens)
			}

			price := func(string, time.Time, PushTokenSplit) (float64, string, bool) { return 0.01, "exact", true }
			spend, err := st.GuardBudgetSpendPriced(context.Background(), "", day, week, month, price, GuardBudgetReadOptions{Managed: true})
			if err != nil {
				t.Fatalf("GuardBudgetSpendPriced: %v", err)
			}
			if spend.UnpricedWindows != want {
				t.Fatalf("USD availability = %+v, want %+v", spend.UnpricedWindows, want)
			}
		})
	}
}

// TestManagedBudgetReadsNeverScanWholeHistory pins the COST half of T-422:
// the managed statements of both budget reads reach every usage table
// through an index or a rowid range, never a full table scan. A full scan is
// what made the managed read proportional to the node's history (about 3.5 s
// of timestamp validation on a 550k-row node) instead of to its window.
func TestManagedBudgetReadsNeverScanWholeHistory(t *testing.T) {
	t.Parallel()
	_, database := newTestStore(t)
	day, week, month := guardBudgetTestWindows()
	now := time.Date(2026, 9, 10, 18, 0, 0, 0, time.UTC)
	fullScan := regexp.MustCompile(`^SCAN (api_turns|token_usage)\b`)
	for _, tc := range []struct {
		name string
		sql  string
		args []any
	}{
		{"tokens managed", guardBudgetTokensSQL(true), guardBudgetTokensArgs(true, "s", day, week, month, month, now)},
		{"tokens advisory", guardBudgetTokensSQL(false), guardBudgetTokensArgs(false, "s", day, week, month, month, now)},
		{"priced managed", guardBudgetSpendPricedSQL(true), append(guardBudgetUsageArgs(true, "s", month, now), guardBudgetUsageArgs(true, "s", month, now)...)},
		{"priced advisory", guardBudgetSpendPricedSQL(false), append(guardBudgetUsageArgs(false, "s", month, now), guardBudgetUsageArgs(false, "s", month, now)...)},
	} {
		rows, err := database.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+tc.sql, tc.args...)
		if err != nil {
			t.Fatalf("%s: explain: %v", tc.name, err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		_ = rows.Close()
		for _, line := range plan {
			if fullScan.MatchString(line) {
				t.Errorf("%s: full table scan %q in plan:\n%s", tc.name, line, strings.Join(plan, "\n"))
			}
		}
	}
}
