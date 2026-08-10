package rollup

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/marmutapp/superbased-observer/internal/briefing"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
)

// briefing.go is the org-server side of the Daily Briefing (Phase C of
// docs/design/standup-briefing.md §4): a team-scoped daily briefing rollup
// over the org server's own api_turns + token_usage tables, composed with the
// SAME pure composer the node dashboard uses (internal/briefing), so a manager
// sees the exact KPI vocabulary (sessions, turns, spend, cache hit rate,
// opportunities) their developers' nodes report. Content-free by construction:
// the loader reads only counts/tokens/costs/model/tool/project_hash columns —
// never prompts, turn bodies, or snapshot content — and every project key is
// the sha256 project_root_hash the nodes already ship.

// Briefing composes the org daily briefing for one UTC day (the handler
// resolves the date; default is the most recent completed UTC day), restricted
// to Scope (admin = whole org, lead = led teams, filtered on the SCIM user_id
// the ingest layer stamps on every row). It mirrors the node-side facts scan
// (dashboard brief/facts loader): the same api_turns + token_usage union with
// the proxy-origin dedup, priced from the RECORDED cost the nodes shipped
// (never re-priced), with the baked-in list-price engine only as the fallback
// for the composer's counterfactual reprice cards.
func Briefing(ctx context.Context, db *sql.DB, day time.Time, scope Scope, now time.Time) (briefing.Briefing, error) {
	since := day.UTC().Truncate(24 * time.Hour)
	until := since.AddDate(0, 0, 1)
	sinceArg := since.Format(time.RFC3339Nano)
	untilArg := until.Format(time.RFC3339Nano)

	atScope, atArgs := userScopeSQL("at.user_id", scope)
	tuScope, tuArgs := userScopeSQL("tu.user_id", scope)

	//nolint:gosec // G202: the query skeleton + userScopeSQL fragments are code constants; every value is a ? arg.
	q := `WITH proxy_turn_ids AS (
		SELECT request_id FROM api_turns
		 WHERE request_id IS NOT NULL AND request_id != ''
		   AND timestamp >= ? AND timestamp < ?
	),
	combined AS (
		SELECT at.session_id, COALESCE(sj.tool, '') AS tool, at.model,
		       at.input_tokens, at.output_tokens,
		       at.cache_read_tokens, at.cache_creation_tokens, at.cache_creation_1h_tokens,
		       0 AS reasoning_tokens, at.web_search_requests, at.timestamp,
		       at.cost_usd, at.http_status, at.error_class,
		       0 AS fast, COALESCE(at.project_root_hash, '') AS project_hash
		FROM api_turns at
		LEFT JOIN sessions sj ON sj.id = at.session_id AND sj.user_id = at.user_id
		WHERE at.timestamp >= ? AND at.timestamp < ? AND ` + atScope + `
		UNION ALL
		SELECT tu.session_id, COALESCE(tu.tool, sj.tool, '') AS tool, tu.model,
		       tu.input_tokens, tu.output_tokens,
		       tu.cache_read_tokens, tu.cache_creation_tokens, tu.cache_creation_1h_tokens,
		       tu.reasoning_tokens, tu.web_search_requests, tu.timestamp,
		       tu.estimated_cost_usd, NULL, NULL,
		       0 AS fast, COALESCE(tu.project_root_hash, '') AS project_hash
		FROM token_usage tu
		LEFT JOIN sessions sj ON sj.id = tu.session_id AND sj.user_id = tu.user_id
		WHERE tu.timestamp >= ? AND tu.timestamp < ? AND ` + tuScope + `
		  AND (tu.source_event_id IS NULL OR tu.source_event_id = ''
		       OR tu.source_event_id NOT IN (SELECT request_id FROM proxy_turn_ids))
	)
	SELECT COALESCE(session_id, ''), COALESCE(tool, ''), COALESCE(model, ''),
	       COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
	       COALESCE(cache_read_tokens, 0), COALESCE(cache_creation_tokens, 0),
	       COALESCE(cache_creation_1h_tokens, 0), COALESCE(reasoning_tokens, 0),
	       COALESCE(web_search_requests, 0), timestamp, COALESCE(cost_usd, 0),
	       COALESCE(http_status, 0), COALESCE(error_class, ''), COALESCE(fast, 0)
	FROM combined
	ORDER BY timestamp`

	args := []any{sinceArg, untilArg}
	args = append(args, sinceArg, untilArg)
	args = append(args, atArgs...)
	args = append(args, sinceArg, untilArg)
	args = append(args, tuArgs...)

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return briefing.Briefing{}, fmt.Errorf("rollup.Briefing: scan: %w", err)
	}
	defer rows.Close()

	engine := cost.NewEngine(config.IntelligenceConfig{})
	f := briefing.Facts{
		Window:      briefing.Window{Since: since, Until: until},
		Basis:       briefing.BasisList,
		GeneratedAt: now,
		Date:        since.Format("2006-01-02"),
		Price:       func(model string, b cost.TokenBundle) (float64, bool) { return engine.Compute(model, b) },
		Thresholds:  briefing.DefaultThresholds(),
	}

	bySession := map[string]*briefing.SessionFacts{}
	var sessionOrder []string
	for rows.Next() {
		var (
			sessionID, tool, model, tsStr, errorClass string
			bundle                                    cost.TokenBundle
			recorded                                  float64
			httpStatus                                int64
			fast                                      int64
		)
		if err := rows.Scan(&sessionID, &tool, &model,
			&bundle.Input, &bundle.Output, &bundle.CacheRead, &bundle.CacheCreation, &bundle.CacheCreation1h,
			&bundle.Reasoning, &bundle.WebSearchRequests, &tsStr, &recorded, &httpStatus, &errorClass, &fast); err != nil {
			return briefing.Briefing{}, fmt.Errorf("rollup.Briefing: scan row: %w", err)
		}
		ts, _ := time.Parse(time.RFC3339Nano, tsStr)
		f.Turns = append(f.Turns, briefing.Turn{
			SessionID: sessionID,
			Tool:      tool,
			Model:     model,
			Timestamp: ts,
			Tokens:    bundle,
			CostUSD:   recorded,
			Error:     httpStatus >= 400 || errorClass != "",
			Fast:      fast != 0,
		})

		sf, ok := bySession[sessionID]
		if !ok {
			sf = &briefing.SessionFacts{ID: sessionID, Models: map[string]cost.TokenBundle{}, ModelTurns: map[string]int{}}
			bySession[sessionID] = sf
			sessionOrder = append(sessionOrder, sessionID)
		}
		sf.Tool = tool
		sf.Turns++
		sf.CostUSD += recorded
		if httpStatus >= 400 || errorClass != "" {
			sf.Errors++
		}
		sf.Models[model] = addBundle(sf.Models[model], bundle)
		sf.ModelTurns[model]++
	}
	if err := rows.Err(); err != nil {
		return briefing.Briefing{}, fmt.Errorf("rollup.Briefing: rows: %w", err)
	}
	for _, id := range sessionOrder {
		f.Sessions = append(f.Sessions, *bySession[id])
	}

	return briefing.Compose(f), nil
}

// BriefingAggregate is one content-free aggregate bucket of the pushed
// Daily-Briefing digest (Phase C of docs/design/standup-briefing.md §4): per
// (day, tool, project_hash) counts + sums, optionally per developer. It is the
// dashboard-facing projection of the briefing_aggregates table (the node's
// [org_client.share] briefing_aggregates opt-in) — attribution is the node's
// user_email, cost/token sums exactly as the node composed them.
type BriefingAggregate struct {
	Day          string  `json:"day"`
	Tool         string  `json:"tool"`
	ProjectHash  string  `json:"project_hash,omitempty"`
	UserEmail    string  `json:"user_email,omitempty"`
	Sessions     int64   `json:"sessions"`
	Turns        int64   `json:"turns"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// BriefingAggregates returns the pushed Daily-Briefing aggregate buckets for
// the UTC day the server's clock considers most-recently-completed (day).
// Content-free by construction: the pushed rows carry only counts + sums keyed
// by (day, tool, project_hash) + the node's user_email.
func BriefingAggregates(ctx context.Context, db *sql.DB, day time.Time) ([]BriefingAggregate, error) {
	since := day.UTC().Truncate(24 * time.Hour)
	until := since.AddDate(0, 0, 1)
	q := `SELECT day, tool, COALESCE(project_hash, ''), COALESCE(user_email, ''),
	             sessions, turns, input_tokens, output_tokens, cost_usd
	        FROM briefing_aggregates
	       WHERE day >= ? AND day < ?
	       ORDER BY day ASC, cost_usd DESC`
	rows, err := db.QueryContext(ctx, q, since.Format("2006-01-02"), until.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("rollup.BriefingAggregates: %w", err)
	}
	defer rows.Close()

	var out []BriefingAggregate
	for rows.Next() {
		var r BriefingAggregate
		if err := rows.Scan(&r.Day, &r.Tool, &r.ProjectHash, &r.UserEmail,
			&r.Sessions, &r.Turns, &r.InputTokens, &r.OutputTokens, &r.CostUSD); err != nil {
			return nil, fmt.Errorf("rollup.BriefingAggregates: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rollup.BriefingAggregates: rows: %w", err)
	}
	return out, nil
}

// addBundle merges a per-turn bundle into a session's aggregated bundle.
func addBundle(a, b cost.TokenBundle) cost.TokenBundle {
	return cost.TokenBundle{
		Input:             a.Input + b.Input,
		Output:            a.Output + b.Output,
		CacheRead:         a.CacheRead + b.CacheRead,
		CacheCreation:     a.CacheCreation + b.CacheCreation,
		CacheCreation1h:   a.CacheCreation1h + b.CacheCreation1h,
		Reasoning:         a.Reasoning + b.Reasoning,
		WebSearchRequests: a.WebSearchRequests + b.WebSearchRequests,
		Fast:              a.Fast || b.Fast,
	}
}
