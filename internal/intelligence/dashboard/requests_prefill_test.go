package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/accessgrant"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestRequestsPrefillByRuleAndSession pins the deep link the guard's
// org-locked deny line carries (request-grants G4):
// /requests?rule=R&session=S. The reply precedes the event's persist, so
// the link names the rule and the session; the prefill resolves them to
// the latest event for that rule in that session (an exact draft, the
// Security page's own shape), and falls back to a rule-wide draft when
// none is recorded. It never borrows another session's event.
func TestRequestsPrefillByRuleAndSession(t *testing.T) {
	s, _ := newTestServer(t)
	st := store.New(s.opts.DB)
	now := time.Now().UTC()
	n, err := st.InsertGuardEvents(context.Background(), []store.GuardEventRow{
		{
			TS: now.Add(-9 * time.Minute), SessionID: "sA", Tool: "codex", EventKind: "shell_exec", RuleID: "R-171",
			Category: "exfil", Severity: "high", Decision: "deny", Enforced: true, Source: "org", TargetHash: "h-old", TargetExcerpt: "curl -T a",
		},
		{
			TS: now.Add(-2 * time.Minute), SessionID: "sA", Tool: "codex", EventKind: "shell_exec", RuleID: "R-171",
			Category: "exfil", Severity: "high", Decision: "deny", Enforced: true, Source: "org", TargetHash: "h-new", TargetExcerpt: "curl -T b",
		},
		{
			TS: now.Add(-1 * time.Minute), SessionID: "sB", Tool: "codex", EventKind: "shell_exec", RuleID: "R-171",
			Category: "exfil", Severity: "high", Decision: "deny", Enforced: true, Source: "org", TargetHash: "h-other", TargetExcerpt: "curl -T c",
		},
		{
			TS: now.Add(-1 * time.Minute), SessionID: "sA", Tool: "codex", EventKind: "shell_exec", RuleID: "R-101",
			Category: "destructive", Severity: "critical", Decision: "deny", Enforced: true, Source: "builtin", TargetHash: "h-rm",
		},
	})
	if err != nil || n != 4 {
		t.Fatalf("seed: %d %v", n, err)
	}
	var ids []int64
	rows, err := s.opts.DB.Query(`SELECT id FROM guard_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if len(ids) != 4 {
		t.Fatalf("ids %v", ids)
	}
	draft := func(path string) (orgcontract.AccessRequestCreate, int) {
		t.Helper()
		rr := doRequests(t, s, http.MethodGet, path, "")
		var out struct {
			Request orgcontract.AccessRequestCreate `json:"request"`
		}
		if rr.Code == http.StatusOK {
			if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
				t.Fatalf("%s: %v %s", path, err, rr.Body)
			}
		}
		return out.Request, rr.Code
	}

	// The latest event for the rule in that session: an exact draft.
	d, code := draft("/api/requests/prefill?rule=R-171&session=sA")
	if code != http.StatusOK || d.Payload == nil || d.Payload.RuleID != "R-171" || d.Payload.OperandHash != "h-new" ||
		d.Payload.Breadth != accessgrant.BreadthExact || d.Context == nil || d.Context.EventID != ids[1] || d.Context.SessionID != "sA" {
		t.Fatalf("rule+session draft: %d %+v %+v", code, d.Payload, d.Context)
	}

	// No event for the rule in that session: a rule-wide draft that names
	// the session, never another session's operand.
	d, code = draft("/api/requests/prefill?rule=R-171&session=sZ")
	if code != http.StatusOK || d.RequestType != accessgrant.TypeGuardExemption || d.Payload == nil ||
		d.Payload.RuleID != "R-171" || d.Payload.Breadth != accessgrant.BreadthRule || d.Payload.OperandHash != "" ||
		d.Context == nil || d.Context.SessionID != "sZ" || d.Context.EventID != 0 {
		t.Fatalf("fallback draft: %d %+v %+v", code, d.Payload, d.Context)
	}

	// Rule only (the link of a reply that had no session id): rule-wide.
	if d, code = draft("/api/requests/prefill?rule=R-101"); code != http.StatusOK || d.Payload.Breadth != accessgrant.BreadthRule || d.Payload.RuleID != "R-101" {
		t.Fatalf("rule-only draft: %d %+v", code, d.Payload)
	}

	// The event-id entry point is unchanged.
	if d, code = draft("/api/requests/prefill?event_id=" + itoa64(ids[0])); code != http.StatusOK || d.Payload.OperandHash != "h-old" {
		t.Fatalf("event draft: %d %+v", code, d.Payload)
	}

	// Nothing to prefill from, or a malformed rule id: 404 / 400.
	if _, code = draft("/api/requests/prefill"); code != http.StatusNotFound {
		t.Fatalf("empty prefill: %d", code)
	}
	if _, code = draft("/api/requests/prefill?rule=" + "R-1%0Ax"); code != http.StatusBadRequest {
		t.Fatalf("malformed rule: %d", code)
	}
}
