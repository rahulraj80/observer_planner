package dashboard

// Hook failure mode on the Security page's Guard posture tile (T-346).
//
// What a gate hook does when it FAILS (times out, cannot load its config or
// build its guard, cannot parse the payload) is decided per gate family by
// the node's enforcement: a family that is enforced fails CLOSED (the call is
// blocked with a reason), every other one fails open (D-040/D-041, D-194).
// [guard].strict does not decide it, so the tile no longer derives its text
// from strict. The answer comes from the daemon through GuardHookFailure,
// the same per-family question every `observer hook` invocation asks
// (cmd/observer guardHookFailureSeam).

// Hook failure modes on the wire.
const (
	hookFailClosed = "closed"
	hookFailOpen   = "open"
)

// guardHookFailureJSON is /api/guard/summary's hook_failure object.
type guardHookFailureJSON struct {
	ToolGate   string `json:"tool_gate"`
	PromptGate string `json:"prompt_gate"`
}

func hookFailMode(closed bool) string {
	if closed {
		return hookFailClosed
	}
	return hookFailOpen
}

// hookFailure reports the daemon's per-family hook failure mode, or nil when
// no seam is wired (the page then says it cannot tell).
func (s *Server) hookFailure() *guardHookFailureJSON {
	if s.opts.GuardHookFailure == nil {
		return nil
	}
	f := s.opts.GuardHookFailure()
	return &guardHookFailureJSON{ToolGate: hookFailMode(f.ToolGateClosed), PromptGate: hookFailMode(f.PromptGateClosed)}
}
