package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// seedReportProject inserts one project + one session for the report tests.
func seedReportProject(t *testing.T, server *Server, id, tool string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := server.opts.DB.Exec(
		`INSERT INTO projects (root_path, name, created_at) VALUES ('/rep-p', 'rep-proj', ?)`,
		now.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := server.opts.DB.Exec(
		`INSERT INTO sessions (id, project_id, tool, started_at)
		 VALUES (?, (SELECT id FROM projects WHERE root_path='/rep-p'), ?, ?)`,
		id, tool, now.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatal(err)
	}
}

// insertTurn inserts an api_turns row with the latency/error dimensions the
// reports read. cost_usd stays 0 so the cache report re-prices from tokens.
func insertTurn(t *testing.T, server *Server, sid, model, provider string, ts time.Time, ttft, total int64, errClass, kind string, cacheRead int64) {
	t.Helper()
	if _, err := server.opts.DB.Exec(
		`INSERT INTO api_turns (session_id, timestamp, provider, model, input_tokens, output_tokens,
		    cache_read_tokens, cache_creation_tokens, cost_usd, time_to_first_token_ms, total_response_ms,
		    error_class, turn_kind)
		 VALUES (?, ?, ?, ?, 1000, 500, ?, 2000, 0, ?, ?, ?, ?)`,
		sid, ts.Format(time.RFC3339Nano), provider, model, cacheRead, ttft, total, errClass, kind,
	); err != nil {
		t.Fatal(err)
	}
}

// TestReportLatency pins /api/reports/latency: per (kind, model, provider)
// nearest-rank TTFT + total percentiles, counts, cost, and error rate.
func TestReportLatency(t *testing.T) {
	server, _ := wizardTestServer(t)
	seedReportProject(t, server, "s1", "claude-code")
	now := time.Now().UTC()

	// claude-opus-4-8: 6 turns. TTFTs 100..500 + one 0 (excluded) →
	// P50/P75/P95 = 300/400/500 over n=5. Totals 1000..1500 → 1300/1400/1500.
	// One of the six is an error (error_class 'http_429', ttft 0).
	opusTTFT := []int64{100, 200, 300, 400, 500, 0}
	for i, ttft := range opusTTFT {
		errClass := ""
		if i == 5 {
			errClass = "http_429"
		}
		insertTurn(t, server, "s1", "claude-opus-4-8", "anthropic",
			now.Add(-time.Duration(i)*time.Minute), ttft, 1000+int64(i)*100, errClass, "plan", 0)
	}
	// claude-haiku-4-5: 2 turns, TTFT 50/60 → P50/P75/P95 = 60/60/60.
	for i, ttft := range []int64{50, 60} {
		insertTurn(t, server, "s1", "claude-haiku-4-5", "anthropic",
			now.Add(-time.Duration(i)*time.Minute), ttft, 400+int64(i)*100, "", "edit", 0)
	}
	// Out of window (60 days ago).
	insertTurn(t, server, "s1", "claude-opus-4-8", "anthropic", now.AddDate(0, 0, -60), 999, 999, "", "plan", 0)

	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/reports/latency?days=30", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Cells []struct {
			Kind       string  `json:"kind"`
			Model      string  `json:"model"`
			Provider   string  `json:"provider"`
			Turns      int64   `json:"turns"`
			CostUSD    float64 `json:"cost_usd"`
			ErrorRate  float64 `json:"error_rate"`
			TTFTP50Ms  int64   `json:"ttft_p50_ms"`
			TTFTP75Ms  int64   `json:"ttft_p75_ms"`
			TTFTP95Ms  int64   `json:"ttft_p95_ms"`
			TotalP50Ms int64   `json:"total_p50_ms"`
			TotalP75Ms int64   `json:"total_p75_ms"`
			TotalP95Ms int64   `json:"total_p95_ms"`
		} `json:"cells"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Cells) != 2 {
		t.Fatalf("cells: got %d want 2 (%+v)", len(got.Cells), got.Cells)
	}
	byModel := map[string]struct {
		TTFTP50Ms, TTFTP75Ms, TTFTP95Ms    int64
		TotalP50Ms, TotalP75Ms, TotalP95Ms int64
		Turns                              int64
		ErrorRate                          float64
	}{}
	for _, c := range got.Cells {
		byModel[c.Model] = struct {
			TTFTP50Ms, TTFTP75Ms, TTFTP95Ms    int64
			TotalP50Ms, TotalP75Ms, TotalP95Ms int64
			Turns                              int64
			ErrorRate                          float64
		}{c.TTFTP50Ms, c.TTFTP75Ms, c.TTFTP95Ms, c.TotalP50Ms, c.TotalP75Ms, c.TotalP95Ms, c.Turns, c.ErrorRate}
	}
	opus := byModel["claude-opus-4-8"]
	if opus.TTFTP50Ms != 300 || opus.TTFTP75Ms != 400 || opus.TTFTP95Ms != 500 {
		t.Errorf("opus ttft = %d/%d/%d want 300/400/500", opus.TTFTP50Ms, opus.TTFTP75Ms, opus.TTFTP95Ms)
	}
	if opus.TotalP50Ms != 1300 || opus.TotalP75Ms != 1400 || opus.TotalP95Ms != 1500 {
		t.Errorf("opus total = %d/%d/%d want 1300/1400/1500", opus.TotalP50Ms, opus.TotalP75Ms, opus.TotalP95Ms)
	}
	if opus.Turns != 6 {
		t.Errorf("opus turns = %d want 6", opus.Turns)
	}
	if opus.ErrorRate != 1.0/6.0 {
		t.Errorf("opus error_rate = %v want %v", opus.ErrorRate, 1.0/6.0)
	}
	haiku := byModel["claude-haiku-4-5"]
	if haiku.TTFTP50Ms != 60 || haiku.Turns != 2 {
		t.Errorf("haiku = %+v want ttft_p50=60 turns=2", haiku)
	}
}

// TestReportErrors pins /api/reports/errors: per-model error rate/count plus
// the top error classes and the window summary.
func TestReportErrors(t *testing.T) {
	server, _ := wizardTestServer(t)
	seedReportProject(t, server, "s1", "claude-code")
	now := time.Now().UTC()

	for i := 0; i < 6; i++ {
		errClass := ""
		if i == 0 {
			errClass = "http_429"
		}
		if i == 1 {
			errClass = "timeout"
		}
		insertTurn(t, server, "s1", "claude-opus-4-8", "anthropic",
			now.Add(-time.Duration(i)*time.Minute), 100, 1000, errClass, "plan", 0)
	}
	for i := 0; i < 3; i++ {
		insertTurn(t, server, "s1", "claude-haiku-4-5", "anthropic",
			now.Add(-time.Duration(i)*time.Minute), 50, 400, "", "edit", 0)
	}

	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/reports/errors?days=36500", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Items []struct {
			Model      string  `json:"model"`
			Turns      int64   `json:"turns"`
			ErrorCount int64   `json:"error_count"`
			ErrorRate  float64 `json:"error_rate"`
		} `json:"items"`
		ErrorClasses []struct {
			ErrorClass string `json:"error_class"`
			Count      int64  `json:"count"`
		} `json:"error_classes"`
		Summary struct {
			Turns      int64   `json:"turns"`
			ErrorCount int64   `json:"error_count"`
			ErrorRate  float64 `json:"error_rate"`
		} `json:"summary"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items: %+v", got.Items)
	}
	opus := got.Items[0]
	if opus.Model != "claude-opus-4-8" || opus.Turns != 6 || opus.ErrorCount != 2 || opus.ErrorRate != 2.0/6.0 {
		t.Errorf("opus row: %+v", opus)
	}
	if got.Summary.Turns != 9 || got.Summary.ErrorCount != 2 || got.Summary.ErrorRate != 2.0/9.0 {
		t.Errorf("summary: %+v", got.Summary)
	}
	classes := map[string]int64{}
	for _, c := range got.ErrorClasses {
		classes[c.ErrorClass] = c.Count
	}
	if classes["http_429"] != 1 || classes["timeout"] != 1 || len(classes) != 2 {
		t.Errorf("error classes: %+v", got.ErrorClasses)
	}
}

// TestReportCache pins /api/reports/cache: counterfactual savings per model
// and provider, proxy rows preferred, and the summary totals.
func TestReportCache(t *testing.T) {
	server, _ := wizardTestServer(t)
	seedReportProject(t, server, "s1", "claude-code")
	now := time.Now().UTC()

	// Three opus turns with cache_read = 100_000 tokens each, cost_usd 0
	// (re-priced). Opus cache_read rate $0.50 vs input $5.00 per MTok →
	// 100_000 × 4.5e-6 = $0.45 per turn → $1.35 for three.
	for i := 0; i < 3; i++ {
		insertTurn(t, server, "s1", "claude-opus-4-8", "anthropic",
			now.Add(-time.Duration(i)*time.Minute), 100, 1000, "", "plan", 100_000)
	}
	// A proxy row already captured as the same request_id (request_id
	// non-empty) must win over the token_usage copy.
	if _, err := server.opts.DB.Exec(
		`UPDATE api_turns SET request_id = 'req-opus' WHERE model = 'claude-opus-4-8'`,
	); err != nil {
		t.Fatal(err)
	}
	// token_usage copy of that same turn — deduped out.
	if _, err := server.opts.DB.Exec(
		`INSERT INTO token_usage (session_id, timestamp, tool, model, input_tokens, output_tokens,
		    cache_read_tokens, cache_creation_tokens, estimated_cost_usd, source, reliability,
		    source_file, source_event_id)
		 VALUES ('s1', ?, 'claude-code', 'claude-opus-4-8', 1000, 500, 900_000, 2000, 0,
		         'jsonl', 'approximate', 'f.jsonl', 'req-opus')`,
		now.Add(-time.Hour).Format(time.RFC3339Nano),
	); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/reports/cache?days=36500", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		ByModel []struct {
			Model           string  `json:"model"`
			Provider        string  `json:"provider"`
			Turns           int64   `json:"turns"`
			SavingsUSD      float64 `json:"savings_usd"`
			CacheReadTokens int64   `json:"cache_read_tokens"`
		} `json:"by_model"`
		Summary struct {
			SavingsUSD      float64 `json:"savings_usd"`
			CacheReadTokens int64   `json:"cache_read_tokens"`
		} `json:"summary"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.ByModel) != 1 {
		t.Fatalf("by_model: %+v", got.ByModel)
	}
	opus := got.ByModel[0]
	if opus.Model != "claude-opus-4-8" || opus.Provider != "anthropic" || opus.Turns != 3 {
		t.Errorf("opus cell: %+v", opus)
	}
	if opus.SavingsUSD < 1.34 || opus.SavingsUSD > 1.36 {
		t.Errorf("savings: %v want ~1.35", opus.SavingsUSD)
	}
	if opus.CacheReadTokens != 300_000 {
		t.Errorf("cache_read_tokens: %d want 300000", opus.CacheReadTokens)
	}
	if got.Summary.SavingsUSD != opus.SavingsUSD {
		t.Errorf("summary savings mismatch: %v vs %v", got.Summary.SavingsUSD, opus.SavingsUSD)
	}
}

// TestHeadlineLatencyAndErrors pins the briefing-numbers additions to
// /api/analysis/headline: latency percentiles + error rate from proxy turns,
// with JSONL-only turns contributing to neither.
func TestHeadlineLatencyAndErrors(t *testing.T) {
	server, _ := wizardTestServer(t)
	seedReportProject(t, server, "s1", "claude-code")
	now := time.Now().UTC()

	// 5 proxy turns: ttft 100..500 → p50/p75/p95 = 300/400/500.
	for i := 0; i < 5; i++ {
		errClass := ""
		if i == 0 {
			errClass = "http_429"
		}
		insertTurn(t, server, "s1", "claude-opus-4-8", "anthropic",
			now.Add(-time.Duration(i)*time.Minute), 100+int64(i)*100, 1000+int64(i)*100, errClass, "plan", 0)
	}
	// A JSONL-only turn (no proxy timing, no error signal) — must not pollute.
	if _, err := server.opts.DB.Exec(
		`INSERT INTO token_usage (session_id, timestamp, tool, model, input_tokens, output_tokens,
		    cache_read_tokens, cache_creation_tokens, estimated_cost_usd, source, reliability,
		    source_file, source_event_id)
		 VALUES ('s1', ?, 'claude-code', 'claude-haiku-4-5', 1000, 500, 0, 0, 0.05,
		         'jsonl', 'approximate', 'f.jsonl', 'ev-tu')`,
		now.Add(-30*time.Minute).Format(time.RFC3339Nano),
	); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/analysis/headline?days=30", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Latency map[string]int64 `json:"latency"`
		Errors  struct {
			APITurns   int64   `json:"api_turns"`
			ErrorTurns int64   `json:"error_turns"`
			ErrorRate  float64 `json:"error_rate"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Latency["ttft_p50_ms"] != 300 || got.Latency["ttft_p75_ms"] != 400 || got.Latency["ttft_p95_ms"] != 500 {
		t.Errorf("ttft = %v", got.Latency)
	}
	if got.Latency["total_p50_ms"] != 1200 || got.Latency["total_p95_ms"] != 1400 {
		t.Errorf("total = %v", got.Latency)
	}
	if got.Latency["sample_turns"] != 5 || got.Latency["total_sample_turns"] != 5 {
		t.Errorf("samples = %v", got.Latency)
	}
	if got.Errors.APITurns != 5 || got.Errors.ErrorTurns != 1 || got.Errors.ErrorRate != 0.2 {
		t.Errorf("errors = %+v", got.Errors)
	}
}
