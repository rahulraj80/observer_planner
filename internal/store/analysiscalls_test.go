package store

import (
	"context"
	"testing"
	"time"
)

func TestAnalysisCalls_RoundTrip(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)

	if got, err := s.ListAnalysisCalls(ctx, 10); err != nil {
		t.Fatal(err)
	} else if len(got) != 0 {
		t.Fatalf("expected no calls, got %d", len(got))
	}

	calls := []AnalysisCall{
		{
			ConnectionID: "local-ollama", Model: "qwen2.5:7b", Task: "narrative",
			InputTokens: 100, OutputTokens: 50, CostUSD: 0.001, LatencyMS: 1200, Status: "ok",
		},
		{
			ConnectionID: "local-ollama", Model: "qwen2.5:7b", Task: "ask",
			InputTokens: 200, OutputTokens: 80, CostUSD: 0.002, LatencyMS: 900, Status: "ok",
		},
		{
			ConnectionID: "remote-anthropic", Model: "claude-haiku", Task: "ask",
			InputTokens: 10, OutputTokens: 5, CostUSD: 0.0005, LatencyMS: 800, Status: "error", ErrorClass: "upstream 429",
		},
	}
	for i, c := range calls {
		if err := s.RecordAnalysisCall(ctx, c, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ListAnalysisCalls(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d calls, want 3", len(got))
	}
	// Newest first.
	if got[0].Task != "ask" || got[0].ConnectionID != "remote-anthropic" || got[0].Status != "error" || got[0].ErrorClass != "upstream 429" {
		t.Errorf("newest row wrong: %+v", got[0])
	}
	if got[2].Task != "narrative" || got[2].CostUSD != 0.001 || got[2].LatencyMS != 1200 {
		t.Errorf("oldest row wrong: %+v", got[2])
	}

	// Limit respected.
	if got, err := s.ListAnalysisCalls(ctx, 2); err != nil {
		t.Fatal(err)
	} else if len(got) != 2 {
		t.Errorf("limit not applied: got %d", len(got))
	}
}
