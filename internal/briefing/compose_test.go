package briefing

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
)

func testWindow() Window {
	since := time.Date(2026, 7, 31, 4, 0, 0, 0, time.UTC)
	until := time.Date(2026, 8, 1, 4, 0, 0, 0, time.UTC)
	return Window{Since: since, Until: until}
}

// testPrice prices a small fixed table; enough to make counterfactual math
// deterministic in tests.
func testPrice(model string, b cost.TokenBundle) (float64, bool) {
	p, ok := map[string]cost.Pricing{
		"claude-opus-4": {
			Input:                15.00,
			Output:               75.00,
			CacheRead:            1.50,
			CacheCreation:        18.75,
			LongContextThreshold: 200_000,
		},
		"gpt-5-mini": {
			Input:         0.15,
			Output:        0.60,
			CacheRead:     0.075,
			CacheCreation: 0.15,
		},
	}[model]
	if !ok {
		return 0, false
	}
	return cost.Compute(p, b), true
}

func newTestFacts() Facts {
	return Facts{
		Window:      testWindow(),
		Basis:       BasisList,
		GeneratedAt: time.Date(2026, 8, 1, 5, 0, 0, 0, time.UTC),
		Date:        "2026-07-31",
		Price:       testPrice,
		Thresholds:  DefaultThresholds(),
	}
}

func TestComposeEmpty(t *testing.T) {
	b := Compose(newTestFacts())
	if b.AtGlance.Turns != 0 || b.AtGlance.Sessions != 0 {
		t.Fatalf("empty facts must produce zero glance, got %+v", b.AtGlance)
	}
	if len(b.Opportunities) != 0 {
		t.Fatalf("empty facts must produce no opportunities, got %d", len(b.Opportunities))
	}
	// The JSON contract: empty collections marshal as [] never null — the web
	// page reads .length on these fields (regression 2026-08-02: a nil
	// opportunities slice marshaled as null crashed the briefing page with
	// "reading 'length'"). questions is non-empty by design (fallback card).
	blob, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(blob)
	for _, field := range []string{`"activity":`, `"top_sessions":`, `"opportunities":`, `"questions":`} {
		idx := strings.Index(s, field)
		if idx < 0 {
			t.Fatalf("marshal missing %q", field)
		}
		next := s[idx+len(field):]
		if strings.HasPrefix(next, "null") {
			t.Fatalf("field %q must not marshal as null", field)
		}
	}
	if opps := `"opportunities":[]`; !strings.Contains(s, opps) {
		t.Fatalf("empty opportunities must marshal as [], got %q", s[strings.Index(s, `"opportunities":`):strings.Index(s, `"opportunities":`)+30])
	}
	if b.Narrative == "" {
		t.Fatal("empty narrative must still be non-empty")
	}
	if b.Date != "2026-07-31" {
		t.Fatalf("date mismatch: %q", b.Date)
	}
	if b.Basis != BasisList {
		t.Fatalf("basis mismatch: %q", b.Basis)
	}
	if b.Window.Since != "2026-07-31T04:00:00Z" {
		t.Fatalf("window since mismatch: %q", b.Window.Since)
	}
}

func TestComposeGlanceAndActivity(t *testing.T) {
	f := newTestFacts()
	f.Turns = []Turn{
		{
			SessionID: "s1", Tool: "claude", Model: "claude-opus-4",
			Timestamp: time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC),
			Tokens:    cost.TokenBundle{Input: 1_000_000, Output: 10_000, CacheRead: 500_000},
			CostUSD:   15.75, Fast: false,
		},
		{
			SessionID: "s1", Tool: "claude", Model: "claude-opus-4",
			Timestamp: time.Date(2026, 7, 31, 10, 30, 0, 0, time.UTC),
			Tokens:    cost.TokenBundle{Input: 200_000, Output: 20_000, CacheRead: 1_000_000},
			CostUSD:   4.5, Fast: true,
		},
		{
			SessionID: "s2", Tool: "codex", Model: "gpt-5-mini",
			Timestamp: time.Date(2026, 7, 31, 14, 0, 0, 0, time.UTC),
			Tokens:    cost.TokenBundle{Input: 100_000, Output: 5_000},
			CostUSD:   0.045, Error: true,
		},
	}
	b := Compose(f)

	g := b.AtGlance
	if g.Sessions != 2 || g.Turns != 3 {
		t.Fatalf("glance sessions/turns: got %d/%d want 2/3", g.Sessions, g.Turns)
	}
	if want := round2(15.75 + 4.5 + 0.045); g.SpendUSD != want {
		t.Fatalf("spend: got %.2f want %.2f", g.SpendUSD, want)
	}
	if g.InputTokens != 1_300_000 || g.OutputTokens != 35_000 {
		t.Fatalf("token sums: got %d/%d", g.InputTokens, g.OutputTokens)
	}
	if g.CacheReadTokens != 1_500_000 || g.CacheCreationTokens != 0 {
		t.Fatalf("cache sums: got %d/%d", g.CacheReadTokens, g.CacheCreationTokens)
	}
	// rate = 1.5M / (1.3M + 1.5M + 0) = 0.536 → 0.54
	if want := 0.54; g.CacheHitRate != want {
		t.Fatalf("cache hit rate: got %.2f want %.2f", g.CacheHitRate, want)
	}
	if g.ErrorTurns != 1 {
		t.Fatalf("error turns: got %d want 1", g.ErrorTurns)
	}
	// two distinct UTC hour buckets
	if g.ActiveHours != 2 {
		t.Fatalf("active hours: got %.0f want 2", g.ActiveHours)
	}
	// fast share 1/3
	if g.FastShare != 0.33 {
		t.Fatalf("fast share: got %.2f want 0.33", g.FastShare)
	}

	if len(b.Activity) != 2 {
		t.Fatalf("activity rows: got %d want 2", len(b.Activity))
	}
	// sorted by spend desc: claude (20.25) then codex (0.045)
	if b.Activity[0].Tool != "claude" || b.Activity[1].Tool != "codex" {
		t.Fatalf("activity order wrong: %+v", b.Activity)
	}
	if b.Activity[0].Sessions != 1 || b.Activity[0].Turns != 2 {
		t.Fatalf("claude activity agg: %+v", b.Activity[0])
	}
	if b.Activity[1].Sessions != 1 || b.Activity[1].Turns != 1 {
		t.Fatalf("codex activity agg: %+v", b.Activity[1])
	}
}

func TestComposeTopSessions(t *testing.T) {
	f := newTestFacts()
	for i := 0; i < 7; i++ {
		f.Sessions = append(f.Sessions, SessionFacts{
			ID: "s" + string(rune('a'+i)), Tool: "claude", Turns: 1, CostUSD: float64(i + 1),
		})
	}
	b := Compose(f)
	if len(b.TopSessions) != 5 {
		t.Fatalf("top sessions: got %d want 5", len(b.TopSessions))
	}
	// cost-desc: sg(7), sf(6), se(5), sd(4), sc(3)
	if b.TopSessions[0].ID != "sg" || b.TopSessions[4].ID != "sc" {
		t.Fatalf("top session order wrong: %+v", b.TopSessions)
	}
}

func TestDetectTrivialModel(t *testing.T) {
	f := newTestFacts()
	// Session s1: all opus turns, tiny workload → should fire.
	f.Sessions = []SessionFacts{{
		ID: "s1", Tool: "claude", Turns: 2, CostUSD: 0.30,
		Models: map[string]cost.TokenBundle{
			"claude-opus-4": {Input: 10_000, Output: 2_000},
		},
	}, {
		// Session s2: mixed models → must NOT fire (not all premium).
		ID: "s2", Tool: "claude", Turns: 2, CostUSD: 0.50,
		Models: map[string]cost.TokenBundle{
			"claude-opus-4": {Input: 10_000, Output: 2_000},
			"gpt-5-mini":    {Input: 10_000, Output: 2_000},
		},
	}}
	b := Compose(f)
	var trivial []Opportunity
	for _, o := range b.Opportunities {
		if o.Detector == "trivial_model_overprovisioning" {
			trivial = append(trivial, o)
		}
	}
	if len(trivial) != 1 {
		t.Fatalf("trivial detector: got %d want 1 (%+v)", len(trivial), b.Opportunities)
	}
	o := trivial[0]
	if o.DedupKey != "trivial_model_s1" {
		t.Fatalf("dedup key: got %q", o.DedupKey)
	}
	// original 0.30; reprice at gpt-5-mini: 10k*0.15/1e6 + 2k*0.60/1e6 = 0.0015+0.0012 = 0.0027
	if o.SavingsUSD < 0.29 {
		t.Fatalf("savings: got %.3f want ~0.2973", o.SavingsUSD)
	}
	j := o.Justification
	if j.Source != "api_turns" || j.PriceBasis != BasisList {
		t.Fatalf("justification stamps: %+v", j)
	}
	if j.Since != "2026-07-31T04:00:00Z" || j.Until != "2026-08-01T04:00:00Z" {
		t.Fatalf("justification window: %q..%q", j.Since, j.Until)
	}
	if len(j.Reprice) != 1 || j.Reprice[0].SelectedModel != "gpt-5-mini" {
		t.Fatalf("reprice replay missing: %+v", j.Reprice)
	}
}

func TestDetectCacheHitRate(t *testing.T) {
	f := newTestFacts()
	// Mostly fresh input, very little cache → low rate.
	f.Turns = []Turn{
		{
			SessionID: "s1", Tool: "claude", Model: "claude-opus-4",
			Timestamp: time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
			Tokens:    cost.TokenBundle{Input: 500_000, CacheRead: 20_000},
		},
		{
			SessionID: "s1", Tool: "claude", Model: "claude-opus-4",
			Timestamp: time.Date(2026, 7, 31, 9, 30, 0, 0, time.UTC),
			Tokens:    cost.TokenBundle{Input: 500_000, CacheRead: 20_000},
		},
	}
	b := Compose(f)
	var card *Opportunity
	for i := range b.Opportunities {
		if b.Opportunities[i].Detector == "cache_hit_rate" {
			card = &b.Opportunities[i]
			break
		}
	}
	if card == nil {
		t.Fatal("cache_hit_rate card did not fire")
	}
	// rate = 40k/1.04M ≈ 0.038; gap to 0.60 → miss ≈ 624k tokens; savings = miss * 15/1e6 * 0.5 ≈ 4.68
	if card.SavingsUSD < 4.0 {
		t.Fatalf("cache savings too low: %.2f", card.SavingsUSD)
	}
	// Math line present
	if card.Justification.Evidence.Math == "" {
		t.Fatal("math line missing")
	}
}

func TestDetectCacheHitRateHealthy(t *testing.T) {
	f := newTestFacts()
	f.Turns = []Turn{
		{
			SessionID: "s1", Tool: "claude", Model: "claude-opus-4",
			Timestamp: time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
			Tokens:    cost.TokenBundle{Input: 100_000, CacheRead: 900_000},
		},
	}
	b := Compose(f)
	for _, o := range b.Opportunities {
		if o.Detector == "cache_hit_rate" {
			t.Fatalf("cache_hit_rate must not fire on healthy rate: %+v", o)
		}
	}
}

func TestDetectLongContext(t *testing.T) {
	f := newTestFacts()
	// 250k input tokens on opus-4 (LC threshold 200k) → surcharge 50k × (15-1.5)/1e6 = 0.675
	f.Turns = []Turn{
		{
			SessionID: "s1", Tool: "claude", Model: "claude-opus-4",
			Timestamp: time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
			Tokens:    cost.TokenBundle{Input: 250_000, Output: 5_000},
			CostUSD:   4.0, LCSurchargeUSD: 0.675,
		},
	}
	b := Compose(f)
	var card *Opportunity
	for i := range b.Opportunities {
		if b.Opportunities[i].Detector == "long_context_surcharge" {
			card = &b.Opportunities[i]
			break
		}
	}
	if card == nil {
		t.Fatal("long_context card did not fire")
	}
	if card.SavingsUSD != 0.68 {
		t.Fatalf("long-context savings: got %.2f want 0.68", card.SavingsUSD)
	}
}

func TestDetectLongContextBelowFloor(t *testing.T) {
	f := newTestFacts()
	f.Turns = []Turn{
		{
			SessionID: "s1", Tool: "claude", Model: "claude-opus-4",
			Timestamp: time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
			Tokens:    cost.TokenBundle{Input: 250_000},
			CostUSD:   4.0, LCSurchargeUSD: 0.05,
		},
	}
	b := Compose(f)
	for _, o := range b.Opportunities {
		if o.Detector == "long_context_surcharge" {
			t.Fatalf("long_context must not fire below floor: %+v", o)
		}
	}
}

func TestDetectRoutingShadow(t *testing.T) {
	f := newTestFacts()
	f.Shadow = &ShadowFacts{
		PolicyName: "cand-1", PolicyVersion: "1", Rerouted: 42,
		WouldSaveUSD: 5.00, ExtraCostUSD: 0.5, NetSaveUSD: 4.5, QualityFlags: 0, Coverage: 0.95,
	}
	b := Compose(f)
	var card *Opportunity
	for i := range b.Opportunities {
		if b.Opportunities[i].Detector == "routing_shadow" {
			card = &b.Opportunities[i]
			break
		}
	}
	if card == nil {
		t.Fatal("routing_shadow card did not fire")
	}
	if card.SavingsUSD != 4.5 {
		t.Fatalf("routing savings: got %.2f want 4.50", card.SavingsUSD)
	}
	if card.Justification.Source != "router_decisions" {
		t.Fatalf("routing justification source: %q", card.Justification.Source)
	}
}

func TestDetectRoutingShadowNoSave(t *testing.T) {
	f := newTestFacts()
	f.Shadow = &ShadowFacts{PolicyName: "cand-1", PolicyVersion: "1", Rerouted: 0, WouldSaveUSD: 0, NetSaveUSD: 0}
	b := Compose(f)
	for _, o := range b.Opportunities {
		if o.Detector == "routing_shadow" {
			t.Fatalf("routing_shadow must not fire on zero net save: %+v", o)
		}
	}
}

func TestJustifyStampsAndNormalizes(t *testing.T) {
	f := newTestFacts()
	f.Basis = BasisEffective
	j := Justify(Claim{
		Title: "t", Source: "api_turns", GroupBy: "session",
		Confidence: 1.5,
		Evidence: Evidence{
			Numbers: map[string]float64{"z": 1, "a": 2},
			Items:   []EvidenceItem{{Label: "b", Value: 1}, {Label: "a", Value: 2}},
			Math:    "1+1=2",
		},
	}, f)
	if j.Claim != "t" || j.Source != "api_turns" || j.GroupBy != "session" {
		t.Fatalf("claim fields: %+v", j)
	}
	if j.PriceBasis != BasisEffective {
		t.Fatalf("price basis: %q", j.PriceBasis)
	}
	if j.EstimateVersion != EstimateVersion {
		t.Fatalf("estimate version: %q", j.EstimateVersion)
	}
	if j.Confidence != 1.0 {
		t.Fatalf("confidence not clamped: %v", j.Confidence)
	}
	// numbers map must not mutate caller's map ordering assumptions; ensure both keys present
	if j.Evidence.Numbers["a"] != 2 || j.Evidence.Numbers["z"] != 1 {
		t.Fatalf("numbers lost: %+v", j.Evidence.Numbers)
	}
	if j.Evidence.Items[0].Label != "a" {
		t.Fatalf("items not sorted: %+v", j.Evidence.Items)
	}
}

func TestComposeDeterministic(t *testing.T) {
	f := newTestFacts()
	f.Turns = []Turn{
		{
			SessionID: "s1", Tool: "claude", Model: "claude-opus-4",
			Timestamp: time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
			Tokens:    cost.TokenBundle{Input: 100_000, CacheRead: 900_000},
		},
	}
	f.Shadow = &ShadowFacts{PolicyName: "cand", PolicyVersion: "1", Rerouted: 3, WouldSaveUSD: 1, ExtraCostUSD: 0, NetSaveUSD: 1, Coverage: 0.9}
	a := Compose(f)
	f.GeneratedAt = f.GeneratedAt.Add(time.Second)
	b := Compose(f)
	if a.Date != b.Date || a.Narrative != b.Narrative {
		t.Fatal("compose output not deterministic")
	}
	if len(a.Opportunities) != len(b.Opportunities) {
		t.Fatal("opportunity count not deterministic")
	}
}

func TestComposeTrendMultiDay(t *testing.T) {
	mk := GlanceFromTurns

	cur := []Turn{
		{SessionID: "a", Model: "claude-opus-4", CostUSD: 1.00, TTFTMS: 400, TotalMS: 2000},
		{SessionID: "a", Model: "claude-opus-4", CostUSD: 1.00, TTFTMS: 600, TotalMS: 3000},
		{SessionID: "b", Model: "claude-sonnet-4", CostUSD: 0.10, TTFTMS: 200, TotalMS: 1500, Error: true},
		{SessionID: "c", Model: "claude-opus-4", CostUSD: 2.00, TTFTMS: 0, TotalMS: 0, Error: true}, // JSONL row: no latency, excluded
	}
	prior := []Turn{
		{SessionID: "p", Model: "claude-opus-4", CostUSD: 2.00, TTFTMS: 900, TotalMS: 5000},
		{SessionID: "p", Model: "claude-opus-4", CostUSD: 2.00, TTFTMS: 1100, TotalMS: 6000},
		{SessionID: "q", Model: "claude-opus-4", CostUSD: 0.50, TTFTMS: 800, TotalMS: 4000},
	}

	f := newTestFacts()
	f.Date = "2026-08-01"
	f.Window = Window{Since: time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC), Until: time.Date(2026, 8, 1, 4, 0, 0, 0, time.UTC)}
	f.WindowDays = 7
	f.Turns = cur
	g := mk(prior)
	f.Prior = &g

	b := Compose(f)

	if b.Period != "week" {
		t.Fatalf("period: %q", b.Period)
	}
	// Latency percentiles only from positive captures: [200,400,600] → 400/600.
	if b.AtGlance.TTFTP50MS != 400 || b.AtGlance.TTFTP95MS != 600 {
		t.Fatalf("cur ttft: p50=%d p95=%d", b.AtGlance.TTFTP50MS, b.AtGlance.TTFTP95MS)
	}
	if b.AtGlance.TotalP95MS != 3000 {
		t.Fatalf("cur total p95: %d", b.AtGlance.TotalP95MS)
	}
	// Sessions: 3 distinct ids (c included), turns 4.
	if b.AtGlance.Turns != 4 || b.AtGlance.Sessions != 3 {
		t.Fatalf("glance: turns=%d sessions=%d", b.AtGlance.Turns, b.AtGlance.Sessions)
	}
	if b.Trend == nil {
		t.Fatal("trend must be present in multi-day mode")
	}
	if b.Trend.WindowDays != 7 {
		t.Fatalf("trend window_days: %d", b.Trend.WindowDays)
	}
	// Prior ttft [800,900,1100] → p50=900, p95=1100 → deltas -500/-500.
	if b.Trend.TTFTP50DeltaMS != -500 || b.Trend.TTFTP95DeltaMS != -500 {
		t.Fatalf("ttft deltas: p50=%d p95=%d", b.Trend.TTFTP50DeltaMS, b.Trend.TTFTP95DeltaMS)
	}
	if b.Trend.SpendUSD != 4.10 || b.Trend.SpendPriorUSD != 4.50 {
		t.Fatalf("trend spend: cur=%v prior=%v", b.Trend.SpendUSD, b.Trend.SpendPriorUSD)
	}
	if b.Trend.SpendDeltaUSD != -0.40 {
		t.Fatalf("spend delta: %v", b.Trend.SpendDeltaUSD)
	}
	if b.Trend.Turns != 4 || b.Trend.TurnsPrior != 3 {
		t.Fatalf("trend turns: cur=%d prior=%d", b.Trend.Turns, b.Trend.TurnsPrior)
	}
	// Error rate: 2/4 current, 0 prior.
	if b.Trend.ErrorRate != 0.5 || b.Trend.ErrorRatePrior != 0 || b.Trend.ErrorRateDelta != 0.5 {
		t.Fatalf("trend error: cur=%v prior=%v delta=%v", b.Trend.ErrorRate, b.Trend.ErrorRatePrior, b.Trend.ErrorRateDelta)
	}
	if !strings.Contains(b.Narrative, "prior 7 days") {
		t.Fatalf("narrative missing trend comparison: %q", b.Narrative)
	}
	// JSON contract: trend block present.
	blob, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), `"trend":{`) {
		t.Fatalf("trend must marshal as an object: %s", blob)
	}
}

func TestComposeTrendAbsentForDaily(t *testing.T) {
	f := newTestFacts()
	f.Turns = []Turn{{SessionID: "a", Model: "claude-opus-4", CostUSD: 1.00}}
	b := Compose(f)
	if b.Period != "day" {
		t.Fatalf("period: %q", b.Period)
	}
	if b.Trend != nil {
		t.Fatal("single-day briefing must not carry a trend block")
	}
	if b.AtGlance.TTFTP50MS != 0 || b.AtGlance.TTFTP95MS != 0 {
		t.Fatalf("no latency observations → zero percentiles, got %+v", b.AtGlance)
	}
}
