package dashboard

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// seedPastSummary inserts a project + session carrying a persisted summary_md
// (the ask-your-past grounding). Timestamps are anchored to real now so the
// handler's time.Now()-based trailing-year window always includes them.
func seedPastSummary(t *testing.T, database *sql.DB, id, root string, started time.Time, summary string) {
	t.Helper()
	res, err := database.Exec(`INSERT INTO projects (root_path, created_at) VALUES (?, ?)`,
		root, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO sessions (id, project_id, tool, model, started_at, summary_md)
		VALUES (?, ?, 'claude', 'opus', ?, ?)`,
		id, pid, started.UTC().Format(time.RFC3339Nano), summary); err != nil {
		t.Fatal(err)
	}
}

func TestBriefingAskPastWithoutRunnerIs404(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/ask-past", strings.NewReader(`{"question":"what broke the build?"}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("ask-past without runner → %d, want 404 (%s)", rr.Code, rr.Body.String())
	}
}

func TestBriefingAskPastWrongMethodIs405(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/ask-past", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET ask-past → %d, want 405", rr.Code)
	}
}

func TestBriefingAskPastEmptyQuestionIs400(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	f := newFakeAnalysisLLM(t)
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/ask-past", strings.NewReader(`{"question":"  "}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("empty question → %d, want 400", rr.Code)
	}
	if f.calls != 0 {
		t.Fatal("empty question must not reach the LLM")
	}
}

// TestBriefingAskPastNoMatchIsDeterministicNoEgress pins that zero matching
// sessions yields a deterministic answer WITHOUT invoking the model (the
// egress-guardrail promise: no hits ⇒ no LLM call at all).
func TestBriefingAskPastNoMatchIsDeterministicNoEgress(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	seedPastSummary(t, s.db(), "s1", "/repo/alpha", time.Now().UTC().Add(-24*time.Hour),
		"Refactored the dashboard; no build involved.")
	f := newFakeAnalysisLLM(t)
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/ask-past", strings.NewReader(`{"question":"aortic dissection of the zorb"}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("no-match ask-past → %d, want 200 (%s)", rr.Code, rr.Body.String())
	}
	var res askPastResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "No sessions") {
		t.Fatalf("deterministic answer expected, got %q", res.Text)
	}
	if res.Retrieved != 0 || res.WindowDays != defaultPastWindowDays {
		t.Fatalf("response metadata: %+v", res)
	}
	if f.calls != 0 {
		t.Fatalf("no-match must not egress; fake LLM called %d times", f.calls)
	}
}

// TestBriefingAskPastAnswersAndGroundsOnSummaries pins the happy path: the
// grounded answer comes back, the retrieval metadata is right, and the prompt
// the model received contains the user's summary but only the bare repo marker
// — never the full project root.
func TestBriefingAskPastAnswersAndGroundsOnSummaries(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	seedPastSummary(t, s.db(), "s1", "/home/alice/secret/repo-x", time.Now().UTC().Add(-24*time.Hour),
		"Build kept failing on the import resolver; finally fixed it.")
	f := newFakeAnalysisLLM(t)
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/ask-past",
		strings.NewReader(`{"question":"build import resolver failing","project":"/home/alice/secret/repo-x"}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ask-past → %d body=%s", rr.Code, rr.Body.String())
	}
	var res askPastResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Text != "the answer" || res.ConnectionID != "local" || res.Model != "m1" {
		t.Fatalf("AskResult = %+v", res)
	}
	if res.Retrieved != 1 || res.Project != "/home/alice/secret/repo-x" {
		t.Fatalf("retrieval metadata = %+v", res)
	}
	if f.calls != 1 {
		t.Fatalf("fake LLM called %d times", f.calls)
	}
	if !strings.Contains(f.body, "import resolver") {
		t.Fatalf("prompt must carry the retrieved summary:\n%s", f.body)
	}
	if strings.Contains(f.body, "/home/alice/secret") {
		t.Fatalf("prompt must not leak the project root:\n%s", f.body)
	}
	if !strings.Contains(f.body, "repo=repo-x") {
		t.Fatalf("prompt must carry the bare repo marker:\n%s", f.body)
	}
}

func TestParsePastWindowDays(t *testing.T) {
	for raw, want := range map[string]int{
		"":        365,
		"7d":      7,
		"7":       7,
		"week":    7,
		"weekly":  7,
		"28d":     28,
		"month":   28,
		"monthly": 28,
		"30":      30,
		"30d":     30,
		"365d":    365,
		"1y":      365,
		"year":    365,
		"2y":      730,
		"3w":      21,
		"5000y":   3650, // capped at the 10-year max
		"garbage": 365,  // fallback to the default
	} {
		if got := parsePastWindowDays(raw); got != want {
			t.Errorf("parsePastWindowDays(%q) = %d, want %d", raw, got, want)
		}
	}
}
