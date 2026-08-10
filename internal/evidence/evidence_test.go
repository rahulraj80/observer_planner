package evidence

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// openDB returns a fresh temp-file SQLite DB with the subset of the real
// schema SelectEvidence reads (api_turns / sessions / projects /
// router_decisions / model_calibration) so the query is validated against
// the actual column names.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ev.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	schema := `
	CREATE TABLE projects (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		root_path TEXT NOT NULL,
		name TEXT,
		created_at TEXT NOT NULL
	);
	CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		project_id INTEGER NOT NULL,
		tool TEXT NOT NULL,
		started_at TEXT NOT NULL
	);
	CREATE TABLE api_turns (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT,
		project_id INTEGER,
		timestamp TEXT NOT NULL,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		turn_kind TEXT NOT NULL DEFAULT '',
		input_tokens INTEGER NOT NULL,
		output_tokens INTEGER NOT NULL,
		cache_read_tokens INTEGER,
		cache_creation_tokens INTEGER,
		cost_usd REAL,
		time_to_first_token_ms INTEGER,
		total_response_ms INTEGER,
		error_class TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE router_decisions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT,
		ts TEXT NOT NULL,
		turn_kind TEXT NOT NULL,
		selected_model TEXT NOT NULL,
		est_savings_usd REAL NOT NULL DEFAULT 0,
		applied INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE model_calibration (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		model TEXT NOT NULL,
		turn_kind TEXT NOT NULL,
		project_id INTEGER NOT NULL DEFAULT 0,
		window_days INTEGER NOT NULL,
		computed_at TEXT NOT NULL,
		n INTEGER NOT NULL DEFAULT 0,
		cost_usd_total REAL NOT NULL DEFAULT 0,
		error_count INTEGER NOT NULL DEFAULT 0
	);`
	for _, stmt := range []string{schema} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	return db
}

// TestSelectEvidence_RealPercentiles pins the repair: the query runs against
// the REAL column names (time_to_first_token_ms / total_response_ms, rd.ts,
// mc.turn_kind) and returns actual nearest-rank percentiles — not the old
// AVG-as-P50 / MAX-as-P75/P95 approximations.
func TestSelectEvidence_RealPercentiles(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx,
		`INSERT INTO projects(id, root_path, name, created_at) VALUES(1, '/work', 'work', '2026-08-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO sessions(id, project_id, tool, started_at) VALUES('s1', 1, 'claude-code', '2026-08-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// Six captured TTFTs 100..600 and totals 1000..1500 (one error turn) so
	// every percentile is distinguishable; a 0ms TTFT row (unset/captured-by-
	// buffering) must NOT pollute the percentile samples.
	ttfts := []int64{100, 200, 300, 400, 500, 600}
	totals := []int64{1000, 1100, 1200, 1300, 1400, 1500}
	for i := 0; i < 6; i++ {
		err := ""
		if i == 0 {
			err = "rate_limit_error"
		}
		ttft := ttfts[i]
		if i == 5 {
			ttft = 0 // not captured — excluded from TTFT percentiles
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO api_turns(session_id, project_id, timestamp, provider, model, turn_kind,
			   input_tokens, output_tokens, cost_usd, time_to_first_token_ms, total_response_ms, error_class)
			 VALUES('s1', 1, ?, 'anthropic', 'claude-sonnet-4', 'plan', 100, 50, 0.01, ?, ?, ?)`,
			time.Date(2026, 8, 1, i, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), ttft, totals[i], err); err != nil {
			t.Fatalf("seed api_turn %d: %v", i, err)
		}
	}

	// Router decision: contributes n + one applied/escalated flag.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO router_decisions(session_id, ts, turn_kind, selected_model, est_savings_usd, applied)
		 VALUES('s1', '2026-08-01T12:00:00Z', 'plan', 'claude-sonnet-4', 0.05, 1)`); err != nil {
		t.Fatalf("seed router decision: %v", err)
	}
	// Calibration cell: contributes n + one error_count (rate numerator).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO model_calibration(model, turn_kind, project_id, window_days, computed_at, n, error_count)
		 VALUES('claude-sonnet-4', 'plan', 0, 30, '2026-08-01T23:00:00Z', 5, 1)`); err != nil {
		t.Fatalf("seed calibration: %v", err)
	}

	rows, err := SelectEvidence(ctx, db, Query{
		Since: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("SelectEvidence: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 source-grouped rows, got %d: %+v", len(rows), rows)
	}
	bySource := map[string]Row{}
	for _, r := range rows {
		bySource[r.Source] = r
	}
	at, ok := bySource["api_turns"]
	if !ok {
		t.Fatal("missing api_turns row")
	}
	if at.Model != "claude-sonnet-4" || at.Kind != "plan" || at.Tool != "claude-code" || at.Provider != "anthropic" {
		t.Errorf("api_turns grouping = %+v", at)
	}
	// Nearest-rank percentiles (repo convention, mirroring org apiTurnPercentile).
	// TTFT: 6 rows, the 0ms one excluded → n=5 → P50/P75/P95 = 300/400/500.
	if at.TTFTP50 != 300 || at.TTFTP75 != 400 || at.TTFTP95 != 500 {
		t.Errorf("TTFT p50/p75/p95 = %d/%d/%d, want 300/400/500", at.TTFTP50, at.TTFTP75, at.TTFTP95)
	}
	// Totals: all six are positive → p50/p75/p95 = 1300/1400/1500.
	if at.TotalMsP50 != 1300 || at.TotalMsP75 != 1400 || at.TotalMsP95 != 1500 {
		t.Errorf("total p50/p75/p95 = %d/%d/%d, want 1300/1400/1500", at.TotalMsP50, at.TotalMsP75, at.TotalMsP95)
	}
	if at.N != 6 {
		t.Errorf("api_turns n = %d, want 6", at.N)
	}
	if at.ErrorRate != 1.0/6.0 {
		t.Errorf("api_turns error_rate = %v, want %v", at.ErrorRate, 1.0/6.0)
	}
	if rd, ok := bySource["router_decisions"]; !ok || rd.N != 1 || rd.EscalatedTurns != 1 {
		t.Errorf("router_decisions row = %+v, want n=1 escalated=1", rd)
	}
	if mc, ok := bySource["model_calibration"]; !ok || mc.N != 5 || mc.ErrorRate != 1.0/5.0 {
		t.Errorf("model_calibration row = %+v, want n=5 error_rate=0.2", mc)
	}
}

// TestSelectEvidence_Filters pins the kind/model/project/session filters and
// the fixed router/mc column joins.
func TestSelectEvidence_Filters(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO projects(id, root_path, name, created_at) VALUES(1, '/work', 'work', '2026-08-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO sessions(id, project_id, tool, started_at) VALUES('s1', 1, 'claude-code', '2026-08-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	for _, kind := range []string{"plan", "edit"} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO api_turns(session_id, project_id, timestamp, provider, model, turn_kind,
			   input_tokens, output_tokens, cost_usd, time_to_first_token_ms, total_response_ms)
			 VALUES('s1', 1, '2026-08-01T10:00:00Z', 'anthropic', 'claude-sonnet-4', ?, 10, 5, 0.01, 300, 2000)`,
			kind); err != nil {
			t.Fatalf("seed %s: %v", kind, err)
		}
	}
	q := Query{
		Since:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		Until:   time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
		Kind:    "plan",
		Model:   "claude-sonnet-4",
		Tool:    "claude-code",
		Project: "/work",
	}
	rows, err := SelectEvidence(ctx, db, q)
	if err != nil {
		t.Fatalf("SelectEvidence: %v", err)
	}
	if len(rows) != 1 || rows[0].Kind != "plan" {
		t.Fatalf("filtered rows = %+v, want exactly the plan row", rows)
	}
	if rows[0].TTFTP50 != 300 || rows[0].TotalMsP50 != 2000 {
		t.Errorf("filtered percentiles = ttft:%d total:%d, want 300/2000", rows[0].TTFTP50, rows[0].TotalMsP50)
	}
}

// TestSelectEvidence_EmptyWindow — no rows in the window yields no rows, and
// no error (the old query errored before ever returning anything).
func TestSelectEvidence_EmptyWindow(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	rows, err := SelectEvidence(ctx, db, Query{
		Since: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("SelectEvidence on empty DB: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("want 0 rows on empty window, got %d", len(rows))
	}
}
