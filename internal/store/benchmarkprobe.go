package store

import (
	"context"
	"fmt"
	"time"
)

// benchmarkprobe.go holds the benchmark-table SQL that core paths name in
// every build, including one without the benchmark component (the tables stay
// in the schema; lane MP-L1E): the snapshot gate's change probe and the
// retention prune.

// probeBenchmarks is the Track R2 change-detection probe for the benchmark
// wire (both BenchmarkRuns and BenchmarkAttempts, which one Select produces).
// It lives here - with the wire's own SQL - so orgsnapgate.go and orgpush.go
// stay free of the table names.
//
// PROBE: per table, (MAX(rowid|id), COUNT(*)) plus COUNT(finished_at) on the
// two tables that are MUTATED in place. Benchmark tables are the smallest
// substrate in the snapshot family - a benchmark run is an explicit, rare
// operator action, not a per-turn event - so a full count on each is
// negligible.
//
// WHY finished_at IS IN THE PROBE: benchmark_runs and benchmark_attempts are
// the clearest UPDATE-in-place families here. A run is INSERTed when it starts
// and later UPDATEd with its manifest, status, spend and finished_at; an
// attempt's status/error_class is UPDATEd on completion. An id/count-only probe
// would ship every run as perpetually "running", so the count of finished rows
// is folded in - that is the transition an admin actually watches for.
//
// RESIDUAL, BOUNDED BY THE FRESHNESS FLOOR: a status transition between two
// non-terminal states, or a spend/notes revision on an already-finished run,
// moves nothing in the probe; snapGate's maxSkipAge recomputes within the hour.
func (s *Store) probeBenchmarks(ctx context.Context) (string, error) {
	return s.snapProbeScalar(ctx, `
		SELECT 'br' || (SELECT COALESCE(MAX(rowid), 0) || '/' || COUNT(*) || '/' || COUNT(finished_at)
		                  FROM benchmark_runs) ||
		       ':ba' || (SELECT COALESCE(MAX(id), 0) || '/' || COUNT(*) || '/' || COUNT(finished_at)
		                   FROM benchmark_attempts) ||
		       ':bs' || (SELECT COALESCE(MAX(id), 0) || '/' || COUNT(*) FROM benchmark_scores)`)
}

// PruneBenchmarkRows deletes benchmark runs (and their children) whose
// started_at is older than retentionDays. ≤ 0 short-circuits (keep forever),
// matching the cachetrack/handoff sweeps orchestrated by runRetention. Returns
// the number of runs removed.
func (s *Store) PruneBenchmarkRows(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id FROM benchmark_runs WHERE datetime(started_at) < datetime(?)`,
		timestamp(cutoff))
	if err != nil {
		return 0, fmt.Errorf("store.PruneBenchmarkRows: list: %w", err)
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("store.PruneBenchmarkRows: scan: %w", err)
		}
		stale = append(stale, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store.PruneBenchmarkRows: rows: %w", err)
	}
	for _, id := range stale {
		if err := s.DeleteBenchmarkRun(ctx, id); err != nil {
			return 0, err
		}
	}
	return int64(len(stale)), nil
}

// DeleteBenchmarkRun removes a run and all its child rows (scores, members,
// attempts, then the run header) in one transaction. Backs
// `observer benchmark delete` and the retention sweep.
func (s *Store) DeleteBenchmarkRun(ctx context.Context, runID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store.DeleteBenchmarkRun: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`DELETE FROM benchmark_scores WHERE run_id = ?`,
		`DELETE FROM benchmark_session_members WHERE run_id = ?`,
		`DELETE FROM benchmark_attempts WHERE run_id = ?`,
		`DELETE FROM benchmark_runs WHERE run_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, runID); err != nil {
			return fmt.Errorf("store.DeleteBenchmarkRun: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store.DeleteBenchmarkRun: commit: %w", err)
	}
	return nil
}
