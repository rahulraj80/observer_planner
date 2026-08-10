package routing

import "testing"

// wellSampled hold/worse/better pools used across cases. Numbers chosen
// so the 95% CI verdicts are deterministic under the normal
// approximation.
func sample(count, graded int64) EvidenceSample {
	return EvidenceSample{N: graded, ErrorGraded: graded, ErrorCount: count}
}

func TestQualityEvidenceFor_UnknownBelowFloor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		candidate EvidenceSample
		baseline  EvidenceSample
		minSample int64
	}{
		{
			name:      "both_sides_below_floor",
			candidate: sample(1, 10), baseline: sample(0, 10),
		},
		{
			name:      "one_side_voluminous_other_below_floor",
			candidate: sample(40, 2000), baseline: sample(0, 10),
		},
		{
			name:      "nothing_graded",
			candidate: EvidenceSample{N: 500}, baseline: EvidenceSample{N: 500},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ev := QualityEvidenceFor(tc.candidate, tc.baseline, tc.minSample)
			if ev.Signal != QualityUnknown || ev.Confidence != 0 {
				t.Errorf("evidence = %+v, want unknown/0", ev)
			}
		})
	}
}

func TestQualityEvidenceFor_Verdicts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		candidate  EvidenceSample
		baseline   EvidenceSample
		minSample  int64
		wantSignal QualitySignal
		wantConfLo float64
		wantConfHi float64
	}{
		{
			// 10% vs 5% at n=1000 each: interval ±~2.3pp, excludes zero.
			name:      "worse_well_sampled",
			candidate: sample(100, 1000), baseline: sample(50, 1000),
			wantSignal: QualityWorse,
			wantConfLo: 0.90, wantConfHi: 0.95,
		},
		{
			// 1% vs 5% at n=2000 each: interval ±~1pp, excludes zero.
			name:      "better_well_sampled",
			candidate: sample(20, 2000), baseline: sample(100, 2000),
			wantSignal: QualityBetter,
			wantConfLo: 0.90, wantConfHi: 0.95,
		},
		{
			// Identical 2% rates at n=2000 each: the §5.4 exemplar —
			// "95% CI no quality delta".
			name:      "hold_tight_interval",
			candidate: sample(40, 2000), baseline: sample(40, 2000),
			wantSignal: QualityHold,
			wantConfLo: 0.95, wantConfHi: 0.95,
		},
		{
			// 5% vs 0% at the n=50 floor: interval still straddles zero,
			// and its upper edge (~11pp) is well under the 20% material
			// regression bar — a weak but present hold.
			name:      "hold_at_floor",
			candidate: sample(3, 50), baseline: sample(0, 50),
			wantSignal: QualityHold,
			wantConfLo: 0.50, wantConfHi: 0.70,
		},
		{
			// 25% vs 15% at n=100 each: the interval straddles zero (hold
			// by the parity rule) but reaches a 21pp upper edge — past the
			// material bar — so confidence floors at 0.5: "we cannot claim
			// it is safe".
			name:      "hold_cannot_exclude_material_regression",
			candidate: sample(25, 100), baseline: sample(15, 100),
			wantSignal: QualityHold,
			wantConfLo: 0.50, wantConfHi: 0.50,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ev := QualityEvidenceFor(tc.candidate, tc.baseline, tc.minSample)
			if ev.Signal != tc.wantSignal {
				t.Errorf("signal = %q, want %q", ev.Signal, tc.wantSignal)
			}
			if ev.Confidence < tc.wantConfLo || ev.Confidence > tc.wantConfHi {
				t.Errorf("confidence = %.4f, want in [%.2f, %.2f]", ev.Confidence, tc.wantConfLo, tc.wantConfHi)
			}
		})
	}
}

func TestQualityEvidenceFor_ToolFailureFallback(t *testing.T) {
	t.Parallel()
	// Error side ungraded on both pools; the tool-failure side passes
	// the floor and grades worse (30% vs 10% at n=100 each).
	cand := EvidenceSample{N: 100, ToolGraded: 100, ToolFailures: 30}
	base := EvidenceSample{N: 100, ToolGraded: 100, ToolFailures: 10}
	ev := QualityEvidenceFor(cand, base, 0)
	if ev.Signal != QualityWorse {
		t.Errorf("signal = %q, want worse", ev.Signal)
	}
	if ev.Confidence < 0.85 || ev.Confidence > 0.95 {
		t.Errorf("confidence = %.4f, want ≈0.89", ev.Confidence)
	}
}

func TestQualityEvidenceFor_CompoundsWithEvidence(t *testing.T) {
	t.Parallel()
	// Identical 2% error rates at increasing sample sizes: the hold
	// confidence tightens toward the 0.95 cap as n grows (CI ∝ 1/√n).
	confidence := func(n int64) float64 {
		return QualityEvidenceFor(sample(2*n/100, n), sample(2*n/100, n), 0).Confidence
	}
	low := confidence(100)
	mid := confidence(1000)
	high := confidence(2000)
	if low < 0.78 || low > 0.84 {
		t.Errorf("n=100 confidence = %.4f, want ≈0.81", low)
	}
	if mid < 0.92 || mid > 0.95 {
		t.Errorf("n=1000 confidence = %.4f, want ≈0.94", mid)
	}
	if high != 0.95 {
		t.Errorf("n=2000 confidence = %v, want capped 0.95", high)
	}
	if !(low < mid && mid <= high) {
		t.Errorf("confidence must compound with n: %v < %v <= %v", low, mid, high)
	}
}

func TestQualityEvidenceFor_MinSampleOverride(t *testing.T) {
	t.Parallel()
	// The caller's floor wins: with minSample=10 the 10-graded pools
	// grade; with the default it would be unknown.
	ev := QualityEvidenceFor(sample(3, 10), sample(0, 10), 10)
	if ev.Signal == QualityUnknown {
		t.Errorf("signal = unknown, want graded under minSample=10")
	}
	if ev.Signal != QualityWorse {
		t.Errorf("signal = %q, want worse (30%% vs 0%%)", ev.Signal)
	}
}

func TestQualityEvidenceFor_RateHelpers(t *testing.T) {
	t.Parallel()
	e := EvidenceSample{N: 100, ErrorGraded: 50, ErrorCount: 5, ToolGraded: 40, ToolFailures: 4}
	if e.ErrorRate() != 0.10 || e.ToolFailureRate() != 0.10 {
		t.Errorf("rates = %v/%v, want 0.10/0.10", e.ErrorRate(), e.ToolFailureRate())
	}
	if (EvidenceSample{}).ErrorRate() != 0 || (EvidenceSample{}).ToolFailureRate() != 0 {
		t.Errorf("empty sample rates must be 0")
	}
}
