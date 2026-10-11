package store

import "time"

// UTC RFC3339Nano strings omit trailing fractional zeros. Normalizing their
// precision preserves ordering at exact and fractional window boundaries;
// for example 00:00:00.1Z otherwise sorts before 00:00:00Z.
const guardBudgetTimestampOrderSQL = `substr(timestamp,1,19) || '.' ||
	CASE WHEN substr(timestamp,20,1) = '.'
	THEN substr(substr(timestamp,21,length(timestamp)-21)||'000000000',1,9)
	ELSE '000000000' END || 'Z'`

// The raw timestamp lower bound preserves the existing timestamp index for
// advisory reads. The second predicate refines the first second exactly.
// Managed reads additionally inspect malformed rows written since the window
// opened (see guardBudgetMalformedFloorSQL), which cannot be assigned to any
// window. Keeping that OR out of advisory SQL is necessary: SQLite does not
// remove a parameter-gated malformed-row scan branch.
const guardBudgetBoundedCondSQL = `((? <> '' AND session_id = ?)
	OR (timestamp >= ? AND (` + guardBudgetTimestampOrderSQL + `) >= ?))`

// guardBudgetMalformedFloorSQL is the insertion-order lower bound of the
// managed malformed-timestamp check for one usage table (T-422).
//
// WHY A BOUND AT ALL. A malformed timestamp cannot be placed in a window, so
// a managed read treats it as belonging to every window. That check used to
// run over ALL history: on a large node (about 550k token rows and 240k proxy
// turns) the julianday/strftime validation alone took about 3.5 s, past the
// guard lookup's 3 s deadline, and a fail-closed managed posture then denied
// work over rows written months before the current budget window.
//
// WHAT THE BOUND IS. Both usage tables key on INTEGER PRIMARY KEY
// AUTOINCREMENT, so id is monotonic in insertion order and never reused or
// renumbered (not even by VACUUM). The floor is the lower of:
//   - the smallest id of a row whose timestamp text lies in the window range
//     [earliest, upper), so a recent backfill of old rows cannot raise it
//     past rows written live during the window; and
//   - one past the id of the newest well-formed row dated before the window
//     (0 when there is none, which validates every row), so a malformed row
//     written before the first in-window row still counts.
//
// Every row at or above the floor is validated, which is every row written
// since the window opened, whatever bytes its timestamp holds. A malformed
// row written before that is outside every window the read can establish and
// no longer closes them. The upper bound keeps one long-ago future-dated row
// from dragging the floor back to the start of history.
//
// Cost: the first candidate is an index-only range over the timestamp index
// (the window's own rows); the second is one descending index probe; the
// validated range is a rowid range. All three are proportional to the window,
// not to history. Bound arguments are 19-char UTC (listed below).
// The unqualified timestamp inside the validity check resolves to the
// innermost FROM, the probe's own row.
//
// THE SECOND CANDIDATE ONLY LOOKS BELOW THE FIRST (review P3-38). "The newest
// row dated before the window" is chosen by timestamp, so a backfill that
// inserts an old row dated just before the window (after the last pre-window
// row written live) used to become that row, with an id from AFTER the window
// opened: the floor jumped past a malformed row written live in the window
// before its first well-formed row. The pre-window row must now also have an
// id below the first in-window row's id; a backfilled row never does. (With
// no well-formed in-window row at all there is nothing to bound it by, and
// such a backfill can still raise the floor: the residual case.)
//
// Bound arguments: earliest, upper, earliest, earliest, upper.
func guardBudgetMalformedFloorSQL(table string) string {
	return `MIN(
	  COALESCE((SELECT MIN(f.id) FROM ` + table + ` f WHERE f.timestamp >= ? AND f.timestamp < ?), 9223372036854775807),
	  COALESCE((SELECT p.id + 1 FROM ` + table + ` p WHERE p.timestamp < ? AND NOT (` + guardBudgetInvalidTimestampSQL + `)
	            AND p.id < COALESCE((SELECT MIN(w.id) FROM ` + table + ` w WHERE w.timestamp >= ? AND w.timestamp < ?), 9223372036854775807)
	            ORDER BY p.timestamp DESC LIMIT 1), 0))`
}

// guardBudgetUsageWhere is the WHERE clause of one arm of the guard's budget
// reads over table: the bounded window (plus, for a managed read, every
// malformed row at or above the arm's insertion floor), AND the arm's
// stored-dedup-verdict predicate (verdict, from internal/spendverdict: the
// rows sessionmsg.Derive counts). The window condition is parenthesized as a
// whole because its managed variant is an OR chain. Its arguments come from
// guardBudgetUsageArgs with the same managed flag.
func guardBudgetUsageWhere(managed bool, table, verdict string) string {
	cond := guardBudgetBoundedCondSQL
	if managed {
		cond = guardBudgetBoundedCondSQL + `
	OR (` + table + `.id >= ` + guardBudgetMalformedFloorSQL(table) + `
	    AND (` + guardBudgetInvalidTimestampSQL + `))`
	}
	return `
	WHERE (` + cond + `)
	  AND ` + verdict
}

// guardBudgetUsageArgs binds one guardBudgetUsageWhere arm, in order.
// earliest is the earliest window start; now anchors the floor's upper bound
// (one day past the later of now and earliest, to tolerate clock skew).
func guardBudgetUsageArgs(managed bool, sessionID string, earliest, now time.Time) []any {
	e19 := earliest.UTC().Format("2006-01-02T15:04:05")
	args := []any{sessionID, sessionID, e19, guardBudgetWindowStamp(earliest)}
	if managed {
		upper := now.UTC()
		if earliest.UTC().After(upper) {
			upper = earliest.UTC()
		}
		u19 := upper.Add(24 * time.Hour).Format("2006-01-02T15:04:05")
		args = append(args, e19, u19, e19, e19, u19)
	}
	return args
}

func guardBudgetWindowStamp(at time.Time) string {
	if at.IsZero() {
		at = time.Now()
	}
	return at.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func nextGuardBudgetWeeklyExpiry(current time.Time, stamp string, amount float64) time.Time {
	if amount <= 0 {
		return current
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil || at.IsZero() {
		return current
	}
	expiry := at.Add(7 * 24 * time.Hour)
	if current.IsZero() || expiry.Before(current) {
		return expiry
	}
	return current
}

// All canonical store writers use UTC RFC3339Nano. Managed accounting rejects
// other encodings because window comparisons are lexical. SQLite accepts dates,
// Julian numbers and overflowing dates which Go's RFC3339 parser rejects; a
// julianday-only validity check would let malformed rows disappear before
// pricing or token accounting could mark their windows unavailable.
// This constant is shared by the two static accounting queries, never built
// from caller input. Fractional seconds may contain one through nine digits.
const guardBudgetInvalidTimestampSQL = `
	 timestamp IS NULL OR julianday(timestamp) IS NULL
	 OR substr(timestamp,1,19) != strftime('%Y-%m-%dT%H:%M:%S',substr(timestamp,1,19)||'Z','+0 seconds')
	 OR substr(timestamp,1,19) = '0001-01-01T00:00:00'
	 OR substr(timestamp,12,2) NOT BETWEEN '00' AND '23'
	 OR substr(timestamp,15,2) NOT BETWEEN '00' AND '59'
	 OR substr(timestamp,18,2) NOT BETWEEN '00' AND '59'
	 OR NOT (
	   (length(timestamp) = 20 AND substr(timestamp,20,1) = 'Z')
	   OR (length(timestamp) BETWEEN 22 AND 30 AND substr(timestamp,20,1) = '.'
	       AND substr(timestamp,-1) = 'Z'
	       AND substr(timestamp,21,length(timestamp)-21) NOT GLOB '*[^0-9]*')
	 )`
