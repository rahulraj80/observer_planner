package dashboard

import (
	"net/http"
)

// handleTerminalSandbox serves GET /api/terminal/sandbox — the B9 sandbox
// probe surface (plan §5, mirroring the B5 GET /api/terminal/launch/models
// fail-soft pattern). It ALWAYS returns HTTP 200: an old daemon build 404s
// this route entirely (unknown path), while a live daemon always 200s with
// an honest verdict — including when the sandbox feature itself is absent
// (nil seam), which reports {"available":false,"verdict":"disabled_by_config",
// "reason":"…"} rather than an error status. This is deliberately DIFFERENT
// from handleTerminalLaunch's fail-CLOSED sandbox validation: that handler
// answers "can I honour a sandbox request RIGHT NOW" (and refuses the launch
// with 501/403/400 when it can't); this endpoint answers "what can this
// daemon do at all" — a read, never a refusal.
func (s *Server) handleTerminalSandbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.opts.SandboxProber == nil {
		writeJSON(w, SandboxAvailability{
			Available: false,
			Verdict:   VerdictDisabledByConfig,
			Reason:    "sandbox support is not enabled on this daemon",
		})
		return
	}
	writeJSON(w, s.opts.SandboxProber.ProbeSandbox(r.Context()))
}
