package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// seedTrivialSession seeds one trivial premium (opus) session that fires the
// trivial_model_overprovisioning card with a known dedup key.
func seedTrivialSession(t *testing.T, st *store.Store, id string, ts time.Time, input, output int64) {
	t.Helper()
	seedBriefingTurn(t, st, id, models.ToolClaudeCode, "claude-opus-4", ts, input, output, 0, 0)
	seedBriefingTurn(t, st, id, models.ToolClaudeCode, "claude-opus-4", ts.Add(2*time.Hour), input/2, output/2, 0, 1)
}

func TestBriefingTriageWithoutRunnerIs404(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/triage", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("triage without runner → %d, want 404 (%s)", rr.Code, rr.Body.String())
	}
}

func TestBriefingTriageWrongMethodIs405(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/triage", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET triage → %d, want 405", rr.Code)
	}
}

func TestBriefingTriageBadBodyIs400(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	f := newFakeAnalysisLLM(t)
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/triage", strings.NewReader(`{not json`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad body → %d, want 400", rr.Code)
	}
	if f.calls != 0 {
		t.Fatal("bad body must not reach the LLM")
	}
}

func TestBriefingTriageNoConnectionIs404(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedTrivialSession(t, st, "s1", time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), 20_000, 8_000)
	runner, _ := llmRunnerFor(t, newFakeAnalysisLLM(t))
	s.opts.LLMRunner = runner // conns nil → ErrNoConnection
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/triage", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("triage with no connection → %d, want 404 (%s)", rr.Code, rr.Body.String())
	}
}

func TestBriefingTriageNoCardsIsDeterministicNoEgress(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	f := newFakeAnalysisLLM(t)
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/triage", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("no-card triage → %d body=%s", rr.Code, rr.Body.String())
	}
	var res triageResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Opportunities) != 0 || len(res.Order) != 0 {
		t.Fatalf("no-card triage must return an empty set: %+v", res)
	}
	if f.calls != 0 {
		t.Fatal("no-card triage must not egress")
	}
}

// TestBriefingTriageReordersCards pins the happy path: the model's JSON order
// is applied over the composed card set (full permutation — the runner appends
// any card the model omitted), the grouping + summary survive, and the prompt
// the model received carries only the content-free card projection.
func TestBriefingTriageReordersCards(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	// Two trivial opus sessions → trivial_model_s1 (costlier) and s2 (cheaper).
	// Deterministic composer order is savings-desc: s1 first. The model reply
	// reorders to s2 first.
	seedTrivialSession(t, st, "s1", time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), 20_000, 8_000)
	seedTrivialSession(t, st, "s2", time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC), 10_000, 4_000)

	_, b := getBriefing(t, s, "/api/briefing?date=2026-08-01")
	if len(b.Opportunities) == 0 {
		t.Fatalf("expected seeded opportunity cards, got none")
	}

	f := newFakeAnalysisLLM(t)
	f.content = `{"order":["trivial_model_s2","trivial_model_s1"],"groups":[{"label":"overprovisioned sessions","keys":["trivial_model_s1","trivial_model_s2"]}],"summary":"the smaller trivial sessions are cheapest wins first"}`
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/triage", strings.NewReader(`{"date":"2026-08-01"}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("triage → %d body=%s", rr.Code, rr.Body.String())
	}
	var res triageResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Date != "2026-08-01" || res.WindowDays != 1 {
		t.Fatalf("triage header: %+v", res)
	}
	if len(res.Order) != len(b.Opportunities) {
		t.Fatalf("order must cover the full card set: order=%v vs %d cards", res.Order, len(b.Opportunities))
	}
	if res.Order[0] != "trivial_model_s2" || res.Order[1] != "trivial_model_s1" {
		t.Fatalf("order = %v, want s2 then s1", res.Order)
	}
	if len(res.Opportunities) != len(res.Order) || res.Opportunities[0].DedupKey != "trivial_model_s2" {
		t.Fatalf("opportunities not reordered: %+v", res.Opportunities)
	}
	if len(res.Groups) != 1 || res.Groups[0].Label != "overprovisioned sessions" || len(res.Groups[0].Keys) != 2 {
		t.Fatalf("groups = %+v", res.Groups)
	}
	if res.Summary == "" || res.ConnectionID != "local" || res.Model != "m1" {
		t.Fatalf("triage metadata = %+v", res)
	}
	if f.calls != 1 {
		t.Fatalf("fake LLM called %d times", f.calls)
	}
	for _, want := range []string{
		"Window: 2026-08-01 (1 days)",
		"trivial_model_s1 | trivial_model_overprovisioning",
		"trivial_model_s2 | trivial_model_overprovisioning",
		"confidence=",
	} {
		if !strings.Contains(f.body, want) {
			t.Errorf("prompt missing %q:\n%s", want, f.body)
		}
	}
}

func TestBriefingTriageUpstreamFailureIs502(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedTrivialSession(t, st, "s1", time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), 20_000, 8_000)
	f := newFakeAnalysisLLM(t)
	f.content = "not json at all"
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/triage", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("garbage reply → %d, want 502 (%s)", rr.Code, rr.Body.String())
	}
}
