package dashapi

// TerminalStatusResult is one run's fused status (metadata only; honest
// "unknown" + evidence + age so a hint is never presented as a fact).
type TerminalStatusResult struct {
	Handle     string  `json:"handle"`
	RunID      string  `json:"run_id,omitempty"`
	Status     string  `json:"status"`
	Evidence   string  `json:"evidence"`
	Confidence string  `json:"confidence"`
	AgeSeconds float64 `json:"age_seconds"`
	// PolicyStop is additively populated when this handle's PTY child was
	// stopped by an org-managed node's own node-intervention loop (see
	// policystop.go). Omitted when the seam is disabled or no matching audit
	// row exists - never fabricated.
	PolicyStop *PolicyStop `json:"policy_stop,omitempty"`
}

// TerminalStatusSubscription is one consumer of the live status stream. The
// caller MUST Close it when done.
type TerminalStatusSubscription interface {
	// Updates delivers status changes; closed when the subscription ends.
	Updates() <-chan TerminalStatusResult
	// Close releases the subscription.
	Close()
}

// TerminalStatusProvider is the dashboard's seam onto the terminal application
// service's status fusion (F4). The nil provider is the disabled state (the
// endpoints 503 / the WS closes immediately).
type TerminalStatusProvider interface {
	// StatusForHandle returns the current fused status for a live PTY handle.
	StatusForHandle(handle string) (TerminalStatusResult, bool)
	// Subscribe opens a live status stream across all live runs.
	Subscribe() TerminalStatusSubscription
}
