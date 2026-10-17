package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/models"
)

func openRoutingTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "routing_test.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return New(database), ctx
}

// TestInsertRouterDecisions_RoundTrip pins the decision-log owner seam:
// rows land with reason codes JSON-encoded, nullable refs as NULL, and
// the stats rollup sees them.
func TestInsertRouterDecisions_RoundTrip(t *testing.T) {
	t.Parallel()
	st, ctx := openRoutingTestStore(t)

	// Seed a real api_turn — router_decisions.api_turn_id carries a
	// foreign key, and decisions must reference observed turns only.
	turnID, err := st.InsertAPITurn(ctx, models.APITurn{
		SessionID: "sess-1", Provider: "anthropic", Model: "claude-opus-4-8",
		Timestamp: time.Date(2026, 6, 10, 11, 59, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("InsertAPITurn: %v", err)
	}
	ts := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	rows := []RouterDecisionRow{
		{
			APITurnID:       &turnID,
			SessionID:       "sess-1",
			Timestamp:       ts,
			Mode:            "advise",
			Channel:         "B",
			OriginalModel:   "claude-opus-4-8",
			SelectedModel:   "claude-haiku-4-5",
			TurnKind:        "read_only",
			PolicyName:      "value",
			PolicyHash:      "abc123",
			ReasonCodes:     []string{"overpowered_read"},
			EstSavingsUSD:   0.42,
			CacheForfeitUSD: 0.05,
			EstimateVersion: "v0",
			Applied:         false,
			BenefitJSON:     `{"prompt_cost_before":0.75,"prompt_cost_after":0.15,"cache_forfeit":0.05,"net_usd":0.55,"expected_ttft_delta_ms":-40,"quality_signal":"hold","evidence_confidence":0.95}`,
			BenefitText:     "route read_only→claude-haiku-4-5: $0.55/prompt, forfeits $0.05 warm prefix, 95% CI no quality delta",
			EntitlementKind: "api_key",
			EffectiveBasis:  "list",
		},
		{
			// Channel-A decision: no api_turn to hang on (§R26 item 1).
			SessionID:     "sess-1",
			Timestamp:     ts.Add(time.Minute),
			Mode:          "advise",
			Channel:       "A",
			OriginalModel: "claude-opus-4-8",
			SelectedModel: "claude-opus-4-8",
			TurnKind:      "unknown",
			PolicyHash:    "abc123",
			ReasonCodes:   []string{"unknown_turn_kind"},
		},
	}
	if err := st.InsertRouterDecisions(ctx, rows); err != nil {
		t.Fatalf("InsertRouterDecisions: %v", err)
	}

	var (
		gotTurnID    *int64
		gotCodes     string
		gotApplied   int
		gotBackref   *int64
		gotSelected  string
		gotEstimateV string
	)
	if err := st.db.QueryRowContext(ctx,
		`SELECT api_turn_id, reason_codes, applied, outcome_backref, selected_model, estimate_version
		   FROM router_decisions WHERE turn_kind = 'read_only'`).
		Scan(&gotTurnID, &gotCodes, &gotApplied, &gotBackref, &gotSelected, &gotEstimateV); err != nil {
		t.Fatalf("select: %v", err)
	}
	if gotTurnID == nil || *gotTurnID != turnID {
		t.Errorf("api_turn_id = %v, want %d", gotTurnID, turnID)
	}
	var codes []string
	if err := json.Unmarshal([]byte(gotCodes), &codes); err != nil || len(codes) != 1 || codes[0] != "overpowered_read" {
		t.Errorf("reason_codes = %q (err %v), want [overpowered_read]", gotCodes, err)
	}
	if gotApplied != 0 {
		t.Errorf("applied = %d, want 0", gotApplied)
	}
	if gotBackref != nil {
		t.Errorf("outcome_backref = %v, want NULL", gotBackref)
	}
	if gotSelected != "claude-haiku-4-5" || gotEstimateV != "v0" {
		t.Errorf("selected/estimate = %q/%q", gotSelected, gotEstimateV)
	}

	// Channel-A row landed NULL api_turn_id.
	var nullTurn *int64
	if err := st.db.QueryRowContext(ctx,
		`SELECT api_turn_id FROM router_decisions WHERE channel = 'A'`).Scan(&nullTurn); err != nil {
		t.Fatalf("select channel A: %v", err)
	}
	if nullTurn != nil {
		t.Errorf("channel-A api_turn_id = %v, want NULL", nullTurn)
	}

	stats, err := st.SelectRouterDecisionStats(ctx)
	if err != nil {
		t.Fatalf("SelectRouterDecisionStats: %v", err)
	}
	if stats.Count != 2 {
		t.Errorf("stats.Count = %d, want 2", stats.Count)
	}
	if !stats.LastTS.Equal(ts.Add(time.Minute)) {
		t.Errorf("stats.LastTS = %v, want %v", stats.LastTS, ts.Add(time.Minute))
	}

	// Benefit columns survive a select-back through the owner seam.
	detail, ok, err := st.SelectRouterDecisionByID(ctx, 1)
	if err != nil {
		t.Fatalf("SelectRouterDecisionByID: %v", err)
	}
	if !ok {
		t.Fatal("SelectRouterDecisionByID: row 1 missing")
	}
	if detail.BenefitText == "" || detail.BenefitJSON == "" ||
		detail.EntitlementKind != "api_key" || detail.EffectiveBasis != "list" ||
		detail.RealizedDeltaUSD != nil {
		t.Errorf("benefit fields = text:%q json:%q ent:%q basis:%q delta:%v, want populated (json+text, api_key, list, nil delta)",
			detail.BenefitText, detail.BenefitJSON, detail.EntitlementKind, detail.EffectiveBasis, detail.RealizedDeltaUSD)
	}
	// The unpriced channel-A row carries empty benefit columns.
	details, err := st.SelectRouterDecisions(ctx, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("SelectRouterDecisions: %v", err)
	}
	if len(details) != 2 {
		t.Fatalf("got %d rows, want 2", len(details))
	}
	var unpriced RouterDecisionDetail
	for _, r := range details {
		if r.Channel == "A" {
			unpriced = r
		}
	}
	if unpriced.BenefitJSON != "" || unpriced.BenefitText != "" || unpriced.EntitlementKind != "" {
		t.Errorf("unpriced row benefit = %+v, want all empty", unpriced)
	}
}

// TestSelectRouterDecisionStats_Empty pins zero-state behavior: empty
// table → zero stats, no error.
func TestSelectRouterDecisionStats_Empty(t *testing.T) {
	t.Parallel()
	st, ctx := openRoutingTestStore(t)
	stats, err := st.SelectRouterDecisionStats(ctx)
	if err != nil {
		t.Fatalf("SelectRouterDecisionStats: %v", err)
	}
	if stats.Count != 0 || !stats.LastTS.IsZero() {
		t.Errorf("empty stats = %+v, want zero", stats)
	}
}

// TestUpsertModelCalibrations_UpsertSemantics pins the cell identity:
// re-writing the same (model, turn_kind, project, window) cell replaces
// its aggregates instead of duplicating the row.
func TestUpsertModelCalibrations_UpsertSemantics(t *testing.T) {
	t.Parallel()
	st, ctx := openRoutingTestStore(t)

	cell := ModelCalibrationRow{
		Model:      "claude-opus-4-8",
		TurnKind:   "edit",
		ProjectID:  0,
		WindowDays: 30,
		ComputedAt: time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC),
		N:          100, CostUSDTotal: 12.5,
		LatencyP50Ms: 800, LatencyP95Ms: 4000, LatencyGraded: 60,
		ErrorCount: 2, ErrorGraded: 60,
		ToolFailureCount: 5, ToolActionCount: 400,
	}
	if err := st.UpsertModelCalibrations(ctx, []ModelCalibrationRow{cell}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	cell.N = 150
	cell.ErrorCount = 3
	cell.ComputedAt = cell.ComputedAt.Add(time.Hour)
	if err := st.UpsertModelCalibrations(ctx, []ModelCalibrationRow{cell}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	count, err := st.CountModelCalibrations(ctx)
	if err != nil {
		t.Fatalf("CountModelCalibrations: %v", err)
	}
	if count != 1 {
		t.Errorf("cell count = %d, want 1 (upsert must not duplicate)", count)
	}
	var n, errCount int64
	if err := st.db.QueryRowContext(ctx,
		`SELECT n, error_count FROM model_calibration`).Scan(&n, &errCount); err != nil {
		t.Fatalf("select: %v", err)
	}
	if n != 150 || errCount != 3 {
		t.Errorf("aggregates = (%d, %d), want (150, 3)", n, errCount)
	}

	// A different window is a different cell.
	cell.WindowDays = 7
	if err := st.UpsertModelCalibrations(ctx, []ModelCalibrationRow{cell}); err != nil {
		t.Fatalf("third upsert: %v", err)
	}
	if count, _ := st.CountModelCalibrations(ctx); count != 2 {
		t.Errorf("cell count = %d, want 2 after distinct window", count)
	}
}

// TestRoutingTables_EmptyBatchNoOp pins the no-op contract for empty
// batches (callers pass through without opening a transaction).
func TestRoutingTables_EmptyBatchNoOp(t *testing.T) {
	t.Parallel()
	st, ctx := openRoutingTestStore(t)
	if err := st.InsertRouterDecisions(ctx, nil); err != nil {
		t.Errorf("InsertRouterDecisions(nil) = %v, want nil", err)
	}
	if err := st.UpsertModelCalibrations(ctx, nil); err != nil {
		t.Errorf("UpsertModelCalibrations(nil) = %v, want nil", err)
	}
}

// TestSelectRealizedRoutingDelta pins the Stage 3 realized-vs-estimated
// aggregate: only APPLIED decisions with a realized grade (non-null
// realized_delta_usd) inside the window count, and the optional mode /
// policy filters attribute savings to the CTA lever that was applied.
func TestSelectRealizedRoutingDelta(t *testing.T) {
	t.Parallel()
	st, ctx := openRoutingTestStore(t)

	delta := func(v float64) *float64 { return &v }
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	rows := []RouterDecisionRow{
		{SessionID: "s", Timestamp: base, Mode: "enforce", PolicyName: "value", Applied: true, RealizedDeltaUSD: delta(0.07)},
		{SessionID: "s", Timestamp: base.Add(5 * time.Minute), Mode: "enforce", PolicyName: "value", Applied: true, RealizedDeltaUSD: delta(0.03)},
		// Different mode — excluded when filtering mode=enforce.
		{SessionID: "s", Timestamp: base.Add(10 * time.Minute), Mode: "advise", PolicyName: "value", Applied: true, RealizedDeltaUSD: delta(0.02)},
		// Different policy — excluded when filtering policy=value.
		{SessionID: "s", Timestamp: base.Add(15 * time.Minute), Mode: "enforce", PolicyName: "other", Applied: true, RealizedDeltaUSD: delta(0.09)},
		// Unapplied — never counted (decisions in advise do not realize).
		{SessionID: "s", Timestamp: base.Add(20 * time.Minute), Mode: "enforce", PolicyName: "value", Applied: false, RealizedDeltaUSD: delta(0.50)},
		// Graded-unaware row (NULL realized_delta_usd) — contributes zero dollars and no turn.
		{SessionID: "s", Timestamp: base.Add(25 * time.Minute), Mode: "enforce", PolicyName: "value", Applied: true},
		// Outside the window bounds — excluded.
		{SessionID: "s", Timestamp: base.Add(-time.Hour), Mode: "enforce", PolicyName: "value", Applied: true, RealizedDeltaUSD: delta(0.10)},
		{SessionID: "s", Timestamp: base.Add(2 * time.Hour), Mode: "enforce", PolicyName: "value", Applied: true, RealizedDeltaUSD: delta(0.10)},
	}
	if err := st.InsertRouterDecisions(ctx, rows); err != nil {
		t.Fatalf("InsertRouterDecisions: %v", err)
	}

	q := RealizedRoutingQuery{Since: base, Until: base.Add(30 * time.Minute), Mode: "enforce"}
	got, err := st.SelectRealizedRoutingDelta(ctx, q)
	if err != nil {
		t.Fatalf("SelectRealizedRoutingDelta: %v", err)
	}
	// mode=enforce matches the two "value" rows (0.07+0.03) plus the
	// "other"-policy row (0.09) — the mode filter alone does not exclude it.
	if got.DeltaUSD != 0.19 || got.GradedTurns != 3 {
		t.Errorf("mode=enforce aggregate = ($%.2f, %d turns), want ($0.19, 3)", got.DeltaUSD, got.GradedTurns)
	}

	q.Policy = "value"
	got, err = st.SelectRealizedRoutingDelta(ctx, q)
	if err != nil {
		t.Fatalf("SelectRealizedRoutingDelta: %v", err)
	}
	if got.DeltaUSD != 0.10 || got.GradedTurns != 2 {
		t.Errorf("mode+policy aggregate = ($%.2f, %d turns), want ($0.10, 2)", got.DeltaUSD, got.GradedTurns)
	}

	got, err = st.SelectRealizedRoutingDelta(ctx, RealizedRoutingQuery{Since: base, Until: base.Add(30 * time.Minute)})
	if err != nil {
		t.Fatalf("SelectRealizedRoutingDelta (no filter): %v", err)
	}
	if got.DeltaUSD != 0.21 || got.GradedTurns != 4 {
		t.Errorf("unfiltered aggregate = ($%.2f, %d turns), want ($0.21, 4)", got.DeltaUSD, got.GradedTurns)
	}

	empty, err := st.SelectRealizedRoutingDelta(ctx, RealizedRoutingQuery{Since: base.Add(time.Hour), Until: base.Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("SelectRealizedRoutingDelta (empty): %v", err)
	}
	if empty.DeltaUSD != 0 || empty.GradedTurns != 0 {
		t.Errorf("empty aggregate = ($%.2f, %d), want (0, 0)", empty.DeltaUSD, empty.GradedTurns)
	}
}
