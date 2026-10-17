package routing

import (
	"fmt"
	"strings"
)

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
		// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
		fmt.Fprintf(&sb, ": -$%.2f/prompt", -b.NetUSD)
	} else {
		// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
		fmt.Fprintf(&sb, ": $%.2f/prompt", b.NetUSD)
	}
	if b.CacheForfeit > 0 {
		// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
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
