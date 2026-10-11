//go:build !sbo_omit_benchmark

package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
)

// TestImportBenchmarkRunFromCarriesTheReport is T-664: a run recorded in an
// isolated benchmark daemon's database, imported into the node's database,
// reports the same facts there (cost from the carried api_turns, the scorer
// verdict through the re-keyed attempt), a session the node already holds
// keeps its own turns (no double count), and a second import is refused
// without writing anything.
func TestImportBenchmarkRunFromCarriesTheReport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srcPath := filepath.Join(t.TempDir(), "bench.db")
	srcDB, err := dbtemplate.Open(ctx, db.Options{Path: srcPath})
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	t.Cleanup(func() { srcDB.Close() })
	src := New(srcDB)
	runID := seedBenchmarkRun(t, src)
	want, err := src.LoadBenchmarkFacts(ctx, runID)
	if err != nil || len(want) != 1 {
		t.Fatalf("source facts: %v %v", want, err)
	}

	dst, dstDB := newTestStore(t)
	// The node already has unrelated work: a project and an attempt id 1, so
	// a naive id copy would collide.
	if _, err := dstDB.ExecContext(ctx, `INSERT INTO projects (id, root_path, created_at) VALUES (5, '/home/dev/other', ?)`, timestamp(time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := dstDB.ExecContext(ctx, `INSERT INTO benchmark_runs (run_id, spec_name, spec_hash, spec_json, started_at, status, planned_cells, completed_cells, spend_usd, judge_spend_usd)
		VALUES ('older', 's', 'h', '{}', ?, 'done', 1, 1, 0, 0)`, timestamp(time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := dstDB.ExecContext(ctx, `INSERT INTO benchmark_attempts (run_id, task_id, config_id, harness, model_requested, repeat_idx, attempt_no, wall_ms, status, spend_usd, turns, started_at)
		VALUES ('older', 't', 'c', 'codex', 'm', 0, 1, 1, 'ok', 0, 0, ?)`, timestamp(time.Now())); err != nil {
		t.Fatal(err)
	}

	stats, err := dst.ImportBenchmarkRunFrom(ctx, srcPath, runID)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.Attempts != 1 || stats.Members != 1 || stats.Scores != 1 || stats.Sessions != 1 || stats.APITurns != 3 {
		t.Fatalf("stats = %+v", stats)
	}
	got, err := dst.LoadBenchmarkFacts(ctx, runID)
	if err != nil || len(got) != 1 {
		t.Fatalf("node facts: %v %v", got, err)
	}
	if got[0] != want[0] {
		t.Fatalf("node facts %+v, want %+v", got[0], want[0])
	}
	var root string
	if err := dstDB.QueryRowContext(ctx, `SELECT p.root_path FROM sessions s JOIN projects p ON p.id = s.project_id WHERE s.id = 'sess-a'`).Scan(&root); err != nil || root != "/tmp/ws-a" {
		t.Fatalf("session project root = %q, err %v", root, err)
	}

	if _, err := dst.ImportBenchmarkRunFrom(ctx, srcPath, runID); !errors.Is(err, ErrBenchmarkRunExists) {
		t.Fatalf("second import err = %v, want ErrBenchmarkRunExists", err)
	}
	var turns int
	if err := dstDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_turns WHERE session_id = 'sess-a'`).Scan(&turns); err != nil || turns != 3 {
		t.Fatalf("api_turns after a refused import = %d, err %v", turns, err)
	}
}

// TestImportBenchmarkRunFromKeepsExistingSessions: a member session the node
// already holds is not copied and its turns are not duplicated.
func TestImportBenchmarkRunFromKeepsExistingSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srcPath := filepath.Join(t.TempDir(), "bench.db")
	srcDB, err := dbtemplate.Open(ctx, db.Options{Path: srcPath})
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	t.Cleanup(func() { srcDB.Close() })
	runID := seedBenchmarkRun(t, New(srcDB))

	dst, dstDB := newTestStore(t)
	if _, err := dstDB.ExecContext(ctx, `INSERT INTO projects (id, root_path, created_at) VALUES (1, '/tmp/ws-a', ?)`, timestamp(time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := dstDB.ExecContext(ctx, `INSERT INTO sessions (id, project_id, tool, started_at) VALUES ('sess-a', 1, 'codex', '2026-07-12T10:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	stats, err := dst.ImportBenchmarkRunFrom(ctx, srcPath, runID)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.Sessions != 0 || stats.APITurns != 0 || stats.Members != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

// TestImportBenchmarkRunFromMissingRun: an unknown run id writes nothing.
func TestImportBenchmarkRunFromMissingRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srcPath := filepath.Join(t.TempDir(), "bench.db")
	srcDB, err := dbtemplate.Open(ctx, db.Options{Path: srcPath})
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	t.Cleanup(func() { srcDB.Close() })
	dst, _ := newTestStore(t)
	if _, err := dst.ImportBenchmarkRunFrom(ctx, srcPath, "nope"); err == nil {
		t.Fatal("import of a missing run succeeded")
	}
}
