package dashapi

import (
	"time"
)

// RemoteSessionInfo is one live remote device session as the management
// surface lists it (metadata only; the full session hash in ID is used
// server-side to target a revoke and is never exposed on the JSON surface).
type RemoteSessionInfo struct {
	// Fingerprint is the first 8 hex chars of the session hash.
	Fingerprint string
	// ID is the full session hash.
	ID string
	// CreatedAt / LastSeen are UTC timestamps; AgeSeconds is now-createdAt.
	CreatedAt  time.Time
	LastSeen   time.Time
	AgeSeconds int64
}

// TerminalControlAuthorizer is the subset of the remote substrate the cmd launch
// adapter needs to run the §4.δ terminal-control conjunction (via
// termlease.Authorize) and to mint at the LOCAL approve-execute step (§4.γ). It
// is exposed via a type assertion on RemoteController so the seam stays minimal
// and the fakes are unaffected. The concrete *remoteController implements it.
//
// Validate + ConsumeTerminalControl deliberately match termlease.SessionValidator
// and termlease.CapabilityConsumer so the same value can be passed as both to
// termlease.Authorize without a further adapter.
type TerminalControlAuthorizer interface {
	// Validate reports whether a raw device-session id is a live session
	// (termlease.SessionValidator).
	Validate(deviceSessionID string) error
	// ConsumeTerminalControl consumes a single-use terminal-control capability +
	// bound confirm for a raw device-session id + terminal handle
	// (termlease.CapabilityConsumer). A failed confirm consumes nothing.
	ConsumeTerminalControl(token, confirm, deviceSessionID, handle string) bool
	// MintTerminalControl mints a single-use terminal-control capability + bound
	// confirm for a device-session HASH (the id surfaced by Sessions()) + handle.
	// Returned only to the LOCAL approve-execute surface (§4.γ / §6).
	MintTerminalControl(deviceSessionHash, handle string) (token, confirm string, err error)
}

// StandingTerminalVerifier is the subset of the remote substrate the cmd launch
// adapter needs to run the OPT-IN standing terminal-control leg via
// termlease.AuthorizeStanding (standing-terminal-access §B). It is separate from
// TerminalControlAuthorizer so a fake that only exercises the single-use path is
// unaffected (additive, CLAUDE.md #6); the concrete *remoteController satisfies
// both. VerifyStandingTerminalControl deliberately matches
// termlease.StandingVerifier so the same value passes straight through.
type StandingTerminalVerifier interface {
	VerifyStandingTerminalControl(secret, deviceSessionID, handle string) bool
	// StandingTerminalGeneration returns a counter that advances on EVERY
	// standing-secret swap (mint/rotate/revoke/disable). The acquire adapter
	// captures it before verifying and re-checks it after the lease install,
	// so a verify that raced a revoke can never leave a surviving writer.
	StandingTerminalGeneration() uint64
}
