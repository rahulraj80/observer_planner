package routing

import "testing"

// benefitTestPrice is a minimal deterministic price seam mirroring the
// engine test's per-token multipliers: $5/M-in $25/M-out cache-write
// $6.25 for opus, $1/$5/$1.25 for haiku, $3/$15/$3.75 for sonnet.
func benefitTestPrice(model string, u PromptUsage) (float64, bool) {
	switch model {
	case "claude-opus-4-8":
		return (float64(u.Input)*5 + float64(u.Output)*25 + float64(u.CacheCreation)*6.25) / 1e6, true
	case "claude-sonnet-4-6":
		return (float64(u.Input)*3 + float64(u.Output)*15 + float64(u.CacheCreation)*3.75) / 1e6, true
	case "claude-haiku-4-5":
		return (float64(u.Input)*1 + float64(u.Output)*5 + float64(u.CacheCreation)*1.25) / 1e6, true
	default:
		return 0, false
	}
}

func TestComputeBenefit(t *testing.T) {
	usage := &PromptUsage{Input: 100_000, Output: 10_000}
	// opus = 0.75, haiku = 0.15, sonnet = 0.45 on that bundle.
	cases := []struct {
		name       string
		original   string
		selected   string
		usage      *PromptUsage
		priorRead  int64
		wantNet    float64
		wantForf   float64
		wantPriced bool
	}{
		{
			name: "cheap_switch_with_forfeit",
			// opus→haiku: gross 0.60, forfeit recreating 100K warm
			// prefix at haiku write rate (100k×1.25/1e6 = 0.125).
			original:   "claude-opus-4-8",
			selected:   "claude-haiku-4-5",
			usage:      usage,
			priorRead:  100_000,
			wantNet:    0.475,
			wantForf:   0.125,
			wantPriced: true,
		},
		{
			name:       "no_warm_prefix_no_forfeit",
			original:   "claude-opus-4-8",
			selected:   "claude-haiku-4-5",
			usage:      usage,
			priorRead:  0,
			wantNet:    0.60,
			wantPriced: true,
		},
		{
			name:       "mid_tier_switch",
			original:   "claude-sonnet-4-6",
			selected:   "claude-haiku-4-5",
			usage:      usage,
			priorRead:  50_000,
			wantNet:    0.30 - 0.0625,
			wantForf:   0.0625,
			wantPriced: true,
		},
		{
			name:       "same_model_zero",
			original:   "claude-opus-4-8",
			selected:   "claude-opus-4-8",
			usage:      usage,
			priorRead:  0,
			wantNet:    0,
			wantPriced: true,
		},
		{
			name:       "unpriceable_selected",
			original:   "claude-opus-4-8",
			selected:   "gpt-unknown",
			usage:      usage,
			priorRead:  0,
			wantPriced: false,
		},
		{
			name:       "nil_usage_unpriced",
			original:   "claude-opus-4-8",
			selected:   "claude-haiku-4-5",
			usage:      nil,
			priorRead:  0,
			wantPriced: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, priced := ComputeBenefit(benefitTestPrice, tc.original, tc.selected, tc.usage, tc.priorRead, 0, QualityUnknown, 0)
			if priced != tc.wantPriced {
				t.Fatalf("priced = %v, want %v (got %+v)", priced, tc.wantPriced, got)
			}
			if !priced {
				return
			}
			const eps = 1e-9
			if !floatNear(got.NetUSD, tc.wantNet, eps) {
				t.Errorf("NetUSD = %v, want %v", got.NetUSD, tc.wantNet)
			}
			if !floatNear(got.CacheForfeit, tc.wantForf, eps) {
				t.Errorf("CacheForfeit = %v, want %v", got.CacheForfeit, tc.wantForf)
			}
			if got.QualitySignal != QualityUnknown || got.EvidenceConfidence != 0 {
				t.Errorf("quality/confidence carried wrong default: %s/%v", got.QualitySignal, got.EvidenceConfidence)
			}
		})
	}
}

func TestComputeBenefit_CarriesInjectedSignals(t *testing.T) {
	usage := &PromptUsage{Input: 100_000, Output: 10_000}
	bb, priced := ComputeBenefit(benefitTestPrice, "claude-opus-4-8", "claude-haiku-4-5",
		usage, 0, -42, QualityHold, 0.95)
	if !priced {
		t.Fatal("expected priced")
	}
	if bb.ExpectedTTFTDeltaMs != -42 {
		t.Errorf("ttft delta = %d, want -42", bb.ExpectedTTFTDeltaMs)
	}
	if bb.QualitySignal != QualityHold || bb.EvidenceConfidence != 0.95 {
		t.Errorf("signal = %s/%.2f, want hold/0.95", bb.QualitySignal, bb.EvidenceConfidence)
	}
}

func TestBenefitText(t *testing.T) {
	usage := &PromptUsage{Input: 100_000, Output: 10_000}
	// priorRead 80K → forfeit 0.10, net 0.50 — clean two-decimal values.
	switched, _ := ComputeBenefit(benefitTestPrice, "claude-opus-4-8", "claude-haiku-4-5",
		usage, 80_000, 0, QualityHold, 0.95)
	kept, _ := ComputeBenefit(benefitTestPrice, "claude-opus-4-8", "claude-opus-4-8",
		usage, 0, 0, QualityUnknown, 0)

	cases := []struct {
		name     string
		kind     TurnKind
		original string
		selected string
		bb       BenefitBreakdown
		want     string
	}{
		{
			name:     "route_with_forfeit_and_confidence",
			kind:     TurnPlan,
			original: "claude-opus-4-8",
			selected: "claude-haiku-4-5",
			bb:       switched,
			want:     "route plan→claude-haiku-4-5: $0.50/prompt, forfeits $0.10 warm prefix, 95% CI no quality delta",
		},
		{
			name:     "keep_no_change_unknown",
			kind:     TurnPlan,
			original: "claude-opus-4-8",
			selected: "claude-opus-4-8",
			bb:       kept,
			want:     "keep claude-opus-4-8: $0.00/prompt, quality unknown",
		},
		{
			name:     "negative_net_shows_sign",
			kind:     TurnReadOnly,
			original: "claude-haiku-4-5",
			selected: "claude-opus-4-8",
			bb: BenefitBreakdown{
				PromptCostBefore: 0.15, PromptCostAfter: 0.75,
				CacheForfeit: 0, NetUSD: -0.60,
				QualitySignal: QualityWorse, EvidenceConfidence: 0.8,
			},
			want: "route read_only→claude-opus-4-8: -$0.60/prompt, 80% CI quality worse",
		},
		{
			name:     "better_signal",
			kind:     TurnEdit,
			original: "claude-haiku-4-5",
			selected: "claude-opus-4-8",
			bb: BenefitBreakdown{
				PromptCostBefore: 0.15, PromptCostAfter: 0.75,
				CacheForfeit: 0.10, NetUSD: -0.70,
				QualitySignal: QualityBetter, EvidenceConfidence: 1,
			},
			want: "route edit→claude-opus-4-8: -$0.70/prompt, forfeits $0.10 warm prefix, 100% CI quality better",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := BenefitText(tc.kind, tc.original, tc.selected, tc.bb); got != tc.want {
				t.Errorf("BenefitText = %q, want %q", got, tc.want)
			}
		})
	}
}

func floatNear(a, b, eps float64) bool { return a-b < eps && b-a < eps }
