package dashboard

import (
	"net/http"
	"strings"
	"time"
)

// remote_seams.go holds the remote-access seams the core dashboard calls
// (lane MP-L1I, T-715, ruling D-512): the session metadata row, the
// type-asserted capability interfaces a concrete RemoteController may
// satisfy, the session cookie / device-key helpers, and the two functions
// the build provides (isStandingSecret and hashSessionID).
// internal/remoteauth implements them through remote_controller.go, which a
// `-tags sbo_omit_remote` build leaves out (remote_omit.go); with no
// controller every seam answers "no remote access".

// standingTerminalReloader is the hot-swap seam the dashboard management
// handlers use to make a standing mint/revoke take effect on the LIVE
// controller without a daemon restart. Type-asserted off the RemoteController so
// nil/fake controllers stay valid.
type standingTerminalReloader interface {
	ReloadStandingTerminalSecret(hashed string, enabled bool)
}

// allowTerminalReloader is the hot-swap seam for the LIVE allow_terminal gate
// (finding 3 residual). Type-asserted off the RemoteController so nil/fake
// controllers stay valid.
type allowTerminalReloader interface {
	ReloadAllowTerminal(allow bool)
}

// allowRemoteTerminalTakeoverReloader is the additive hot-swap seam for the
// live authenticated-remote takeover policy. Nil/fake controllers remain valid.
type allowRemoteTerminalTakeoverReloader interface {
	ReloadAllowRemoteTerminalTakeover(allow bool)
}

// allowTerminalViewer is the READ seam for the LIVE allow_terminal_view gate
// (session-attach design §3.2, Phase 4). It is type-asserted off the
// RemoteController - rather than added to the base interface - so nil/fake
// controllers (which deny by omission) stay valid, additive per CLAUDE.md #6.
// The concrete *remoteController satisfies it.
type allowTerminalViewer interface {
	AllowTerminalView() bool
}

// allowTerminalViewReloader is the hot-swap seam for the LIVE allow_terminal_view
// gate, mirroring allowTerminalReloader. Type-asserted off the RemoteController
// so nil/fake controllers stay valid.
type allowTerminalViewReloader interface {
	ReloadAllowTerminalView(allow bool)
}

// sessionRevokeHooker is the wiring seam (F2) letting the Server register a
// callback the controller fires on a device self-logout, so the device's open
// read-only sensitive viewers are torn down. Type-asserted off the
// RemoteController so nil/fake controllers stay valid (additive, CLAUDE.md #6).
type sessionRevokeHooker interface {
	SetSessionRevokeHook(func(sessionKey string))
}

// deviceSessionLifetimer is the READ seam (session-attach F1a/F1b) binding a
// privileged read-only viewer to its device session's lifetime. SessionLifetime
// returns the session's revocation channel (closed on revoke/rotate/eviction),
// the duration until its next TTL/idle expiry, and whether it is currently live
// - used BOTH to re-validate after registration (F1a: the drain-then-register
// race) AND to cancel the viewer on a later session-end (F1b). Type-asserted off
// the RemoteController so nil/fake controllers (loopback, which never expose a
// remote-sensitive viewer) stay valid, additive per CLAUDE.md #6.
type deviceSessionLifetimer interface {
	SessionLifetime(raw string) (revoked <-chan struct{}, until time.Duration, live bool)
}

// deviceFingerprint derives the short, non-reversible device identity used in
// the execute-tier audit rows from a RAW device-session id: the first 8 hex
// chars of its sha256 hash - byte-identical to remoteauth's SessionInfo
// fingerprint (and the value the local approve-execute step records), so the
// capability-lifecycle events correlate on ONE device token. Never the raw
// bearer. Empty in ⇒ empty out.
func deviceFingerprint(rawSession string) string {
	if rawSession == "" {
		return ""
	}
	h := hashSessionID(rawSession)
	if len(h) <= 8 {
		return h
	}
	return h[:8]
}

// deviceSessionKey derives the FULL, non-reversible device-session key from a
// RAW cookie: its sha256 hash (HashSessionID) - the SAME identity the session
// store keys on. It is the registry key for read-only sensitive viewers (F2b),
// distinct from deviceFingerprint (the 32-bit display prefix used only in audit
// rows / the management surface). Keying the viewer registry on the full hash -
// never the display prefix - closes the collision hole where a device whose
// fingerprint prefix matches another's could disconnect that other's viewer.
// Empty in ⇒ empty out (the owner-local path, never registered per-device).
func deviceSessionKey(rawSession string) string {
	if rawSession == "" {
		return ""
	}
	return hashSessionID(rawSession)
}

func sessionCookie(r *http.Request) string {
	ck, err := r.Cookie(remoteSessionCookie)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(ck.Value)
}

func isUnsafeMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}

// Cookie + header names for the remote auth flow (plan §4.3/§4.5).
const (
	remoteSessionCookie = "sb_remote_session"
	remoteCSRFHeader    = "X-Remote-CSRF"    //nolint:gosec // header name, not a credential
	remoteExecuteHeader = "X-Remote-Execute" //nolint:gosec // header name, not a credential
)
