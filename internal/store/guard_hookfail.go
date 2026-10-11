package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// HookFailureCount is one (tool, outcome, failure kind) count of the gate
// hook failures the daemon drained from the hook failure spool into
// guard_events (hook-parity T-049): a fail-closed block is an enforced
// guard_error row, a fail-open failure a non-enforced one, the failure kind
// in degraded_from.
type HookFailureCount struct {
	Tool string
	// FailClosed is true for blocks (enforced rows), false for fail-open
	// failures.
	FailClosed bool
	Kind       string
	Count      int
	LastAt     time.Time
}

// HookFailureCounts counts guard_error rows whose degraded_from is one of
// kinds (the hook failure kinds, guardposture.Kind*) since since, grouped
// by tool, outcome and kind. Read-only.
func (s *Store) HookFailureCounts(ctx context.Context, ruleID string, kinds []string, since time.Time) ([]HookFailureCount, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",")
	//nolint:gosec // G202: the placeholder list is built from len(kinds); every value binds via ?.
	q := `SELECT COALESCE(tool,''), enforced, COALESCE(degraded_from,''), COUNT(*), MAX(ts)
  FROM guard_events
 WHERE rule_id = ? AND degraded_from IN (` + ph + `) AND ts >= ?
 GROUP BY tool, enforced, degraded_from
 ORDER BY tool, enforced DESC, degraded_from`
	args := []any{ruleID}
	for _, k := range kinds {
		args = append(args, k)
	}
	args = append(args, timestamp(since))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store.HookFailureCounts: %w", err)
	}
	defer rows.Close()
	var out []HookFailureCount
	for rows.Next() {
		var (
			c        HookFailureCount
			enforced int
			last     string
		)
		if err := rows.Scan(&c.Tool, &enforced, &c.Kind, &c.Count, &last); err != nil {
			return nil, fmt.Errorf("store.HookFailureCounts: scan: %w", err)
		}
		c.FailClosed = enforced != 0
		c.LastAt = parseStoredTime(last)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.HookFailureCounts: %w", err)
	}
	return out, nil
}
