package routingcore

// observer_planner fork: the expected-benefit record types (api-intelligence.md
// §5.4) live in routingcore because Snapshot and Decision carry them;
// package routing re-exports them (routing/benefit_aliases.go).

// QualitySignal is the evidence-based expectation of how the selected
// model's quality compares to the original for this turn-kind
// (api-intelligence.md §5.4). Computed by the evidence substrate; the
// engine carries it through. Unknown when no calibration exists.
type QualitySignal string

// Quality signals.
const (
	QualityHold    QualitySignal = "hold"    // evidence: no material delta
	QualityBetter  QualitySignal = "better"  // evidence: selected >= original
	QualityWorse   QualitySignal = "worse"   // evidence: selected < original
	QualityUnknown QualitySignal = "unknown" // no evidence / uncalibrated
)

// QualityEvidence is the calibration-derived expectation for one
// (turn-kind, target tier) move: the quality signal plus the confidence
// in it (0..1). Zero confidence means uncalibrated — callers then render
// the benefit as "quality unknown" (§5.4).
type QualityEvidence struct {
	Signal     QualitySignal
	Confidence float64
}

// BenefitBreakdown is the structured expected-benefit record for a
// routing decision (api-intelligence.md §5.4). Pure data — computed by
// ComputeBenefit, rendered by BenefitText, persisted by the store seam
// as benefit_json on the router_decisions row. No prompt content, no
// paths, no model-content fields: models, dollars, counts, and enum
// signals only (§R9.1 privacy posture).
type BenefitBreakdown struct {
	// PromptCostBefore is price(original, observed usage): what the
	// turn would have cost un-routed.
	PromptCostBefore float64 `json:"prompt_cost_before"`
	// PromptCostAfter is price(selected, observed usage): what the
	// turn costs on the routed model.
	PromptCostAfter float64 `json:"prompt_cost_after"`
	// CacheForfeit prices re-creating the session's warm prefix at the
	// selected model's cache-write rate (§R13). Zero when the session
	// had no warm prefix to forfeit.
	CacheForfeit float64 `json:"cache_forfeit"`
	// NetUSD is the per-prompt expected net: prompt_cost_before −
	// prompt_cost_after − cache_forfeit. Positive = the route pays;
	// negative = the route costs (the engine already refuses such
	// switches with ReasonCacheHold, but the record can still show one
	// for a forced/pinned route).
	NetUSD float64 `json:"net_usd"`
	// ExpectedTTFTDeltaMs is the expected change in time-to-first-token
	// (selected p75 latency − original p75 latency, snapshot-derived).
	// Negative = faster.
	ExpectedTTFTDeltaMs int64 `json:"expected_ttft_delta_ms"`
	// QualitySignal is the evidence expectation for the route.
	QualitySignal QualitySignal `json:"quality_signal"`
	// EvidenceConfidence is the calibration confidence in the quality
	// signal (0..1); 0 when uncalibrated.
	EvidenceConfidence float64 `json:"evidence_confidence"`
	// UsageBasis names the token bundle the dollar math priced:
	// "realized" (replay-exact) or "live_prompt" (the coarse live
	// prompt-side estimate). Empty on records built before the field
	// existed — treat as realized. Calibration reads it to grade
	// estimates against realized outcomes (§R18.3).
	UsageBasis string `json:"usage_basis"`
}
