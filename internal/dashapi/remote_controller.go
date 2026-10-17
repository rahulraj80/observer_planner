//go:build !sbo_omit_remote

package dashapi

import (
	"time"

	"github.com/marmutapp/superbased-observer/internal/remoteauth"
)

// RemoteOptions configures a concrete RemoteController (plan §4). Built by cmd
// from [remote] once exposure is wired (Phase 2); assembled here so the HTTP
// glue (cookies, CSRF, pairing) lives with the mux, not in the pure
// internal/remoteauth primitives it injects.
type RemoteOptions struct {
	// HashedSecret is the argon2id hash of the pairing secret at rest. Empty ⇒
	// not ready (Ready() is false).
	HashedSecret string
	// AllowedHosts is the Host allow-list (configured bind + trusted_hosts).
	AllowedHosts []string
	// RateLimitPerMin throttles pairing attempts (0 disables).
	RateLimitPerMin int
	// AllowTerminal mirrors [remote].allow_terminal for remote fresh terminal
	// launch gating. Writer control is still authorized separately by
	// termlease.Authorize.
	AllowTerminal bool
	// AllowRemoteTerminalTakeover mirrors
	// [remote].allow_remote_terminal_takeover. It is consulted only after the
	// credential conjunction succeeds and defaults true through config.Default.
	// Hot-swapped by ReloadAllowRemoteTerminalTakeover.
	AllowRemoteTerminalTakeover bool
	// AllowTerminalView mirrors [remote].allow_terminal_view - the independent
	// READ opt-in that relaxes the remote-sensitive (attach/resume) deny-by-
	// default (session-attach design §3.2). It ONLY relaxes the VIEW gate
	// (snapshot row + read-only subscribe); the writer path stays governed by
	// AllowTerminal + the execute-tier conjunction. Default TRUE (seeded in
	// config.Default, mirroring AllowRemoteTerminalTakeover) - an explicit
	// allow_terminal_view = false restores the deny-read posture. Hot-swapped
	// by ReloadAllowTerminalView.
	AllowTerminalView bool
	// StandingTerminalSecretHash is the argon2id hash-at-rest of the OPT-IN
	// standing terminal-control secret (standing-terminal-access §B), read from
	// remotecfg.StandingTerminalSecretPath at construction. Empty ⇒ no standing
	// secret provisioned (the standing path denies). Hot-swapped by
	// ReloadStandingTerminalSecret on a dashboard mint/revoke.
	StandingTerminalSecretHash string
	// StandingTerminalEnabled mirrors [remote].allow_standing_terminal_control:
	// the master gate the standing verification leg checks BEFORE the hash
	// compare. Default false. Hot-swapped alongside the hash.
	StandingTerminalEnabled bool
	// StandingSecretAtRest answers, at verify time, whether a standing
	// terminal-control secret still EXISTS at rest on this node. It is the ONE
	// piece of evidence that separates a genuine REVOKE from a temporary
	// DISABLE, which are otherwise byte-identical on the live controller: both
	// arrive as ReloadStandingTerminalSecret("", false).
	//
	//	revoke  → remotecfg.StandingTerminalDisable DELETES the hash file
	//	          ⇒ probe false ⇒ no secret any device holds can ever be
	//	          accepted again (a re-mint issues a DIFFERENT one), so the
	//	          denial is PERMANENT and the device may discard its copy
	//	disable → allow_terminal→false / remote-disable / a config toggle only
	//	          flip the gate and LEAVE the hash file ⇒ probe true ⇒ the
	//	          refusal is TRANSIENT and the device must keep its secret
	//
	// It is deliberately a live probe rather than a reload-time snapshot: the
	// disk is the durable truth, and no hot-reload plumbing can then drift out
	// of sync with it. It MUST fail safe - return true whenever the answer is
	// unknown (stat error, no probe wired) - because a wrong "false" destroys a
	// user's saved secret irreversibly, while a wrong "true" costs one more
	// denied attempt. Nil ⇒ every such denial stays transient (the pre-A2
	// behaviour). Wired by cmd/observer from
	// remotecfg.StandingTerminalSecretPath.
	StandingSecretAtRest func() bool
	// Session tunes the device-session lifecycle.
	Session remoteauth.SessionParams
	// CapabilityTTL is the execute-capability lifetime (0 ⇒ default).
	CapabilityTTL time.Duration
	// Audit receives metadata-only pairing/logout records (never secrets).
	Audit func(RemoteAuditRecord)
	// Now is the clock (test hook). Nil defaults to time.Now.UTC.
	Now func() time.Time
}
