package dashboard

import (
	"net/http"
	"sort"
	"time"

	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
)

// reportScope builds the shared api_turns filter fragment for the /api/reports/*
// endpoints. The window placeholders are NOT included — each caller binds its
// own (since, until) pair first. Returns the join fragment, the AND-where
// fragment ("" when empty), and the bound filter args in placeholder order.
func reportScope(r *http.Request) (joins, whereExtra string, args []any) {
	var jb, wb []string
	if tool := r.URL.Query().Get("tool"); tool != "" {
		jb = append(jb, "LEFT JOIN sessions s ON s.id = at.session_id")
		wb = append(wb, "s.tool = ?")
		args = append(args, tool)
	}
	if project := r.URL.Query().Get("project"); project != "" {
		jb = append(jb, "LEFT JOIN projects p ON p.id = at.project_id")
		wb = append(wb, "p.root_path = ?")
		args = append(args, project)
	}
	if model := r.URL.Query().Get("model"); model != "" {
		wb = append(wb, "at.model = ?")
		args = append(args, model)
	}
	if provider := r.URL.Query().Get("provider"); provider != "" {
		wb = append(wb, "COALESCE(at.provider, '') = ?")
		args = append(args, provider)
	}
	if kind := r.URL.Query().Get("kind"); kind != "" {
		wb = append(wb, "COALESCE(at.turn_kind, 'unknown') = ?")
		args = append(args, kind)
	}
	joins = " " + joinStrings(jb, " ")
	if len(wb) > 0 {
		whereExtra = " AND " + joinStrings(wb, " AND ")
	}
	return
}

// reportWindow builds the window predicate for the report queries. A zero
// `until` (days-only request) means no upper bound — binding a zero time
// against `timestamp < ?` would match nothing, so the upper bound is omitted.
func reportWindow(since, until time.Time) (clause string, args []any) {
	clause = "at.timestamp >= ?"
	args = []any{since.Format(time.RFC3339Nano)}
	if !until.IsZero() {
		clause += " AND at.timestamp < ?"
		args = append(args, until.Format(time.RFC3339Nano))
	}
	return
}

// handleReportLatency serves /api/reports/latency — real nearest-rank latency
// percentiles (P50/P75/P95) per (kind, model, provider) over the window, plus
// the turn count, cost, and error rate per group. Only positive, non-null
// proxy-captured timings participate (time_to_first_token_ms /
// total_response_ms > 0), mirroring the org rollup's capture convention. This
// is the self-hosted standard report behind the briefing numbers section.
func (s *Server) handleReportLatency(w http.ResponseWriter, r *http.Request) {
	days := intArg(r, "days", 30, 1, 36500)
	since, until := windowRange(r, 30, 1, 36500)
	joins, where, filters := reportScope(r)
	winClause, winArgs := reportWindow(since, until)

	// Placeholder order follows text order: tt WHERE, to_ WHERE, base WHERE.
	// Each branch binds its window args then its own filter args.
	branch := func() []any {
		return append(append([]any{}, winArgs...), filters...)
	}
	args := append(branch(), branch()...)
	args = append(args, branch()...)

	//nolint:gosec // G201: reportScope + rank math are code constants; values bind via ? args.
	q := `WITH tt AS (
		SELECT COALESCE(at.turn_kind,'unknown') AS kind, at.model AS model,
		       COALESCE(at.provider,'') AS provider, at.time_to_first_token_ms AS ms
		  FROM api_turns at` + joins + `
		 WHERE ` + winClause + `
		   AND at.time_to_first_token_ms IS NOT NULL AND at.time_to_first_token_ms > 0` + where + `
	),
	ttr AS (
		SELECT kind, model, provider, ms,
		       ROW_NUMBER() OVER (w ORDER BY ms) AS rn, COUNT(*) OVER w AS n
		  FROM tt
		 WINDOW w AS (PARTITION BY kind, model, provider)
	),
	to_ AS (
		SELECT COALESCE(at.turn_kind,'unknown') AS kind, at.model AS model,
		       COALESCE(at.provider,'') AS provider, at.total_response_ms AS ms
		  FROM api_turns at` + joins + `
		 WHERE ` + winClause + `
		   AND at.total_response_ms IS NOT NULL AND at.total_response_ms > 0` + where + `
	),
	tor AS (
		SELECT kind, model, provider, ms,
		       ROW_NUMBER() OVER (w ORDER BY ms) AS rn, COUNT(*) OVER w AS n
		  FROM to_
		 WINDOW w AS (PARTITION BY kind, model, provider)
	),
	base AS (
		SELECT COALESCE(at.turn_kind,'unknown') AS kind, at.model AS model,
		       COALESCE(at.provider,'') AS provider,
		       1 AS n, COALESCE(at.cost_usd,0) AS cost,
		       CASE WHEN at.error_class != '' THEN 1 ELSE 0 END AS is_error
		  FROM api_turns at` + joins + `
		 WHERE ` + winClause + where + `
	),
	pcts AS (
		SELECT t.kind, t.model, t.provider,
		       MAX(CASE WHEN t.rn = CAST(0.5*(t.n-1)+0.5 AS INT)+1 THEN t.ms END) AS ttft_p50,
		       MAX(CASE WHEN t.rn = CAST(0.75*(t.n-1)+0.5 AS INT)+1 THEN t.ms END) AS ttft_p75,
		       MAX(CASE WHEN t.rn = CAST(0.95*(t.n-1)+0.5 AS INT)+1 THEN t.ms END) AS ttft_p95,
		       MAX(CASE WHEN o.rn = CAST(0.5*(o.n-1)+0.5 AS INT)+1 THEN o.ms END) AS total_p50,
		       MAX(CASE WHEN o.rn = CAST(0.75*(o.n-1)+0.5 AS INT)+1 THEN o.ms END) AS total_p75,
		       MAX(CASE WHEN o.rn = CAST(0.95*(o.n-1)+0.5 AS INT)+1 THEN o.ms END) AS total_p95
		  FROM ttr t
		  LEFT JOIN tor o USING (kind, model, provider)
		 GROUP BY t.kind, t.model, t.provider
	)
	SELECT b.kind, b.model, b.provider,
	       SUM(b.n) AS n, SUM(b.cost) AS cost_usd,
	       SUM(b.is_error) * 1.0 / NULLIF(SUM(b.n), 0) AS error_rate,
	       COALESCE(p.ttft_p50,0), COALESCE(p.ttft_p75,0), COALESCE(p.ttft_p95,0),
	       COALESCE(p.total_p50,0), COALESCE(p.total_p75,0), COALESCE(p.total_p95,0)
	  FROM base b
	  LEFT JOIN pcts p USING (kind, model, provider)
	 GROUP BY b.kind, b.model, b.provider
	 ORDER BY cost_usd DESC, b.model, b.kind`

	rows, err := s.db().QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()

	type cell struct {
		Kind       string  `json:"kind"`
		Model      string  `json:"model"`
		Provider   string  `json:"provider"`
		Turns      int64   `json:"turns"`
		CostUSD    float64 `json:"cost_usd"`
		ErrorRate  float64 `json:"error_rate"`
		TTFTP50Ms  int64   `json:"ttft_p50_ms"`
		TTFTP75Ms  int64   `json:"ttft_p75_ms"`
		TTFTP95Ms  int64   `json:"ttft_p95_ms"`
		TotalP50Ms int64   `json:"total_p50_ms"`
		TotalP75Ms int64   `json:"total_p75_ms"`
		TotalP95Ms int64   `json:"total_p95_ms"`
	}
	var out []cell
	for rows.Next() {
		var c cell
		if err := rows.Scan(&c.Kind, &c.Model, &c.Provider, &c.Turns, &c.CostUSD,
			&c.ErrorRate, &c.TTFTP50Ms, &c.TTFTP75Ms, &c.TTFTP95Ms,
			&c.TotalP50Ms, &c.TotalP75Ms, &c.TotalP95Ms); err != nil {
			writeErr(w, err)
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"days":  days,
		"since": since.Format(time.RFC3339),
		"until": until.Format(time.RFC3339),
		"cells": out,
		"method_note": "nearest-rank percentiles over positive proxy-captured timings; " +
			"groups without proxy timing show 0",
	})
}

// handleReportErrors serves /api/reports/errors — error rate and count per
// (kind, model, provider) over the window plus the top error classes overall.
// "Error" is any api_turns row with a non-empty error_class (migration 013);
// non-proxy turns carry no error signal and are excluded by construction.
func (s *Server) handleReportErrors(w http.ResponseWriter, r *http.Request) {
	days := intArg(r, "days", 30, 1, 36500)
	since, until := windowRange(r, 30, 1, 36500)
	joins, where, filters := reportScope(r)
	winClause, winArgs := reportWindow(since, until)

	args := append(append([]any{}, winArgs...), filters...)

	//nolint:gosec // G201: reportScope is a code constant; values bind via ? args.
	q := `SELECT COALESCE(at.turn_kind,'unknown') AS kind, at.model AS model,
	       COALESCE(at.provider,'') AS provider,
	       COUNT(*) AS n,
	       SUM(CASE WHEN at.error_class != '' THEN 1 ELSE 0 END) AS error_count,
	       SUM(CASE WHEN at.error_class != '' THEN 1 ELSE 0 END) * 1.0 / COUNT(*) AS error_rate,
	       COALESCE(SUM(at.cost_usd),0) AS cost_usd
	  FROM api_turns at` + joins + `
	 WHERE ` + winClause + where + `
	 GROUP BY kind, model, provider
	 ORDER BY error_count DESC, cost_usd DESC, model`

	rows, err := s.db().QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()

	type item struct {
		Kind       string  `json:"kind"`
		Model      string  `json:"model"`
		Provider   string  `json:"provider"`
		Turns      int64   `json:"turns"`
		ErrorCount int64   `json:"error_count"`
		ErrorRate  float64 `json:"error_rate"`
		CostUSD    float64 `json:"cost_usd"`
	}
	var (
		items                   []item
		totalTurns, totalErrors int64
	)
	for rows.Next() {
		var i item
		if err := rows.Scan(&i.Kind, &i.Model, &i.Provider, &i.Turns,
			&i.ErrorCount, &i.ErrorRate, &i.CostUSD); err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, i)
		totalTurns += i.Turns
		totalErrors += i.ErrorCount
	}
	if err := rows.Err(); err != nil {
		writeErr(w, err)
		return
	}

	// Top error classes overall (same scope).
	//nolint:gosec // G201: reportScope is a code constant; values bind via ? args.
	clsQ := `SELECT COALESCE(at.error_class,''), COUNT(*) AS n
	  FROM api_turns at` + joins + `
	 WHERE ` + winClause + `
	   AND at.error_class != ''` + where + `
	 GROUP BY at.error_class
	 ORDER BY n DESC
	 LIMIT 10`
	clsRows, err := s.db().QueryContext(r.Context(), clsQ, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer clsRows.Close()
	type errClass struct {
		ErrorClass string `json:"error_class"`
		Count      int64  `json:"count"`
	}
	var classes []errClass
	for clsRows.Next() {
		var e errClass
		if err := clsRows.Scan(&e.ErrorClass, &e.Count); err != nil {
			writeErr(w, err)
			return
		}
		classes = append(classes, e)
	}
	if err := clsRows.Err(); err != nil {
		writeErr(w, err)
		return
	}

	var errorRate float64
	if totalTurns > 0 {
		errorRate = float64(totalErrors) / float64(totalTurns)
	}
	writeJSON(w, map[string]any{
		"days":          days,
		"since":         since.Format(time.RFC3339),
		"until":         until.Format(time.RFC3339),
		"items":         items,
		"error_classes": classes,
		"summary": map[string]any{
			"turns":       totalTurns,
			"error_count": totalErrors,
			"error_rate":  errorRate,
		},
		"method_note": "error = api_turns row with non-empty error_class; proxy-captured turns only",
	})
}

// handleReportCache serves /api/reports/cache — cache-money-saved per model
// and provider over the window. Counterfactual per turn: cost(input +=
// cache_read, cache_read = 0) − cost(actual), matching
// /api/analysis/cache-savings-trend; this report adds the per-model/provider
// breakdown the briefing and org rollup need. Proxy rows are preferred and
// JSONL token_usage rows dedup against them by request_id. token_usage carries
// no provider, so estimated rows group under provider "".
func (s *Server) handleReportCache(w http.ResponseWriter, r *http.Request) {
	days := intArg(r, "days", 30, 1, 36500)
	since, until := windowRange(r, 30, 1, 36500)
	tool := r.URL.Query().Get("tool")
	project := r.URL.Query().Get("project")
	model := r.URL.Query().Get("model")
	provider := r.URL.Query().Get("provider")

	atJoins, atWhere, atArgs := analysisScopeClause("api_turns", "at", tool, project)
	tuJoins, tuWhere, tuArgs := analysisScopeClause("token_usage", "tu", tool, project)
	if model != "" {
		atWhere += " AND at.model = ?"
		atArgs = append(atArgs, model)
		tuWhere += " AND tu.model = ?"
		tuArgs = append(tuArgs, model)
	}
	if provider != "" {
		atWhere += " AND COALESCE(at.provider, '') = ?"
		atArgs = append(atArgs, provider)
	}

	tsSince := since.Format(time.RFC3339Nano)

	//nolint:gosec // G202: SQL structure and analysisScopeClause fragments are code constants; values bind via ? args.
	q := `WITH proxy_turn_ids AS (
		SELECT request_id FROM api_turns
		 WHERE request_id IS NOT NULL AND request_id != ''
		   AND timestamp >= ?
	),
	combined AS (
		SELECT at.model, COALESCE(at.provider,'') AS provider,
		       at.input_tokens, at.output_tokens, at.cache_read_tokens,
		       at.cache_creation_tokens, at.cache_creation_1h_tokens,
		       0 AS reasoning_tokens,
		       at.web_search_requests, at.timestamp,
		       at.cost_usd
		FROM api_turns at` + atJoins + `
		WHERE at.timestamp >= ?` + atWhere + `
		UNION ALL
		SELECT tu.model, '',
		       tu.input_tokens, tu.output_tokens, tu.cache_read_tokens,
		       tu.cache_creation_tokens, tu.cache_creation_1h_tokens,
		       tu.reasoning_tokens,
		       tu.web_search_requests, tu.timestamp,
		       tu.estimated_cost_usd
		FROM token_usage tu` + tuJoins + `
		WHERE tu.timestamp >= ?` + tuWhere + `
		  AND (tu.source_event_id IS NULL OR tu.source_event_id = ''
		       OR tu.source_event_id NOT IN (SELECT request_id FROM proxy_turn_ids))
	)
	SELECT COALESCE(model, ''),
	       COALESCE(provider, ''),
	       COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
	       COALESCE(cache_read_tokens, 0), COALESCE(cache_creation_tokens, 0),
	       COALESCE(cache_creation_1h_tokens, 0),
	       COALESCE(reasoning_tokens, 0),
	       COALESCE(web_search_requests, 0),
	       timestamp,
	       COALESCE(cost_usd, 0)
	FROM combined`

	args := []any{tsSince, tsSince}
	args = append(args, atArgs...)
	args = append(args, tsSince)
	args = append(args, tuArgs...)
	rows, err := s.db().QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()

	type cell struct {
		Model            string  `json:"model"`
		Provider         string  `json:"provider"`
		Turns            int64   `json:"turns"`
		SavingsUSD       float64 `json:"savings_usd"`
		CacheReadTokens  int64   `json:"cache_read_tokens"`
		CacheWriteTokens int64   `json:"cache_write_tokens"`
	}
	byKey := map[string]*cell{}
	var totalSavings float64
	var totalRead int64

	for rows.Next() {
		var (
			model, provider string
			bundle          cost.TokenBundle
			tsStr           string
			recorded        float64
		)
		if err := rows.Scan(&model, &provider,
			&bundle.Input, &bundle.Output,
			&bundle.CacheRead, &bundle.CacheCreation, &bundle.CacheCreation1h,
			&bundle.Reasoning,
			&bundle.WebSearchRequests,
			&tsStr, &recorded); err != nil {
			writeErr(w, err)
			return
		}
		if bundle.CacheRead <= 0 || recorded > 0 {
			continue
		}
		p, ok := s.opts.CostEngine.Lookup(model)
		if !ok {
			continue
		}
		actual := cost.Compute(p, bundle)
		alt := bundle
		alt.Input += bundle.CacheRead
		alt.CacheRead = 0
		altCost := cost.Compute(p, alt)
		savings := altCost - actual
		if savings <= 0 {
			continue
		}

		key := model + "\x00" + provider
		c := byKey[key]
		if c == nil {
			c = &cell{Model: model, Provider: provider}
			byKey[key] = c
		}
		c.Turns++
		c.SavingsUSD += savings
		c.CacheReadTokens += bundle.CacheRead
		c.CacheWriteTokens += bundle.CacheCreation
		totalSavings += savings
		totalRead += bundle.CacheRead
	}
	if err := rows.Err(); err != nil {
		writeErr(w, err)
		return
	}

	out := make([]cell, 0, len(byKey))
	for _, c := range byKey {
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SavingsUSD > out[j].SavingsUSD })

	writeJSON(w, map[string]any{
		"days":     days,
		"since":    since.Format(time.RFC3339),
		"until":    until.Format(time.RFC3339),
		"by_model": out,
		"summary": map[string]any{
			"turns":             int64(len(out)),
			"savings_usd":       totalSavings,
			"cache_read_tokens": totalRead,
		},
		"method_note": "per-turn counterfactual cost(input+cache_read, cache_read=0) − cost(actual); " +
			"token_usage rows have no provider so group under ''",
	})
}
