package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db/migrations"
)

// TestForkLineageStoreReconciles builds a store the way a pre-2026-10-17
// observer_planner build left it — upstream migrations through 077, then the
// fork's own migrations recorded as 078..085 — and proves the current runner
// brings it to the latest version: upstream's real 078..085 apply (they were
// skipped before), and the fork's renumbered migrations replay over columns
// and rows that already exist without failing or duplicating.
func TestForkLineageStoreReconciles(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fork.db")
	dsn := "file:" + path + "?_pragma=busy_timeout(30000)&_pragma=foreign_keys(1)&_txlock=immediate"
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer database.Close()

	entries, err := readMigrationEntries()
	if err != nil {
		t.Fatalf("readMigrationEntries: %v", err)
	}
	exec := func(sqlText string) {
		t.Helper()
		if _, err := database.ExecContext(ctx, sqlText); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	exec(`CREATE TABLE IF NOT EXISTS schema_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)
	var latest int
	for _, e := range entries {
		latest = e.version
		if e.version > forkLineageBase {
			continue // upstream 078..163 never ran; the fork's run below
		}
		body, err := fs.ReadFile(migrations.Files, e.filename)
		if err != nil {
			t.Fatal(err)
		}
		exec(string(body))
	}
	// The old fork build's own migrations (now 164..170) ran as 078..084.
	for _, e := range entries {
		if !isForkMigration(e.version) {
			continue
		}
		body, err := fs.ReadFile(migrations.Files, e.filename)
		if err != nil {
			t.Fatal(err)
		}
		exec(string(body))
	}
	exec(`INSERT INTO schema_meta(key, value) VALUES ('version', '` + strconv.Itoa(forkLineageMax) + `') ON CONFLICT(key) DO UPDATE SET value = excluded.value`)

	if err := runMigrations(ctx, database); err != nil {
		t.Fatalf("runMigrations on a fork-lineage store: %v", err)
	}
	v, err := currentVersion(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	if v != latest {
		t.Fatalf("version = %d, want %d", v, latest)
	}
	// Upstream's 081/082 (skipped by the old watermark) are now present.
	for _, table := range []string{"org_policy_resource_state", "org_enrolment_grant", "briefing_snapshots", "analysis_calls", "license_usage"} {
		var n int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s missing after reconcile (n=%d, err=%v)", table, n, err)
		}
	}
	// Upstream's 080 column and the fork's 166 column both exist.
	for _, c := range [][2]string{{"session_annotations", "rating"}, {"api_turns", "turn_kind"}, {"router_decisions", "realized_delta_usd"}} {
		var n int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, c[0], c[1]).Scan(&n); err != nil || n != 1 {
			t.Errorf("column %s.%s missing after reconcile", c[0], c[1])
		}
	}
	// A second open is a no-op.
	if err := runMigrations(ctx, database); err != nil {
		t.Fatalf("second runMigrations: %v", err)
	}
}

// TestUpstreamStoreAt78To85IsNotRewound pins the probe: an upstream-lineage
// store at a version in 78..85 has no briefing_snapshots table and must keep
// its watermark.
func TestUpstreamStoreAt78To85IsNotRewound(t *testing.T) {
	ctx := context.Background()
	database, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "up.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, `CREATE TABLE schema_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	got, err := reconcileForkLineage(ctx, conn, 82)
	if err != nil || got != 82 {
		t.Fatalf("reconcileForkLineage(82) on an upstream store = %d, %v; want 82, nil", got, err)
	}
}
