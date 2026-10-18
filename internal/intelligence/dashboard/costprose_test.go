package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMaskDollarProse: under the token view, prose in a JSON response loses
// its dollar amounts while fields and numbers stay (the client formats those
// by mode); under the dollar view, and for non-JSON bodies, nothing changes.
func TestMaskDollarProse(t *testing.T) {
	h := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"narrative": "Routing saved $0.42 today; you spent $12.10.",
			"cost_usd":  12.1,
		})
	}
	run := func(s *Server) (*httptest.ResponseRecorder, map[string]any) {
		rec := httptest.NewRecorder()
		s.maskDollarProse(h)(rec, httptest.NewRequest(http.MethodGet, "/api/briefing", nil))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec, body
	}

	// Token view (an unverifiable display fails to tokens).
	tok := &Server{opts: Options{OrgClient: &fakeEnrolment{statusErr: errors.New("status read failed")}}}
	if got := tok.statuslineCostDisplay(context.Background()); got != "tokens" {
		t.Fatalf("fixture mode = %q, want tokens", got)
	}
	rec, body := run(tok)
	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d; the wrapper must keep the handler's status", rec.Code)
	}
	if n, _ := body["narrative"].(string); strings.Contains(n, "$") || !strings.Contains(n, "[hidden]") {
		t.Errorf("token view narrative = %q; want dollar amounts hidden", n)
	}
	if body["cost_usd"] != 12.1 {
		t.Errorf("numeric field = %v; must be kept for the client's own formatting", body["cost_usd"])
	}

	// Dollar view (no org enrolment): untouched.
	_, body = run(&Server{})
	if n, _ := body["narrative"].(string); !strings.Contains(n, "$0.42") {
		t.Errorf("dollar view narrative = %q; want unchanged", n)
	}
}
