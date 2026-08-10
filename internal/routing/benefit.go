package routing

import (
	"fmt"
	"strings"
)

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

// UsageBasis values for BenefitBreakdown.UsageBasis.
const (
	// UsageBasisRealized marks replay-exact math: the turn's realized
	// bundle (simulate / replay paths). The cache-hold gate trusts it.
	UsageBasisRealized = "realized"
	// UsageBasisLivePrompt marks the live pre-response estimate: the
	// request's prompt-token estimate only. Prompt-side dollars are
	// real; output/cache dollars are absent by design — the engine
	// never cache-holds off this partial math (§R13).
	UsageBasisLivePrompt = "live_prompt"
)

// ComputeBenefit prices a proposed route under the p1-v1 estimate and
// returns the structured expected-benefit record (§5.4). priced=false
// when the price seam is nil, the input carries no observed usage, or
// either model is unpriceable — callers then make no dollar claim (the
// exact posture of switchEconomics). The ttft delta and quality signal
// are injected by the caller (latency snapshot / evidence basis) and
// carried through unmodified.
func ComputeBenefit(price PriceFn, original, selected string, usage *PromptUsage, priorCacheReadTokens int64, ttftDeltaMs int64, quality QualitySignal, confidence float64) (BenefitBreakdown, bool) {
	if price == nil || usage == nil {
		return BenefitBreakdown{}, false
	}
	before, ok1 := price(original, *usage)
	after, ok2 := price(selected, *usage)
	if !ok1 || !ok2 {
		return BenefitBreakdown{}, false
	}
	bb := BenefitBreakdown{
		PromptCostBefore:    before,
		PromptCostAfter:     after,
		ExpectedTTFTDeltaMs: ttftDeltaMs,
		QualitySignal:       quality,
		EvidenceConfidence:  confidence,
	}
	if priorCacheReadTokens > 0 {
		if f, ok := price(selected, PromptUsage{CacheCreation: priorCacheReadTokens}); ok {
			bb.CacheForfeit = f
		}
	}
	bb.NetUSD = before - after - bb.CacheForfeit
	return bb, true
}

// BenefitText renders the breakdown as one human sentence for
// dashboards (api-intelligence.md §5.4), e.g.
//
//	"route plan→opus: −$0.18/prompt, forfeits $0.02 warm prefix, 95% CI no quality delta"
//
// Pure: identical inputs always produce identical output. original/selected
// are the model ids; kind is the classified turn-kind. The forfeit clause
// is omitted when the forfeit is zero; the confidence prefix is omitted
// when the signal is uncalibrated.
func BenefitText(kind TurnKind, original, selected string, b BenefitBreakdown) string {
	var sb strings.Builder
	if original != selected {
		fmt.Fprintf(&sb, "route %s→%s", kind, selected)
	} else {
		fmt.Fprintf(&sb, "keep %s", selected)
	}
	if b.NetUSD < 0 {
		fmt.Fprintf(&sb, ": -$%.2f/prompt", -b.NetUSD)
	} else {
		fmt.Fprintf(&sb, ": $%.2f/prompt", b.NetUSD)
	}
	if b.CacheForfeit > 0 {
		fmt.Fprintf(&sb, ", forfeits $%.2f warm prefix", b.CacheForfeit)
	}
	sb.WriteString(", ")
	sb.WriteString(benefitQualityClause(b))
	return sb.String()
}

// benefitQualityClause renders the quality part of the benefit sentence:
// "95% CI no quality delta", "no quality delta", "quality unknown", etc.
func benefitQualityClause(b BenefitBreakdown) string {
	phrase := qualityPhrase(b.QualitySignal)
	if b.EvidenceConfidence > 0 {
		pct := int(b.EvidenceConfidence*100 + 0.5)
		return fmt.Sprintf("%d%% CI %s", pct, phrase)
	}
	return phrase
}

func qualityPhrase(q QualitySignal) string {
	switch q {
	case QualityHold:
		return "no quality delta"
	case QualityBetter:
		return "quality better"
	case QualityWorse:
		return "quality worse"
	default:
		return "quality unknown"
	}
}
