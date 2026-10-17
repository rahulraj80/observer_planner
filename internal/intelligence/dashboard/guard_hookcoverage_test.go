package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/guardposture"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/policy"
	"github.com/marmutapp/superbased-observer/internal/store"
)

func hookFailureRow(tool, kind string, enforced bool, at time.Time) guard.ActionVerdict {
	dec := policy.DecisionFlag
	if enforced {
		dec = policy.DecisionDeny
	}
	return guard.ActionVerdict{
		Input:      guard.ActionInput{Tool: tool, Target: "PreToolUse", Timestamp: at},
		Kind:       policy.KindToolCall,
		Category:   "guard",
		Verdict:    policy.Verdict{Decision: dec, RuleID: guard.GuardErrorRuleID, Severity: policy.SeverityHigh, Source: policy.SourceBuiltin},
		GuardError: true, Enforced: enforced, DegradedFrom: kind,
	}
}

// TestGuardHookCoverage: the endpoint serves the generated matrix (every
// registry row), the node posture, and the recorded gate hook failures split
// fail-closed / fail-open (a posture-tamper guard_error is not a hook
// failure), plus a confirm token for the probe action.
func TestGuardHookCoverage(t *testing.T) {
	s, _ := newTestServer(t)
	now := time.Now().UTC()
	st := store.New(s.opts.DB)
	if _, err := st.PersistGuardVerdicts(context.Background(), []guard.ActionVerdict{
		hookFailureRow("claude-code", guardposture.KindTimeout, true, now.Add(-time.Hour)),
		hookFailureRow("claude-code", guardposture.KindTimeout, true, now.Add(-30*time.Minute)),
		hookFailureRow("cursor", guardposture.KindCrash, false, now.Add(-time.Hour)),
		hookFailureRow("observer", "posture_tamper", false, now.Add(-time.Hour)),
		hookFailureRow("codex", guardposture.KindConfig, true, now.Add(-30*24*time.Hour)),
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/guard/hook-coverage", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp hookCoverageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Matrix.Rows) != len(integration.Capabilities()) || len(resp.Matrix.Groups) == 0 {
		t.Errorf("matrix rows %d groups %d", len(resp.Matrix.Rows), len(resp.Matrix.Groups))
	}
	if resp.ConfirmToken == "" || resp.WindowHours != 168 || resp.Posture.State == "" {
		t.Errorf("token %q window %d posture %+v", resp.ConfirmToken, resp.WindowHours, resp.Posture)
	}
	got := map[string]int{}
	for _, f := range resp.Failures {
		got[f.Tool+"/"+f.Outcome+"/"+f.Kind] = f.Count
	}
	want := map[string]int{"claude-code/fail_closed/timeout": 2, "cursor/fail_open/crash": 1}
	if len(got) != len(want) {
		t.Fatalf("failures = %v, want %v", got, want)
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s = %d, want %d (all %v)", k, got[k], n, got)
		}
	}
}

// TestGuardHookActionsValidate: the doctor and probe actions refuse an
// unknown tool or gate before running anything, and the probe (which
// records a guard verdict) requires the confirm token.
func TestGuardHookActionsValidate(t *testing.T) {
	s, _ := newTestServer(t)
	post := func(path, body string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := post("/api/guard/hook-doctor", `{"tool":"no-such-tool"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("doctor unknown tool = %d", rec.Code)
	}
	if rec := post("/api/guard/hook-probe", `{"tool":"claude-code","gate":"tool"}`, nil); rec.Code != http.StatusForbidden {
		t.Errorf("probe without confirm token = %d, want 403", rec.Code)
	}
	tok := strings.Repeat("ab", 32)
	hdr := map[string]string{remoteConfirmHeader: tok, "Cookie": remoteConfirmCookie + "=" + tok}
	if rec := post("/api/guard/hook-probe", `{"tool":"claude-code","gate":"everything"}`, hdr); rec.Code != http.StatusBadRequest {
		t.Errorf("probe bad gate = %d, want 400", rec.Code)
	}
}

// TestHookPostureView: absent, valid and stale postures read the way a
// hook reads them.
func TestHookPostureView(t *testing.T) {
	dir := t.TempDir()
	cp := dir + "/config.toml"
	if v := hookPostureView(cp, time.Now()); v.State != "absent" {
		t.Errorf("no posture = %+v", v)
	}
	key, err := guardposture.LoadOrCreateKey(guardposture.KeyPath(cp))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	p := guardposture.Posture{Enforced: true, Source: "local", GateCaps: []string{"tool_gate"}, WrittenAt: now.Add(-10 * time.Second)}
	if _, err := guardposture.Write(guardposture.PosturePath(cp), p, key); err != nil {
		t.Fatal(err)
	}
	if v := hookPostureView(cp, now); v.State != "valid" || !v.Enforced || len(v.GateCaps) != 1 {
		t.Errorf("fresh posture = %+v", v)
	}
	if v := hookPostureView(cp, now.Add(time.Hour)); v.State != "stale" || v.Detail == "" {
		t.Errorf("old posture = %+v", v)
	}
}
