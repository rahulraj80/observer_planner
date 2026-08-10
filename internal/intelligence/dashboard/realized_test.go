package dashboard

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/briefing"
	"github.com/marmutapp/superbased-observer/internal/configapply"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// writeLedgerApply appends one apply event directly to the briefing-CTA
// ledger file (bypassing RecordApply's wall clock so the stamp is pinned
// inside the briefing window).
func writeLedgerApply(t *testing.T, s *Server, ev configapply.LedgerEvent) {
	t.Helper()
	path := configapply.DefaultLedgerPath(s.opts.DBPath)
	line, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}

// TestBriefing_RealizedVSEstimated pins the Stage 3 loader: a CTA apply
// recorded in the ledger surfaces as a report card carrying the realized
// savings measured on router_decisions AFTER the apply stamp (mode-
// filtered so pre-restart / old-config turns never count).
func TestBriefing_RealizedVSEstimated(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)

	// In-window apply: window = [2026-07-31T22:00Z, 2026-08-01T22:00Z).
	applyTS := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	writeLedgerApply(t, s, configapply.LedgerEvent{
		Time: applyTS, Kind: "apply", Source: "briefing-cta",
		Section: "routing", Key: "mode", From: "advise", To: "enforce",
		DedupKey: "routing_shadow_1", Detector: "routing_shadow",
		ProjectedUSD: 6.00,
	})

	// Realized decisions: one in-window enforced+applied turn that counts
	// ($0.07), plus three that must NOT (pre-apply, unapplied, wrong mode).
	delta := func(v float64) *float64 { return &v }
	rows := []store.RouterDecisionRow{
		{SessionID: "s1", Timestamp: applyTS.Add(2 * time.Hour), Mode: "enforce", PolicyName: "value", Applied: true, RealizedDeltaUSD: delta(0.07)},
		{SessionID: "s1", Timestamp: applyTS.Add(-2 * time.Hour), Mode: "enforce", PolicyName: "value", Applied: true, RealizedDeltaUSD: delta(0.99)},
		{SessionID: "s1", Timestamp: applyTS.Add(3 * time.Hour), Mode: "enforce", PolicyName: "value", Applied: false, RealizedDeltaUSD: delta(0.50)},
		{SessionID: "s1", Timestamp: applyTS.Add(4 * time.Hour), Mode: "advise", PolicyName: "value", Applied: true, RealizedDeltaUSD: delta(0.30)},
	}
	if err := st.InsertRouterDecisions(context.Background(), rows); err != nil {
		t.Fatal(err)
	}

	_, b := getBriefing(t, s, "/api/briefing")
	var card *briefing.Opportunity
	for i := range b.Opportunities {
		if b.Opportunities[i].Detector == "realized_vs_estimated" {
			card = &b.Opportunities[i]
		}
	}
	if card == nil {
		t.Fatal("no realized_vs_estimated card in briefing")
	}
	wantTitle := "Applied routing.mode on 2026-08-01 → saved $0.07 of $0.10 estimated"
	if card.Title != wantTitle {
		t.Errorf("title = %q, want %q", card.Title, wantTitle)
	}
	if !card.Report || card.SavingsUSD != 0.07 {
		t.Errorf("report/savings = %v/$%.2f, want true/$0.07", card.Report, card.SavingsUSD)
	}
	j := card.Justification
	if j.Evidence.Numbers["realized_turns"] != 1 {
		t.Errorf("realized_turns = %.0f, want 1 (only the enforced+applied in-window turn)", j.Evidence.Numbers["realized_turns"])
	}
	if j.Evidence.Numbers["realized_usd"] != 0.07 || j.Evidence.Numbers["projected_window_usd"] != 0.10 {
		t.Errorf("realized/projected = %.2f/%.2f, want 0.07/0.10", j.Evidence.Numbers["realized_usd"], j.Evidence.Numbers["projected_window_usd"])
	}
	if j.Evidence.Numbers["confidence_before"] != 0.8 || j.Evidence.Numbers["confidence_after"] != 0.55 {
		t.Errorf("before/after confidence = %.1f/%.2f, want 0.8/0.55 (1 graded turn)",
			j.Evidence.Numbers["confidence_before"], j.Evidence.Numbers["confidence_after"])
	}
}

// TestBriefing_RealizedVSEstimated_SkipFilters pins what does NOT surface:
// applies outside the window, non-routing applies (no measurement channel),
// and reverts are all skipped.
func TestBriefing_RealizedVSEstimated_SkipFilters(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)

	applyTS := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	writeLedgerApply(t, s, configapply.LedgerEvent{Time: applyTS.Add(-48 * time.Hour), Kind: "apply", Source: "briefing-cta", Section: "routing", Key: "mode", From: "advise", To: "enforce", Detector: "routing_shadow", ProjectedUSD: 6.00})
	writeLedgerApply(t, s, configapply.LedgerEvent{Time: applyTS, Kind: "apply", Source: "briefing-cta", Section: "cache", Key: "warm", From: "off", To: "on", Detector: "cache_hit_rate", ProjectedUSD: 9.00})
	writeLedgerApply(t, s, configapply.LedgerEvent{Time: applyTS, Kind: "revert", Source: "briefing-cta", Section: "routing", Key: "mode", From: "enforce", To: "advise", Detector: "routing_shadow", ProjectedUSD: 6.00})

	_, b := getBriefing(t, s, "/api/briefing")
	for _, o := range b.Opportunities {
		if o.Detector == "realized_vs_estimated" {
			t.Fatalf("expected no realized card (out-of-window / non-routing / revert), got %q", o.Title)
		}
	}
}

// TestBriefing_VirtuousCycle pins the Stage 3 item 2 loader: the cumulative
// virtuous-cycle trend card is built from ALL ledger applies (not just
// in-window) and queries router_decisions for realized savings since the
// EARLIEST apply (cumulative, no mode/policy filter to avoid double-count).
func TestBriefing_VirtuousCycle(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)

	// Two applies at different times (one older, one recent)
	applyTS1 := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC) // older
	applyTS2 := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)  // recent (in-window)

	writeLedgerApply(t, s, configapply.LedgerEvent{Time: applyTS1, Kind: "apply", Source: "briefing-cta", Section: "routing", Key: "mode", From: "advise", To: "enforce", Detector: "routing_shadow", ProjectedUSD: 6.00})
	writeLedgerApply(t, s, configapply.LedgerEvent{Time: applyTS2, Kind: "apply", Source: "briefing-cta", Section: "routing", Key: "policy", From: "value", To: "balanced", Detector: "routing_shadow", ProjectedUSD: 12.00})

	// Realized decisions: cumulative from earliest apply (2026-07-20) to now (2026-08-01T22:00Z).
	// Counts all applied turns in that window (no mode/policy filter to avoid double-count):
	// - applyTS1+2h, mode=enforce: $4.20
	// - applyTS2+2h, mode=enforce: $8.30
	// - applyTS2+3h, mode=advise: $3.00
	// Pre-apply (applyTS1-2h) excluded (before earliest); unapplied excluded.
	delta := func(v float64) *float64 { return &v }
	rows := []store.RouterDecisionRow{
		{SessionID: "s1", Timestamp: applyTS1.Add(2 * time.Hour), Mode: "enforce", PolicyName: "value", Applied: true, RealizedDeltaUSD: delta(4.20)},
		{SessionID: "s1", Timestamp: applyTS1.Add(-2 * time.Hour), Mode: "enforce", PolicyName: "value", Applied: true, RealizedDeltaUSD: delta(9.99)}, // pre-apply, excluded
		{SessionID: "s1", Timestamp: applyTS2.Add(2 * time.Hour), Mode: "enforce", PolicyName: "balanced", Applied: true, RealizedDeltaUSD: delta(8.30)},
		{SessionID: "s1", Timestamp: applyTS2.Add(3 * time.Hour), Mode: "advise", PolicyName: "balanced", Applied: true, RealizedDeltaUSD: delta(3.00)}, // counts (no mode filter)
		{SessionID: "s1", Timestamp: applyTS2.Add(4 * time.Hour), Mode: "enforce", PolicyName: "value", Applied: false, RealizedDeltaUSD: delta(5.00)},  // unapplied, excluded
	}
	if err := st.InsertRouterDecisions(context.Background(), rows); err != nil {
		t.Fatal(err)
	}

	_, b := getBriefing(t, s, "/api/briefing")
	var card *briefing.Opportunity
	for i := range b.Opportunities {
		if b.Opportunities[i].Detector == "virtuous_cycle" {
			card = &b.Opportunities[i]
		}
	}
	if card == nil {
		t.Fatal("no virtuous_cycle card in briefing")
	}
	if !card.Report || card.SavingsUSD != 15.50 {
		t.Errorf("report/savings = %v/$%.2f, want true/$15.50", card.Report, card.SavingsUSD)
	}
	wantTitle := "Virtuous cycle: 2 applies → $18.00 projected, $15.50 realized (86%)"
	if card.Title != wantTitle {
		t.Errorf("title = %q, want %q", card.Title, wantTitle)
	}
	j := card.Justification
	if j.Evidence.Numbers["total_applies"] != 2 || j.Evidence.Numbers["total_realized_usd"] != 15.50 {
		t.Errorf("total_applies/realized = %.0f/%.2f, want 2/15.50", j.Evidence.Numbers["total_applies"], j.Evidence.Numbers["total_realized_usd"])
	}
	// Two applies in different weeks → 2 weekly buckets in evidence
	if len(j.Evidence.Items) != 2 {
		t.Errorf("weekly evidence items = %d, want 2", len(j.Evidence.Items))
	}
}

// TestBriefing_VirtuousCycle_SkipFilters pins what does NOT surface:
// non-routing applies are skipped.
func TestBriefing_VirtuousCycle_SkipFilters(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)

	applyTS := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	writeLedgerApply(t, s, configapply.LedgerEvent{Time: applyTS, Kind: "apply", Source: "briefing-cta", Section: "cache", Key: "warm", From: "off", To: "on", Detector: "cache_hit_rate", ProjectedUSD: 9.00})

	_, b := getBriefing(t, s, "/api/briefing")
	for _, o := range b.Opportunities {
		if o.Detector == "virtuous_cycle" {
			t.Fatalf("expected no virtuous cycle card (non-routing apply), got %q", o.Title)
		}
	}
}
