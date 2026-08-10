package store

import (
	"context"
	"fmt"
	"time"
)

// analysis_calls persistence seam (docs/design/standup-briefing.md §3.6,
// Phase B): one row per OPTIONAL LLM analysis call. Content-free — ids,
// counts, cost, latency only; no prompt or reply text. Node-local: the table
// is never selected by SelectUnpushedSince (see the migration's privacy note).

// AnalysisCall is one recorded LLM analysis call.
type AnalysisCall struct {
	ConnectionID string
	Model        string
	Task         string // narrative | ask | summarize | triage
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	LatencyMS    int64
	Status       string // ok | error
	ErrorClass   string
}

// RecordAnalysisCall appends an analysis-call accounting row. Idempotency is
// by nature append-only (each row is a distinct call); the caller owns
// deduping retries.
func (s *Store) RecordAnalysisCall(ctx context.Context, c AnalysisCall, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO analysis_calls
    (ts, connection_id, model, task, input_tokens, output_tokens, cost_usd,
     latency_ms, status, error_class, api_turn_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		now.UTC().Format(time.RFC3339), c.ConnectionID, c.Model, c.Task,
		c.InputTokens, c.OutputTokens, c.CostUSD, c.LatencyMS, c.Status, c.ErrorClass)
	if err != nil {
		return fmt.Errorf("store.RecordAnalysisCall: %w", err)
	}
	return nil
}

// ListAnalysisCalls returns the most-recent n analysis calls, newest first
// (used by the LLM-connections status surface and `observer llm status`).
func (s *Store) ListAnalysisCalls(ctx context.Context, n int) ([]AnalysisCall, error) {
	limit := n
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT connection_id, model, task, input_tokens, output_tokens, cost_usd,
       latency_ms, status, COALESCE(error_class, '')
FROM analysis_calls
ORDER BY ts DESC, id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store.ListAnalysisCalls: %w", err)
	}
	defer rows.Close()
	var out []AnalysisCall
	for rows.Next() {
		var c AnalysisCall
		if err := rows.Scan(&c.ConnectionID, &c.Model, &c.Task, &c.InputTokens,
			&c.OutputTokens, &c.CostUSD, &c.LatencyMS, &c.Status, &c.ErrorClass); err != nil {
			return nil, fmt.Errorf("store.ListAnalysisCalls: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.ListAnalysisCalls: %w", err)
	}
	return out, nil
}
