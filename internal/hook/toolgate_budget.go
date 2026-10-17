package hook

import (
	"encoding/json"
	"time"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/policy"
)

// toolGateBudgetStop asks an evaluator that carries the hook path's budget
// stop (guard.HookBudgetStopper) about a tool-gate call no policy event was
// built for (T-519). It is the ONE place a tool gate judges a call without
// classifying it, and it can only ever deny on a published managed budget
// stop: everything else about such a call stays exactly as before (it
// passes, and an unclassified one is recorded after the reply).
func toolGateBudgetStop(req ToolGateRequest, ev guard.Evaluator) (policy.Event, guard.ActionVerdict, bool) {
	bs, ok := ev.(guard.HookBudgetStopper)
	if !ok {
		return policy.Event{}, guard.ActionVerdict{}, false
	}
	pe := policy.Event{
		Kind:      policy.KindToolCall,
		Tool:      req.Tool,
		SessionID: toolGateSessionID(req.Body),
		Caps:      toolGateChannelCaps(req),
		Now:       time.Now().UTC(),
	}
	av, stopped := bs.HookBudgetStopFor(pe)
	return pe, av, stopped
}

// toolGateSessionID reads the session id every spelling of the Claude-shaped
// pre-tool family carries, for the budget stop's audit row. "" when the body
// does not parse (the stop does not need it).
func toolGateSessionID(body []byte) string {
	var p genericToolPayload
	if json.Unmarshal(body, &p) != nil {
		return ""
	}
	return firstNonEmpty(p.SessionID, p.SessionIDCamel, p.ConversationID)
}
