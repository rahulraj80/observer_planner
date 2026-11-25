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
// has four times since shipped DIFFERENT migrations under the fork's numbers:
//
//   - Builds before 2026-10-17 ran the fork's steps as 078-085; upstream
//     later used 078-163.
//   - Builds from 2026-10-17 to 2026-11-01 ran them as 164-170; upstream's
//     v1.37 release used 164-211.
//   - Builds from 2026-11-01 to 2026-11-11 ran them as 212-218; upstream's
//     v1.38 release used 212, 215 and 217-248.
//   - Builds from 2026-11-11 to 2026-11-25 ran them as 249-255 and the
//     telemetry rail's two steps as 256-257; upstream's v1.39 to v1.43.1
//     releases used 250, 251, 253-256 and 259-261, and reserve 249, 252,
//     257 and 258 for lines not merged yet.
//
// A store from one of the first two generations is pre-ledger (its runner
// kept only the 'version' record), so the ledger would adopt upstream's
// same-numbered steps as applied without running them. A store from the
// third generation has a ledger whose per-step records 212-218 describe the
// fork's steps, so the plan would read upstream's 212, 215, 217 and 218 as
// recorded and skip them; a store from the fourth has fork records 249-257,
// so upstream's 250, 251 and 253-256 would be skipped the same way. reconcileForkLineage rewinds such a store to the
// last step both lineages share, so upstream's steps from there on run, and
// dropExistingAddColumns lets the renumbered fork migrations run again over
// columns the old build already added.

// forkGeneration is one old numbering of the fork's migrations: a store
// whose records are in (base, max] and that holds the fork-only
// briefing_snapshots table was written by that generation's builds.
type forkGeneration struct {
	base int // last version both lineages shared
	max  int // highest version that generation's builds recorded
	// marker is 0 for a pre-ledger generation, recognised by its version
	// record alone. For a generation whose builds kept the per-step ledger
	// it is a step number in (base, max] that the public upstream lineage
	// does not carry: upstream retired 213, 214 and 216 at its own main
	// merge (P-247, see lateSteps in db.go), so on a store that also holds
	// briefing_snapshots a 'migration:213' record is the fork's
	// analysis_calls step.
	marker int
	// next is the first step of the numbering that replaced a ledgered
	// generation. A store with a 'migration:<next>' record has been through
	// this reconcile, or was never the generation's: an unreleased upstream
	// build also left 213, 214 and 216 records (TestMergeMainLedgerShapes
	// keeps them as orphans), and once a current fork build has migrated
	// such a store it holds briefing_snapshots too. It never changes, even
	// when the fork is renumbered again; a later generation's rewind deletes
	// this record, so the next of every LATER ledgered generation counts as
	// moved too (movedKeys).
	next int
}

// forkGenerations is walked top-down; the ranges do not overlap.
var forkGenerations = []forkGeneration{
	{base: 77, max: 85},                           // fork steps as 078-085 (before 2026-10-17)
	{base: 163, max: 170},                         // fork steps as 164-170 (2026-10-17 to 2026-11-01)
	{base: 211, max: 218, marker: 213, next: 249}, // fork steps as 212-218 (2026-11-01 to 2026-11-11), ledgered
	// Fork steps as 249-257 (2026-11-11 to 2026-11-25), ledgered. The
	// marker 249 is the fork's briefing_snapshots step, recorded on every
	// store of the generation (256 and 257 only since the telemetry merge).
	// Upstream reserves 249 for a line not merged yet; should it ship one,
	// a store this build migrated still has 262 and is left alone.
	{base: 248, max: 257, marker: 249, next: 262},
}

// The fork's migrations, renumbered on 2026-11-25 above upstream v1.43.1's
// 261: Briefing through the summaries index (262-268) and the telemetry
// rail's staging tables (269, 270). No upstream build carries them.
const (
	forkFirst = 262 // first renumbered fork migration
	forkLast  = 270 // last renumbered fork migration
)

// isForkMigration reports whether v is one of the fork's own steps; each
// replays over columns an older fork build already added
// (dropExistingAddColumns).
func isForkMigration(v int) bool { return v >= forkFirst && v <= forkLast }

// isForkStep reports whether v is a step only this fork carries.
func isForkStep(v int) bool { return isForkMigration(v) }

// reconcileForkLineage rewinds a store written by an old fork generation to
// that generation's base, inside the caller's migration transaction, so a
// failure later in the batch rolls the rewind back too. An upstream store, a
// store this build already reconciled, or a fresh database is untouched.
func reconcileForkLineage(ctx context.Context, conn *sql.Conn) error {
	var ledgered int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_meta WHERE key = ?`, migplan.HorizonKey,
	).Scan(&ledgered); err != nil {
		return fmt.Errorf("db.reconcileForkLineage: probe ledger: %w", err)
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
	for i, g := range forkGenerations {
		if (g.marker != 0) != (ledgered > 0) {
			continue
		}
		matches, err := g.wrote(ctx, conn, applied, movedKeys(i))
		if err != nil {
			return err
		}
		if !matches {
			continue
		}
		hasFork, err := forkTableExists(ctx, conn)
		if err != nil {
			return err
		}
		if !hasFork {
			return nil // an upstream store in this range: nothing to reconcile
		}
		return g.rewind(ctx, conn, applied)
	}
	return nil
}

// wrote reports whether the store's records have the shape g's builds left.
// A pre-ledger generation is known by a version record in (base, max]. A
// ledgered one is known by its marker step record without a record of the
// numbering that replaced it, whatever the version record says: an upstream
// binary run on the store since then raises the version past max and leaves
// the fork's records in place.
func (g forkGeneration) wrote(ctx context.Context, conn *sql.Conn, applied int, moved []string) (bool, error) {
	if g.marker == 0 {
		return applied > g.base && applied <= g.max, nil
	}
	var marker int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_meta WHERE key = ?`, migplan.StepKey(g.marker),
	).Scan(&marker); err != nil {
		return false, fmt.Errorf("db.reconcileForkLineage: probe marker: %w", err)
	}
	if marker == 0 {
		return false, nil
	}
	for _, key := range moved {
		var n int
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_meta WHERE key = ?`, key,
		).Scan(&n); err != nil {
			return false, fmt.Errorf("db.reconcileForkLineage: probe moved: %w", err)
		}
		if n > 0 {
			return false, nil
		}
	}
	return true, nil
}

// movedKeys are the step records that show a store has left the ledgered
// generation at index i: its own next, and the next of every later ledgered
// generation (whose rewind deletes the earlier next record, which lies in
// its range).
func movedKeys(i int) []string {
	var out []string
	for _, g := range forkGenerations[i:] {
		if g.marker != 0 {
			out = append(out, migplan.StepKey(g.next))
		}
	}
	return out
}

// rewind makes the plan run upstream's steps in (base, max]. A pre-ledger
// store gets its version record set to base, which becomes the ledger
// horizon. A ledgered store loses the fork's per-step records for the range;
// its version record goes back to base only when it is still inside the
// range, because a version record above the highest remaining step record
// would otherwise read as "a pre-ledger runner advanced the version record"
// (migplan's horizon rule) and adopt the range instead of running it.
func (g forkGeneration) rewind(ctx context.Context, conn *sql.Conn, applied int) error {
	if g.marker != 0 {
		for v := g.base + 1; v <= g.max; v++ {
			if _, err := conn.ExecContext(ctx,
				`DELETE FROM schema_meta WHERE key = ?`, migplan.StepKey(v),
			); err != nil {
				return fmt.Errorf("db.reconcileForkLineage: forget step %d: %w", v, err)
			}
		}
		if applied > g.max {
			return nil
		}
	}
	if _, err := conn.ExecContext(ctx,
		`UPDATE schema_meta SET value = ? WHERE key = 'version'`, strconv.Itoa(g.base),
	); err != nil {
		return fmt.Errorf("db.reconcileForkLineage: rewind: %w", err)
	}
	return nil
}

// forkTableExists probes for briefing_snapshots, the table only the fork's
// first migration creates.
func forkTableExists(ctx context.Context, conn *sql.Conn) (bool, error) {
	var n int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'briefing_snapshots'`,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("db.reconcileForkLineage: probe: %w", err)
	}
	return n > 0, nil
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
