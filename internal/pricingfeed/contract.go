package pricingfeed

import (
	"errors"

	"github.com/marmutapp/superbased-observer/internal/pricingfeedwire"
)

// SupportedSchemaVersion is the only feed schema_version this build understands.
// A feed carrying any other value is refused ([ErrUnsupportedSchema]) rather
// than half-parsed: the envelope is all-or-nothing (§E). A later optional field
// (e.g. the D4 min-cacheable column) would bump this.
const SupportedSchemaVersion = 1

// FeedSigningDomain domain-separates the pricing-FEED signature from every
// other Ed25519 use in the protocol — in particular from the org PRICING
// POLICY rail's sbo-pricing-policy-v1 (orgcontract), whose body has the same
// row vocabulary. A signature minted on one rail must never verify on another
// (docs/security.md ROUTING-SIG-1); the plan names this tag in §A.
const FeedSigningDomain = "sbo-pricing-feed-v1"

// The feed's wire types live in internal/pricingfeedwire, a neutral core
// package, so the store's persisted copy, the context-window ladder and the
// cost engine wiring hold no egress.pricing-feed package (lane MP-L1H,
// T-715). These aliases keep every existing caller compiling unchanged.
type (
	// Row is pricingfeedwire.Row.
	Row = pricingfeedwire.Row
	// Economics is pricingfeedwire.Economics.
	Economics = pricingfeedwire.Economics
	// Envelope is pricingfeedwire.Envelope.
	Envelope = pricingfeedwire.Envelope
)

// Cache mode vocabulary (Economics.CacheMode).
const (
	CacheModeExplicit = "explicit"
	CacheModeImplicit = "implicit"
	CacheModeNone     = "none"
)

// Cache-write billing vocabulary (Economics.CacheWriteBilling).
const (
	CacheWriteBillingPerWrite = "per_write"
	CacheWriteBillingIncluded = "included"
	CacheWriteBillingNone     = "none"
)

// Reasoning billing vocabulary (Economics.ReasoningBilling).
const (
	ReasoningBillingOutputRate   = "output_rate"
	ReasoningBillingSeparateRate = "separate_rate"
	ReasoningBillingIncluded     = "included"
	ReasoningBillingNone         = "none"
)

// Typed verification errors. Verify is all-or-nothing: it returns the FIRST of
// these that applies and never partially applies a body (§E). Callers map every
// one of them to "refuse the body, keep the previously applied prices".
var (
	// ErrUnsigned — the envelope carries no signature. A build that produced
	// it lacked the signing secret (§B.4); consumers fail closed.
	ErrUnsigned = errors.New("pricingfeed: envelope is unsigned")
	// ErrBadSignature — a signature is present but does not verify under the
	// named key.
	ErrBadSignature = errors.New("pricingfeed: signature does not verify")
	// ErrDigestMismatch — the envelope's Digest does not equal the recomputed
	// digest of its rows (tampering, or a publisher bug).
	ErrDigestMismatch = errors.New("pricingfeed: digest does not match rows")
	// ErrUnsupportedSchema — SchemaVersion is not SupportedSchemaVersion.
	ErrUnsupportedSchema = errors.New("pricingfeed: unsupported schema_version")
	// ErrUnknownKey — KeyID names no key this build accepts.
	ErrUnknownKey = errors.New("pricingfeed: envelope signed by an unknown key_id")
	// ErrInvalidRows — a row is malformed (e.g. an empty model id) or
	// FeedVersion is not positive. The whole set is rejected.
	ErrInvalidRows = errors.New("pricingfeed: envelope rows are invalid")
)
