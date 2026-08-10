package rollup

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// virtuous_cycle.go is the org-server side of the Virtuous-Cycle trend rollup
// (Stage 3 of docs/design/standup-briefing.md). It mirrors the node-side
// virtuouscycleorg.go but reads from the org server's own database where the
// pushed virtuous_cycle rows have been upserted. Content-free by construction:
// per (week_start) cumulative applies + projected + realized (broad filter
// from earliest apply to now). Ships only under the node-side
// [org_client.share] virtuous_cycle opt-in.

// SelectVirtuousCycle returns the cumulative virtuous-cycle trend buckets
// from the org server's database for the trailing windowDays ending at now.
// Each row represents one week (Monday-start) with cumulative applies,
// projected monthly savings, and realized savings measured from the earliest
// apply to now (broad filter — no mode/policy filter to avoid double-count).
func SelectVirtuousCycle(ctx context.Context, db *sql.DB, windowDays int, now time.Time) ([]orgcontract.VirtuousCycleRow, error) {
	if windowDays <= 0 {
		windowDays = 90
	}
	since := now.UTC().AddDate(0, 0, -windowDays).Truncate(24 * time.Hour)
	until := now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)

	//nolint:gosec // G202: static SQL skeleton; all values are ? args.
	q := `SELECT org_id, user_email, week_start, applies, projected_usd, realized_usd, realized_turns
	        FROM virtuous_cycle
	       WHERE week_start >= ? AND week_start < ?
	       ORDER BY week_start ASC`

	rows, err := db.QueryContext(ctx, q, since.Format("2006-01-02"), until.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("rollup.SelectVirtuousCycle: query: %w", err)
	}
	defer rows.Close()

	var out []orgcontract.VirtuousCycleRow
	for rows.Next() {
		var r orgcontract.VirtuousCycleRow
		if err := rows.Scan(&r.OrgID, &r.UserEmail, &r.WeekStart, &r.Applies, &r.ProjectedUSD, &r.RealizedUSD, &r.RealizedTurns); err != nil {
			return nil, fmt.Errorf("rollup.SelectVirtuousCycle: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rollup.SelectVirtuousCycle: rows: %w", err)
	}
	return out, nil
}
