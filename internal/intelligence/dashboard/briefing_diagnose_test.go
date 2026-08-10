package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBriefingDiagnoseWithoutRunnerIs404(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/diagnose", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("diagnose without runner → %d, want 404 (%s)", rr.Code, rr.Body.String())
	}
}

func TestBriefingDiagnoseWrongMethodIs405(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/diagnose", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET diagnose → %d, want 405", rr.Code)
	}
}

func TestBriefingDiagnoseBadBodyIs400(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	f := newFakeAnalysisLLM(t)
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/diagnose", strings.NewReader(`{not json`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad body → %d, want 400", rr.Code)
	}
	if f.calls != 0 {
		t.Fatal("bad body must not reach the LLM")
	}
}

func TestBriefingDiagnoseNoConnectionIs404(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedCoachingFailure(t, st, e, "s1", e.fixedUTC.Add(-24*time.Hour), 1, "permission denied on /etc/hosts")
	seedCoachingFailure(t, st, e, "s2", e.fixedUTC.Add(-48*time.Hour), 2, "permission denied on /etc/hosts")
	runner, _ := llmRunnerFor(t, newFakeAnalysisLLM(t))
	s.opts.LLMRunner = runner // conns nil → ErrNoConnection
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/diagnose", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("diagnose with no connection → %d, want 404 (%s)", rr.Code, rr.Body.String())
	}
}

func TestBriefingDiagnoseNoEvidenceIsDeterministicNoEgress(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	f := newFakeAnalysisLLM(t)
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/diagnose", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("no-evidence diagnose → %d, want 200 (%s)", rr.Code, rr.Body.String())
	}
	var res diagnoseResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Patterns != 0 || res.WindowDays != defaultFailureWindowDays || len(res.Diagnoses) != 0 {
		t.Fatalf("no-evidence diagnose = %+v", res)
	}
	if f.calls != 0 {
		t.Fatal("no-evidence diagnose must not egress")
	}
}

// TestBriefingDiagnoseAnswersAndGroundsOnFailureEvidence pins the happy path:
// one per-pattern diagnosis comes back grounded on the seeded recurring
// failure, enriched with the pattern evidence, and the prompt the model
// received carries only the normalized evidence — never the machine paths.
func TestBriefingDiagnoseAnswersAndGroundsOnFailureEvidence(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedCoachingFailure(t, st, e, "s1", e.fixedUTC.Add(-24*time.Hour), 1, "permission denied on /etc/hosts")
	seedCoachingFailure(t, st, e, "s2", e.fixedUTC.Add(-48*time.Hour), 2, "permission denied on /etc/hosts")
	seedCoachingFailure(t, st, e, "s3", e.fixedUTC.Add(-72*time.Hour), 3, "permission denied  on  /etc/hosts")
	f := newFakeAnalysisLLM(t)
	f.content = `{"diagnoses":[{"index":0,"diagnosis":"credential scope too narrow for the target host","next_step":"verify the host write permission and retry"}]}`
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/diagnose", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("diagnose → %d body=%s", rr.Code, rr.Body.String())
	}
	var res diagnoseResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Patterns != 1 || res.WindowDays != defaultFailureWindowDays {
		t.Fatalf("diagnose metadata = %+v", res)
	}
	if len(res.Diagnoses) != 1 {
		t.Fatalf("diagnoses = %+v", res.Diagnoses)
	}
	d := res.Diagnoses[0]
	if d.Index != 0 || d.Category != "Permission" || d.Occurrences != 3 || d.Sessions != 3 || d.Days != 3 {
		t.Fatalf("diagnosis entry = %+v", d)
	}
	if d.Diagnosis != "credential scope too narrow for the target host" || d.NextStep == "" {
		t.Fatalf("diagnosis text = %+v", d)
	}
	if res.ConnectionID != "local" || res.Model != "m1" {
		t.Fatalf("diagnose metadata = %+v", res)
	}
	if f.calls != 1 {
		t.Fatalf("fake LLM called %d times", f.calls)
	}
	for _, want := range []string{
		"Evidence window: last 30 days",
		"k0 | category=permission (Permission)",
		"permission denied on /etc/hosts",
	} {
		if !strings.Contains(f.body, want) {
			t.Errorf("prompt missing %q:\n%s", want, f.body)
		}
	}
}

func TestBriefingDiagnoseUpstreamFailureIs502(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedCoachingFailure(t, st, e, "s1", e.fixedUTC.Add(-24*time.Hour), 1, "permission denied on /etc/hosts")
	seedCoachingFailure(t, st, e, "s2", e.fixedUTC.Add(-48*time.Hour), 2, "permission denied on /etc/hosts")
	f := newFakeAnalysisLLM(t)
	f.content = `{"diagnoses":[]}`
	runner, conns := llmRunnerFor(t, f)
	s.opts.LLMRunner, s.opts.LLMConns = runner, conns
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/diagnose", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("empty diagnoses reply → %d, want 502 (%s)", rr.Code, rr.Body.String())
	}
}
