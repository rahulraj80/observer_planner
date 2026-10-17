package db

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// observer_planner fork lineage.
//
// Builds of the observer_planner fork made before 2026-10-17 numbered their
// own migrations 078-085 (briefing snapshots, analysis calls, turn kind,
// license usage, dispatch benefit, cross-provider, session-summary FTS, a
// token_usage index). Upstream later shipped DIFFERENT migrations under the
// same numbers (078-085, then up to 163). The runner keeps one applied-version
// watermark, so a store created by an old fork build would sit at version
// 78..85 and silently skip upstream's 078-085.
//
// The fork's migrations now live at forkFirst..forkLast, after upstream's.
// reconcileForkLineage rewinds such a store to version 77 so upstream's
// 078 onward apply, and dropExistingAddColumns lets the renumbered fork
// migrations run again over columns the old build already added.
const (
	forkLineageBase = 77  // last version both lineages share
	forkLineageMax  = 85  // highest version an old fork build recorded
	forkFirst       = 164 // first renumbered fork migration
	forkLast        = 170 // last renumbered fork migration
)

func isForkMigration(v int) bool { return v >= forkFirst && v <= forkLast }

// reconcileForkLineage returns the applied version to migrate from. For a
// store written by an old fork build (watermark 78..85 and the fork-only
// briefing_snapshots table present) it rewinds the recorded watermark to
// forkLineageBase inside the caller's migration transaction, so a failure
// later in the batch rolls the rewind back too. Any other store is untouched.
func reconcileForkLineage(ctx context.Context, conn *sql.Conn, applied int) (int, error) {
	if applied <= forkLineageBase || applied > forkLineageMax {
		return applied, nil
	}
	var n int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'briefing_snapshots'`,
	).Scan(&n); err != nil {
		return applied, fmt.Errorf("db.reconcileForkLineage: probe: %w", err)
	}
	if n == 0 {
		return applied, nil // an upstream store at 78..85: nothing to reconcile
	}
	if _, err := conn.ExecContext(ctx,
		`UPDATE schema_meta SET value = ? WHERE key = 'version'`, fmt.Sprint(forkLineageBase),
	); err != nil {
		return applied, fmt.Errorf("db.reconcileForkLineage: rewind: %w", err)
	}
	return forkLineageBase, nil
}

var addColumnRE = regexp.MustCompile(`(?im)^\s*ALTER\s+TABLE\s+(\w+)\s+ADD\s+COLUMN\s+(\w+)[^;]*;\s*$`)

// dropExistingAddColumns removes each single-line `ALTER TABLE t ADD COLUMN c
// ...;` statement whose column already exists, so a fork migration replayed on
// a store the old fork build already migrated does not fail with "duplicate
// column name". SQLite has no ADD COLUMN IF NOT EXISTS. Applied only to the
// fork's own migrations; upstream migrations run unchanged.
func dropExistingAddColumns(ctx context.Context, conn *sql.Conn, sqlText string) (string, error) {
	var firstErr error
	out := addColumnRE.ReplaceAllStringFunc(sqlText, func(stmt string) string {
		m := addColumnRE.FindStringSubmatch(stmt)
		exists, err := forkColumnExists(ctx, conn, m[1], m[2])
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return stmt
		}
		if exists {
			return "-- (column already present) " + strings.TrimSpace(stmt)
		}
		return stmt
	})
	return out, firstErr
}

func forkColumnExists(ctx context.Context, conn *sql.Conn, table, column string) (bool, error) {
	var n int
	err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("db.columnExists %s.%s: %w", table, column, err)
	}
	return n > 0, nil
}
