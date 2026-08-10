package store

import (
	"testing"
	"time"
)

// TestAdviseShadowSignal pins the X3.1 composite: an empty window
// early-outs to a not-ready zero report (no modelvalue load), and with
// advise history the gate composes through BuildAdviseShadowReport —
// reroutes without parity evidence flag, so Ready stays false (maximum
// caution; the gate logic itself is pinned by the shadow-report tests).
func TestAdviseShadowSignal(t *testing.T) {
	t.Parallel()
	st, ctx := openRoutingTestStore(t)

	rep, err := st.AdviseShadowSignal(ctx, 14, nil)
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if rep.AdviseDecisions != 0 || rep.ReadyToPromote || rep.MinDecisions == 0 {
		t.Fatalf("empty signal = %+v, want zero not-ready with the floor carried", rep)
	}

	now := time.Now().UTC()
	rows := []RouterDecisionRow{
		{
			SessionID: "sA", Timestamp: now.Add(-time.Hour), Mode: "advise", Channel: "B",
			OriginalModel: "claude-opus-4-8", SelectedModel: "claude-haiku-4-5",
			TurnKind: "read_only", PolicyName: "value", PolicyHash: "h",
			ReasonCodes: []string{"overpowered_read"}, EstSavingsUSD: 0.4, EstimateVersion: "p1-v1",
		},
		{
			SessionID: "sA", Timestamp: now.Add(-2 * time.Hour), Mode: "advise", Channel: "B",
			OriginalModel: "claude-opus-4-8", SelectedModel: "claude-opus-4-8",
			TurnKind: "plan", PolicyName: "value", PolicyHash: "h",
			ReasonCodes: []string{"no_route"}, EstimateVersion: "p1-v1",
		},
	}
	if err := st.InsertRouterDecisions(ctx, rows); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rep, err = st.AdviseShadowSignal(ctx, 14, nil)
	if err != nil {
		t.Fatalf("seeded: %v", err)
	}
	if rep.AdviseDecisions != 2 || rep.WouldReroute != 1 {
		t.Fatalf("signal = %+v, want 2 advise decisions / 1 reroute", rep)
	}
	if rep.PolicyName != "value" {
		t.Errorf("policy_name = %q, want the dominant decide-under policy %q (the CTA apply lever)", rep.PolicyName, "value")
	}
	if rep.QualityFlags != 1 {
		t.Errorf("reroute without parity evidence must flag (maximum caution): %+v", rep)
	}
	if rep.ReadyToPromote {
		t.Error("2 decisions with a flag must not read as ready")
	}
}

// TestAdviseShadowSignal_StickinessHolds verifies the stickiness hold
// breakdown is computed: held switches are counted and their estimated
// savings are projected from the average reroute savings.
func TestAdviseShadowSignal_StickinessHolds(t *testing.T) {
	t.Parallel()
	st, ctx := openRoutingTestStore(t)

	now := time.Now().UTC()
	rows := []RouterDecisionRow{
		// 2 reroutes with savings
		{
			SessionID: "s1", Timestamp: now.Add(-time.Hour), Mode: "advise", Channel: "B",
			OriginalModel: "claude-opus-4-8", SelectedModel: "claude-haiku-4-5",
			TurnKind: "read_only", PolicyName: "value", PolicyHash: "h",
			ReasonCodes: []string{"overpowered_read"}, EstSavingsUSD: 0.5, EstimateVersion: "p1-v1",
		},
		{
			SessionID: "s2", Timestamp: now.Add(-2 * time.Hour), Mode: "advise", Channel: "B",
			OriginalModel: "claude-sonnet-4", SelectedModel: "claude-haiku-4-5",
			TurnKind: "read_only", PolicyName: "value", PolicyHash: "h",
			ReasonCodes: []string{"overpowered_read"}, EstSavingsUSD: 0.3, EstimateVersion: "p1-v1",
		},
		// 3 stickiness holds
		{
			SessionID: "s3", Timestamp: now.Add(-3 * time.Hour), Mode: "advise", Channel: "B",
			OriginalModel: "claude-opus-4-8", SelectedModel: "claude-opus-4-8",
			TurnKind: "edit", PolicyName: "value", PolicyHash: "h",
			ReasonCodes: []string{"stickiness_hold"}, EstimateVersion: "p1-v1",
		},
		{
			SessionID: "s4", Timestamp: now.Add(-4 * time.Hour), Mode: "advise", Channel: "B",
			OriginalModel: "claude-opus-4-8", SelectedModel: "claude-opus-4-8",
			TurnKind: "edit", PolicyName: "value", PolicyHash: "h",
			ReasonCodes: []string{"stickiness_hold"}, EstimateVersion: "p1-v1",
		},
		{
			SessionID: "s5", Timestamp: now.Add(-5 * time.Hour), Mode: "advise", Channel: "B",
			OriginalModel: "claude-opus-4-8", SelectedModel: "claude-opus-4-8",
			TurnKind: "edit", PolicyName: "value", PolicyHash: "h",
			ReasonCodes: []string{"stickiness_hold"}, EstimateVersion: "p1-v1",
		},
		// 1 cache hold (should NOT count as stickiness)
		{
			SessionID: "s6", Timestamp: now.Add(-6 * time.Hour), Mode: "advise", Channel: "B",
			OriginalModel: "claude-opus-4-8", SelectedModel: "claude-opus-4-8",
			TurnKind: "edit", PolicyName: "value", PolicyHash: "h",
			ReasonCodes: []string{"cache_hold"}, EstimateVersion: "p1-v1",
		},
	}
	if err := st.InsertRouterDecisions(ctx, rows); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rep, err := st.AdviseShadowSignal(ctx, 14, nil)
	if err != nil {
		t.Fatalf("seeded: %v", err)
	}
	if rep.AdviseDecisions != 6 {
		t.Fatalf("advise decisions = %d, want 6", rep.AdviseDecisions)
	}
	if rep.WouldReroute != 2 {
		t.Fatalf("would reroute = %d, want 2", rep.WouldReroute)
	}
	if rep.StickinessHolds != 3 {
		t.Fatalf("stickiness holds = %d, want 3", rep.StickinessHolds)
	}
	// avg reroute savings = (0.5 + 0.3) / 2 = 0.4; × 3 holds = 1.2
	if rep.StickinessHoldSavingsUSD < 1.19 || rep.StickinessHoldSavingsUSD > 1.21 {
		t.Fatalf("stickiness hold savings = %.2f, want ~1.20", rep.StickinessHoldSavingsUSD)
	}
	if rep.HoldsByReason["stickiness_hold"] != 3 {
		t.Fatalf("holds by reason stickiness = %d, want 3", rep.HoldsByReason["stickiness_hold"])
	}
	if rep.HoldsByReason["cache_hold"] != 1 {
		t.Fatalf("holds by reason cache = %d, want 1", rep.HoldsByReason["cache_hold"])
	}
}

// TestDominantPolicyName pins the report's decide-under-policy read:
// the most frequent advise-row policy wins, ties break alphabetically
// for determinism, and enforced rows never participate.
func TestDominantPolicyName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		rows []RouterDecisionDetail
		want string
	}{
		{"empty", nil, ""},
		{
			"enforce rows excluded",
			[]RouterDecisionDetail{{Mode: "enforce", PolicyName: "frugal"}, {Mode: "advise", Applied: true, PolicyName: "fast"}},
			"",
		},
		{
			"most frequent wins",
			[]RouterDecisionDetail{
				{Mode: "advise", PolicyName: "value"},
				{Mode: "advise", PolicyName: "frugal"},
				{Mode: "advise", PolicyName: "value"},
			},
			"value",
		},
		{
			"tie breaks alphabetically",
			[]RouterDecisionDetail{
				{Mode: "advise", PolicyName: "frugal"},
				{Mode: "advise", PolicyName: "fast"},
			},
			"fast",
		},
		{
			"unnamed rows ignored",
			[]RouterDecisionDetail{{Mode: "advise"}, {Mode: "advise", PolicyName: "plan-exec"}},
			"plan-exec",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dominantPolicyName(tc.rows); got != tc.want {
				t.Fatalf("dominantPolicyName = %q, want %q", got, tc.want)
			}
		})
	}
}
