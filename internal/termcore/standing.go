package termcore

// StandingDenial classifies WHY a standing-credential verify denied. It
// exists so one verify call reports both the outcome and its blame: a
// separate "why did the last one fail?" query would race concurrent
// acquires. internal/termlease re-exports it as termlease.StandingDenial.
//
// The classification changes NO authorization outcome (every non-None value
// denies exactly as hard); it decides only whether the DEVICE should destroy
// its saved standing secret, which is irreversible for the user (the operator
// must mint and convey a new one).
type StandingDenial uint8

const (
	// StandingDenialNone is the zero value: no denial. Meaningful only when the
	// verify reported ok; it must never be used to describe a refusal.
	StandingDenialNone StandingDenial = iota
	// StandingDenialUnavailable: the verifier refused WITHOUT ever judging the
	// presented secret, in a state that can flip back on its own: standing
	// access temporarily switched off (secret still at rest), or rate limiting.
	// The device MUST keep its secret. This is the fail-safe default for any
	// condition a verifier cannot positively classify.
	StandingDenialUnavailable
	// StandingDenialBadSecret: the verifier JUDGED the presented value and
	// rejected it (wrong/rotated secret, or an undecodable one). The device may
	// discard it.
	StandingDenialBadSecret
	// StandingDenialRevoked: no standing secret exists at rest at all: it was
	// revoked and never re-provisioned. No secret any device holds can be
	// accepted again (a fresh mint issues a different one), so the device may
	// discard it. Reserved for a state the server can PROVE, never inferred
	// from a merely-disabled gate.
	StandingDenialRevoked
)
