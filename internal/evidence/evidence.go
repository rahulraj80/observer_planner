// Package evidence provides the historical-evidence substrate that powers both
// Ask-1 (Model Fit) and Ask-3 (evidence-driven dispatch): per (kind × model ×
// provider) quality/cost/latency statistics over a window (docs/design/
// api-intelligence.md §5.3).
package evidence

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/routing"
	"github.com/marmutapp/superbased-observer/internal/turnkind"
)

// Query specifies the parameters for an evidence query.
type Query struct {
	Since      time.Time
	Until      time.Time
	Kind       string   // turn.Kind (plan, edit, read_only, test_run, housekeeping, subagent, long_context, unknown)
	Model      string   // exact model filter
	Provider   string   // exact provider filter
	Tool       string   // exact tool filter
	Project    string   // exact project filter
	SessionIDs []string // optional session filter
	GroupBy    string   // "kind_model_provider" | "kind_model" | "kind" | "model_provider" | "day_kind"
	Limit      int      // max rows (0 = no limit)
}

// Row is one aggregated evidence row.
type Row struct {
	Kind                string
	Model               string
	Provider            string
	Tool                string
	WindowBucket        time.Time // bucket start (day/hour depending on GroupBy)
	N                   int64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	CostUSD             float64
	TTFTP50             int64 // milliseconds
	TTFTP75             int64
	TTFTP95             int64
	TotalMsP50          int64
	TotalMsP75          int64
	TotalMsP95          int64
	ErrorRate           float64
	EscalatedTurns      int64
	CorrectionRateEst   float64 // estimated — see docs/design/api-intelligence.md §5.3
	Source              string  // "api_turns" | "router_decisions" | "model_calibration"
}

// SelectEvidence loads aggregated evidence for a query.
// Reads api_turns (JOIN sessions/projects) UNION ALL router_decisions
// (for routed kind + applied outcome) and model_calibration. Latency
// percentiles are real nearest-rank percentiles over the api_turns timing
// columns (time_to_first_token_ms / total_response_ms) via window functions
// — the api_turns branch is the only source that carries timings, so the
// P50/P75/P95 values are computed solely from proxy-captured streaming rows
// (>0 ms, mirroring the org rollup's capture convention).
func SelectEvidence(ctx context.Context, db *sql.DB, q Query) ([]Row, error) {
	where, _ := buildWhere(q)
	since, until := q.Since.Format(time.RFC3339Nano), q.Until.Format(time.RFC3339Nano)
	//nolint:gosec // G201: query template is constant; where clauses are built from code constants, not user input
	query := fmt.Sprintf(`
		WITH base AS (
			-- api_turns: primary source, deduped by request_id
			SELECT
				COALESCE(at.turn_kind, '')                     AS kind,
				at.model                                       AS model,
				COALESCE(at.provider, '')                      AS provider,
				s.tool                                         AS tool,
				DATE(at.timestamp)                             AS window_bucket,
				1                                              AS n,
				COALESCE(at.input_tokens, 0)                   AS input_tokens,
				COALESCE(at.output_tokens, 0)                  AS output_tokens,
				COALESCE(at.cache_read_tokens, 0)              AS cache_read_tokens,
				COALESCE(at.cache_creation_tokens, 0)          AS cache_creation_tokens,
				COALESCE(at.cost_usd, 0)                       AS cost_usd,
				at.time_to_first_token_ms                      AS ttft_ms,
				at.total_response_ms                           AS total_ms,
				CASE WHEN at.error_class != '' THEN 1 ELSE 0 END AS is_error,
				0                                              AS escalated,
				0                                              AS correction_est,
				'api_turns'                                    AS source
			FROM api_turns at
			LEFT JOIN sessions s ON at.session_id = s.id
			LEFT JOIN projects p ON p.id = at.project_id
			WHERE at.timestamp >= ? AND at.timestamp < ?
			%s

			UNION ALL

			-- router_decisions: routed turns with applied kind and outcome
			SELECT
				rd.turn_kind                                   AS kind,
				rd.selected_model                              AS model,
				''                                             AS provider,
				s.tool                                         AS tool,
				DATE(rd.ts)                                    AS window_bucket,
				1                                              AS n,
				0                                              AS input_tokens,
				0                                              AS output_tokens,
				0                                              AS cache_read_tokens,
				0                                              AS cache_creation_tokens,
				COALESCE(rd.est_savings_usd, 0)                AS cost_usd,
				NULL                                           AS ttft_ms,
				NULL                                           AS total_ms,
				0                                              AS is_error,
				CASE WHEN rd.applied THEN 1 ELSE 0 END         AS escalated,
				0                                              AS correction_est,
				'router_decisions'                             AS source
			FROM router_decisions rd
			LEFT JOIN sessions s ON rd.session_id = s.id
			LEFT JOIN projects p ON p.id = s.project_id
			WHERE rd.ts >= ? AND rd.ts < ?
			%s

			UNION ALL

			-- model_calibration: calibration samples per kind×model
			SELECT
				mc.turn_kind                                   AS kind,
				mc.model                                       AS model,
				''                                             AS provider,
				''                                             AS tool,
				DATE(mc.computed_at)                           AS window_bucket,
				mc.n                                           AS n,
				0                                              AS input_tokens,
				0                                              AS output_tokens,
				0                                              AS cache_read_tokens,
				0                                              AS cache_creation_tokens,
				COALESCE(mc.cost_usd_total, 0)                 AS cost_usd,
				NULL                                           AS ttft_ms,
				NULL                                           AS total_ms,
				mc.error_count                                 AS is_error,
				0                                              AS escalated,
				0                                              AS correction_est,
				'model_calibration'                            AS source
			FROM model_calibration mc
			WHERE mc.computed_at >= ? AND mc.computed_at < ?
			%s
		),
		ttft AS (
			-- api_turns rows carrying a real TTFT observation, ranked within
			-- their group so the nearest-rank percentiles can be picked below.
			SELECT kind, model, provider, tool, window_bucket, ttft_ms AS v,
			       ROW_NUMBER() OVER (w ORDER BY ttft_ms) AS rn,
			       COUNT(*) OVER w AS n
			FROM base
			WHERE source = 'api_turns' AND ttft_ms IS NOT NULL AND ttft_ms > 0
			WINDOW w AS (PARTITION BY kind, model, provider, tool, window_bucket)
		),
		total AS (
			SELECT kind, model, provider, tool, window_bucket, total_ms AS v,
			       ROW_NUMBER() OVER (w ORDER BY total_ms) AS rn,
			       COUNT(*) OVER w AS n
			FROM base
			WHERE source = 'api_turns' AND total_ms IS NOT NULL AND total_ms > 0
			WINDOW w AS (PARTITION BY kind, model, provider, tool, window_bucket)
		),
		pcts AS (
			SELECT t.kind, t.model, t.provider, t.tool, t.window_bucket,
			       MAX(CASE WHEN t.rn = CAST(0.5*(t.n-1)+0.5 AS INT)+1   THEN t.v END) AS ttft_p50,
			       MAX(CASE WHEN t.rn = CAST(0.75*(t.n-1)+0.5 AS INT)+1  THEN t.v END) AS ttft_p75,
			       MAX(CASE WHEN t.rn = CAST(0.95*(t.n-1)+0.5 AS INT)+1  THEN t.v END) AS ttft_p95,
			       MAX(CASE WHEN o.rn = CAST(0.5*(o.n-1)+0.5 AS INT)+1   THEN o.v END) AS total_p50,
			       MAX(CASE WHEN o.rn = CAST(0.75*(o.n-1)+0.5 AS INT)+1  THEN o.v END) AS total_p75,
			       MAX(CASE WHEN o.rn = CAST(0.95*(o.n-1)+0.5 AS INT)+1  THEN o.v END) AS total_p95
			FROM ttft t
			LEFT JOIN total o USING (kind, model, provider, tool, window_bucket)
			GROUP BY t.kind, t.model, t.provider, t.tool, t.window_bucket
		),
		agg AS (
			SELECT
				kind, model, provider, tool, window_bucket,
				SUM(n) AS n,
				SUM(input_tokens) AS input_tokens,
				SUM(output_tokens) AS output_tokens,
				SUM(cache_read_tokens) AS cache_read_tokens,
				SUM(cache_creation_tokens) AS cache_creation_tokens,
				SUM(cost_usd) AS cost_usd,
				SUM(is_error) * 1.0 / NULLIF(SUM(n), 0) AS error_rate,
				SUM(escalated) AS escalated_turns,
				SUM(correction_est) AS correction_rate_est,
				MAX(source) AS source
			FROM base
			GROUP BY kind, model, provider, tool, window_bucket
		)
		SELECT
			agg.kind, agg.model, agg.provider, agg.tool, agg.window_bucket,
			agg.n, agg.input_tokens, agg.output_tokens, agg.cache_read_tokens,
			agg.cache_creation_tokens, agg.cost_usd,
			COALESCE(p.ttft_p50, 0), COALESCE(p.ttft_p75, 0), COALESCE(p.ttft_p95, 0),
			COALESCE(p.total_p50, 0), COALESCE(p.total_p75, 0), COALESCE(p.total_p95, 0),
			agg.error_rate, agg.escalated_turns, agg.correction_rate_est, agg.source
		FROM agg
		LEFT JOIN pcts p USING (kind, model, provider, tool, window_bucket)
		ORDER BY agg.window_bucket DESC, agg.kind, agg.model
		%s
	`, where.apiTurns, where.routerDecisions, where.modelCalibration, limitClause(q.Limit))

	// Args are interleaved per branch: each branch binds its window (since,
	// until) followed by that branch's own filter values.
	args := []any{since, until}
	args = append(args, where.apiArgs...)
	args = append(args, since, until)
	args = append(args, where.routerArgs...)
	args = append(args, since, until)
	args = append(args, where.calibrationArgs...)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		var r Row
		var wb string
		if err := rows.Scan(&r.Kind, &r.Model, &r.Provider, &r.Tool, &wb,
			&r.N, &r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheCreationTokens,
			&r.CostUSD,
			&r.TTFTP50, &r.TTFTP75, &r.TTFTP95,
			&r.TotalMsP50, &r.TotalMsP75, &r.TotalMsP95,
			&r.ErrorRate, &r.EscalatedTurns, &r.CorrectionRateEst, &r.Source); err != nil {
			return nil, err
		}
		r.WindowBucket, _ = time.Parse("2006-01-02", wb)
		out = append(out, r)
	}
	return out, rows.Err()
}

type whereClauses struct {
	apiTurns         string
	routerDecisions  string
	modelCalibration string
	// Per-branch bound values, in placeholder order for that branch's WHERE.
	apiArgs         []any
	routerArgs      []any
	calibrationArgs []any
}

func buildWhere(q Query) (whereClauses, []any) {
	atClauses, atArgs := []string{}, []any{}
	if q.Kind != "" {
		atClauses = append(atClauses, "at.turn_kind = ?")
		atArgs = append(atArgs, q.Kind)
	}
	if q.Model != "" {
		atClauses = append(atClauses, "at.model = ?")
		atArgs = append(atArgs, q.Model)
	}
	if q.Provider != "" {
		atClauses = append(atClauses, "COALESCE(at.provider, '') = ?")
		atArgs = append(atArgs, q.Provider)
	}
	if q.Tool != "" {
		atClauses = append(atClauses, "s.tool = ?")
		atArgs = append(atArgs, q.Tool)
	}
	if q.Project != "" {
		atClauses = append(atClauses, "p.root_path = ?")
		atArgs = append(atArgs, q.Project)
	}
	if len(q.SessionIDs) > 0 {
		placeholders := strings.Repeat("?,", len(q.SessionIDs))
		placeholders = placeholders[:len(placeholders)-1]
		atClauses = append(atClauses, "at.session_id IN ("+placeholders+")")
		for _, id := range q.SessionIDs {
			atArgs = append(atArgs, id)
		}
	}
	atClause := ""
	if len(atClauses) > 0 {
		atClause = " AND " + strings.Join(atClauses, " AND ")
	}

	// Router decisions use the same filters but different column names.
	rdClauses, rdArgs := []string{}, []any{}
	if q.Kind != "" {
		rdClauses = append(rdClauses, "rd.turn_kind = ?")
		rdArgs = append(rdArgs, q.Kind)
	}
	if q.Model != "" {
		rdClauses = append(rdClauses, "rd.selected_model = ?")
		rdArgs = append(rdArgs, q.Model)
	}
	rdClause := ""
	if len(rdClauses) > 0 {
		rdClause = " AND " + strings.Join(rdClauses, " AND ")
	}

	// Model calibration filters (no provider/tool/project columns on the table).
	mcClauses, mcArgs := []string{}, []any{}
	if q.Kind != "" {
		mcClauses = append(mcClauses, "mc.turn_kind = ?")
		mcArgs = append(mcArgs, q.Kind)
	}
	if q.Model != "" {
		mcClauses = append(mcClauses, "mc.model = ?")
		mcArgs = append(mcArgs, q.Model)
	}
	mcClause := ""
	if len(mcClauses) > 0 {
		mcClause = " AND " + strings.Join(mcClauses, " AND ")
	}

	return whereClauses{
		apiTurns:         atClause,
		routerDecisions:  rdClause,
		modelCalibration: mcClause,
		apiArgs:          atArgs,
		routerArgs:       rdArgs,
		calibrationArgs:  mcArgs,
	}, nil
}

func limitClause(limit int) string {
	if limit > 0 {
		return fmt.Sprintf(" LIMIT %d", limit)
	}
	return ""
}

// Summarize computes the evidence summary statistics from raw rows.
// This is the pure function that dashboard pages and routing quality floors consume.
func Summarize(rows []Row) Summary {
	if len(rows) == 0 {
		return Summary{}
	}

	var totalN int64
	var totalCost float64
	var totalInput, totalOutput int64
	var totalCacheRead, totalCacheCreation int64
	var totalErrors int64
	var totalEscalated int64

	byKind := make(map[string]KindSummary)
	byModel := make(map[string]ModelSummary)
	byProvider := make(map[string]ProviderSummary)

	for _, r := range rows {
		totalN += r.N
		totalCost += r.CostUSD
		totalInput += r.InputTokens
		totalOutput += r.OutputTokens
		totalCacheRead += r.CacheReadTokens
		totalCacheCreation += r.CacheCreationTokens
		totalErrors += int64(r.ErrorRate * float64(r.N))
		totalEscalated += r.EscalatedTurns

		// By kind
		ks := byKind[r.Kind]
		ks.N += r.N
		ks.CostUSD += r.CostUSD
		ks.InputTokens += r.InputTokens
		ks.OutputTokens += r.OutputTokens
		ks.CacheReadTokens += r.CacheReadTokens
		ks.CacheCreationTokens += r.CacheCreationTokens
		ks.Errors += int64(r.ErrorRate * float64(r.N))
		ks.Escalated += r.EscalatedTurns
		byKind[r.Kind] = ks

		// By model
		ms := byModel[r.Model]
		ms.N += r.N
		ms.CostUSD += r.CostUSD
		ms.InputTokens += r.InputTokens
		ms.OutputTokens += r.OutputTokens
		ms.CacheReadTokens += r.CacheReadTokens
		ms.CacheCreationTokens += r.CacheCreationTokens
		ms.Errors += int64(r.ErrorRate * float64(r.N))
		ms.Escalated += r.EscalatedTurns
		byModel[r.Model] = ms

		// By provider
		ps := byProvider[r.Provider]
		ps.N += r.N
		ps.CostUSD += r.CostUSD
		ps.InputTokens += r.InputTokens
		ps.OutputTokens += r.OutputTokens
		ps.CacheReadTokens += r.CacheReadTokens
		ps.CacheCreationTokens += r.CacheCreationTokens
		ps.Errors += int64(r.ErrorRate * float64(r.N))
		ps.Escalated += r.EscalatedTurns
		byProvider[r.Provider] = ps
	}

	return Summary{
		TotalTurns:         totalN,
		TotalCostUSD:       totalCost,
		TotalInputTokens:   totalInput,
		TotalOutputTokens:  totalOutput,
		TotalCacheRead:     totalCacheRead,
		TotalCacheCreation: totalCacheCreation,
		ErrorRate:          float64(totalErrors) / float64(totalN),
		EscalationRate:     float64(totalEscalated) / float64(totalN),
		ByKind:             byKind,
		ByModel:            byModel,
		ByProvider:         byProvider,
	}
}

// Summary is the aggregated evidence summary.
type Summary struct {
	TotalTurns         int64
	TotalCostUSD       float64
	TotalInputTokens   int64
	TotalOutputTokens  int64
	TotalCacheRead     int64
	TotalCacheCreation int64
	ErrorRate          float64
	EscalationRate     float64
	ByKind             map[string]KindSummary
	ByModel            map[string]ModelSummary
	ByProvider         map[string]ProviderSummary
}

type KindSummary struct {
	N                   int64
	CostUSD             float64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	Errors              int64
	Escalated           int64
}

type ModelSummary struct {
	N                   int64
	CostUSD             float64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	Errors              int64
	Escalated           int64
}

type ProviderSummary struct {
	N                   int64
	CostUSD             float64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	Errors              int64
	Escalated           int64
}

// KindFromRouting maps a routing.TurnKind to the evidence kind string.
func KindFromRouting(k routing.TurnKind) string {
	switch k {
	case routing.TurnPlan:
		return "plan"
	case routing.TurnReadOnly:
		return "read_only"
	case routing.TurnEdit:
		return "edit"
	case routing.TurnTestRun:
		return "test_run"
	case routing.TurnHousekeeping:
		return "housekeeping"
	case routing.TurnSubagent:
		return "subagent"
	case routing.TurnLongContext:
		return "long_context"
	default:
		return "unknown"
	}
}

// KindFromTurnkind maps a turnkind.Kind to the evidence kind string.
func KindFromTurnkind(k turnkind.Kind) string {
	return string(k)
}
