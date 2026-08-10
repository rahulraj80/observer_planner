package store

import (
	"context"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestSelectBriefingAggregates verifies the Phase C hash-only aggregate
// composition: per (completed-local-day, tool, project_hash) counts + sums over
// api_turns + token_usage with the proxy-origin dedup, and that only COMPLETED
// days are bucketed (the current in-progress day is excluded).
func TestSelectBriefingAggregates(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()
	pid, err := s.UpsertProject(ctx, "/tmp/proj", "git@example.com:acme/app.git")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	if err := s.UpsertSession(ctx, models.Session{
		ID: "s1", ProjectID: pid, Tool: "claude-code", Model: "claude",
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	// Seed yesterday at local noon (a COMPLETED local day) — three real turns
	// plus one token_usage dup of a proxy request_id (must be excluded).
	yesterdayNoon := time.Now().In(time.Local).AddDate(0, 0, -1)
	yesterday := time.Date(yesterdayNoon.Year(), yesterdayNoon.Month(), yesterdayNoon.Day(), 12, 0, 0, 0, time.Local)
	ts := yesterday.UTC().Format(time.RFC3339)

	apiTurn := func(req string, cost float64) {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO api_turns(session_id, project_id, timestamp, provider, model, request_id,
			    input_tokens, output_tokens, cost_usd) VALUES('s1', ?, ?, 'anthropic', 'claude', ?, 100, 50, ?)`,
			pid, ts, req, cost); err != nil {
			t.Fatalf("insert api_turn: %v", err)
		}
	}
	apiTurn("req1", 0.10)
	apiTurn("req2", 0.20)

	tu := func(ev string, cost float64) {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO token_usage(session_id, timestamp, tool, model, input_tokens, output_tokens,
			    estimated_cost_usd, source, source_file, source_event_id)
			 VALUES('s1', ?, 'claude-code', 'claude', 100, 50, ?, 'jsonl', 'f.jsonl', ?)`,
			ts, cost, ev); err != nil {
			t.Fatalf("insert token_usage: %v", err)
		}
	}
	tu("req1", 0.99) // DUP of proxy req1 → excluded by the briefing dedup
	tu("tu1", 0.05)

	rows, err := s.SelectBriefingAggregates(ctx, 7)
	if err != nil {
		t.Fatalf("SelectBriefingAggregates: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (single completed-day, tool, project bucket)", len(rows))
	}
	r := rows[0]
	if r.Day != yesterday.Format("2006-01-02") {
		t.Errorf("day = %q, want %q", r.Day, yesterday.Format("2006-01-02"))
	}
	if r.Tool != "claude-code" {
		t.Errorf("tool = %q, want claude-code", r.Tool)
	}
	if r.ProjectHash == "" {
		t.Error("project_hash is empty — expected the root_path_hash of /tmp/proj")
	}
	if r.Sessions != 1 || r.Turns != 3 {
		t.Errorf("counts = sessions %d turns %d, want 1/3 (req1+req2+tu1; req1 dup dropped)", r.Sessions, r.Turns)
	}
	if r.InputTokens != 300 || r.OutputTokens != 150 {
		t.Errorf("tokens = %d/%d, want 300/150", r.InputTokens, r.OutputTokens)
	}
	if !nearEq(r.CostUSD, 0.35) {
		t.Errorf("cost = %v, want 0.35", r.CostUSD)
	}
}

func nearEq(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}
