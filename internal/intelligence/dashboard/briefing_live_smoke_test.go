package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// postJSON performs one request against the live server and returns status + body.
func postBriefingJSON(t *testing.T, s *Server, path, body string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	s.Handler().ServeHTTP(rr, req)
	return rr.Code, rr.Body.String()
}

// TestLiveSmokeTriageAndDiagnose exercises both new endpoints over the real
// HTTP + DB stack with a realistic dataset and prints the actual behavior.
func TestLiveSmokeTriageAndDiagnose(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)

	// Two trivial premium sessions on the completed day → trivial_model cards.
	seedBriefingTurn(t, st, "s1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC), 20_000, 8_000, 0, 0)
	seedBriefingTurn(t, st, "s1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), 10_000, 4_000, 0, 1)
	seedBriefingTurn(t, st, "s2", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC), 10_000, 4_000, 0, 0)
	seedBriefingTurn(t, st, "s2", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC), 5_000, 2_000, 0, 1)

	// Recurring permission failures across three sessions on three days →
	// one failure pattern for diagnose.
	seedBriefingAPITurnWithError(t, st, "p1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC), 1_000, 200, 0, 0, 500, "permission", "permission denied on /etc/hosts", 0)
	seedBriefingAPITurnWithError(t, st, "p2", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC), 1_000, 200, 0, 0, 500, "permission", "permission denied on /etc/hosts", 0)
	seedBriefingAPITurnWithError(t, st, "p3", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC), 1_000, 200, 0, 0, 500, "permission", "permission denied on /etc/hosts", 0)

	fmt.Println("==== 1. Composed briefing (GET /api/briefing?date=2026-08-01) ====")
	_, b := getBriefing(t, s, "/api/briefing?date=2026-08-01")
	for i, o := range b.Opportunities {
		fmt.Printf("  card[%d] key=%s title=%q savings=$%.2f conf=%.2f\n", i, o.DedupKey, o.Title, o.SavingsUSD, o.Confidence)
	}

	// ---- triage happy path: model reorders + groups + summarizes ----
	fmt.Println("\n==== 2. POST /api/briefing/triage (happy path) ====")
	ft := newFakeAnalysisLLM(t)
	ft.content = `{"order":["trivial_model_s2","junk_key_not_in_set","trivial_model_s1"],"groups":[{"label":"overprovisioned sessions","keys":["trivial_model_s1","trivial_model_s2"]}],"summary":"smallest trivial sessions first — same pattern"}`
	runner, conns := llmRunnerFor(t, ft)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns

	code, body := postBriefingJSON(t, s, "/api/briefing/triage", `{"date":"2026-08-01"}`)
	fmt.Printf("  status=%d\n  body=%s\n", code, body)
	if code != http.StatusOK {
		t.Fatalf("triage happy path → %d", code)
	}
	var tr triageResponse
	if err := json.Unmarshal([]byte(body), &tr); err != nil {
		t.Fatal(err)
	}
	keys := make(map[string]bool, len(b.Opportunities))
	for _, o := range b.Opportunities {
		keys[o.DedupKey] = true
	}
	if len(tr.Order) != len(b.Opportunities) {
		t.Fatalf("order len=%d, composed cards=%d (must be a full permutation)", len(tr.Order), len(b.Opportunities))
	}
	for _, k := range tr.Order {
		if !keys[k] {
			t.Fatalf("order contains non-card key %q", k)
		}
	}
	if tr.Order[0] != "trivial_model_s2" || tr.Order[1] != "trivial_model_s1" {
		t.Fatalf("order = %v, want s2 then s1", tr.Order)
	}
	if tr.Opportunities[0].DedupKey != "trivial_model_s2" || len(tr.Groups) != 1 || tr.Summary == "" {
		t.Fatalf("reorder/groups/summary not applied: %+v", tr)
	}
	fmt.Println("  model reply reorder applied; junk key dropped; full permutation held; groups + summary present")

	// ---- diagnose happy path ----
	fmt.Println("\n==== 3. POST /api/briefing/diagnose (happy path) ====")
	fd := newFakeAnalysisLLM(t)
	fd.content = `{"diagnoses":[{"index":0,"diagnosis":"write credential lacks host-level ACL","next_step":"grant the scoped write ACL and retry"}]}`
	runner2, conns2 := llmRunnerFor(t, fd)
	s.opts.LLMRunner, s.opts.LLMConns = runner2, conns2

	code, body = postBriefingJSON(t, s, "/api/briefing/diagnose", `{}`)
	fmt.Printf("  status=%d\n  body=%s\n", code, body)
	if code != http.StatusOK {
		t.Fatalf("diagnose happy path → %d", code)
	}
	var dr diagnoseResponse
	if err := json.Unmarshal([]byte(body), &dr); err != nil {
		t.Fatal(err)
	}
	if dr.Patterns != 1 || len(dr.Diagnoses) != 1 {
		t.Fatalf("diagnose = %+v", dr)
	}
	d := dr.Diagnoses[0]
	if d.Category != "Permission" || d.Occurrences != 3 || d.Sessions != 3 || d.Days != 3 {
		t.Fatalf("enriched entry = %+v", d)
	}
	fmt.Printf("  one pattern (3 occurrences / 3 sessions / 3 days, category=%s), diagnosis grounded\n", d.Category)

	fmt.Println("\n==== 4. Prompts the models actually received ====")
	fmt.Printf("--- triage prompt ---\n%s\n--- diagnose prompt ---\n%s\n", ft.body, fd.body)
	for _, bad := range []string{"TempDir", "C:\\", "/home/", ".jsonl"} {
		if strings.Contains(ft.body, bad) {
			t.Errorf("triage prompt leaks %q (content-free projection broken)", bad)
		}
		if strings.Contains(fd.body, bad) {
			t.Errorf("diagnose prompt leaks %q (content-free projection broken)", bad)
		}
	}

	// ---- deterministic no-egress paths ----
	fmt.Println("\n==== 5. Deterministic no-egress (zero cards / zero patterns) ====")
	fz := newFakeAnalysisLLM(t)
	runner3, conns3 := llmRunnerFor(t, fz)
	s.opts.LLMRunner, s.opts.LLMConns = runner3, conns3

	code, body = postBriefingJSON(t, s, "/api/briefing/triage", `{"date":"2026-01-01"}`)
	fmt.Printf("  triage date=2026-01-01 (no data): status=%d body=%s\n", code, body)
	if code != http.StatusOK || !strings.Contains(body, `"order":[]`) || !strings.Contains(body, `"opportunities":[]`) {
		t.Fatalf("empty triage: %d %s", code, body)
	}
	code, body = postBriefingJSON(t, s, "/api/briefing/diagnose", `{"project":"/definitely/not/seeded"}`)
	fmt.Printf("  diagnose project-filter (no patterns): status=%d body=%s\n", code, body)
	if code != http.StatusOK || !strings.Contains(body, `"diagnoses":[]`) {
		t.Fatalf("empty diagnose: %d %s", code, body)
	}
	if fz.calls != 0 {
		t.Fatalf("deterministic paths must not egress; calls=%d", fz.calls)
	}
	fmt.Println("  both answered 200 with ZERO LLM egress")

	// ---- error discipline ----
	fmt.Println("\n==== 6. Error discipline ====")
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"triage GET", http.MethodGet, "/api/briefing/triage", "", http.StatusMethodNotAllowed},
		{"triage bad body", http.MethodPost, "/api/briefing/triage", `{not json`, http.StatusBadRequest},
		{"triage bad window", http.MethodPost, "/api/briefing/triage", `{"window":"bogus"}`, http.StatusBadRequest},
		{"diagnose GET", http.MethodGet, "/api/briefing/diagnose", "", http.StatusMethodNotAllowed},
		{"diagnose bad body", http.MethodPost, "/api/briefing/diagnose", `{not json`, http.StatusBadRequest},
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		s.Handler().ServeHTTP(rr, req)
		fmt.Printf("  %-22s → %d (want %d)\n", c.name, rr.Code, c.want)
		if rr.Code != c.want {
			t.Errorf("%s → %d, want %d", c.name, rr.Code, c.want)
		}
	}

	// no runner → 404
	noRunner := newBriefingTestEnv(t)
	s2, _ := newBriefingServer(t, noRunner)
	code, _ = postBriefingJSON(t, s2, "/api/briefing/triage", `{}`)
	fmt.Printf("  triage no runner            → %d (want 404)\n", code)
	if code != http.StatusNotFound {
		t.Errorf("triage no runner → %d", code)
	}
	code, _ = postBriefingJSON(t, s2, "/api/briefing/diagnose", `{}`)
	fmt.Printf("  diagnose no runner          → %d (want 404)\n", code)
	if code != http.StatusNotFound {
		t.Errorf("diagnose no runner → %d", code)
	}

	// upstream garbage → 502
	fg := newFakeAnalysisLLM(t)
	fg.content = "not json"
	runner4, conns4 := llmRunnerFor(t, fg)
	s.opts.LLMRunner, s.opts.LLMConns = runner4, conns4
	code, _ = postBriefingJSON(t, s, "/api/briefing/triage", `{"date":"2026-08-01"}`)
	fmt.Printf("  triage garbage reply        → %d (want 502)\n", code)
	if code != http.StatusBadGateway {
		t.Errorf("triage garbage → %d", code)
	}
	fg2 := newFakeAnalysisLLM(t)
	fg2.content = `{"diagnoses":[]}`
	runner5, conn5 := llmRunnerFor(t, fg2)
	s.opts.LLMRunner, s.opts.LLMConns = runner5, conn5
	code, _ = postBriefingJSON(t, s, "/api/briefing/diagnose", `{}`)
	fmt.Printf("  diagnose empty diagnoses    → %d (want 502)\n", code)
	if code != http.StatusBadGateway {
		t.Errorf("diagnose empty → %d", code)
	}

	fmt.Println("\nALL SMOKE CHECKS DONE")
}
