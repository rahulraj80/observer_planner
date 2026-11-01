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

// TestForkLineageStoreReconciles builds a store the way each old
// observer_planner generation left it: upstream migrations through the
// generation's base, then the fork's own migrations recorded up to its max
// (pre-ledger, so only the 'version' record exists). It proves the current
// runner brings it to the latest version: upstream's same-numbered steps
// apply (the old watermark skipped them), and the fork's renumbered
// migrations replay over columns and rows that already exist without
// failing or duplicating.
func TestForkLineageStoreReconciles(t *testing.T) {
	for _, g := range forkGenerations {
		t.Run(strconv.Itoa(g.base+1)+"-"+strconv.Itoa(g.max), func(t *testing.T) {
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
			run := func(e migrationEntry) {
				t.Helper()
				body, err := fs.ReadFile(migrations.Files, e.filename)
				if err != nil {
					t.Fatal(err)
				}
				exec(string(body))
			}
			exec(`CREATE TABLE IF NOT EXISTS schema_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)
			var latest int
			for _, e := range entries {
				latest = e.version
				if e.version <= g.base {
					run(e)
				}
			}
			// The old build ran the fork's own migrations under (base, max].
			for _, e := range entries {
				if isForkMigration(e.version) {
					run(e)
				}
			}
			exec(`INSERT INTO schema_meta(key, value) VALUES ('version', '` + strconv.Itoa(g.max) + `') ON CONFLICT(key) DO UPDATE SET value = excluded.value`)

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
			// Upstream's 081/082, 166, 169 and 177 (each skipped by an old
			// watermark) are present, beside the fork's own tables.
			for _, table := range []string{
				"org_policy_resource_state", "org_enrolment_grant", "project_remotes", "pull_requests", "sec_findings",
				"briefing_snapshots", "analysis_calls", "license_usage",
			} {
				var n int
				if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
					t.Errorf("table %s missing after reconcile (n=%d, err=%v)", table, n, err)
				}
			}
			// Upstream's 080 column and the fork's 214/216 columns all exist.
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
		})
	}
}

// TestUpstreamStoreInForkRangeIsNotRewound pins the probe: an upstream-
// lineage store at a version inside a fork generation's range has no
// briefing_snapshots table and must keep its version record.
func TestUpstreamStoreInForkRangeIsNotRewound(t *testing.T) {
	for _, version := range []int{82, 168} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			ctx := context.Background()
			database, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "up.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if _, err := database.ExecContext(ctx, `CREATE TABLE schema_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(ctx, `INSERT INTO schema_meta(key, value) VALUES ('version', ?)`, strconv.Itoa(version)); err != nil {
				t.Fatal(err)
			}
			conn, err := database.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := reconcileForkLineage(ctx, conn); err != nil {
				t.Fatalf("reconcileForkLineage: %v", err)
			}
			var got string
			if err := conn.QueryRowContext(ctx, `SELECT value FROM schema_meta WHERE key = 'version'`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != strconv.Itoa(version) {
				t.Fatalf("version record = %s after reconcile on an upstream store, want %d", got, version)
			}
		})
	}
}
