package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/migplan"
)

// observer_planner fork lineage.
//
// The fork numbers its own migrations after upstream's highest, and upstream
// has twice since shipped DIFFERENT migrations under the fork's numbers:
//
//   - Builds before 2026-10-17 ran the fork's steps as 078-085; upstream
//     later used 078-163.
//   - Builds from 2026-10-17 to 2026-11-01 ran them as 164-170; upstream's
//     v1.37 release used 164-211.
//
// A store from such a build is pre-ledger (its runner kept only the 'version'
// record), so the ledger would adopt upstream's same-numbered steps as
// applied without running them. reconcileForkLineage rewinds such a store's
// version record to the last step both lineages share, so upstream's steps
// from there on run, and dropExistingAddColumns lets the renumbered fork
// migrations run again over columns the old build already added.

// forkGeneration is one old numbering of the fork's migrations: a pre-ledger
// store whose version record is in (base, max] and that holds the fork-only
// briefing_snapshots table was written by that generation's builds.
type forkGeneration struct {
	base int // last version both lineages shared
	max  int // highest version that generation's builds recorded
}

// forkGenerations is walked top-down; the ranges do not overlap.
var forkGenerations = []forkGeneration{
	{base: 77, max: 85},   // fork steps as 078-085 (before 2026-10-17)
	{base: 163, max: 170}, // fork steps as 164-170 (2026-10-17 to 2026-11-01)
}

const (
	forkFirst = 212 // first renumbered fork migration
	forkLast  = 218 // last renumbered fork migration
)

func isForkMigration(v int) bool { return v >= forkFirst && v <= forkLast }

// reconcileForkLineage rewinds the version record of a store written by an
// old fork generation to that generation's base, inside the caller's
// migration transaction, so a failure later in the batch rolls the rewind
// back too. A store with a ledger, an upstream store, or a fresh database is
// untouched.
func reconcileForkLineage(ctx context.Context, conn *sql.Conn) error {
	var ledgered int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_meta WHERE key = ?`, migplan.HorizonKey,
	).Scan(&ledgered); err != nil {
		return fmt.Errorf("db.reconcileForkLineage: probe ledger: %w", err)
	}
	if ledgered > 0 {
		return nil
	}
	var raw string
	err := conn.QueryRowContext(ctx, `SELECT value FROM schema_meta WHERE key = 'version'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("db.reconcileForkLineage: read version: %w", err)
	}
	applied, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("db.reconcileForkLineage: version %q: %w", raw, err)
	}
	for _, g := range forkGenerations {
		if applied <= g.base || applied > g.max {
			continue
		}
		var n int
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'briefing_snapshots'`,
		).Scan(&n); err != nil {
			return fmt.Errorf("db.reconcileForkLineage: probe: %w", err)
		}
		if n == 0 {
			return nil // an upstream store in this range: nothing to reconcile
		}
		if _, err := conn.ExecContext(ctx,
			`UPDATE schema_meta SET value = ? WHERE key = 'version'`, strconv.Itoa(g.base),
		); err != nil {
			return fmt.Errorf("db.reconcileForkLineage: rewind: %w", err)
		}
		return nil
	}
	return nil
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
