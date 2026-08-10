package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/llmconn"
	"github.com/marmutapp/superbased-observer/internal/llmrun"
)

// fakeAnalysisLLM is a loopback chat/completions server that counts calls.
type fakeAnalysisLLM struct {
	ts      *httptest.Server
	calls   int
	body    string
	content string
}

func newFakeAnalysisLLM(t *testing.T) *fakeAnalysisLLM {
	f := &fakeAnalysisLLM{content: "the answer"}
	f.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls++
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) > 0 {
			f.body = req.Messages[0].Content
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": f.content}}},
			"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 3},
		})
	}))
	t.Cleanup(f.ts.Close)
	return f
}

func llmRunnerFor(t *testing.T, f *fakeAnalysisLLM) (*llmrun.Runner, []llmconn.Connection) {
	t.Helper()
	return &llmrun.Runner{Now: func() time.Time { return time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC) }},
		[]llmconn.Connection{{ID: "local", Kind: llmconn.KindLocalOpenAI, BaseURL: f.ts.URL, DefaultModel: "m1", Enabled: true}}
}

func TestBriefingAskWithoutRunnerIs404(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/ask", strings.NewReader(`{"question":"what cost the most?"}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("ask without runner → %d, want 404 (%s)", rr.Code, rr.Body.String())
	}
}

func TestBriefingAskNoConnectionIs404(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	runner, _ := llmRunnerFor(t, newFakeAnalysisLLM(t))
	s.opts.LLMRunner = runner // conns nil → ErrNoConnection
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/ask", strings.NewReader(`{"question":"q"}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("ask with no connection → %d, want 404", rr.Code)
	}
}

func TestBriefingAskAnswers(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedBriefingTurn(t, st, "s1", "claude", "claude-opus-4",
		time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 0, 0)
	f := newFakeAnalysisLLM(t)
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/ask", strings.NewReader(`{"question":"what cost the most?","date":"2026-08-01"}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ask → %d body=%s", rr.Code, rr.Body.String())
	}
	var res llmrun.AskResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Text != "the answer" || res.ConnectionID != "local" {
		t.Fatalf("AskResult = %+v", res)
	}
	if f.calls != 1 {
		t.Fatalf("fake LLM called %d times", f.calls)
	}
}

func TestBriefingNarrativeOnlyOnExplicitOptIn(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedBriefingTurn(t, st, "s1", "claude", "claude-opus-4",
		time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 0, 0)
	f := newFakeAnalysisLLM(t)
	f.content = "The day: 1 turn of work."
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns

	// Without ?narrative=llm the template narrative must survive — the runner
	// is configured, but egress is still per-request opt-in (no implicit calls).
	_, b := getBriefing(t, s, "/api/briefing?date=2026-08-01")
	if f.calls != 0 {
		t.Fatalf("no-opt-in GET fired %d LLM calls", f.calls)
	}
	if b.Narrative == "The day: 1 turn of work." {
		t.Fatal("narrative must stay template without explicit opt-in")
	}

	// With ?narrative=llm the paragraph is upgraded.
	_, b2 := getBriefing(t, s, "/api/briefing?date=2026-08-01&narrative=llm")
	if b2.Narrative != "The day: 1 turn of work." {
		t.Fatalf("opt-in narrative not applied: %q", b2.Narrative)
	}
	if f.calls != 1 {
		t.Fatalf("opt-in GET fired %d LLM calls", f.calls)
	}
}

func TestBriefingAskWrongMethodIs405(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/ask", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET ask → %d, want 405", rr.Code)
	}
}
