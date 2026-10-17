package dashapi

// UpdateApplyRequest is what the CLI asks for.
type UpdateApplyRequest struct {
	// Version pins a target; empty means whatever the org published for
	// this node.
	Version string `json:"version,omitempty"`
	// Force ignores the local maintenance window and, inside an admin
	// maintenance window, permits an apply with a live dashboard terminal.
	// It never shortens the drain and never skips verification.
	Force bool `json:"force,omitempty"`
	// DryRun prints the plan and performs nothing.
	DryRun bool `json:"dry_run,omitempty"`
	// Rollback restores the previous binary instead of applying.
	Rollback bool `json:"rollback,omitempty"`
}

// UpdateApplyResult is what the daemon reports back. Every field is either a
// closed enum or operator-facing prose that stays on this machine - the route
// is loopback-only, and the org-bound posture is a separate, narrower row.
type UpdateApplyResult struct {
	State      string   `json:"state"`
	Reason     string   `json:"reason,omitempty"`
	ErrorClass string   `json:"error_class,omitempty"`
	Detail     string   `json:"detail,omitempty"`
	Steps      []string `json:"steps,omitempty"`
	Applied    bool     `json:"applied,omitempty"`
	RolledBack bool     `json:"rolled_back,omitempty"`
	Deferred   bool     `json:"deferred,omitempty"`
	Error      string   `json:"error,omitempty"`
	// Accepted reports that the daemon TOOK the request and is running the
	// apply off this request's goroutine; EventID names the update_events row
	// that recorded the acceptance.
	//
	// They exist because an apply is a drain plus a fork-exec handshake, and
	// holding the request open for it made the dashboard's own 5s Shutdown
	// budget able to kill the supervising parent mid-handshake. An accepted
	// apply is answered 202 and followed in `observer update history`.
	Accepted bool  `json:"accepted,omitempty"`
	EventID  int64 `json:"event_id,omitempty"`
}

// UpdateStatusResult is the node's own update posture, for its own dashboard.
//
// This is a LOCAL surface, not the org wire: it may carry the operator-facing
// advice string and the install-method verdict, which the enum-only
// orgcontract.UpdatePostureRow deliberately cannot. It still carries no path.
type UpdateStatusResult struct {
	// Enabled is false when [update].enabled = false. Every other field is
	// then zero and the card says the feature is off rather than implying
	// this node is up to date.
	Enabled bool `json:"enabled"`
	// Version is the running binary; Channel is the assigned channel.
	Version string `json:"version,omitempty"`
	Channel string `json:"channel,omitempty"`
	// State / Reason / ErrorClass are update.State / Reason / ErrorClass.
	State      string `json:"state"`
	Reason     string `json:"reason,omitempty"`
	ErrorClass string `json:"error_class,omitempty"`
	// TargetVersion and ManifestVersion say what the org has offered this
	// node; LastManifestSeenAt is when it last heard.
	TargetVersion      string `json:"target_version,omitempty"`
	ManifestVersion    int64  `json:"manifest_version,omitempty"`
	LastManifestSeenAt string `json:"last_manifest_seen_at,omitempty"`
	// InstallMethod / SelfApply / Advice are the §3.7 install-method table's
	// verdict. Advice is what to run INSTEAD when SelfApply is false, with
	// the target version already substituted - an honest disabled state, never
	// a silent "up to date".
	InstallMethod string `json:"install_method,omitempty"`
	SelfApply     bool   `json:"self_apply"`
	Advice        string `json:"advice,omitempty"`
	// AutoApply and AutoApplyReason answer "will this node update itself, and
	// who decided". The reason matters: "off" because the operator said so
	// and "off" because this fleet is not managed are different facts.
	AutoApply       bool   `json:"auto_apply"`
	AutoApplyReason string `json:"auto_apply_reason,omitempty"`
	// Window is [update].window verbatim ("" = any time the quiescence checks
	// pass).
	Window string `json:"window,omitempty"`
	// OrgRefusal is set when the org server most recently REFUSED this node's
	// push because it is below [server].min_agent_version (W5 skew
	// enforcement). It is the one condition where the node is neither idle nor
	// updating and yet urgently needs the operator's attention, so it gets its
	// own field rather than being buried in a push error string.
	OrgRefusal *UpdateOrgRefusal `json:"org_refusal,omitempty"`
}

// UpdateOrgRefusal names a 426 the org server returned.
type UpdateOrgRefusal struct {
	Message     string `json:"message"`
	MinVersion  string `json:"min_version,omitempty"`
	YourVersion string `json:"your_version,omitempty"`
	At          string `json:"at,omitempty"`
}
