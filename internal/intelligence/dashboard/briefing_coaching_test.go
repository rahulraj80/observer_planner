package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// seedCoachingFailure seeds one api_turn error (the coaching evidence source)
// inside the pinned briefing window, with a distinct session per call so a
// repeated message aggregates into a multi-session failure pattern.
func seedCoachingFailure(t *testing.T, st *store.Store, e briefingTestEnv, id string, ts time.Time, seq int, errorMessage string) {
	t.Helper()
	seedBriefingAPITurnWithError(t, st, id, "claude", "opus", ts,
		20_000, 8_000, 50, 500, 500, "permission_error", errorMessage, seq)
}

func TestBriefingCoachingWithoutRunnerIs404(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/coaching", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("coaching without runner → %d, want 404 (%s)", rr.Code, rr.Body.String())
	}
}

func TestBriefingCoachingWrongMethodIs405(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/coaching", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET coaching → %d, want 405", rr.Code)
	}
}

func TestBriefingCoachingBadBodyIs400(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	f := newFakeAnalysisLLM(t)
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/coaching", strings.NewReader(`{not json`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad body → %d, want 400", rr.Code)
	}
	if f.calls != 0 {
		t.Fatal("bad body must not reach the LLM")
	}
}

func TestBriefingCoachingNoConnectionIs404(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	// Seed evidence so the handler reaches the connection check (with zero
	// evidence it answers deterministically without ever consulting conns).
	seedCoachingFailure(t, st, e, "s1", e.fixedUTC.Add(-24*time.Hour), 1, "permission denied on /etc/hosts")
	seedCoachingFailure(t, st, e, "s2", e.fixedUTC.Add(-48*time.Hour), 2, "permission denied on /etc/hosts")
	runner, _ := llmRunnerFor(t, newFakeAnalysisLLM(t))
	s.opts.LLMRunner = runner // conns nil → ErrNoConnection
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/coaching", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("coaching with no connection → %d, want 404", rr.Code)
	}
}

// TestBriefingCoachingNoEvidenceIsDeterministicNoEgress pins that zero failure
// evidence yields a deterministic note WITHOUT invoking the model (the
// egress-guardrail promise: no evidence ⇒ no LLM call at all).
func TestBriefingCoachingNoEvidenceIsDeterministicNoEgress(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	f := newFakeAnalysisLLM(t)
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/coaching", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("no-evidence coaching → %d, want 200 (%s)", rr.Code, rr.Body.String())
	}
	var res coachingResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "No recurring failures") {
		t.Fatalf("deterministic note expected, got %q", res.Text)
	}
	if res.Patterns != 0 || res.Rules != 0 || res.WindowDays != defaultFailureWindowDays {
		t.Fatalf("response metadata: %+v", res)
	}
	if f.calls != 0 {
		t.Fatalf("no-evidence must not egress; fake LLM called %d times", f.calls)
	}
}

// TestBriefingCoachingAnswersAndGroundsOnFailureEvidence pins the happy path:
// the coaching note comes back grounded on the seeded recurring failure, and
// the prompt the model received carries only the normalized evidence — never
// the machine paths.
func TestBriefingCoachingAnswersAndGroundsOnFailureEvidence(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	// Three sessions across three local days, same normalized message →
	// one multi-session failure pattern.
	seedCoachingFailure(t, st, e, "s1", e.fixedUTC.Add(-24*time.Hour), 1, "permission denied on /etc/hosts")
	seedCoachingFailure(t, st, e, "s2", e.fixedUTC.Add(-48*time.Hour), 2, "permission denied on /etc/hosts")
	seedCoachingFailure(t, st, e, "s3", e.fixedUTC.Add(-72*time.Hour), 3, "permission denied  on  /etc/hosts")
	f := newFakeAnalysisLLM(t)
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/coaching", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("coaching → %d body=%s", rr.Code, rr.Body.String())
	}
	var res coachingResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Text != "the answer" || res.ConnectionID != "local" || res.Model != "m1" {
		t.Fatalf("AskResult = %+v", res)
	}
	if res.Patterns != 1 || res.Rules != 0 || res.WindowDays != defaultFailureWindowDays {
		t.Fatalf("evidence metadata = %+v", res)
	}
	if f.calls != 1 {
		t.Fatalf("fake LLM called %d times", f.calls)
	}
	for _, want := range []string{
		"Evidence window: last 30 days",
		"permission: 3 occurrences across 3 sessions",
		"permission denied on /etc/hosts",
	} {
		if !strings.Contains(f.body, want) {
			t.Errorf("prompt missing %q:\n%s", want, f.body)
		}
	}
}

func TestParseFailureWindowDays(t *testing.T) {
	for raw, want := range map[string]int{
		"":        30,
		"  ":      30,
		"7d":      7,
		"30d":     30,
		"90":      90,
		"28d":     28,
		"1y":      365,
		"garbage": 365, // unparseable falls back to the ask-past grammar default
	} {
		if got := parseFailureWindowDays(raw); got != want {
			t.Errorf("parseFailureWindowDays(%q) = %d, want %d", raw, got, want)
		}
	}
}
