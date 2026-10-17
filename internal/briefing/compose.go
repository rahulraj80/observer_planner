package briefing

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/failure"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
)

// DefaultThresholds returns the reference thresholds for opportunity
// detection. Loaders may override individual fields before composing.
func DefaultThresholds() Thresholds {
	return Thresholds{
		HealthyCacheHitRate: 0.60,
		TrivialPromptTokens: 50_000,
		TrivialOutputTokens: 50_000,
		RoutingTargetModel:  "gpt-5-mini",
		LCFloorUSD:          0.20,
		MinSavingsUSD:       0.05,
	}
}

// Compose turns Facts into a complete Briefing. It is pure and deterministic:
// given the same Facts it always returns the same Briefing.
func Compose(f Facts) Briefing {
	if f.GeneratedAt.IsZero() {
		f.GeneratedAt = f.Window.Since
	}
	th := f.Thresholds
	if th.HealthyCacheHitRate <= 0 {
		th = DefaultThresholds()
		if f.Thresholds.HealthyCacheHitRate > 0 {
			th = f.Thresholds
		}
	}
	f.Thresholds = th

	glance := GlanceFromTurns(f.Turns)
	activity := activityFromTurns(f.Turns)
	top := topSessions(f.Sessions, 5, f.Price)
	opps := detectOpportunities(f)
	trend := buildTrend(&glance, f.Prior, f.WindowDays)

	return Briefing{
		Date:          f.Date,
		GeneratedAt:   f.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Basis:         f.Basis,
		Period:        periodLabel(f.WindowDays),
		Window:        windowJSON{Since: f.Window.Since.UTC().Format("2006-01-02T15:04:05Z07:00"), Until: f.Window.Until.UTC().Format("2006-01-02T15:04:05Z07:00")},
		AtGlance:      glance,
		Trend:         trend,
		Activity:      activity,
		TopSessions:   top,
		Opportunities: opps,
		Narrative:     narrative(glance, f.Basis, len(activity), top, opps, trend),
		Questions:     questions(glance, opps),
	}
}

// periodLabel maps a window length to the user-facing period name.
func periodLabel(days int) string {
	switch {
	case days <= 1:
		return "day"
	case days == 7:
		return "week"
	case days >= 28:
		return "month"
	default:
		return fmt.Sprintf("%d-day", days)
	}
}

// GlanceFromTurns aggregates the headline KPI chips (spend, tokens, cache,
// errors, active hours, fast share, latency percentiles) from the window's
// turns. It is exported so loaders can compute a prior-window glance for the
// multi-day trend comparison without composing a whole Briefing.
func GlanceFromTurns(turns []Turn) AtGlance {
	g := AtGlance{}
	seen := map[string]struct{}{}
	hours := map[string]struct{}{}
	fast := 0
	for _, t := range turns {
		seen[t.SessionID] = struct{}{}
		g.Turns++
		g.SpendUSD += t.CostUSD
		g.InputTokens += t.Tokens.Input
		g.OutputTokens += t.Tokens.Output
		g.CacheReadTokens += t.Tokens.CacheRead
		g.CacheCreationTokens += t.Tokens.CacheCreation + t.Tokens.CacheCreation1h
		if t.Error {
			g.ErrorTurns++
		}
		hours[t.Timestamp.UTC().Format("2006-01-02T15")] = struct{}{}
		if t.Fast {
			fast++
		}
	}
	g.Sessions = len(seen)
	g.ActiveHours = float64(len(hours))
	g.FastShare = 0
	if g.Turns > 0 {
		g.FastShare = round2(float64(fast) / float64(g.Turns))
	}
	window := g.InputTokens + g.CacheReadTokens + g.CacheCreationTokens
	if window > 0 {
		g.CacheHitRate = round2(float64(g.CacheReadTokens) / float64(window))
	}
	g.TTFTP50MS = nearestRank(latencyValues(turns, func(t Turn) int64 { return t.TTFTMS }), 0.50)
	g.TTFTP95MS = nearestRank(latencyValues(turns, func(t Turn) int64 { return t.TTFTMS }), 0.95)
	g.TotalP50MS = nearestRank(latencyValues(turns, func(t Turn) int64 { return t.TotalMS }), 0.50)
	g.TotalP95MS = nearestRank(latencyValues(turns, func(t Turn) int64 { return t.TotalMS }), 0.95)
	g.SpendUSD = round2(g.SpendUSD)
	return g
}

// latencyValues collects a per-turn timing column, keeping only positive
// observations — negative/zero values are not real proxy captures (JSONL
// rows, sniffed-SSE fallback, buffered reads) and would pollute percentiles.
func latencyValues(turns []Turn, sel func(Turn) int64) []int64 {
	var vals []int64
	for _, t := range turns {
		if v := sel(t); v > 0 {
			vals = append(vals, v)
		}
	}
	return vals
}

// nearestRank returns the nearest-rank percentile value of the sorted
// observations, or 0 when there are none. Mirrors the org rollup's
// apiTurnPercentile convention: sorted value at int(f*(n-1)+0.5).
func nearestRank(vals []int64, f float64) int64 {
	if len(vals) == 0 {
		return 0
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	return vals[int(f*float64(len(vals)-1)+0.5)]
}

// buildTrend emits the multi-day comparison block when the composer was given
// a prior window. Absent for the daily briefing and for windows with no prior
// activity to compare against.
func buildTrend(cur, prior *AtGlance, days int) *Trend {
	if prior == nil || days <= 1 {
		return nil
	}
	if prior.Turns == 0 && prior.SpendUSD == 0 {
		return nil
	}
	deltaPct := func(c, p float64) float64 {
		if p == 0 {
			return 0
		}
		return (c - p) / p * 100
	}
	return &Trend{
		WindowDays:        days,
		SpendUSD:          cur.SpendUSD,
		SpendPriorUSD:     prior.SpendUSD,
		SpendDeltaUSD:     round2(cur.SpendUSD - prior.SpendUSD),
		SpendDeltaPct:     round1(deltaPct(cur.SpendUSD, prior.SpendUSD)),
		Turns:             cur.Turns,
		TurnsPrior:        prior.Turns,
		TurnsDeltaPct:     round1(deltaPct(float64(cur.Turns), float64(prior.Turns))),
		Sessions:          cur.Sessions,
		SessionsPrior:     prior.Sessions,
		SessionsDeltaPct:  round1(deltaPct(float64(cur.Sessions), float64(prior.Sessions))),
		InputTokens:       cur.InputTokens,
		OutputTokens:      cur.OutputTokens,
		CacheReadTokens:   cur.CacheReadTokens,
		CacheHitRate:      cur.CacheHitRate,
		CacheHitRateDelta: round2(cur.CacheHitRate - prior.CacheHitRate),
		ErrorRate:         errorRateOf(cur),
		ErrorRatePrior:    round3(errorRateOf(prior)),
		ErrorRateDelta:    round3(errorRateOf(cur) - errorRateOf(prior)),
		TTFTP50MS:         cur.TTFTP50MS,
		TTFTP95MS:         cur.TTFTP95MS,
		TTFTP50DeltaMS:    cur.TTFTP50MS - prior.TTFTP50MS,
		TTFTP95DeltaMS:    cur.TTFTP95MS - prior.TTFTP95MS,
	}
}

func errorRateOf(g *AtGlance) float64 {
	if g == nil || g.Turns == 0 {
		return 0
	}
	return float64(g.ErrorTurns) / float64(g.Turns)
}

func activityFromTurns(turns []Turn) []Activity {
	by := map[string]*Activity{}
	var order []string
	for _, t := range turns {
		if a, ok := by[t.Tool]; ok {
			a.Turns++
			a.SpendUSD += t.CostUSD
		} else {
			by[t.Tool] = &Activity{Tool: t.Tool, Turns: 1, SpendUSD: t.CostUSD}
			order = append(order, t.Tool)
		}
	}
	// Session counts per tool: distinct session ids.
	sess := map[string]map[string]struct{}{}
	for _, t := range turns {
		if sess[t.Tool] == nil {
			sess[t.Tool] = map[string]struct{}{}
		}
		sess[t.Tool][t.SessionID] = struct{}{}
	}
	acts := make([]Activity, 0, len(order))
	for _, tool := range order {
		a := by[tool]
		a.Sessions = len(sess[tool])
		a.SpendUSD = round2(a.SpendUSD)
		acts = append(acts, *a)
	}
	sort.Slice(acts, func(i, j int) bool { return acts[i].SpendUSD > acts[j].SpendUSD })
	return acts
}

func topSessions(sessions []SessionFacts, n int, price PriceFn) []SessionRef {
	ranked := make([]SessionFacts, len(sessions))
	copy(ranked, sessions)
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].CostUSD != ranked[j].CostUSD {
			return ranked[i].CostUSD > ranked[j].CostUSD
		}
		return ranked[i].ID < ranked[j].ID
	})
	if len(ranked) > n {
		ranked = ranked[:n]
	}
	out := make([]SessionRef, 0, len(ranked))
	for _, s := range ranked {
		ref := SessionRef{ID: s.ID, Tool: s.Tool, Turns: s.Turns, CostUSD: round2(s.CostUSD), Errors: s.Errors}
		if len(s.Models) > 0 {
			models := make([]SessionModelRef, 0, len(s.Models))
			for m, b := range s.Models {
				var costUSD float64
				if price != nil {
					if c, ok := price(m, b); ok {
						costUSD = c
					}
				}
				models = append(models, SessionModelRef{Name: m, Turns: s.ModelTurns[m], CostUSD: round2(costUSD)})
			}
			sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
			ref.Models = models
		}
		out = append(out, ref)
	}
	return out
}

func narrative(g AtGlance, basis Basis, toolCount int, top []SessionRef, opps []Opportunity, tr *Trend) string {
	if g.Turns == 0 {
		return "No LLM activity captured for the window."
	}
	parts := []string{
		fmt.Sprintf("%d sessions and %d turns across %s, at %s pricing.",
			g.Sessions, g.Turns, plural("tool", toolCount), basisLabel(basis)),
	}
	if g.SpendUSD > 0 {
		// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
		parts = append(parts, fmt.Sprintf("Estimated spend $%.2f (%.1f%% cached, %.1f%% fast).",
			g.SpendUSD, g.CacheHitRate*100, g.FastShare*100))
	}
	if g.ErrorTurns > 0 {
		parts = append(parts, fmt.Sprintf("%d turns errored.", g.ErrorTurns))
	}
	if tr != nil && tr.WindowDays > 1 {
		spend := "flat"
		switch {
		case tr.SpendDeltaPct > 0:
			spend = fmt.Sprintf("up %.1f%%", tr.SpendDeltaPct)
		case tr.SpendDeltaPct < 0:
			spend = fmt.Sprintf("down %.1f%%", -tr.SpendDeltaPct)
		}
		// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
		parts = append(parts, fmt.Sprintf("Compared with the prior %d days, spend is %s ($%.2f → $%.2f), turns %+d (%.1f%%), cache hit rate %+.0f pts, and TTFT p95 %+dms.",
			tr.WindowDays, spend, tr.SpendPriorUSD, tr.SpendUSD,
			tr.Turns-tr.TurnsPrior, tr.TurnsDeltaPct, tr.CacheHitRateDelta*100, tr.TTFTP95DeltaMS))
	}
	if len(opps) > 0 {
		best := opps[0]
		savings := "covered by evidence"
		if best.SavingsUSD > 0 {
			// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
			savings = fmt.Sprintf("up to $%.2f", best.SavingsUSD)
		}
		parts = append(parts, fmt.Sprintf("Top opportunity: %s (%s).", best.Title, savings))
	} else {
		parts = append(parts, "No efficiency opportunities found above the reporting threshold.")
	}
	return strings.Join(parts, " ")
}

func questions(g AtGlance, opps []Opportunity) []string {
	var qs []string
	if g.CacheHitRate > 0 && g.CacheHitRate < 0.5 {
		qs = append(qs, "Cache hit rate is low for the day — do you restart sessions frequently or churn on prompt prefixes?")
	}
	if g.FastShare > 0 && g.FastShare < 0.3 {
		qs = append(qs, "Most turns ran the full model — were any of them repetitive or could they have used the fast path?")
	}
	for _, o := range opps {
		if strings.HasPrefix(o.DedupKey, "routing_shadow") {
			qs = append(qs, "The candidate routing policy would have saved money — want to enable it?")
			break
		}
	}
	if len(qs) == 0 {
		qs = append(qs, "Anything you'd like the next briefing to track differently?")
	}
	return qs
}

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func basisLabel(b Basis) string {
	if b == BasisEffective {
		return "effective"
	}
	return "list"
}

func detectOpportunities(f Facts) []Opportunity {
	opps := []Opportunity{}
	opps = append(opps, detectRoutingShadow(f)...)
	opps = append(opps, detectTrivialModel(f)...)
	opps = append(opps, detectCacheHitRate(f)...)
	opps = append(opps, detectLongContext(f)...)
	opps = append(opps, detectRealizedVSEstimated(f)...)
	opps = append(opps, detectVirtuousCycle(f)...)
	opps = append(opps, detectRecurrence(f)...)
	opps = append(opps, detectFailurePatterns(f)...)
	sort.SliceStable(opps, func(i, j int) bool { return opps[i].SavingsUSD > opps[j].SavingsUSD })
	filtered := opps[:0]
	for _, o := range opps {
		if o.Recurrence || o.Report || o.SavingsUSD >= f.Thresholds.MinSavingsUSD || (o.SavingsUSD == 0 && o.SavingsMin > 0) {
			filtered = append(filtered, o)
		}
	}
	return filtered
}

// detectRoutingShadow surfaces the advise-shadow report when it would have
// saved money under the candidate policy.
func detectRoutingShadow(f Facts) []Opportunity {
	if f.Shadow == nil || f.Shadow.NetSaveUSD <= 0 {
		return nil
	}
	s := f.Shadow
	savings := round2(s.NetSaveUSD)
	title := "Rerouting these turns to a cheaper model would have saved money"
	if s.Rerouted == 0 {
		title = "A different model split would have saved money"
	}
	j := Justify(Claim{
		Detector:       "routing_shadow",
		Title:          title,
		Nudge:          "Run `observer routing apply` to enable the candidate policy, or review the shadow report in the dashboard.",
		Source:         "router_decisions",
		GroupBy:        "model",
		SavingsUSD:     savings,
		SavingsMin:     savings,
		Confidence:     0.8,
		FlipConditions: "If the candidate policy's quality flags turn out to reflect real regressions, the savings shrink.",
		Evidence: Evidence{
			Numbers: map[string]float64{
				"would_reroute":  float64(s.Rerouted),
				"would_save_usd": round2(s.WouldSaveUSD),
				"extra_cost_usd": round2(s.ExtraCostUSD),
				"net_save_usd":   savings,
				"quality_flags":  float64(s.QualityFlags),
				"coverage":       round3(s.Coverage),
			},
			Math: fmt.Sprintf("net_save = would_save (%.2f) - extra_cost (%.2f) = %.2f; rerouted %d turns, policy %s v%s",
				s.WouldSaveUSD, s.ExtraCostUSD, savings, s.Rerouted, s.PolicyName, s.PolicyVersion),
		},
	}, f)
	return []Opportunity{{DedupKey: "routing_shadow_1", Detector: "routing_shadow", Category: "routing", Scope: "all-sessions", Severity: "info", Title: title, Nudge: "Run `observer routing apply` to enable the candidate policy, or review the shadow report in the dashboard.", SavingsUSD: savings, Confidence: j.Confidence, Justification: j}}
}

// detectTrivialModel fires when an entire session ran on a premium model but
// stayed under the trivial token thresholds; it reprices the session at the
// routing target model to estimate savings.
func detectTrivialModel(f Facts) []Opportunity {
	var out []Opportunity
	for _, s := range f.Sessions {
		if !sessionAllPremium(s) {
			continue
		}
		var pTok, oTok int64
		for _, b := range s.Models {
			pTok += b.Input
			oTok += b.Output
		}
		if pTok > f.Thresholds.TrivialPromptTokens || oTok > f.Thresholds.TrivialOutputTokens {
			continue
		}
		target := f.Thresholds.RoutingTargetModel
		total := cost.TokenBundle{}
		for _, b := range s.Models {
			total = addBundle(total, b)
		}
		repriced, ok := f.Price(target, total)
		if !ok {
			continue
		}
		orig := round2(s.CostUSD)
		repriced = round2(repriced)
		savings := round2(orig - repriced)
		if savings <= 0 {
			continue
		}
		// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
		title := fmt.Sprintf("Session used a premium model for a trivial workload — ~$%.2f could have been saved", savings)
		j := Justify(Claim{
			Detector:       "trivial_model_overprovisioning",
			Title:          title,
			Nudge:          fmt.Sprintf("Route trivial sessions like this one to %s; the data below is the counterfactual replay.", target),
			Source:         "api_turns",
			GroupBy:        "session",
			SavingsUSD:     savings,
			SavingsMin:     savings,
			Confidence:     0.7,
			Tool:           s.Tool,
			FlipConditions: "If the trivial session was actually part of a longer, harder context, the model choice may be justified.",
			Evidence: Evidence{
				Numbers: map[string]float64{
					"session_cost_usd": orig,
					"reprice_cost_usd": repriced,
					"prompt_tokens":    float64(pTok),
					"output_tokens":    float64(oTok),
				},
				Math: fmt.Sprintf("session cost %.2f at %s vs %.2f at %s (same %d prompt + %d output tokens) = %.2f saved",
					orig, sessionModels(s), repriced, target, pTok, oTok, savings),
			},
			Reprice: []RepriceTurn{{
				OriginalModel: sessionModels(s),
				SelectedModel: target,
				EstimatedCost: repriced,
				Reason:        fmt.Sprintf("%d prompt / %d output tokens, under trivial thresholds", pTok, oTok),
			}},
		}, f)
		out = append(out, Opportunity{DedupKey: "trivial_model_" + s.ID, Detector: "trivial_model_overprovisioning", Category: "model-selection", Scope: "session", Severity: "suggestion", Title: title, Nudge: fmt.Sprintf("Route trivial sessions like this one to %s.", target), SavingsUSD: savings, Confidence: j.Confidence, Justification: j})
	}
	return out
}

func sessionAllPremium(s SessionFacts) bool {
	for m := range s.Models {
		if !isPremium(m) {
			return false
		}
	}
	return len(s.Models) > 0
}

func isPremium(m string) bool {
	m = strings.ToLower(m)
	switch {
	case strings.Contains(m, "opus"):
		return true
	case strings.Contains(m, "gemini-2.5-pro") || strings.Contains(m, "gemini-2.0-pro"):
		return true
	case strings.Contains(m, "claude-sonnet"):
		return true
	case strings.Contains(m, "o3") || strings.Contains(m, "o4"):
		return true
	}
	return false
}

func sessionModels(s SessionFacts) string {
	var ms []string
	for m := range s.Models {
		ms = append(ms, m)
	}
	sort.Strings(ms)
	return strings.Join(ms, " + ")
}

// detectCacheHitRate fires when the day's effective cache hit rate is well
// below the healthy threshold; it prices the would-be-read cache tokens that
// were instead re-read as fresh input.
func detectCacheHitRate(f Facts) []Opportunity {
	var in, cr, cc int64
	for _, t := range f.Turns {
		in += t.Tokens.Input
		cr += t.Tokens.CacheRead
		cc += t.Tokens.CacheCreation + t.Tokens.CacheCreation1h
	}
	total := in + cr + cc
	if total <= 0 {
		return nil
	}
	rate := float64(cr) / float64(total)
	if rate >= f.Thresholds.HealthyCacheHitRate {
		return nil
	}
	// Compute the marginal cost of the "missing" cache: what the cache reads
	// that never happened would have cost. Estimate the miss volume as the
	// gap between the healthy rate and the observed rate over the window.
	gap := f.Thresholds.HealthyCacheHitRate - rate
	missTokens := float64(total) * gap
	// Price one average token at the blended input rate (list pricing, cheap
	// baseline: half the input price, mirroring advisor's 50% cache-saving rule).
	blend := blendedInputRate(f)
	savings := round2(missTokens * blend * 0.5)
	if savings < f.Thresholds.MinSavingsUSD {
		return nil
	}
	// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
	title := fmt.Sprintf("Cache hit rate was %.0f%% — ~$%.2f went to re-reading fresh input", rate*100, savings)
	j := Justify(Claim{
		Detector:       "cache_hit_rate",
		Title:          title,
		Nudge:          "Keep the stable parts of your prompt prefix identical across turns so providers reuse the cache.",
		Source:         "token_usage",
		GroupBy:        "none",
		SavingsUSD:     savings,
		SavingsMin:     savings,
		Confidence:     0.6,
		FlipConditions: "If the day's work was genuinely novel (few repeated prefixes), the rate is not a problem.",
		Evidence: Evidence{
			Numbers: map[string]float64{
				"cache_read_tokens":      float64(cr),
				"cache_creation_tokens":  float64(cc),
				"input_tokens":           float64(in),
				"observed_rate":          round3(rate),
				"healthy_rate":           f.Thresholds.HealthyCacheHitRate,
				"miss_tokens":            missTokens,
				"blended_input_rate_usd": round6(blend),
			},
			Math: fmt.Sprintf("rate = %d/(%d+%d+%d) = %.3f; gap to %.2f = %.3f; miss tokens = %d; savings = miss * rate/1e6 * 0.5 = %.2f",
				cr, in, cr, cc, rate, f.Thresholds.HealthyCacheHitRate, gap, int64(missTokens), savings),
		},
	}, f)
	return []Opportunity{{DedupKey: "cache_hit_rate_1", Detector: "cache_hit_rate", Category: "cache", Scope: "all-sessions", Severity: "info", Title: title, Nudge: "Keep the stable parts of your prompt prefix identical across turns so providers reuse the cache.", SavingsUSD: savings, Confidence: j.Confidence, Justification: j}}
}

// detectLongContext fires when the aggregate long-context surcharge paid in the
// window exceeds the floor.
func detectLongContext(f Facts) []Opportunity {
	var surcharge float64
	var turns int
	for _, t := range f.Turns {
		if t.LCSurchargeUSD > 0 {
			surcharge += t.LCSurchargeUSD
			turns++
		}
	}
	surcharge = round2(surcharge)
	if surcharge < f.Thresholds.LCFloorUSD {
		return nil
	}
	// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
	title := fmt.Sprintf("Long-context surcharge cost $%.2f across %d turns", surcharge, turns)
	j := Justify(Claim{
		Detector:       "long_context_surcharge",
		Title:          title,
		Nudge:          "Compact the context before turns exceed the model's long-context threshold, or route to a model with a higher threshold.",
		Source:         "api_turns",
		GroupBy:        "none",
		SavingsUSD:     surcharge,
		SavingsMin:     0,
		Confidence:     0.9,
		FlipConditions: "None: this is the exact surcharge already paid, repriceable only by changing models or context length.",
		Evidence: Evidence{
			Numbers: map[string]float64{"long_context_surcharge_usd": surcharge, "affected_turns": float64(turns)},
			Math:    fmt.Sprintf("sum over turns of (priced_cost - priced_cost_without_long_context) = %.2f across %d turns", surcharge, turns),
		},
	}, f)
	return []Opportunity{{DedupKey: "long_context_surcharge_1", Detector: "long_context_surcharge", Category: "context", Scope: "all-sessions", Severity: "info", Title: title, Nudge: "Compact the context before turns exceed the model's long-context threshold, or route to a model with a higher threshold.", SavingsUSD: surcharge, Confidence: j.Confidence, Justification: j}}
}

// detectRealizedVSEstimated reports every applied CTA change with its
// measured realized savings — the "applied X on DATE → saved $Y of $Z
// estimated, confidence before/after" card (Stage 3, virtuous-cycle
// reporting). It is a status report, not a suggestion: it renders
// regardless of the dollar threshold (Report=true) so the applied-change
// loop stays visible even while the realized figure is still tiny.
func detectRealizedVSEstimated(f Facts) []Opportunity {
	if len(f.RealizedVSEstimated) == 0 {
		return nil
	}
	var out []Opportunity
	for _, r := range f.RealizedVSEstimated {
		date := r.ApplyStamp.In(time.Local).Format("2006-01-02")
		daysObserved := f.Window.Until.Sub(r.ApplyStamp).Hours() / 24
		if daysObserved < 0 {
			daysObserved = 0
		}
		// Projected is prorated to the observed span so $Y and $Z share the
		// same denominator: monthly projection × observed-days / 30.
		projected := round2(r.ProjectedMonthlyUSD * daysObserved / 30)
		realized := round2(r.RealizedUSD)
		before := applyConfidence(r.Detector)
		after := clamp01(r.Confidence)
		if after == 0 {
			after = RealizedConfidence(r.RealizedTurns)
		}
		// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
		title := fmt.Sprintf("Applied %s on %s → saved $%.2f of $%.2f estimated", r.ConfigSurface, date, realized, projected)
		nudge := fmt.Sprintf("Measured over %d graded turns under %s since the apply; confidence in the realized figure is %.1f (was %.1f at apply).",
			r.RealizedTurns, r.ConfigSurface, after, before)
		j := Justify(Claim{
			Detector:       "realized_vs_estimated",
			Title:          title,
			Nudge:          nudge,
			Source:         "router_decisions",
			GroupBy:        "none",
			SavingsUSD:     realized,
			SavingsMin:     0,
			Confidence:     after,
			FlipConditions: "Realized savings are measured from router_decisions.realized_delta_usd for turns served under the applied lever after the apply stamp; pre-restart turns and ungraded decisions are not counted.",
			Evidence: Evidence{
				Numbers: map[string]float64{
					"apply_unix":            float64(r.ApplyStamp.Unix()),
					"projected_monthly_usd": r.ProjectedMonthlyUSD,
					"projected_window_usd":  projected,
					"realized_usd":          realized,
					"realized_turns":        float64(r.RealizedTurns),
					"days_observed":         round3(daysObserved),
					"confidence_before":     before,
					"confidence_after":      after,
				},
				// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
				Math: fmt.Sprintf("realized = Σ router_decisions.realized_delta_usd for applied decisions in [%s, %s) under %s → $%.2f across %d graded turns; projected prorated = $%.2f/mo × %.2f days / 30 = $%.2f",
					r.ApplyStamp.UTC().Format("2006-01-02T15:04:05Z"), f.Window.Until.UTC().Format("2006-01-02T15:04:05Z"),
					r.ConfigSurface, realized, r.RealizedTurns, r.ProjectedMonthlyUSD, daysObserved, projected),
			},
		}, f)
		out = append(out, Opportunity{
			DedupKey:      fmt.Sprintf("realized_vs_%s_%d", r.ConfigSurface, r.ApplyStamp.Unix()),
			Detector:      "realized_vs_estimated",
			Category:      "routing",
			Scope:         "apply",
			Severity:      "info",
			Title:         title,
			Nudge:         nudge,
			SavingsUSD:    realized,
			Confidence:    after,
			Report:        true,
			Justification: j,
		})
	}
	return out
}

// RealizedConfidence is the measured-support confidence in a realized
// figure given the number of graded turns: 0.5 while unconfirmed, then
// +0.05 per square-root of graded turns up to 1.0 (100 graded turns
// saturate). Exported so the loader (which fills Facts) and the composer
// share one formula and the number can never drift.
func RealizedConfidence(graded int) float64 {
	if graded < 0 {
		graded = 0
	}
	return clamp01(0.5 + 0.05*math.Sqrt(float64(graded)))
}

// applyConfidence is the confidence the applied change carried at
// decision time, keyed by the detector that produced the card — the
// "before" half of the confidence-before/after report.
func applyConfidence(detector string) float64 {
	switch detector {
	case "routing_shadow":
		return 0.8
	default:
		return 0.5
	}
}

// detectVirtuousCycle produces a single Report card summarizing the
// cumulative virtuous cycle: all CTA applies to date, their projected
// savings, and the realized savings measured from router_decisions.
// It renders regardless of dollar threshold so the loop stays visible.
func detectVirtuousCycle(f Facts) []Opportunity {
	if f.VirtuousCycle == nil || f.VirtuousCycle.TotalApplies == 0 {
		return nil
	}
	vc := f.VirtuousCycle
	realizedPct := 0.0
	if vc.TotalProjectedUSD > 0 {
		realizedPct = vc.TotalRealizedUSD / vc.TotalProjectedUSD * 100
	}
	// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
	title := fmt.Sprintf("Virtuous cycle: %d applies → $%.2f projected, $%.2f realized (%.0f%%)",
		vc.TotalApplies, vc.TotalProjectedUSD, vc.TotalRealizedUSD, realizedPct)
	// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
	nudge := fmt.Sprintf("%d applies tracked across %d weeks; %d graded turns realized $%.2f of $%.2f projected.",
		vc.TotalApplies, len(vc.ByWeek), vc.TotalRealizedTurns, vc.TotalRealizedUSD, vc.TotalProjectedUSD)
	j := Justify(Claim{
		Detector:       "virtuous_cycle",
		Title:          title,
		Nudge:          nudge,
		Source:         "router_decisions",
		GroupBy:        "week",
		SavingsUSD:     vc.TotalRealizedUSD,
		SavingsMin:     0,
		Confidence:     RealizedConfidence(vc.TotalRealizedTurns),
		FlipConditions: "Realized savings depend on router_decisions grading coverage; ungraded turns contribute zero.",
		Evidence: Evidence{
			Numbers: map[string]float64{
				"total_applies":        float64(vc.TotalApplies),
				"total_projected_usd":  vc.TotalProjectedUSD,
				"total_realized_usd":   vc.TotalRealizedUSD,
				"total_realized_turns": float64(vc.TotalRealizedTurns),
				"realized_pct":         round1(realizedPct),
				"weeks_tracked":        float64(len(vc.ByWeek)),
			},
			// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
			Math: fmt.Sprintf("cumulative: %d applies → Σ projected = $%.2f/mo, Σ realized = $%.2f across %d graded turns = %.0f%%",
				vc.TotalApplies, vc.TotalProjectedUSD, vc.TotalRealizedUSD, vc.TotalRealizedTurns, realizedPct),
		},
	}, f)
	// Include weekly breakdown in evidence items for drill-down
	for _, w := range vc.ByWeek {
		j.Evidence.Items = append(j.Evidence.Items, EvidenceItem{
			Label: w.WeekStart,
			Value: w.RealizedUSD,
			Unit:  "USD",
		})
	}
	return []Opportunity{{
		DedupKey:      "virtuous_cycle",
		Detector:      "virtuous_cycle",
		Category:      "routing",
		Scope:         "all-time",
		Severity:      "info",
		Title:         title,
		Nudge:         nudge,
		SavingsUSD:    vc.TotalRealizedUSD,
		Confidence:    RealizedConfidence(vc.TotalRealizedTurns),
		Report:        true,
		Justification: j,
	}}
}

// Pattern is one aggregated recurring failure signature within a window: a
// failure category + normalized message signature grouped by tool/model, with
// its occurrence/session/day counts and first/last-seen span. It feeds both
// the failure_pattern opportunity cards (detectFailurePatterns) and the LLM
// coaching-notes evidence bundle (internal/llmrun).
type Pattern struct {
	Category  string
	NormMsg   string
	Tool      string
	Model     string
	Count     int
	Sessions  []string
	Days      []string
	FirstSeen time.Time
	LastSeen  time.Time
}

// AggregateFailurePatterns groups error turns by normalized failure pattern
// (category + message signature) and keeps only patterns spanning multiple
// sessions or days. Sorted by occurrence count desc, label asc as tiebreak;
// session and day slices are sorted for determinism.
func AggregateFailurePatterns(turns []Turn) []Pattern {
	type acc struct {
		category  string
		normMsg   string
		tool      string
		model     string
		count     int
		sessions  map[string]bool
		days      map[string]bool
		firstSeen time.Time
		lastSeen  time.Time
	}

	patterns := map[string]*acc{}
	for _, t := range turns {
		if !t.Error || strings.TrimSpace(t.ErrorMessage) == "" {
			continue
		}
		cat := failure.Categorize(t.ErrorMessage)
		norm := failure.Normalize(t.ErrorMessage)
		key := cat + "|" + norm + "|" + t.Tool + "|" + t.Model
		p, ok := patterns[key]
		if !ok {
			p = &acc{
				category:  cat,
				normMsg:   norm,
				tool:      t.Tool,
				model:     t.Model,
				sessions:  map[string]bool{},
				days:      map[string]bool{},
				firstSeen: t.Timestamp,
				lastSeen:  t.Timestamp,
			}
			patterns[key] = p
		}
		p.count++
		p.sessions[t.SessionID] = true
		day := t.Timestamp.In(time.Local).Format("2006-01-02")
		p.days[day] = true
		if t.Timestamp.Before(p.firstSeen) {
			p.firstSeen = t.Timestamp
		}
		if t.Timestamp.After(p.lastSeen) {
			p.lastSeen = t.Timestamp
		}
	}

	var out []Pattern
	for _, p := range patterns {
		if len(p.sessions) < 2 && len(p.days) < 2 {
			continue
		}
		sessions := keys(p.sessions)
		days := keys(p.days)
		sort.Strings(sessions)
		sort.Strings(days)
		out = append(out, Pattern{
			Category:  p.category,
			NormMsg:   p.normMsg,
			Tool:      p.tool,
			Model:     p.model,
			Count:     p.count,
			Sessions:  sessions,
			Days:      days,
			FirstSeen: p.firstSeen,
			LastSeen:  p.lastSeen,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return failureCategoryLabel(out[i].Category) < failureCategoryLabel(out[j].Category)
	})
	return out
}

// detectFailurePatterns surfaces recurring failure patterns that span
// multiple sessions or days as opportunity cards, with categorized "try this"
// nudges based on the failure category.
func detectFailurePatterns(f Facts) []Opportunity {
	var opps []Opportunity
	for _, p := range AggregateFailurePatterns(f.Turns) {
		title := fmt.Sprintf("%s errors recur (%d sessions, %d days, %d occurrences)",
			failureCategoryLabel(p.Category), len(p.Sessions), len(p.Days), p.Count)
		nudge := failureNudge(p.Category)
		j := Justify(Claim{
			Detector:       "failure_pattern",
			Title:          title,
			Nudge:          nudge,
			Source:         "api_turns",
			GroupBy:        "error_pattern",
			SavingsUSD:     0,
			SavingsMin:     0,
			Confidence:     0.7,
			Tool:           p.Tool,
			FlipConditions: "If the errors are unrelated (different root causes), the pattern is coincidental.",
			Evidence: Evidence{
				Numbers: map[string]float64{
					"affected_sessions": float64(len(p.Sessions)),
					"affected_days":     float64(len(p.Days)),
					"total_occurrences": float64(p.Count),
				},
				Math: fmt.Sprintf("category: %s; normalized message: %q; tool: %s; model: %s; first seen %s, last seen %s; nudge: %s",
					p.Category, p.NormMsg, p.Tool, p.Model, p.FirstSeen.Format("2006-01-02"), p.LastSeen.Format("2006-01-02"), nudge),
			},
		}, f)
		j.Days = p.Days
		opps = append(opps, Opportunity{
			DedupKey:      fmt.Sprintf("failure_pattern_%s|%s|%s|%s", p.Category, p.NormMsg, p.Tool, p.Model),
			Detector:      "failure_pattern",
			Category:      "reliability",
			Scope:         "multi-session",
			Severity:      "warning",
			Title:         title,
			Nudge:         nudge,
			SavingsUSD:    0,
			SavingsMin:    0,
			Confidence:    j.Confidence,
			Recurrence:    true,
			Justification: j,
		})
	}
	return opps
}

// CategoryLabel returns the capitalized display label for the pattern's
// failure category ("test_failure" → "Test failure"). Exported so the LLM
// diagnosis/triage surfaces and the dashboard can render the same label the
// cards use.
func (p Pattern) CategoryLabel() string { return failureCategoryLabel(p.Category) }

// failureCategoryLabel maps a failure.Category* value to the capitalized
// display label used in card titles ("test_failure" → "Test failure").
func failureCategoryLabel(category string) string {
	switch category {
	case failure.CategoryTimeout:
		return "Timeout"
	case failure.CategoryCompilation:
		return "Compilation"
	case failure.CategoryTestFailure:
		return "Test failure"
	case failure.CategoryPermission:
		return "Permission"
	case failure.CategoryRuntime:
		return "Runtime"
	default:
		return "Error"
	}
}

// failureNudge returns a categorized "try this" suggestion based on error category.
func failureNudge(category string) string {
	switch category {
	case failure.CategoryTimeout:
		return "Increase request timeout or simplify the prompt; long-running requests often hit provider limits."
	case failure.CategoryCompilation:
		return "Check for missing imports, syntax errors, or version mismatches in the code being generated."
	case failure.CategoryTestFailure:
		return "Run the failing test in isolation to reproduce; the test may be flaky or the code change introduced a regression."
	case failure.CategoryPermission:
		return "Verify file/permissions and API credentials; the tool may lack access to the target resource."
	case failure.CategoryRuntime:
		return "Add defensive checks (nil guards, bounds checks) or handle the specific runtime condition in the code."
	default:
		return "Review the error message and context; consider adding logging or retry logic for transient failures."
	}
}

// keys returns the keys of a string->bool map as a slice.
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// blendedInputRate prices one input token across the window's models at the
// mean input rate. Returns 0 if no model priced.
func blendedInputRate(f Facts) float64 {
	var total float64
	var n int
	for _, t := range f.Turns {
		if c, ok := f.Price(t.Model, cost.TokenBundle{Input: 1_000_000}); ok {
			total += c
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return total / float64(n) / 1_000_000
}

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

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }
