package hook

import (
	"bytes"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/policy"
)

// budgetStopEvaluator allows every classified call and, when stop is set,
// answers the budget-only question with a B-602 deny.
type budgetStopEvaluator struct {
	stop     bool
	asked    int
	gotTool  string
	gotSessn string
}

func (e *budgetStopEvaluator) EvaluateHook(policy.Event) (guard.ActionVerdict, bool) {
	return guard.ActionVerdict{Verdict: policy.Verdict{Decision: policy.DecisionAllow}}, false
}

func (e *budgetStopEvaluator) HookBudgetStopFor(ev policy.Event) (guard.ActionVerdict, bool) {
	e.asked++
	e.gotTool, e.gotSessn = ev.Tool, ev.SessionID
	if !e.stop || !ev.Caps.CanBlock {
		return guard.ActionVerdict{}, false
	}
	return guard.ActionVerdict{Kind: ev.Kind, Verdict: policy.Verdict{
		Decision: policy.DecisionDeny, RuleID: "B-602", Reason: "Organization budget B-602: spent.",
	}}, true
}

// TestT519ToolGateBudgetStopsAnUnclassifiedCall pins that a pre-tool call the
// gate cannot classify (node 1's Cursor "Grep", recorded as unclassified and
// let through unjudged) is still stopped by an exhausted managed budget, and
// that without a stop it keeps its pre-fix behaviour (passes, recorded after
// the reply).
func TestT519ToolGateBudgetStopsAnUnclassifiedCall(t *testing.T) {
	t.Parallel()
	body := []byte(`{"conversation_id":"c-1","tool_name":"Grep","tool_input":{"pattern":"x"},"hook_event_name":"preToolUse"}`)
	for _, tc := range []struct {
		name     string
		stop     bool
		wantDeny bool
	}{
		{"exhausted budget: denied in Cursor's shape", true, true},
		{"no stop: passes as before", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ev := &budgetStopEvaluator{stop: tc.stop}
			unclassified := false
			var out, errOut bytes.Buffer
			res := HandleToolGate(ToolGateRequest{
				Tool: "cursor", Dialect: ToolGateDialectCursor, NativeEvent: "preToolUse", Label: "cursor:preToolUse",
				Body: body, Unclassified: func(string) { unclassified = true },
			}, ev, nil, &out, &errOut)
			if ev.asked != 1 || ev.gotTool != "cursor" || ev.gotSessn != "c-1" {
				t.Fatalf("budget asked %d times for tool %q session %q", ev.asked, ev.gotTool, ev.gotSessn)
			}
			if tc.wantDeny {
				if !res.Replied || !strings.Contains(out.String(), `"permission":"deny"`) || !strings.Contains(out.String(), "B-602") {
					t.Fatalf("result %+v reply %q, want a deny naming B-602", res, out.String())
				}
				return
			}
			if res.Replied || out.Len() != 0 {
				t.Fatalf("no stop must leave the reply to the caller, got %q", out.String())
			}
			if res.RecordAfterReply == nil {
				t.Fatal("an unclassified call is still recorded after the reply")
			}
			res.RecordAfterReply()
			if !unclassified {
				t.Fatal("the unclassified record did not run")
			}
		})
	}
}
