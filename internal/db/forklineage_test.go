package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db/migrations"
	"github.com/marmutapp/superbased-observer/internal/migplan"
)

// TestForkLineageStoreReconciles builds a store the way each old
// observer_planner generation left it: upstream migrations through the
// generation's base, then the fork's own migrations recorded up to its max.
// A pre-ledger generation leaves only the 'version' record; a ledgered one
// (marker set) also leaves the horizon and one applied record per step. It
// proves the current runner brings it to the latest version: upstream's
// same-numbered steps apply (the old watermark, or the fork's step records,
// skipped them), and the fork's renumbered migrations replay over columns
// and rows that already exist without failing or duplicating.
func TestForkLineageStoreReconciles(t *testing.T) {
	type shape struct {
		forkGeneration
		// upstreamRanTo is the highest upstream step an upstream binary ran
		// on the store after the fork build (0: none did). Only a ledgered
		// store can be in this state.
		upstreamRanTo int
	}
	var shapes []shape
	for _, g := range forkGenerations {
		shapes = append(shapes, shape{forkGeneration: g})
		if g.marker != 0 {
			shapes = append(shapes, shape{forkGeneration: g, upstreamRanTo: 248})
		}
	}
	for _, g := range shapes {
		name := strconv.Itoa(g.base+1) + "-" + strconv.Itoa(g.max)
		if g.upstreamRanTo > 0 {
			name += "-then-upstream-" + strconv.Itoa(g.upstreamRanTo)
		}
		t.Run(name, func(t *testing.T) {
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
			recorded := g.max
			record := func(version int) {
				t.Helper()
				exec(`INSERT INTO schema_meta(key, value) VALUES ('` + migplan.StepKey(version) + `', '` + migplan.RecordApplied + `')`)
			}
			if g.marker != 0 {
				// The generation's builds kept the ledger: a horizon, and an
				// applied record for every step they ran, the fork's own
				// under (base, max].
				exec(`INSERT INTO schema_meta(key, value) VALUES ('` + migplan.HorizonKey + `', '0')`)
				for _, e := range entries {
					if e.version <= g.base {
						record(e.version)
					}
				}
				for v := g.base + 1; v <= g.max; v++ {
					record(v)
				}
				// An upstream binary run on the store since then read the
				// fork's records as its own steps, skipped those, and ran and
				// recorded everything above them.
				for _, e := range entries {
					if e.version > g.max && e.version <= g.upstreamRanTo {
						run(e)
						record(e.version)
						recorded = e.version
					}
				}
			}
			exec(`INSERT INTO schema_meta(key, value) VALUES ('version', '` + strconv.Itoa(recorded) + `') ON CONFLICT(key) DO UPDATE SET value = excluded.value`)

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
			// Upstream's 081/082, 166, 169, 177 and 212 (each skipped by an
			// old watermark or a fork step record) are present, beside the
			// fork's own tables.
			for _, table := range []string{
				"org_policy_resource_state", "org_enrolment_grant", "project_remotes", "pull_requests", "sec_findings",
				"process_rollups", "process_capture_stats",
				"briefing_snapshots", "analysis_calls", "license_usage",
			} {
				var n int
				if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
					t.Errorf("table %s missing after reconcile (n=%d, err=%v)", table, n, err)
				}
			}
			// Upstream's 080, 212 and 215 columns and the fork's turn_kind and
			// dispatch-benefit columns all exist.
			for _, c := range [][2]string{
				{"session_annotations", "rating"},
				{"process_runs", "capture_class"},
				{"codeintel_nodes", "complexity"},
				{"api_turns", "turn_kind"},
				{"router_decisions", "realized_delta_usd"},
			} {
				var n int
				if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, c[0], c[1]).Scan(&n); err != nil || n != 1 {
					t.Errorf("column %s.%s missing after reconcile", c[0], c[1])
				}
			}
			// Upstream's 218 unique key exists.
			var idx int
			if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_guard_approvals_key'`).Scan(&idx); err != nil || idx != 1 {
				t.Errorf("index idx_guard_approvals_key missing after reconcile (n=%d, err=%v)", idx, err)
			}
			// Every embedded step is recorded, and no record is left under a
			// number the lineage does not carry (the fork's old 213, 214, 216).
			var records int
			if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_meta WHERE key LIKE 'migration:%'`).Scan(&records); err != nil || records != len(entries) {
				t.Errorf("step records = %d, want %d (err=%v)", records, len(entries), err)
			}
			// A second open is a no-op: nothing left to plan, and the
			// reconcile does not fire again.
			plan, err := PlanMigrations(ctx, database)
			if err != nil || !plan.Noop() {
				t.Fatalf("plan after reconcile = %+v (err=%v), want a no-op", plan, err)
			}
			if err := runMigrations(ctx, database); err != nil {
				t.Fatalf("second runMigrations: %v", err)
			}
		})
	}
}

// TestReconciledForkStoreIsNotRewoundAgain pins the ledgered generation's
// marker: a store this build migrated has briefing_snapshots (the fork's 249)
// and a version record above every generation, and its upstream 212-218
// records must stay.
func TestReconciledForkStoreIsNotRewoundAgain(t *testing.T) {
	ctx := context.Background()
	database, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "fresh.db")+"?_pragma=busy_timeout(30000)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := runMigrations(ctx, database); err != nil {
		t.Fatalf("runMigrations on a fresh store: %v", err)
	}
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := reconcileForkLineage(ctx, conn); err != nil {
		t.Fatalf("reconcileForkLineage: %v", err)
	}
	for _, v := range []int{212, 215, 217, 218} {
		var got string
		if err := conn.QueryRowContext(ctx, `SELECT value FROM schema_meta WHERE key = ?`, migplan.StepKey(v)).Scan(&got); err != nil || got != migplan.RecordApplied {
			t.Errorf("step %d record = %q (err=%v) after reconcile on a current store, want applied", v, got, err)
		}
	}
	var marker int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_meta WHERE key = ?`, migplan.StepKey(213)).Scan(&marker); err != nil || marker != 0 {
		t.Errorf("a current store holds a migration:213 record (n=%d, err=%v); the ledgered generation's marker must be a number the lineage never records", marker, err)
	}

	// An unreleased upstream build left orphan 213, 214 and 216 records
	// (TestMergeMainLedgerShapes). Once this build has migrated such a store
	// it holds briefing_snapshots as well, and must still be left alone:
	// upstream's 212 cannot run twice.
	for _, v := range []int{213, 214, 216} {
		if _, err := conn.ExecContext(ctx, `INSERT INTO schema_meta(key, value) VALUES (?, ?)`, migplan.StepKey(v), migplan.RecordApplied); err != nil {
			t.Fatal(err)
		}
	}
	if err := reconcileForkLineage(ctx, conn); err != nil {
		t.Fatalf("reconcileForkLineage with orphan records: %v", err)
	}
	var kept int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_meta WHERE key IN (?, ?, ?, ?, ?)`,
		migplan.StepKey(212), migplan.StepKey(213), migplan.StepKey(215), migplan.StepKey(217), migplan.StepKey(218)).Scan(&kept); err != nil || kept != 5 {
		t.Errorf("records kept = %d of 5 (err=%v): a migrated store with upstream's orphan records was rewound", kept, err)
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
