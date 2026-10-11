package dashboard

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestAPIGuardSummaryHookFailure pins T-346: /api/guard/summary reports the
// per-gate-family hook failure mode from the daemon's seam (never from
// [guard].strict), and omits it when no seam is wired.
func TestAPIGuardSummaryHookFailure(t *testing.T) {
	t.Parallel()
	get := func(t *testing.T, s *Server) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/guard/summary", nil))
		if rec.Code != 200 {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return m
	}
	for _, tc := range []struct {
		name         string
		seam         func() GuardHookFailure
		strict       bool
		tool, prompt string
	}{
		{"no seam: not claimed", nil, false, "", ""},
		{"enforcing both families, not strict", func() GuardHookFailure { return GuardHookFailure{ToolGateClosed: true, PromptGateClosed: true} }, false, "closed", "closed"},
		{"tool family only", func() GuardHookFailure { return GuardHookFailure{ToolGateClosed: true} }, false, "closed", "open"},
		{"strict but nothing enforced", func() GuardHookFailure { return GuardHookFailure{} }, true, "open", "open"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			s.opts.GuardEnabled, s.opts.GuardMode, s.opts.GuardStrict = true, "enforce", tc.strict
			s.opts.GuardHookFailure = tc.seam
			hf, ok := get(t, s)["hook_failure"].(map[string]any)
			if tc.seam == nil {
				if ok {
					t.Fatalf("hook_failure = %v, want absent with no seam", hf)
				}
				return
			}
			if !ok || hf["tool_gate"] != tc.tool || hf["prompt_gate"] != tc.prompt {
				t.Errorf("hook_failure = %v, want tool_gate=%s prompt_gate=%s", hf, tc.tool, tc.prompt)
			}
		})
	}
}
