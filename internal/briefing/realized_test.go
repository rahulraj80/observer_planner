package briefing

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRealizedConfidence pins the measured-support curve: 0.5 while
// unconfirmed, +0.05 per sqrt-graded-turn, saturated at 1.0.
func TestRealizedConfidence(t *testing.T) {
	if got := RealizedConfidence(0); got != 0.5 {
		t.Errorf("RealizedConfidence(0) = %.3f, want 0.5", got)
	}
	if got := RealizedConfidence(9); got != 0.65 {
		t.Errorf("RealizedConfidence(9) = %.3f, want 0.65", got)
	}
	if got := RealizedConfidence(100); got != 1.0 {
		t.Errorf("RealizedConfidence(100) = %.3f, want 1.0", got)
	}
	if got := RealizedConfidence(-5); got != 0.5 {
		t.Errorf("RealizedConfidence(-5) = %.3f, want 0.5 (clamped)", got)
	}
}

func realizedFacts(t *testing.T) Facts {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	f := newTestFacts()
	f.RealizedVSEstimated = []RealizedVSEstimated{{
		ApplyStamp:          time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC), // 0.5 day observed to window Until
		Detector:            "routing_shadow",
		ChangeID:            "mode",
		ConfigSurface:       "routing.mode",
		From:                "advise",
		To:                  "enforce",
		ProjectedMonthlyUSD: 6.00,
		RealizedUSD:         0.07,
		RealizedTurns:       9,
		Confidence:          RealizedConfidence(9), // 0.65
	}}
	return f
}

// TestDetectRealizedVSEstimated pins the Stage 3 report card: title
// "applied X on DATE → saved $Y of $Z", prorated projection over the
// observed span, before/after confidence, and the report flag that keeps
// it above the dollar filter.
func TestDetectRealizedVSEstimated(t *testing.T) {
	f := realizedFacts(t)
	opps := detectRealizedVSEstimated(f)
	if len(opps) != 1 {
		t.Fatalf("want 1 card, got %d", len(opps))
	}
	o := opps[0]
	if o.Detector != "realized_vs_estimated" || !o.Report || o.Recurrence {
		t.Errorf("detector/report/recurrence = %q/%v/%v, want realized_vs_estimated/true/false", o.Detector, o.Report, o.Recurrence)
	}
	wantTitle := "Applied routing.mode on 2026-07-31 → saved $0.07 of $0.10 estimated"
	if o.Title != wantTitle {
		t.Errorf("title = %q, want %q", o.Title, wantTitle)
	}
	if o.SavingsUSD != 0.07 {
		t.Errorf("savings = $%.2f, want $0.07", o.SavingsUSD)
	}
	if o.Confidence != 0.65 {
		t.Errorf("confidence = %.2f, want 0.65", o.Confidence)
	}
	wantDedup := "realized_vs_routing.mode_" + strconv.FormatInt(f.RealizedVSEstimated[0].ApplyStamp.Unix(), 10)
	if o.DedupKey != wantDedup {
		t.Errorf("dedup_key = %q, want %q", o.DedupKey, wantDedup)
	}
	j := o.Justification
	if j.Source != "router_decisions" || j.Confidence != 0.65 {
		t.Errorf("justification source/confidence = %q/%.2f", j.Source, j.Confidence)
	}
	if j.Evidence.Numbers["confidence_before"] != 0.8 || j.Evidence.Numbers["confidence_after"] != 0.65 {
		t.Errorf("before/after confidence = %.1f/%.2f, want 0.8/0.65",
			j.Evidence.Numbers["confidence_before"], j.Evidence.Numbers["confidence_after"])
	}
	if j.Evidence.Numbers["projected_window_usd"] != 0.10 || j.Evidence.Numbers["days_observed"] != 0.5 {
		t.Errorf("projected prorated = %.2f over %.2f days, want 0.10 / 0.5",
			j.Evidence.Numbers["projected_window_usd"], j.Evidence.Numbers["days_observed"])
	}
	if j.Evidence.Numbers["projected_monthly_usd"] != 6.00 || j.Evidence.Numbers["realized_turns"] != 9 {
		t.Errorf("monthly projection / realized turns = %.2f / %.0f, want 6.00 / 9",
			j.Evidence.Numbers["projected_monthly_usd"], j.Evidence.Numbers["realized_turns"])
	}
	if !strings.Contains(j.Evidence.Math, "routing.mode") || !strings.Contains(o.Nudge, "graded turns") {
		t.Errorf("math/nudge must name the lever and turns; math=%q nudge=%q", j.Evidence.Math, o.Nudge)
	}
}

// TestDetectRealizedVSEstimated_Empty pins the zero-state: no applied
// changes → no cards.
func TestDetectRealizedVSEstimated_Empty(t *testing.T) {
	f := newTestFacts()
	if opps := detectRealizedVSEstimated(f); len(opps) != 0 {
		t.Fatalf("empty RealizedVSEstimated must yield no cards, got %d", len(opps))
	}
}

// TestRealizedCard_SurvivesDollarFilter pins the report flag: a realized
// card under MinSavingsUSD must still surface (the applied-change loop
// stays visible even when the realized figure is tiny).
func TestRealizedCard_SurvivesDollarFilter(t *testing.T) {
	f := realizedFacts(t)
	f.RealizedVSEstimated[0].RealizedUSD = 0.01
	b := Compose(f)
	found := false
	for _, o := range b.Opportunities {
		if o.Detector == "realized_vs_estimated" {
			found = true
			if o.SavingsUSD != 0.01 {
				t.Errorf("realized savings = $%.2f, want $0.01", o.SavingsUSD)
			}
		}
	}
	if !found {
		t.Fatal("realized card suppressed below MinSavingsUSD — Report must exempt it")
	}
}

// virtuousCycleFacts returns Facts with a VirtuousCycle populated for testing.
func virtuousCycleFacts(t *testing.T) Facts {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	f := newTestFacts()
	f.VirtuousCycle = &VirtuousCycleFacts{
		TotalApplies:       3,
		TotalProjectedUSD:  18.00,
		TotalRealizedUSD:   12.50,
		TotalRealizedTurns: 25,
		ByWeek: []VirtuousCycleWeek{
			{WeekStart: "2026-07-20", Applies: 1, ProjectedUSD: 6.00, RealizedUSD: 4.20, RealizedTurns: 8},
			{WeekStart: "2026-07-27", Applies: 2, ProjectedUSD: 12.00, RealizedUSD: 8.30, RealizedTurns: 17},
		},
	}
	return f
}

// TestDetectVirtuousCycle pins the Stage 3 virtuous-cycle trend card:
// cumulative applies, projected vs realized, with weekly breakdown in evidence.
func TestDetectVirtuousCycle(t *testing.T) {
	f := virtuousCycleFacts(t)
	opps := detectVirtuousCycle(f)
	if len(opps) != 1 {
		t.Fatalf("want 1 card, got %d", len(opps))
	}
	o := opps[0]
	if o.Detector != "virtuous_cycle" || !o.Report || o.Recurrence {
		t.Errorf("detector/report/recurrence = %q/%v/%v, want virtuous_cycle/true/false", o.Detector, o.Report, o.Recurrence)
	}
	if o.DedupKey != "virtuous_cycle" {
		t.Errorf("dedup_key = %q, want virtuous_cycle", o.DedupKey)
	}
	if o.SavingsUSD != 12.50 {
		t.Errorf("savings = $%.2f, want $12.50", o.SavingsUSD)
	}
	expectedConf := RealizedConfidence(25)
	if o.Confidence != expectedConf {
		t.Errorf("confidence = %.2f, want %.2f", o.Confidence, expectedConf)
	}
	if !strings.Contains(o.Title, "3 applies") || !strings.Contains(o.Title, "$18.00") || !strings.Contains(o.Title, "$12.50") {
		t.Errorf("title = %q, want contains '3 applies', '$18.00', '$12.50'", o.Title)
	}
	j := o.Justification
	if j.Source != "router_decisions" {
		t.Errorf("source = %q, want router_decisions", j.Source)
	}
	if j.Evidence.Numbers["total_applies"] != 3 || j.Evidence.Numbers["total_realized_usd"] != 12.50 {
		t.Errorf("evidence numbers: applies=%.0f, realized=%.2f, want 3, 12.50",
			j.Evidence.Numbers["total_applies"], j.Evidence.Numbers["total_realized_usd"])
	}
	if len(j.Evidence.Items) != 2 {
		t.Errorf("weekly evidence items = %d, want 2", len(j.Evidence.Items))
	}
}

// TestDetectVirtuousCycle_Empty pins the zero-state: no virtuous cycle data → no cards.
func TestDetectVirtuousCycle_Empty(t *testing.T) {
	f := newTestFacts()
	if opps := detectVirtuousCycle(f); len(opps) != 0 {
		t.Fatalf("empty VirtuousCycle must yield no cards, got %d", len(opps))
	}
}

// TestVirtuousCycleCard_SurvivesDollarFilter pins the report flag.
func TestVirtuousCycleCard_SurvivesDollarFilter(t *testing.T) {
	f := virtuousCycleFacts(t)
	f.VirtuousCycle.TotalRealizedUSD = 0.01
	b := Compose(f)
	found := false
	for _, o := range b.Opportunities {
		if o.Detector == "virtuous_cycle" {
			found = true
			if o.SavingsUSD != 0.01 {
				t.Errorf("realized savings = $%.2f, want $0.01", o.SavingsUSD)
			}
		}
	}
	if !found {
		t.Fatal("virtuous cycle card suppressed below MinSavingsUSD — Report must exempt it")
	}
}
