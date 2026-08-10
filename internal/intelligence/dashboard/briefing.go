package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/briefing"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/configapply"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
	"github.com/marmutapp/superbased-observer/internal/intelligence/learn"
	"github.com/marmutapp/superbased-observer/internal/llmrun"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// briefing.go is the dashboard side of the daily briefing product
// (docs/design/standup-briefing.md, Phase A): the FACTS loader (the one
// place that scans api_turns + token_usage and prices per-turn through the
// cost engine), the /api/briefing* handlers, and the snapshot persistence
// wiring. The pure composition lives in internal/briefing.

// briefingQueryOptions carries the /api/briefing query params.
type briefingQueryOptions struct {
	date    string // YYYY-MM-DD override (default: most recent completed local day)
	tool    string // activity filter
	project string // activity filter (project root path)
	basis   string // list (default) | effective
	window  string // 1d|7d|28d|weekly|monthly (default: daily)
}

// basisOf maps the ?basis= query value to the composer's Basis; anything
// other than "effective" resolves to list.
func basisOf(raw string) briefing.Basis {
	if raw == "effective" {
		return briefing.BasisEffective
	}
	return briefing.BasisList
}

// parseBriefingWindowDays maps the ?window= value to a completed-day count.
// "" and "1d" are the daily briefing (unchanged); 7d/week/weekly → 7 and
// 28d/4w/month/monthly → 28 are the weekly/monthly modes; a bare integer (or
// N-d/N-w suffix) is accepted as an explicit day count, capped at 90.
func parseBriefingWindowDays(raw string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "1d", "day", "daily":
		return 1, nil
	case "7d", "7", "week", "weekly":
		return 7, nil
	case "28d", "4w", "month", "monthly":
		return 28, nil
	}
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	mult := 1
	if strings.HasSuffix(trimmed, "d") {
		trimmed = strings.TrimSuffix(trimmed, "d")
	} else if strings.HasSuffix(trimmed, "w") {
		trimmed = strings.TrimSuffix(trimmed, "w")
		mult = 7
	}
	n, err := strconv.Atoi(trimmed)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("briefing window %q: want 1d|7d|28d|weekly|monthly or a day count 1..90", raw)
	}
	n *= mult
	if n > 90 {
		return 0, fmt.Errorf("briefing window %q: want 1d|7d|28d|weekly|monthly or a day count 1..90", raw)
	}
	return n, nil
}

// briefingWindow resolves the briefing window. The default is the most recent
// COMPLETED local-machine window (decision 2026-08-01): the last `days`
// full local days, ending at today's local midnight — so a 9am stand-up shows
// yesterday (days=1), the last week (days=7), or the last 28 days (days=28),
// never an in-progress day. A ?date=YYYY-MM-DD override is parsed in the
// local zone as the window's END day, so the same stored day reads back
// identically. Storage stays UTC; only the day-boundary arithmetic is local.
func briefingWindow(nowUTC time.Time, dateRaw string, days int) (date string, since, until time.Time, err error) {
	localNow := nowUTC.In(time.Local)
	if days < 1 {
		days = 1
	}
	if dateRaw != "" {
		t, perr := time.ParseInLocation("2006-01-02", dateRaw, time.Local)
		if perr != nil {
			return "", time.Time{}, time.Time{}, fmt.Errorf("briefing date %q: %w", dateRaw, perr)
		}
		// date is the END day: window = [date-(days-1), date+1).
		return dateRaw, t.AddDate(0, 0, -(days - 1)), t.AddDate(0, 0, 1), nil
	}
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	since = today.AddDate(0, 0, -days)
	return since.AddDate(0, 0, days-1).Format("2006-01-02"), since, today, nil
}

// loadBriefingFacts assembles the pure composer's input bundle for the
// window: one deduped per-turn scan of api_turns + token_usage (the same
// proxy-origin-dedup the analysis headline uses), priced per-turn through
// the cost engine with long-context + cache-savings attribution, aggregated
// into sessions, plus the 1-day routing advise-shadow signal (best-effort).
func (s *Server) loadBriefingFacts(ctx context.Context, opts briefingQueryOptions) (briefing.Facts, error) {
	days, err := parseBriefingWindowDays(opts.window)
	if err != nil {
		return briefing.Facts{}, err
	}
	date, since, until, err := briefingWindow(s.now(), opts.date, days)
	if err != nil {
		return briefing.Facts{}, err
	}
	basis := briefing.BasisList
	if opts.basis == "effective" {
		basis = briefing.BasisEffective
	}
	engine := s.opts.CostEngine
	if engine == nil {
		engine = cost.NewEngine(config.IntelligenceConfig{})
	}

	turns, err := s.scanBriefingTurns(ctx, since, until, opts, engine)
	if err != nil {
		return briefing.Facts{}, err
	}

	f := briefing.Facts{
		Window:      briefing.Window{Since: since.UTC(), Until: until.UTC()},
		Basis:       basis,
		GeneratedAt: s.now(),
		Date:        date,
		WindowDays:  days,
		Turns:       turns,
		Price:       briefingPriceFn(engine),
		Thresholds:  briefing.DefaultThresholds(),
	}

	// Multi-day mode compares against the same-length window immediately
	// before [since, until). Single-day briefings carry no trend block.
	if days > 1 {
		priorSince := since.AddDate(0, 0, -days)
		priorTurns, perr := s.scanBriefingTurns(ctx, priorSince, since, opts, engine)
		if perr != nil {
			return briefing.Facts{}, perr
		}
		priorGlance := briefing.GlanceFromTurns(priorTurns)
		f.Prior = &priorGlance
	}

	bySession := map[string]*briefing.SessionFacts{}
	var sessionOrder []string
	for _, turn := range f.Turns {
		sf, ok := bySession[turn.SessionID]
		if !ok {
			sf = &briefing.SessionFacts{ID: turn.SessionID, Models: map[string]cost.TokenBundle{}, ModelTurns: map[string]int{}}
			bySession[turn.SessionID] = sf
			sessionOrder = append(sessionOrder, turn.SessionID)
		}
		sf.Tool = turn.Tool
		sf.Turns++
		sf.CostUSD += turn.CostUSD
		if turn.Error {
			sf.Errors++
		}
		sf.Models[turn.Model] = addTokenBundle(sf.Models[turn.Model], turn.Tokens)
		sf.ModelTurns[turn.Model]++
	}
	for _, id := range sessionOrder {
		f.Sessions = append(f.Sessions, *bySession[id])
	}

	// Routing advise-shadow: 1 rolling day, best-effort (never fails the
	// briefing). The shadow window is [now-24h, now] — approximately the
	// completed local day; exact-window shadowing is Phase A+ refinement.
	if st := store.New(s.db()); st != nil {
		if sh, serr := st.AdviseShadowSignal(ctx, 1, enginePriceFn(engine)); serr == nil && sh.AdviseDecisions > 0 {
			coverage := 0.0
			if sh.AdviseDecisions > 0 {
				coverage = float64(sh.WouldReroute) / float64(sh.AdviseDecisions)
			}
			f.Shadow = &briefing.ShadowFacts{
				PolicyName:               sh.PolicyName,
				PolicyVersion:            fmt.Sprintf("%d-day", sh.WindowDays),
				Rerouted:                 sh.WouldReroute,
				WouldSaveUSD:             sh.WouldSaveUSD,
				ExtraCostUSD:             0,
				NetSaveUSD:               sh.WouldSaveUSD,
				QualityFlags:             sh.QualityFlags,
				Coverage:                 coverage,
				StickinessHolds:          sh.StickinessHolds,
				StickinessHoldSavingsUSD: sh.StickinessHoldSavingsUSD,
			}
		}
	}

	// Realized-vs-estimated (Stage 3): every CTA apply recorded in the
	// briefing-CTA ledger within the window, matched against realized
	// routing outcomes measured after the apply. Best-effort like the
	// shadow — a missing ledger or DB never fails the briefing.
	f.RealizedVSEstimated = s.realizedVSEstimated(ctx, since, until)

	// Virtuous-cycle trend (Stage 3 item 2): cumulative history across ALL
	// recorded CTA applies, not limited to the window. Best-effort.
	f.VirtuousCycle = s.virtuousCycleFacts(ctx)
	return f, nil
}

// realizedVSEstimated (Stage 3 item 1) matches every CTA apply recorded
// in the briefing-CTA ledger against realized routing outcomes measured
// on router_decisions after the apply — the "applied X on DATE → saved
// $Y of $Z estimated" report. Measurement is wired for routing-section
// applies (the only levered detector today); applies with no measurement
// channel are skipped rather than reported as zero. Best-effort: a
// missing/unreadable ledger or a failed aggregate yields an empty set.
func (s *Server) realizedVSEstimated(ctx context.Context, since, until time.Time) []briefing.RealizedVSEstimated {
	events, _, err := s.ctaLedger().Read()
	if err != nil {
		return nil
	}
	var out []briefing.RealizedVSEstimated
	for _, ev := range events {
		if ev.Kind != "apply" || ev.Section != "routing" {
			continue
		}
		if ev.Time.Before(since) || !ev.Time.Before(until) {
			continue
		}
		// Attribute realized savings to the specific lever: the mode lever
		// counts only decisions served in the target mode (so pre-restart,
		// old-config turns never inflate it), the policy lever only the
		// target template; stickiness has no decision attribute to filter on.
		q := store.RealizedRoutingQuery{Since: ev.Time.UTC(), Until: until.UTC()}
		switch ev.Key {
		case "mode":
			q.Mode = ev.To
		case "policy":
			q.Policy = ev.To
		}
		delta, qerr := store.New(s.db()).SelectRealizedRoutingDelta(ctx, q)
		if qerr != nil {
			continue
		}
		out = append(out, briefing.RealizedVSEstimated{
			ApplyStamp:          ev.Time,
			Detector:            ev.Detector,
			ChangeID:            ev.Key,
			ConfigSurface:       ev.Section + "." + ev.Key,
			From:                ev.From,
			To:                  ev.To,
			ProjectedMonthlyUSD: ev.ProjectedUSD,
			RealizedUSD:         round2f(delta.DeltaUSD),
			RealizedTurns:       delta.GradedTurns,
			Confidence:          briefing.RealizedConfidence(delta.GradedTurns),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ApplyStamp.Before(out[j].ApplyStamp) })
	return out
}

// virtuousCycleFacts (Stage 3 item 2) builds the cumulative virtuous-cycle
// trend from ALL recorded CTA applies in the ledger. It measures the total
// realized savings from the EARLIEST apply to now (avoiding double-count
// from per-apply attribution), and aggregates by week. Best-effort: missing
// ledger/DB yields nil (no card), never fails the briefing.
func (s *Server) virtuousCycleFacts(ctx context.Context) *briefing.VirtuousCycleFacts {
	events, _, err := s.ctaLedger().Read()
	if err != nil {
		return nil
	}
	// Collect all routing applies (not just in-window)
	var applies []configapply.LedgerEvent
	for _, ev := range events {
		if ev.Kind == "apply" && ev.Section == "routing" {
			applies = append(applies, ev)
		}
	}
	if len(applies) == 0 {
		return nil
	}

	// Find earliest apply timestamp for the cumulative realized window
	earliest := applies[0].Time
	for _, ev := range applies[1:] {
		if ev.Time.Before(earliest) {
			earliest = ev.Time
		}
	}

	// Query cumulative realized from earliest apply to now (broad filter:
	// all routing modes/policies since the first apply, to avoid double-count).
	q := store.RealizedRoutingQuery{Since: earliest.UTC(), Until: s.now().UTC()}
	delta, qerr := store.New(s.db()).SelectRealizedRoutingDelta(ctx, q)
	if qerr != nil {
		return nil
	}

	// Sum projected across all applies
	var totalProjected float64
	for _, ev := range applies {
		totalProjected += ev.ProjectedUSD
	}

	// Bucket each apply by week for the trend
	byWeek := map[string]*briefing.VirtuousCycleWeek{}
	for _, ev := range applies {
		weekStart := weekStart(ev.Time)
		w := byWeek[weekStart]
		if w == nil {
			w = &briefing.VirtuousCycleWeek{WeekStart: weekStart}
			byWeek[weekStart] = w
		}
		w.Applies++
		w.ProjectedUSD += ev.ProjectedUSD
		// Note: RealizedUSD per week is NOT attributed here to avoid double-count;
		// the weekly view shows applies + projected only. Total realized is the
		// cumulative figure from earliest apply to now.
	}

	// Sort weeks chronologically
	weeks := make([]briefing.VirtuousCycleWeek, 0, len(byWeek))
	for _, w := range byWeek {
		weeks = append(weeks, *w)
	}
	sort.Slice(weeks, func(i, j int) bool { return weeks[i].WeekStart < weeks[j].WeekStart })

	return &briefing.VirtuousCycleFacts{
		TotalApplies:       len(applies),
		TotalProjectedUSD:  totalProjected,
		TotalRealizedUSD:   delta.DeltaUSD,
		TotalRealizedTurns: delta.GradedTurns,
		ByWeek:             weeks,
	}
}

// weekStart returns the Monday of the week containing t, formatted YYYY-MM-DD.
func weekStart(t time.Time) string {
	// Adjust to Monday (0 = Sunday in Go, so Monday = 1)
	daysToMonday := int(t.Weekday() - time.Monday)
	if daysToMonday < 0 {
		daysToMonday += 7
	}
	monday := t.AddDate(0, 0, -daysToMonday)
	return monday.Format("2006-01-02")
}

// scanBriefingTurns is the one per-window scan of api_turns + token_usage
// (the same proxy-origin-dedup the analysis headline uses), priced per-turn
// through the cost engine with long-context + cache-savings attribution, and
// now carrying the proxy-captured TTFT / total latency per streamed turn.
func (s *Server) scanBriefingTurns(ctx context.Context, since, until time.Time, opts briefingQueryOptions, engine *cost.Engine) ([]briefing.Turn, error) {
	sinceArg := since.UTC().Format(time.RFC3339Nano)
	untilArg := until.UTC().Format(time.RFC3339Nano)

	atJoins, atWhere, atArgs := analysisScopeClause("api_turns", "at", opts.tool, opts.project)
	tuJoins, tuWhere, tuArgs := analysisScopeClause("token_usage", "tu", opts.tool, opts.project)

	//nolint:gosec // G202: the query skeleton + analysisScopeClause fragments are code constants; every value is a ? arg.
	q := `WITH proxy_turn_ids AS (
		SELECT request_id FROM api_turns
		 WHERE request_id IS NOT NULL AND request_id != ''
		   AND timestamp >= ? AND timestamp < ?
	),
	combined AS (
		SELECT at.session_id, sj.tool AS tool, at.model, at.input_tokens, at.output_tokens,
		       at.cache_read_tokens, at.cache_creation_tokens, at.cache_creation_1h_tokens,
		       0 AS reasoning_tokens, at.web_search_requests, at.timestamp,
		       at.cost_usd, at.http_status, at.error_class, at.error_message, at.fast,
		       at.time_to_first_token_ms AS ttft_ms, at.total_response_ms AS total_ms
		FROM api_turns at
		LEFT JOIN sessions sj ON sj.id = at.session_id` + atJoins + `
		WHERE at.timestamp >= ? AND at.timestamp < ?` + atWhere + `
		UNION ALL
		SELECT tu.session_id, tu.tool, tu.model, tu.input_tokens, tu.output_tokens,
		       tu.cache_read_tokens, tu.cache_creation_tokens, tu.cache_creation_1h_tokens,
		       tu.reasoning_tokens, tu.web_search_requests, tu.timestamp,
		       tu.estimated_cost_usd, NULL, NULL, NULL, tu.fast,
		       NULL AS ttft_ms, NULL AS total_ms
		FROM token_usage tu` + tuJoins + `
		WHERE tu.timestamp >= ? AND tu.timestamp < ?` + tuWhere + `
		  AND (tu.source_event_id IS NULL OR tu.source_event_id = ''
		       OR tu.source_event_id NOT IN (SELECT request_id FROM proxy_turn_ids))
	)
	SELECT COALESCE(session_id, ''), COALESCE(tool, ''), COALESCE(model, ''),
	       COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
	       COALESCE(cache_read_tokens, 0), COALESCE(cache_creation_tokens, 0),
	       COALESCE(cache_creation_1h_tokens, 0), COALESCE(reasoning_tokens, 0),
	       COALESCE(web_search_requests, 0), timestamp, COALESCE(cost_usd, 0),
	       COALESCE(http_status, 0), COALESCE(error_class, ''), COALESCE(error_message, ''), COALESCE(fast, 0),
	       ttft_ms, total_ms
	FROM combined
	ORDER BY timestamp`

	args := []any{sinceArg, untilArg}
	args = append(args, sinceArg, untilArg)
	args = append(args, atArgs...)
	args = append(args, sinceArg, untilArg)
	args = append(args, tuArgs...)

	rows, err := s.db().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("briefing scan: %w", err)
	}
	defer rows.Close()

	var turns []briefing.Turn
	for rows.Next() {
		var (
			sessionID, tool, model, tsStr, errorClass, errorMessage string
			bundle                                                  cost.TokenBundle
			recorded                                                float64
			httpStatus                                              int64
			fast                                                    int64
			ttftMS, totalMS                                         sql.NullInt64
		)
		if err := rows.Scan(&sessionID, &tool, &model,
			&bundle.Input, &bundle.Output, &bundle.CacheRead, &bundle.CacheCreation, &bundle.CacheCreation1h,
			&bundle.Reasoning, &bundle.WebSearchRequests, &tsStr, &recorded, &httpStatus, &errorClass, &errorMessage, &fast,
			&ttftMS, &totalMS); err != nil {
			return nil, fmt.Errorf("briefing scan row: %w", err)
		}
		ts, _ := time.Parse(time.RFC3339Nano, tsStr)

		turn := briefing.Turn{
			SessionID:    sessionID,
			Tool:         tool,
			Model:        model,
			Timestamp:    ts,
			Tokens:       bundle,
			Error:        httpStatus >= 400 || errorClass != "",
			ErrorMessage: errorMessage,
			Fast:         fast != 0,
			TTFTMS:       ttftMS.Int64,
			TotalMS:      totalMS.Int64,
		}
		// Price per-turn exactly like the analysis headline loop
		// (analysis.go:179-197): recorded proxy cost wins; otherwise
		// engine-priced, LC-aware, with cache-savings attribution.
		if recorded > 0 {
			turn.CostUSD = recorded
		} else if p, ok := engine.Lookup(model); ok {
			turn.CostUSD = cost.Compute(p, bundle)
			turn.LCSurchargeUSD = cost.Compute(p, bundle) - cost.Compute(stripLongContext(p), bundle)
			if bundle.CacheRead > 0 {
				alt := bundle
				alt.Input += bundle.CacheRead
				alt.CacheRead = 0
				turn.CacheSavingsUSD = cost.Compute(p, alt) - turn.CostUSD
			}
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("briefing scan: %w", err)
	}
	return turns, nil
}

// briefingPriceFn adapts the dashboard's cost engine to the composer's pure
// PriceFn seam.
func briefingPriceFn(engine *cost.Engine) briefing.PriceFn {
	if engine == nil {
		return nil
	}
	return func(model string, b cost.TokenBundle) (float64, bool) {
		return engine.Compute(model, b)
	}
}

// addTokenBundle merges a per-turn bundle into a session's aggregated bundle.
func addTokenBundle(a, b cost.TokenBundle) cost.TokenBundle {
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

// composeBriefing builds the Briefing for the window and returns it. Only
// the single-day briefing persists its snapshot (the table is keyed by date
// UNIQUE); multi-day windows are computed on demand. The persisted snapshot
// keeps the FULL card set (so an expired suspension restores a card); the
// returned briefing is filtered for suspended cards on every read.
func (s *Server) composeBriefing(ctx context.Context, opts briefingQueryOptions) (briefing.Briefing, error) {
	days, err := parseBriefingWindowDays(opts.window)
	if err != nil {
		return briefing.Briefing{}, err
	}
	facts, err := s.loadBriefingFacts(ctx, opts)
	if err != nil {
		return briefing.Briefing{}, err
	}
	b := briefing.Compose(facts)
	if days != 1 {
		return s.filterSuspendedOpportunities(b)
	}
	blob, err := json.Marshal(b)
	if err != nil {
		return briefing.Briefing{}, fmt.Errorf("briefing marshal: %w", err)
	}
	if serr := store.New(s.db()).SaveBriefingSnapshot(ctx, b.Date, string(blob), s.now()); serr != nil {
		// Persistence is best-effort for the read surface; the JSON is
		// still served below. Only compose/lookup errors are fatal.
		if s.opts.Logger != nil {
			s.opts.Logger.Warn("briefing snapshot save failed", "err", serr)
		}
	}
	return s.filterSuspendedOpportunities(b)
}

// filterSuspendedOpportunities removes every opportunity card that is
// currently suspended (snoozed or paused) from the briefing. Suspended
// cards surface on the /api/briefing/suspensions strip instead. The
// filter runs on every read (fresh composes AND served snapshots) so a
// suspend lands immediately and an expired one restores its card.
func (s *Server) filterSuspendedOpportunities(b briefing.Briefing) (briefing.Briefing, error) {
	susp, err := s.ctaLedger().ActiveSuspensions(s.now())
	if err != nil {
		// A broken ledger must not block the briefing read — surface as
		// WARN and continue with all cards visible.
		if s.opts.Logger != nil {
			s.opts.Logger.Warn("briefing suspension filter failed; showing all cards", "err", err)
		}
		return b, nil
	}
	if len(susp) == 0 {
		return b, nil
	}
	muted := map[string]bool{}
	for _, su := range susp {
		muted[su.DedupKey] = true
	}
	kept := make([]briefing.Opportunity, 0, len(b.Opportunities))
	for _, o := range b.Opportunities {
		if !muted[o.DedupKey] {
			kept = append(kept, o)
		}
	}
	b.Opportunities = kept
	return b, nil
}

// ComposeBriefing assembles + persists the daily briefing for an explicit
// date (YYYY-MM-DD, local-machine day) — or the most recent completed local
// day when date is "". Exported so the digest scheduler (cmd/observer/digest.go)
// reuses the exact same facts scan + pricing as the web surface.
func (s *Server) ComposeBriefing(ctx context.Context, date string) (briefing.Briefing, error) {
	return s.composeBriefing(ctx, briefingQueryOptions{date: date})
}

// briefingForDate returns the last-composed snapshot for a date, composing a
// fresh one when none exists yet (a direct URL hit before the page render).
// Multi-day windows are never served from the per-day snapshot — the drill
// surfaces (opportunities/justify) recompose on demand so their numbers match
// the weekly/monthly report.
func (s *Server) briefingForDate(ctx context.Context, opts briefingQueryOptions) (briefing.Briefing, error) {
	days, err := parseBriefingWindowDays(opts.window)
	if err != nil {
		return briefing.Briefing{}, err
	}
	if days > 1 {
		return s.composeBriefing(ctx, opts)
	}
	date, _, _, err := briefingWindow(s.now(), opts.date, 1)
	if err != nil {
		return briefing.Briefing{}, err
	}
	blob, ok, err := store.New(s.db()).LoadBriefingSnapshot(ctx, date)
	if err != nil {
		return briefing.Briefing{}, err
	}
	if ok {
		var b briefing.Briefing
		if uerr := json.Unmarshal([]byte(blob), &b); uerr == nil {
			return s.filterSuspendedOpportunities(b)
		}
		// Fall through to compose on corrupt/legacy snapshot.
	}
	return s.composeBriefing(ctx, opts)
}

// handleBriefing — GET /api/briefing?date=&tool=&project=&basis=&narrative=llm :
// the full composed daily briefing. Data-only; no prompt text, no paths. The
// narrative upgrade runs ONLY when the caller explicitly requests
// ?narrative=llm AND an LLM runner is configured; any failure (including no
// configured connection) keeps the template narrative — never load-bearing.
func (s *Server) handleBriefing(w http.ResponseWriter, r *http.Request) {
	b, err := s.composeBriefing(r.Context(), briefingQueryOptions{
		date:    r.URL.Query().Get("date"),
		tool:    r.URL.Query().Get("tool"),
		project: r.URL.Query().Get("project"),
		basis:   r.URL.Query().Get("basis"),
		window:  r.URL.Query().Get("window"),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	if r.URL.Query().Get("narrative") == "llm" && s.opts.LLMRunner != nil {
		var lerr error
		b, _, lerr = s.opts.LLMRunner.Narrative(r.Context(), s.opts.LLMConns, b)
		if lerr != nil && !errors.Is(lerr, llmrun.ErrNoConnection) {
			// A real upstream failure degrades to the template narrative
			// (the accounting row is already recorded by the runner); the
			// briefing is never blocked on a model answering.
			if s.opts.Logger != nil {
				s.opts.Logger.Warn("briefing narrative failed; keeping template", "err", lerr)
			}
		}
	}
	// Open Questions upgrade — opt-in exactly like the narrative (?questions=llm):
	// the model proposes questions grounded in the same content-free evidence.
	// No configured connection (ErrNoConnection) or a failed call keeps the
	// template questions; the briefing is never blocked on a model answering.
	if r.URL.Query().Get("questions") == "llm" && s.opts.LLMRunner != nil {
		if qs, qerr := s.opts.LLMRunner.Questions(r.Context(), s.opts.LLMConns, b); qerr == nil {
			b.Questions = qs
		} else if !errors.Is(qerr, llmrun.ErrNoConnection) {
			if s.opts.Logger != nil {
				s.opts.Logger.Warn("briefing open questions failed; keeping template", "err", qerr)
			}
		}
	}
	writeJSON(w, b)
}

// handleBriefingAsk — POST /api/briefing/ask {"question", "date"} : a grounded
// answer over the briefing's evidence items only (content-free; never raw
// transcripts or paths). Requires an LLM runner + a configured connection;
// otherwise 404 with error code no_connection. A failed upstream call is 502
// with error code llm_error (after recording the accounting row).
func (s *Server) handleBriefingAsk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrStatus(w, fmt.Errorf("briefing ask: method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	if s.opts.LLMRunner == nil {
		writeErrStatus(w, llmrun.ErrNoConnection, http.StatusNotFound)
		return
	}
	var body struct {
		Question string `json:"question"`
		Date     string `json:"date"`
		Window   string `json:"window"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(&body); err != nil {
		writeErrStatus(w, fmt.Errorf("briefing ask: decode body: %w", err), http.StatusBadRequest)
		return
	}
	b, err := s.briefingForDate(r.Context(), briefingQueryOptions{date: body.Date, window: body.Window})
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := s.opts.LLMRunner.Ask(r.Context(), s.opts.LLMConns, body.Question, b)
	if err != nil {
		if errors.Is(err, llmrun.ErrNoConnection) {
			writeErrStatus(w, err, http.StatusNotFound)
			return
		}
		writeErrStatus(w, fmt.Errorf("briefing ask: %w", err), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

// defaultPastWindowDays is the ask-past retrieval window (the trailing year).
const defaultPastWindowDays = 365

// maxPastWindowDays caps the ask-past retrieval window (10 years).
const maxPastWindowDays = 3650

// parsePastWindowDays maps the ask-past window value to a day count: "" → the
// trailing-year default; "7d"/"7"/"week" or an explicit N-d/N-w/N-y count.
// Unlike the briefing window, ask-past has no 90-day cap. Unparseable input
// falls back to the default.
func parsePastWindowDays(raw string) int {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "year", "1y":
		return defaultPastWindowDays
	case "7d", "7", "week", "weekly":
		return 7
	case "28d", "4w", "month", "monthly":
		return 28
	case "30d", "30":
		return 30
	}
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	mult := 1
	switch {
	case strings.HasSuffix(trimmed, "d"):
		trimmed = strings.TrimSuffix(trimmed, "d")
	case strings.HasSuffix(trimmed, "w"):
		trimmed = strings.TrimSuffix(trimmed, "w")
		mult = 7
	case strings.HasSuffix(trimmed, "y"):
		trimmed = strings.TrimSuffix(trimmed, "y")
		mult = 365
	}
	n, err := strconv.Atoi(trimmed)
	if err != nil || n < 1 {
		return defaultPastWindowDays
	}
	n *= mult
	if n > maxPastWindowDays {
		return maxPastWindowDays
	}
	return n
}

// askPastResponse is the /api/briefing/ask-past reply: the grounded answer
// plus the retrieval context (how many of the user's own summaries the answer
// is grounded on, and over which window/project).
type askPastResponse struct {
	llmrun.AskResult
	Retrieved  int    `json:"retrieved"`
	WindowDays int    `json:"window_days"`
	Project    string `json:"project,omitempty"`
}

// handleBriefingAskPast — POST /api/briefing/ask-past {"question","window",
// "project"} : natural-language archaeology over the user's OWN persisted
// session summaries (sessions.summary_md, FTS5 index 084 — never raw
// transcripts; project roots are reduced to a bare repo marker in the prompt).
// Retrieval window defaults to the trailing year. Requires an LLM runner + a
// configured connection for the ask_past task, else 404 no_connection. With
// zero matching sessions the handler answers deterministically WITHOUT any LLM
// call (200, no egress). A failed upstream call is 502 llm_error (after
// accounting).
func (s *Server) handleBriefingAskPast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrStatus(w, fmt.Errorf("briefing ask-past: method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	if s.opts.LLMRunner == nil {
		writeErrStatus(w, llmrun.ErrNoConnection, http.StatusNotFound)
		return
	}
	var body struct {
		Question string `json:"question"`
		Window   string `json:"window"`
		Project  string `json:"project"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(&body); err != nil {
		writeErrStatus(w, fmt.Errorf("briefing ask-past: decode body: %w", err), http.StatusBadRequest)
		return
	}
	question := strings.TrimSpace(body.Question)
	if question == "" {
		writeErrStatus(w, fmt.Errorf("briefing ask-past: empty question"), http.StatusBadRequest)
		return
	}
	windowDays := parsePastWindowDays(body.Window)

	hits, err := store.SelectPastSessions(r.Context(), s.db(), store.PastQuery{
		Since:   time.Now().AddDate(0, 0, -windowDays),
		Terms:   store.PastSearchTerms(question),
		Project: body.Project,
	})
	if err != nil {
		writeErr(w, err)
		return
	}

	response := askPastResponse{WindowDays: windowDays, Project: body.Project}
	if len(hits) == 0 {
		// Deterministic answer, no egress: the model is never invoked.
		response.AskResult = llmrun.AskResult{Text: "No sessions in the retrieved window matched that question. Try rephrasing it, widening the window, or narrowing to a project."}
		writeJSON(w, response)
		return
	}

	res, err := s.opts.LLMRunner.AskPast(r.Context(), s.opts.LLMConns, llmrun.PastContext{
		Question:   question,
		WindowDays: windowDays,
		Project:    body.Project,
		Sessions:   hits,
	})
	if err != nil {
		if errors.Is(err, llmrun.ErrNoConnection) {
			writeErrStatus(w, err, http.StatusNotFound)
			return
		}
		writeErrStatus(w, fmt.Errorf("briefing ask-past: %w", err), http.StatusBadGateway)
		return
	}
	response.AskResult = res
	response.Retrieved = len(hits)
	writeJSON(w, response)
}

// defaultFailureWindowDays is the failure-evidence window (the trailing
// month of recent behavior) shared by the coaching-note and diagnosis
// surfaces.
const defaultFailureWindowDays = 30

// maxFailurePatterns and maxCoachingRules cap the evidence bundle sent to the
// model (top-N by occurrence/frequency — the most load-bearing patterns).
const (
	maxFailurePatterns = 5
	maxCoachingRules   = 5
)

// parseFailureWindowDays maps a failure-evidence window value to a day count:
// "" → the trailing-month default; otherwise the ask-past window grammar
// (unparseable input falls back to the trailing year, clamped to the 10-year
// cap). Shared by the coaching-note and diagnosis surfaces.
func parseFailureWindowDays(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return defaultFailureWindowDays
	}
	return parsePastWindowDays(raw)
}

// coachingResponse is the /api/briefing/coaching reply: the coaching note plus
// the evidence it was grounded on (how many recurring failure patterns and
// failure→recovery rules were found, over which window/project).
type coachingResponse struct {
	llmrun.AskResult
	Patterns   int    `json:"patterns"`
	Rules      int    `json:"rules"`
	WindowDays int    `json:"window_days"`
	Project    string `json:"project,omitempty"`
}

// handleBriefingCoaching — POST /api/briefing/coaching {"window","project"} :
// a micro-feedback coaching note grounded in the user's OWN recurring failure
// and failure→recovery evidence (api_turns error patterns via the briefing
// scan + failure_context correction rules — never raw transcripts; project
// roots and edited-file paths are reduced to bare markers/basenames in the
// prompt). Evidence window defaults to the trailing month. Requires an LLM
// runner + a configured connection for the coach task, else 404 no_connection.
// With zero failure evidence the handler answers deterministically WITHOUT any
// LLM call (200, no egress). A failed upstream call is 502 llm_error (after
// accounting).
func (s *Server) handleBriefingCoaching(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrStatus(w, fmt.Errorf("briefing coaching: method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	if s.opts.LLMRunner == nil {
		writeErrStatus(w, llmrun.ErrNoConnection, http.StatusNotFound)
		return
	}
	var body struct {
		Window  string `json:"window"`
		Project string `json:"project"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(&body); err != nil {
		writeErrStatus(w, fmt.Errorf("briefing coaching: decode body: %w", err), http.StatusBadRequest)
		return
	}
	windowDays := parseFailureWindowDays(body.Window)

	engine := s.opts.CostEngine
	if engine == nil {
		engine = cost.NewEngine(config.IntelligenceConfig{})
	}
	now := s.now()
	since := now.AddDate(0, 0, -windowDays)
	turns, err := s.scanBriefingTurns(r.Context(), since, now, briefingQueryOptions{project: body.Project}, engine)
	if err != nil {
		writeErr(w, err)
		return
	}
	patterns := briefing.AggregateFailurePatterns(turns)
	if len(patterns) > maxFailurePatterns {
		patterns = patterns[:maxFailurePatterns]
	}
	rules, err := learn.New(s.db()).Derive(r.Context(), learn.Options{
		ProjectRoot: body.Project,
		Since:       since,
		MinFailures: 2,
		Limit:       maxCoachingRules,
	})
	if err != nil {
		writeErr(w, err)
		return
	}

	response := coachingResponse{WindowDays: windowDays, Project: body.Project}
	if len(patterns) == 0 && len(rules) == 0 {
		// Deterministic answer, no egress: the model is never invoked.
		response.AskResult = llmrun.AskResult{
			Text: fmt.Sprintf("No recurring failures or failure-to-recovery patterns in the last %d days to coach on.", windowDays),
		}
		writeJSON(w, response)
		return
	}

	res, err := s.opts.LLMRunner.Coach(r.Context(), s.opts.LLMConns, llmrun.CoachingContext{
		WindowDays: windowDays,
		Project:    body.Project,
		Patterns:   patterns,
		Rules:      rules,
	})
	if err != nil {
		if errors.Is(err, llmrun.ErrNoConnection) {
			writeErrStatus(w, err, http.StatusNotFound)
			return
		}
		writeErrStatus(w, fmt.Errorf("briefing coaching: %w", err), http.StatusBadGateway)
		return
	}
	response.AskResult = res
	response.Patterns = len(patterns)
	response.Rules = len(rules)
	writeJSON(w, response)
}

// maxTriageCards bounds the opportunity card set sent to the triage model
// (top-of-list — the composer already sorts by savings; the model reorders).
const maxTriageCards = 30

// triageResponse is the /api/briefing/triage reply: the opportunity cards in
// the model's priority order (always a permutation of the composed set — the
// runner appends any card the model omitted), the grouping + rationale, and
// the connection metadata.
type triageResponse struct {
	Date           string                 `json:"date"`
	Basis          briefing.Basis         `json:"basis"`
	WindowDays     int                    `json:"window_days"`
	Opportunities  []briefing.Opportunity `json:"opportunities"`
	Order          []string               `json:"order"`
	Groups         []llmrun.TriageGroup   `json:"groups,omitempty"`
	Summary        string                 `json:"summary,omitempty"`
	Model          string                 `json:"model,omitempty"`
	ConnectionID   string                 `json:"connection_id,omitempty"`
	ConnectionName string                 `json:"connection_name,omitempty"`
	LatencyMS      int64                  `json:"latency_ms,omitempty"`
}

// handleBriefingTriage — POST /api/briefing/triage {"date","window","project"} :
// LLM prioritization + grouping of the composed opportunity card set (Stage 1
// "smarter opportunities" follow-up). Grounded in a content-free projection of
// the cards (dedup keys, titles, savings, confidence, flags); the reply is
// always a full permutation of the composed set so no data-backed card is
// silently dropped. Requires an LLM runner + a configured connection for the
// triage task, else 404 no_connection. A window with zero cards answers
// deterministically WITHOUT any LLM call (200, no egress). A failed upstream
// call is 502 llm_error (after accounting) — the caller falls back to the
// deterministic sort.
func (s *Server) handleBriefingTriage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrStatus(w, fmt.Errorf("briefing triage: method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	if s.opts.LLMRunner == nil {
		writeErrStatus(w, llmrun.ErrNoConnection, http.StatusNotFound)
		return
	}
	var body struct {
		Date    string `json:"date"`
		Window  string `json:"window"`
		Project string `json:"project"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(&body); err != nil {
		writeErrStatus(w, fmt.Errorf("briefing triage: decode body: %w", err), http.StatusBadRequest)
		return
	}
	days, err := parseBriefingWindowDays(body.Window)
	if err != nil {
		writeErrStatus(w, err, http.StatusBadRequest)
		return
	}
	b, err := s.briefingForDate(r.Context(), briefingQueryOptions{date: body.Date, window: body.Window, project: body.Project})
	if err != nil {
		writeErr(w, err)
		return
	}
	opps := b.Opportunities
	if len(opps) > maxTriageCards {
		opps = opps[:maxTriageCards]
	}
	response := triageResponse{Date: b.Date, Basis: b.Basis, WindowDays: days, Opportunities: opps, Order: make([]string, 0, len(opps))}
	for _, o := range opps {
		response.Order = append(response.Order, o.DedupKey)
	}
	if len(opps) == 0 {
		// Deterministic answer, no egress: there is nothing to prioritize.
		writeJSON(w, response)
		return
	}
	res, err := s.opts.LLMRunner.Triage(r.Context(), s.opts.LLMConns, llmrun.TriageContext{
		WindowDays:    days,
		Date:          b.Date,
		Opportunities: opps,
	})
	if err != nil {
		if errors.Is(err, llmrun.ErrNoConnection) {
			writeErrStatus(w, err, http.StatusNotFound)
			return
		}
		writeErrStatus(w, fmt.Errorf("briefing triage: %w", err), http.StatusBadGateway)
		return
	}
	byKey := make(map[string]briefing.Opportunity, len(opps))
	for _, o := range opps {
		byKey[o.DedupKey] = o
	}
	ordered := make([]briefing.Opportunity, 0, len(res.Order))
	for _, k := range res.Order {
		if o, ok := byKey[k]; ok {
			ordered = append(ordered, o)
		}
	}
	response.Opportunities = ordered
	response.Order = res.Order
	response.Groups = res.Groups
	response.Summary = res.Summary
	response.Model = res.Model
	response.ConnectionID = res.ConnectionID
	response.ConnectionName = res.ConnectionName
	response.LatencyMS = res.LatencyMS
	writeJSON(w, response)
}

// diagnoseEntry is one enriched diagnosis in the /api/briefing/diagnose reply:
// the pattern evidence the model was grounded on, plus its root-cause analysis
// and one next step.
type diagnoseEntry struct {
	Index       int    `json:"index"`
	Category    string `json:"category"`
	Tool        string `json:"tool,omitempty"`
	Model       string `json:"model,omitempty"`
	Occurrences int    `json:"occurrences"`
	Sessions    int    `json:"sessions"`
	Days        int    `json:"days"`
	Diagnosis   string `json:"diagnosis"`
	NextStep    string `json:"next_step"`
}

// diagnoseResponse is the /api/briefing/diagnose reply: the per-pattern
// diagnosis set plus the connection metadata.
type diagnoseResponse struct {
	WindowDays     int             `json:"window_days"`
	Project        string          `json:"project,omitempty"`
	Patterns       int             `json:"patterns"`
	Diagnoses      []diagnoseEntry `json:"diagnoses"`
	Model          string          `json:"model,omitempty"`
	ConnectionID   string          `json:"connection_id,omitempty"`
	ConnectionName string          `json:"connection_name,omitempty"`
	LatencyMS      int64           `json:"latency_ms,omitempty"`
}

// handleBriefingDiagnose — POST /api/briefing/diagnose {"window","project"} :
// per-pattern LLM diagnosis (root cause + one next step) of the user's OWN
// recurring failure patterns (Stage 1 "failure analysis" follow-up). Grounded
// in the same content-free aggregation behind the failure_pattern cards and
// coaching notes; project roots are reduced to a bare repo marker in the
// prompt. Evidence window defaults to the trailing month. Requires an LLM
// runner + a configured connection for the diagnose task, else 404
// no_connection. With zero failure patterns the handler answers
// deterministically WITHOUT any LLM call (200, no egress). A failed upstream
// call is 502 llm_error (after accounting).
func (s *Server) handleBriefingDiagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrStatus(w, fmt.Errorf("briefing diagnose: method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	if s.opts.LLMRunner == nil {
		writeErrStatus(w, llmrun.ErrNoConnection, http.StatusNotFound)
		return
	}
	var body struct {
		Window  string `json:"window"`
		Project string `json:"project"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(&body); err != nil {
		writeErrStatus(w, fmt.Errorf("briefing diagnose: decode body: %w", err), http.StatusBadRequest)
		return
	}
	windowDays := parseFailureWindowDays(body.Window)

	engine := s.opts.CostEngine
	if engine == nil {
		engine = cost.NewEngine(config.IntelligenceConfig{})
	}
	now := s.now()
	since := now.AddDate(0, 0, -windowDays)
	turns, err := s.scanBriefingTurns(r.Context(), since, now, briefingQueryOptions{project: body.Project}, engine)
	if err != nil {
		writeErr(w, err)
		return
	}
	patterns := briefing.AggregateFailurePatterns(turns)
	if len(patterns) > maxFailurePatterns {
		patterns = patterns[:maxFailurePatterns]
	}

	response := diagnoseResponse{WindowDays: windowDays, Project: body.Project, Diagnoses: make([]diagnoseEntry, 0, len(patterns))}
	if len(patterns) == 0 {
		// Deterministic answer, no egress: the model is never invoked.
		writeJSON(w, response)
		return
	}

	res, err := s.opts.LLMRunner.Diagnose(r.Context(), s.opts.LLMConns, llmrun.DiagnoseContext{
		WindowDays: windowDays,
		Project:    body.Project,
		Patterns:   patterns,
	})
	if err != nil {
		if errors.Is(err, llmrun.ErrNoConnection) {
			writeErrStatus(w, err, http.StatusNotFound)
			return
		}
		writeErrStatus(w, fmt.Errorf("briefing diagnose: %w", err), http.StatusBadGateway)
		return
	}
	response.Patterns = len(patterns)
	for _, d := range res.Diagnoses {
		if d.Index < 0 || d.Index >= len(patterns) {
			continue
		}
		p := patterns[d.Index]
		response.Diagnoses = append(response.Diagnoses, diagnoseEntry{
			Index:       d.Index,
			Category:    p.CategoryLabel(),
			Tool:        p.Tool,
			Model:       p.Model,
			Occurrences: p.Count,
			Sessions:    len(p.Sessions),
			Days:        len(p.Days),
			Diagnosis:   d.Diagnosis,
			NextStep:    d.NextStep,
		})
	}
	response.Model = res.Model
	response.ConnectionID = res.ConnectionID
	response.ConnectionName = res.ConnectionName
	response.LatencyMS = res.LatencyMS
	writeJSON(w, response)
}

// handleBriefingOpportunities — GET /api/briefing/opportunities?date=&window= :
// the flat opportunity cards from the (possibly snapshot-cached) briefing.
func (s *Server) handleBriefingOpportunities(w http.ResponseWriter, r *http.Request) {
	b, err := s.briefingForDate(r.Context(), briefingQueryOptions{
		date:    r.URL.Query().Get("date"),
		tool:    r.URL.Query().Get("tool"),
		project: r.URL.Query().Get("project"),
		basis:   r.URL.Query().Get("basis"),
		window:  r.URL.Query().Get("window"),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"date":          b.Date,
		"basis":         b.Basis,
		"opportunities": b.Opportunities,
	})
}

// handleBriefingJustify — GET /api/briefing/justify?id=<dedup_key>&date= :
// the drill-down justification for one opportunity card, served from the
// persisted snapshot so the page and the drill-down can never drift.
func (s *Server) handleBriefingJustify(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, fmt.Errorf("briefing justify: missing id"))
		return
	}
	b, err := s.briefingForDate(r.Context(), briefingQueryOptions{
		date:   r.URL.Query().Get("date"),
		window: r.URL.Query().Get("window"),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	for _, o := range b.Opportunities {
		if o.DedupKey == id {
			writeJSON(w, o.Justification)
			return
		}
	}
	writeErrStatus(w, fmt.Errorf("briefing justify: no opportunity %q for %s", id, b.Date), http.StatusNotFound)
}

// handleBriefingSession — GET /api/briefing/session/<id> : drill-down into
// one top session, reusing the existing /api/session/<id> handler verbatim
// (same enrichment, same shape) so there is exactly one session-detail
// implementation.
func (s *Server) handleBriefingSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/briefing/session/")
	if id == "" || strings.Contains(id, "/") {
		writeErr(w, fmt.Errorf("briefing session: invalid id"))
		return
	}
	cloned := r.Clone(r.Context())
	cloned.URL.Path = "/api/session/" + id
	s.handleSessionDetail(w, cloned)
}
