//nolint:gosec // G202: every concatenated name is a fixed table name or a column read from PRAGMA table_info; values are bound parameters.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// benchmarkimport.go carries one finished benchmark run out of an isolated
// benchmark daemon's throwaway database into this node's database (T-664).
//
// A claude-code benchmark runs against an ephemeral daemon (its own small DB,
// cmd/observer/benchmark_ephemeral.go) because its hooks stall against a large
// shared DB. Its results land in that throwaway DB, which is deleted when the
// run ends, and the report (LoadBenchmarkFacts) joins the attempts' session
// members to api_turns in the SAME database, so copying only the benchmark_*
// rows would report zero cost. ImportBenchmarkRunFrom therefore copies, in one
// transaction: the run, its attempts (re-keyed), session members and scores,
// the member sessions this node does not already hold, their api_turns, and the
// projects those rows point at (matched by root_path, the projects unique key).
// Columns are copied by name over the intersection of both schemas, so a
// column added later is carried without a change here.

// BenchmarkImportStats counts what ImportBenchmarkRunFrom copied.
type BenchmarkImportStats struct {
	Attempts int64
	Members  int64
	Scores   int64
	Sessions int64
	APITurns int64
}

// ErrBenchmarkRunExists is returned when this node already holds the run.
var ErrBenchmarkRunExists = errors.New("benchmark run already exists in this database")

// benchmarkImportSource is the schema name the source database is attached as.
const benchmarkImportSource = "bench_src"

// ImportBenchmarkRunFrom copies run runID and the sessions / api_turns its
// members reference from the observer database at srcPath into s. It never
// writes srcPath. A run already present here is refused with
// ErrBenchmarkRunExists and nothing is written.
func (s *Store) ImportBenchmarkRunFrom(ctx context.Context, srcPath, runID string) (BenchmarkImportStats, error) {
	var stats BenchmarkImportStats
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: conn: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS "+benchmarkImportSource, srcPath); err != nil {
		return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: attach: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "DETACH DATABASE "+benchmarkImportSource)
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM main.benchmark_runs WHERE run_id = ?`, runID).Scan(&n); err != nil {
		return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: check: %w", err)
	}
	if n > 0 {
		return stats, ErrBenchmarkRunExists
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+benchmarkImportSource+`.benchmark_runs WHERE run_id = ?`, runID).Scan(&n); err != nil {
		return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: source check: %w", err)
	}
	if n == 0 {
		return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: run %s not found in the source database", runID)
	}

	// The member sessions this node does not hold yet; only their api_turns
	// are copied, so a session already here is never double counted.
	for _, q := range []string{
		`CREATE TEMP TABLE IF NOT EXISTS bench_import_sessions (id TEXT PRIMARY KEY)`,
		`DELETE FROM temp.bench_import_sessions`,
		`CREATE TEMP TABLE IF NOT EXISTS bench_import_attempts (old_id INTEGER PRIMARY KEY, new_id INTEGER NOT NULL)`,
		`DELETE FROM temp.bench_import_attempts`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: temp: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO temp.bench_import_sessions (id)
		SELECT DISTINCT m.session_id
		  FROM `+benchmarkImportSource+`.benchmark_session_members m
		 WHERE m.run_id = ?
		   AND m.session_id IN (SELECT id FROM `+benchmarkImportSource+`.sessions)
		   AND m.session_id NOT IN (SELECT id FROM main.sessions)`, runID); err != nil {
		return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: sessions to copy: %w", err)
	}

	// Projects referenced by those sessions and their turns, keyed by root_path.
	projCols, err := benchmarkImportColumns(ctx, tx, "projects", "id")
	if err != nil {
		return stats, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO main.projects (`+strings.Join(projCols, ", ")+`)
		SELECT `+benchImportCols("p", projCols)+`
		  FROM `+benchmarkImportSource+`.projects p
		 WHERE p.id IN (SELECT project_id FROM `+benchmarkImportSource+`.sessions WHERE id IN (SELECT id FROM temp.bench_import_sessions))
		    OR p.id IN (SELECT project_id FROM `+benchmarkImportSource+`.api_turns WHERE session_id IN (SELECT id FROM temp.bench_import_sessions))`); err != nil {
		return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: projects: %w", err)
	}
	projectMap := func(alias string) string {
		return `(SELECT mp.id FROM main.projects mp JOIN ` + benchmarkImportSource + `.projects sp ON sp.root_path = mp.root_path WHERE sp.id = ` + alias + `.project_id)`
	}

	sessCols, err := benchmarkImportColumns(ctx, tx, "sessions")
	if err != nil {
		return stats, err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO main.sessions (`+strings.Join(sessCols, ", ")+`)
		SELECT `+benchImportColsMapped("s", sessCols, map[string]string{"project_id": projectMap("s")})+`
		  FROM `+benchmarkImportSource+`.sessions s
		 WHERE s.id IN (SELECT id FROM temp.bench_import_sessions)`)
	if err != nil {
		return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: sessions: %w", err)
	}
	stats.Sessions, _ = res.RowsAffected()

	turnCols, err := benchmarkImportColumns(ctx, tx, "api_turns", "id")
	if err != nil {
		return stats, err
	}
	res, err = tx.ExecContext(ctx, `
		INSERT INTO main.api_turns (`+strings.Join(turnCols, ", ")+`)
		SELECT `+benchImportColsMapped("t", turnCols, map[string]string{"project_id": projectMap("t")})+`
		  FROM `+benchmarkImportSource+`.api_turns t
		 WHERE t.session_id IN (SELECT id FROM temp.bench_import_sessions)
		 ORDER BY t.id`)
	if err != nil {
		return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: api_turns: %w", err)
	}
	stats.APITurns, _ = res.RowsAffected()

	runCols, err := benchmarkImportColumns(ctx, tx, "benchmark_runs")
	if err != nil {
		return stats, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO main.benchmark_runs (`+strings.Join(runCols, ", ")+`)
		SELECT `+benchImportCols("r", runCols)+` FROM `+benchmarkImportSource+`.benchmark_runs r WHERE r.run_id = ?`, runID); err != nil {
		return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: run: %w", err)
	}

	if err := importBenchmarkAttempts(ctx, tx, runID, &stats); err != nil {
		return stats, err
	}

	if err := tx.Commit(); err != nil {
		return stats, fmt.Errorf("store.ImportBenchmarkRunFrom: commit: %w", err)
	}
	return stats, nil
}

// importBenchmarkAttempts copies the run's attempts with new ids, then its
// session members and scores re-keyed through the attempt map.
func importBenchmarkAttempts(ctx context.Context, tx *sql.Tx, runID string, stats *BenchmarkImportStats) error {
	// Attempts get new ids here; the map re-keys members and scores.
	attCols, err := benchmarkImportColumns(ctx, tx, "benchmark_attempts", "id")
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM `+benchmarkImportSource+`.benchmark_attempts WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return fmt.Errorf("store.ImportBenchmarkRunFrom: attempts: %w", err)
	}
	var oldIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("store.ImportBenchmarkRunFrom: attempt scan: %w", err)
		}
		oldIDs = append(oldIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store.ImportBenchmarkRunFrom: attempts: %w", err)
	}
	insertAttempt := `INSERT INTO main.benchmark_attempts (` + strings.Join(attCols, ", ") + `)
		SELECT ` + benchImportCols("a", attCols) + ` FROM ` + benchmarkImportSource + `.benchmark_attempts a WHERE a.id = ?`
	for _, old := range oldIDs {
		res, err := tx.ExecContext(ctx, insertAttempt, old)
		if err != nil {
			return fmt.Errorf("store.ImportBenchmarkRunFrom: attempt %d: %w", old, err)
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("store.ImportBenchmarkRunFrom: attempt id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO temp.bench_import_attempts (old_id, new_id) VALUES (?, ?)`, old, newID); err != nil {
			return fmt.Errorf("store.ImportBenchmarkRunFrom: attempt map: %w", err)
		}
		stats.Attempts++
	}

	for _, child := range []struct {
		table string
		count *int64
	}{
		{"benchmark_session_members", &stats.Members},
		{"benchmark_scores", &stats.Scores},
	} {
		cols, err := benchmarkImportColumns(ctx, tx, child.table, "id")
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO main.`+child.table+` (`+strings.Join(cols, ", ")+`)
			SELECT `+benchImportColsMapped("c", cols, map[string]string{"attempt_id": "am.new_id"})+`
			  FROM `+benchmarkImportSource+`.`+child.table+` c
			  JOIN temp.bench_import_attempts am ON am.old_id = c.attempt_id
			 WHERE c.run_id = ?
			 ORDER BY c.rowid`, runID)
		if err != nil {
			return fmt.Errorf("store.ImportBenchmarkRunFrom: %s: %w", child.table, err)
		}
		*child.count, _ = res.RowsAffected()
	}
	return nil
}

// benchmarkImportColumns returns the columns table has in BOTH the main and
// the attached source schema, in main's order, without skip.
func benchmarkImportColumns(ctx context.Context, tx *sql.Tx, table string, skip ...string) ([]string, error) {
	read := func(schema string) ([]string, error) {
		rows, err := tx.QueryContext(ctx, `PRAGMA `+schema+`.table_info(`+table+`)`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var (
				cid       int
				name, typ string
				notnull   int
				dflt      sql.NullString
				pk        int
			)
			if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
				return nil, err
			}
			out = append(out, name)
		}
		return out, rows.Err()
	}
	mainCols, err := read("main")
	if err != nil {
		return nil, fmt.Errorf("store.ImportBenchmarkRunFrom: columns of %s: %w", table, err)
	}
	srcCols, err := read(benchmarkImportSource)
	if err != nil {
		return nil, fmt.Errorf("store.ImportBenchmarkRunFrom: source columns of %s: %w", table, err)
	}
	inSrc := map[string]bool{}
	for _, c := range srcCols {
		inSrc[c] = true
	}
	skipped := map[string]bool{}
	for _, c := range skip {
		skipped[c] = true
	}
	var out []string
	for _, c := range mainCols {
		if inSrc[c] && !skipped[c] {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("store.ImportBenchmarkRunFrom: %s has no shared columns", table)
	}
	return out, nil
}

// benchImportCols renders cols as alias.col, comma separated.
func benchImportCols(alias string, cols []string) string {
	return benchImportColsMapped(alias, cols, nil)
}

// benchImportColsMapped is benchImportCols with some columns replaced by an expression.
func benchImportColsMapped(alias string, cols []string, repl map[string]string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		if e, ok := repl[c]; ok {
			parts[i] = e
			continue
		}
		parts[i] = alias + "." + c
	}
	return strings.Join(parts, ", ")
}
