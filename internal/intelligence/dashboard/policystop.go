package dashboard

import (
	"context"
)

// policystop.go is the UX-honesty seam for a dashboard terminal an org-
// managed node's OWN node-intervention loop stopped (a process-control
// policy verdict, e.g. rule B-602 "daily spend accounting is unavailable").
// Without it, an operator's "New Terminal" session just goes to
// EXITED (255) with no explanation — this surfaces the SAME durable audit
// row the daemon already wrote (internal/store.LookupProcessControlStop over
// guard_events, category process_control) on the terminal's exit event and
// its status payload. The dashboard never imports internal/store or
// internal/termsession — cmd supplies a PolicyStopProvider closure, the same
// injected-seam discipline as TerminalStatusProvider.

// resolvePolicyStop is the one call site both the exit-frame path
// (bridgeTerminalWS) and the status payload (handleTerminalStatus) use to
// consult the optional seam. A nil provider or a not-found lookup both
// resolve to (zero value, false) — the caller then simply omits policy_stop.
func (s *Server) resolvePolicyStop(ctx context.Context, handle string) (PolicyStop, bool) {
	if s.opts.PolicyStop == nil {
		return PolicyStop{}, false
	}
	return s.opts.PolicyStop.PolicyStopForHandle(ctx, handle)
}
