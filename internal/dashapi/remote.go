package dashapi

import (
	"net/http"
)

// Capability classifies a dashboard route's authorization tier for remote
// exposure (remote-dashboard-access plan §4.1/§4.2). Every route reachable on
// a remotely-exposed bind must be classified; an UNCLASSIFIED route FAILS
// CLOSED (is refused) rather than defaulting to open. The ordering is
// deliberate - a principal granted capability C may reach any route requiring
// a capability <= C (Execute ⊇ View ⊇ Public), so the enum increases with
// privilege.
type Capability int

const (
	// CapabilityUnclassified is the zero value: a route with no explicit
	// classification. On a remotely-exposed bind it is REFUSED (fail closed).
	CapabilityUnclassified Capability = iota
	// CapabilityPublic - reachable with no authentication (the SPA shell,
	// static assets, the pairing endpoint, health liveness).
	CapabilityPublic
	// CapabilityView - read-only surfaces; requires an authenticated device
	// session (plan §4.2 view tier).
	CapabilityView
	// CapabilityExecute - anything that reaches the machine (launch, config
	// writes, restart, terminal input); requires a single-use, session- and
	// action-bound execute capability minted after local approval (plan §4.2
	// execute tier). Never a reusable token.
	CapabilityExecute
	// CapabilityLocal - owner-local-only (dashboard-management-surface plan
	// §A/§9). ORTHOGONAL to the Public<View<Execute privilege ladder: a Local
	// route is reachable ONLY from the owner-trusted direct loopback listener
	// (browserGuard, no remoteAuthz), and REFUSED on every remotely-exposed
	// bind BEFORE the principal is resolved - mirroring the funnel-execute early
	// refusal, so no single-use execute capability is ever consumed on a
	// refused request. A remote principal is NEVER granted Local;
	// requiredCapability(Local,…) fails closed as defense-in-depth. This is the
	// class for config/consent/machine-reaching mutations (config writes, setup
	// writes, daemon restart, remote arm/disarm, session revoke) that a remote
	// viewer must never reach at any tier.
	CapabilityLocal
)

// String renders a Capability for logs / audit rows.
func (c Capability) String() string {
	switch c {
	case CapabilityPublic:
		return "public"
	case CapabilityView:
		return "view"
	case CapabilityExecute:
		return "execute"
	case CapabilityLocal:
		return "local"
	default:
		return "unclassified"
	}
}

// RemoteController is the injected seam onto the remote-access security
// substrate (plan §4). The dashboard package DEFINES this interface rather
// than importing the substrate, keeping the pure primitives
// (internal/remoteauth) free of net/http and letting tests inject a fake - the
// same injected-seam discipline as BuildHandoff / LaunchManager.
//
// The zero (nil) controller is the default: no remote exposure. Per the §4.6
// atomic-safety rule, a non-loopback bind is REFUSED unless a non-nil
// controller reports Ready() - which is true only when auth is configured AND
// the route-capability registry is complete AND a Host allow-list is active
// AND rate limiting is active. Pre-Phase-1 no controller is ever wired, so a
// non-loopback bind is simply refused, closing the historical
// `--addr 0.0.0.0` unauthenticated-RCE hole.
type RemoteController interface {
	// Ready reports the §4.6 predicate: the full auth + route-registry +
	// host-allowlist + rate-limit stack is assembled. A false result MUST
	// keep the listener loopback-only.
	Ready() bool
	// AllowedHosts returns the Host-header allow-list browserGuard enforces on
	// a remotely-exposed bind (plan §4.5). It is built from the explicit
	// configured bind address plus [remote].trusted_hosts and never resolves
	// to "allow any Host" - that would reopen DNS-rebind.
	AllowedHosts() []string
	// Principal resolves the capability a request's authentication grants
	// (device-session cookie + CSRF for mutations; the execute capability for
	// execute routes). An unauthenticated request resolves to CapabilityPublic.
	Principal(r *http.Request) Capability
	// AllowTerminal reports whether [remote].allow_terminal is enabled in the
	// live config snapshot. It only relaxes the fresh terminal launch gate; the
	// writer path still requires the one-time terminal-control capability.
	AllowTerminal() bool
	// Routes returns the controller's own HTTP routes (/api/remote/pair,
	// /whoami, /logout) to mount into the dashboard mux. Each carries its own
	// Capability classification.
	Routes() []ExtraRoute
	// Sessions lists the live device sessions as metadata-only fingerprints
	// (dashboard-management-surface plan §2F). The full session id is NEVER
	// returned on the wire; the management handlers use it server-side only.
	Sessions() []RemoteSessionInfo
	// RevokeSession revokes ONE live session by its full id (the handler
	// resolves the id from a fingerprint). Takes effect instantly - no restart
	// (§C). Returns whether a live session was found + revoked.
	RevokeSession(id string) bool
	// RotateSessions revokes ALL live device sessions immediately
	// (terminate-all). Instant, no restart (§C). Returns an error only when the
	// durable rotate fails (fail-closed - sessions stay live for a retry).
	RotateSessions() error
	// ReloadSecret swaps the live pairing-secret hash so a dashboard
	// rotate/enable takes effect on the RUNNING controller immediately (a
	// freshly-minted QR pairs without a daemon restart). Empty ⇒ not-Ready.
	ReloadSecret(hashed string)
}

// RemoteAuditRecord is the metadata-only record the dashboard emits to the
// injected Options.RemoteAudit sink for each remote-access decision (plan
// §4.8). The dashboard defines its own type (not store.RemoteAuditEvent) to
// avoid importing the store; cmd adapts it. NEVER carries a secret.
type RemoteAuditRecord struct {
	Kind       string
	SessionID  string
	Principal  string
	RemoteAddr string
	Route      string
	Decision   string
	Detail     string
}
