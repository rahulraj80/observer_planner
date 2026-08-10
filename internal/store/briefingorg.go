package store

import (
	"context"
	"fmt"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// briefingorg.go is the node side of the Daily-Briefing AGGREGATE push (Phase C
// of docs/design/standup-briefing.md §4). It lives OUTSIDE orgpush.go on
// purpose: the privacy sentinel (tests/invariant/privacy_test.go) forbids
// orgpush.go from naming any node-local briefing table, and this file is the
// single place the node-local read for the briefing aggregate is owned — the
// same file-boundary rule that keeps the obs_* reads inside internal/obs.
//
// The aggregate is CONTENT-FREE: per (local-day, tool, project_hash) counts +
// sums, composed from the same api_turns + token_usage scan the dashboard
// briefing facts use (briefing.go::loadBriefingFacts, proxy-origin dedup
// included), and keyed by root_path_hash exactly like sessions/api_turns
// already ship. It never reads briefing_snapshots or analysis_calls, never
// carries a prompt/turn/snapshot body, and ships only under the node-side
// [org_client.share] briefing_aggregates opt-in.

// briefingAggWindowDays is the number of COMPLETED local days the aggregate
// covers. Completed-only means a day bucket is immutable once closed, so a
// re-pushed window is exactly idempotent at the server (INSERT OR IGNORE by
// the (org, user, day, tool, project_hash) natural key).
const briefingAggWindowDays = 7

// SelectBriefingAggregates returns the content-free Daily-Briefing aggregate
// buckets for the trailing briefingAggWindowDays COMPLETED local days. Day keys
// are local-machine day boundaries (the same convention the dashboard briefing
// uses), computed with the SQLite 'localtime' modifier against the UTC-stored
// timestamps — consistent with the node's own /api/briefing on the same box.
func (s *Store) SelectBriefingAggregates(ctx context.Context, windowDays int) ([]orgcontract.BriefingAggregateRow, error) {
	if windowDays <= 0 {
		windowDays = briefingAggWindowDays
	}
	localNow := time.Now().In(time.Local)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	untilLocal := today
	sinceLocal := today.AddDate(0, 0, -windowDays)
	sinceArg := sinceLocal.UTC().Format(time.RFC3339Nano)
	untilArg := untilLocal.UTC().Format(time.RFC3339Nano)

	//nolint:gosec // G202: static SQL skeleton; all values are ? args.
	q := `WITH proxy_turn_ids AS (
		SELECT request_id FROM api_turns
		 WHERE request_id IS NOT NULL AND request_id != ''
		   AND timestamp >= ? AND timestamp < ?
	),
	combined AS (
		SELECT at.session_id, COALESCE(sj.tool, '') AS tool,
		       COALESCE(pj.root_path_hash, '') AS project_hash,
		       at.input_tokens, at.output_tokens, at.cost_usd, at.timestamp
		FROM api_turns at
		LEFT JOIN sessions sj ON sj.id = at.session_id
		LEFT JOIN projects pj ON pj.id = COALESCE(at.project_id, sj.project_id)
		WHERE at.timestamp >= ? AND at.timestamp < ?
		UNION ALL
		SELECT tu.session_id, COALESCE(tu.tool, sj.tool, '') AS tool,
		       COALESCE(pj.root_path_hash, '') AS project_hash,
		       tu.input_tokens, tu.output_tokens, tu.estimated_cost_usd, tu.timestamp
		FROM token_usage tu
		LEFT JOIN sessions sj ON sj.id = tu.session_id
		LEFT JOIN projects pj ON pj.id = sj.project_id
		WHERE tu.timestamp >= ? AND tu.timestamp < ?
		  AND (tu.source_event_id IS NULL OR tu.source_event_id = ''
		       OR tu.source_event_id NOT IN (SELECT request_id FROM proxy_turn_ids))
	)
	SELECT date(timestamp, 'localtime') AS day, tool, project_hash,
	       COUNT(DISTINCT session_id) AS sessions,
	       COUNT(*) AS turns,
	       COALESCE(SUM(input_tokens), 0) AS input_tokens,
	       COALESCE(SUM(output_tokens), 0) AS output_tokens,
	       COALESCE(SUM(cost_usd), 0) AS cost_usd
	FROM combined
	WHERE tool != ''
	GROUP BY day, tool, project_hash
	ORDER BY day ASC, cost_usd DESC`

	rows, err := s.db.QueryContext(ctx, q, sinceArg, untilArg, sinceArg, untilArg, sinceArg, untilArg)
	if err != nil {
		return nil, fmt.Errorf("store.SelectBriefingAggregates: %w", err)
	}
	defer rows.Close()

	var out []orgcontract.BriefingAggregateRow
	for rows.Next() {
		var r orgcontract.BriefingAggregateRow
		if err := rows.Scan(&r.Day, &r.Tool, &r.ProjectHash,
			&r.Sessions, &r.Turns, &r.InputTokens, &r.OutputTokens, &r.CostUSD); err != nil {
			return nil, fmt.Errorf("store.SelectBriefingAggregates: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.SelectBriefingAggregates: rows: %w", err)
	}
	return out, nil
}
