package pricingfeedwire

import (
	"encoding/json"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// Row is ONE model's feed entry: the platform's canonical rate vocabulary
// (orgcontract.PricingPolicyRow, embedded VERBATIM so no consumer translates
// rates) plus the Tokenomics quality Grade.
//
// The embedded row's rate fields are POINTERS, preserving the nil-vs-quoted-
// zero distinction node migration 135 needs end to end: a nil rate is "not
// quoted" (the node falls through to its seed) and a &0.0 is "quoted FREE"
// (state='known_free' in Tokenomics). The embedding flattens in JSON, so a Row
// marshals as { ...the policy-row fields... , "grade": "..." } - exactly the
// §A envelope shape.
//
// The 2026-09-30 pricing-chain contract's ADDITIVE fields - reasoning_per_mtok,
// request_fee_usd, cache_write_other_per_mtok, image_input_per_mtok,
// image_output_per_image, audio_input_per_mtok, audio_output_per_mtok and the
// top-level fast_multiplier - live on the embedded policy row, so they reach
// every history period too, with the same presence rule (absent = not quoted,
// a present 0 = quoted free). A present long_context_threshold of 0 is an
// explicit FLAT row (no long-context tier); an absent one is an old feed that
// did not know the tier. Every consumer rounds the decoded floats to
// orgcontract.PricePrecision (Tokenomics' NUMERIC(20,10)) at its projection
// step - after Verify, never inside the signed bytes.
type Row struct {
	orgcontract.PricingPolicyRow
	// Grade is the Tokenomics source grade (e.g. "verified", "observed") the
	// export carried for this row. It is OUT OF BAND of the rate vocabulary:
	// the org importer reads it to decide auto-apply vs park (§C.1), and it
	// never reaches the node engine (which speaks only orgcontract rates).
	Grade string `json:"grade,omitempty"`

	// Economics is OPTIONAL per-model economics beyond the bare rates
	// (operator directive 2026-09-11): cache systems differ (Anthropic's
	// EXPLICIT cache writes vs OpenAI/Gemini-style IMPLICIT caching) and
	// thinking-vs-non-thinking billing differs, and a price alone cannot say
	// which regime a model is in. It is a POINTER so its absence is honestly
	// "unknown" rather than a struct full of fabricated zero-value defaults;
	// every field inside it is likewise optional. It stays inside this same
	// canonical body (no map, fixed field order) so the digest and signature
	// cover it, and it is additive under SchemaVersion 1.
	Economics *Economics `json:"economics,omitempty"`

	// History is the model's COMPLETE effective-dated price timeline
	// (Tokenomics migration 0028, lane R2-PRICING-2): every period ascending,
	// the one in force included, each a whole Row (its own rates,
	// effective_from, peak, grade and - in Economics - fast multiplier) with no
	// History of its own. A PERIOD IS A COMPLETE STATEMENT: a nil rate means
	// the dimension did not exist in that period, never "same as today".
	//
	// The top-level fields above still carry the period in force when the feed
	// was built, so a consumer that ignores History prices exactly as before;
	// a consumer that reads it can price an OLD session at the rate that was
	// in force at the session's own timestamp (the node cost engine, the org
	// importer, the snapshot generator).
	//
	// ADDITIVE UNDER SchemaVersion 1. nil is omitted, so a row with no history
	// is byte-identical to a pre-history row; Verify digests the RAW received
	// row bytes, so a build that predates this field verifies a history-
	// bearing body and simply ignores the key. (A build older than the
	// verify-over-received-bytes fix re-marshals and refuses it - the same
	// named cutover residual as the peak field; see
	// docs/plans/peak-off-peak-pricing-plan-2026-09-20.md §3.4.)
	//
	// It SHADOWS the embedded orgcontract.PricingPolicyRow.History (the org
	// rail's own history, of plain policy rows): Go and encoding/json both
	// resolve the shallower field, so on this rail "history" decodes into
	// feed Rows, which is what carries a per-period fast multiplier.
	History []Row `json:"history,omitempty"`
}

// Economics carries per-model economic shape the bare rate vocabulary cannot
// express. EVERY field is optional: a nil pointer or an empty string means
// UNKNOWN, never a default - a consumer that reads "unknown" keeps whatever it
// already believed (its code-level registry), and a consumer must never invent
// a value the feed did not state. The enum-valued strings have CLOSED
// vocabularies validated by Verify when Economics is present; an unrecognised
// value fails the whole envelope (all-or-nothing, §E), so a typo in the
// publisher can never be half-applied.
type Economics struct {
	// CacheMode is the caching regime: "explicit" (Anthropic - the caller
	// marks cache-write points and pays a write rate), "implicit" (OpenAI/
	// Gemini - the provider caches transparently, no explicit write), or
	// "none". Empty = unknown.
	CacheMode string `json:"cache_mode,omitempty"`
	// CacheWriteBilling is how a cache WRITE is billed: "per_write" (a distinct
	// write rate applies), "included" (writes are folded into the input rate),
	// or "none". Empty = unknown.
	CacheWriteBilling string `json:"cache_write_billing,omitempty"`
	// CacheTTLSeconds is the default cache entry lifetime in seconds.
	CacheTTLSeconds *int64 `json:"cache_ttl_seconds,omitempty"`
	// CacheTTL1HAvailable reports whether a 1-hour cache tier is offered.
	CacheTTL1HAvailable *bool `json:"cache_ttl_1h_available,omitempty"`
	// MinCacheableTokens is the smallest prefix length that can be cached (the
	// cachetrack min-cacheable fact, carried here as DATA rather than code when
	// the publisher knows it).
	MinCacheableTokens *int64 `json:"min_cacheable_tokens,omitempty"`
	// ReasoningSupported reports whether the model has a thinking/reasoning
	// mode at all.
	ReasoningSupported *bool `json:"reasoning_supported,omitempty"`
	// ReasoningBilling is how reasoning/thinking tokens are billed:
	// "output_rate" (at the output rate, the Anthropic convention),
	// "separate_rate" (a distinct reasoning rate - then ReasoningPerMTok),
	// "included", or "none". Empty = unknown.
	ReasoningBilling string `json:"reasoning_billing,omitempty"`
	// ReasoningPerMTok is the distinct reasoning rate in USD per 1M tokens,
	// meaningful when ReasoningBilling is "separate_rate".
	ReasoningPerMTok *float64 `json:"reasoning_per_mtok,omitempty"`
	// ContextWindowTokens is the model's maximum context window.
	ContextWindowTokens *int64 `json:"context_window_tokens,omitempty"`
	// FastMultiplier is the latency-premium multiplier (cost.Pricing's
	// seed-only FastMultiplier, carried here as DATA when known). It is a
	// premium, not a negotiated rate, which is why it rides in Economics rather
	// than in the embedded rate row.
	FastMultiplier *float64 `json:"fast_multiplier,omitempty"`
	// Notes is free-form provenance, SafeText-constrained: Verify rejects any
	// control character, bidi-formatting code point or ANSI escape, so a note
	// can never smuggle a terminal-control sequence onto an operator surface.
	Notes string `json:"notes,omitempty"`
}

// Envelope is the signed feed document served at the publish endpoint (§A) and
// verified offline by every consumer.
type Envelope struct {
	// SchemaVersion is the feed wire version; only SupportedSchemaVersion is
	// accepted by this build.
	SchemaVersion int `json:"schema_version"`
	// FeedVersion is the publisher's monotonic content version, bumped on
	// every content change. It is bound INTO the signature (alongside the
	// canonical rows) so a replayed lower version can be refused; a consumer
	// also checks FeedVersion > 0.
	FeedVersion int64 `json:"feed_version"`
	// GeneratedAt is the RFC3339 instant the publisher materialized the
	// export. Provenance only - it is NOT in the signing message, so a
	// re-publish of identical rows at a new clock does not invalidate a cached
	// body (the Digest is the change detector).
	GeneratedAt string `json:"generated_at"`
	// Rows are the feed rows, one per model. CanonicalRows sorts them by model
	// before signing/digesting so the bytes are stable regardless of the order
	// the publisher emitted them.
	Rows []Row `json:"rows"`
	// Digest is the sha256 hex of CanonicalRows(Rows). It is the ETag
	// substrate (a corrected-in-place rate without a version bump still
	// re-applies) and is re-checked on verify.
	Digest string `json:"digest"`
	// Signature is base64 (std) of the raw 64-byte Ed25519 signature over
	// [SigningMessage]. An empty Signature is [ErrUnsigned] - never applied.
	Signature string `json:"signature"`
	// KeyID names which compiled vendor key signed this body, so a rotation
	// can ship an overlap window (the vendorkey.go slot pattern). Verify looks
	// the key up by this id and refuses an unknown one ([ErrUnknownKey]).
	KeyID string `json:"key_id"`

	// rawRows is the exact, unparsed bytes of the top-level "rows" JSON array
	// as received over the wire, captured by UnmarshalJSON. It is unexported
	// (never marshalled - a re-encoded Envelope round-trips through the typed
	// Rows field exactly as before) and exists so [Verify] can digest/sign
	// over what was ACTUALLY RECEIVED rather than a re-marshal of the decoded
	// typed rows, which silently drops any field this build's Row/
	// PricingPolicyRow/Economics structs don't know about (docs/plans/
	// peak-off-peak-pricing-plan-2026-09-20.md §3.4/§R M1+M2 - the
	// fleet-no-freeze fix). nil when the envelope was built programmatically
	// (e.g. by a signer or a test) rather than decoded from JSON; Verify falls
	// back to re-marshalling Rows in that case.
	rawRows []byte
}

// envelopeAlias is Envelope's field set without its methods, used by
// UnmarshalJSON to decode the ordinary fields without recursing back into
// UnmarshalJSON itself.
type envelopeAlias Envelope

// UnmarshalJSON decodes b into e exactly as the default struct decoding would
// (every exported field, unknown JSON keys ignored, same as before this
// method existed), and additionally captures the raw bytes of the top-level
// "rows" array into the unexported rawRows field for [Verify] to
// digest/sign over. This is the ONLY behavioural change: json.Marshal(e)
// still produces exactly the same bytes as before (rawRows is unexported so
// it never marshals), so every existing consumer, and the signer's golden
// tests, are unaffected.
func (e *Envelope) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, (*envelopeAlias)(e)); err != nil {
		return err
	}
	var rowsHolder struct {
		Rows json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(b, &rowsHolder); err != nil {
		return err
	}
	e.rawRows = rowsHolder.Rows
	return nil
}

// RawRows returns the exact received bytes of the top-level "rows" array
// captured by UnmarshalJSON, or nil when the envelope was built in code. It is
// the verifier's read of the field (internal/pricingfeed.Verify digests and
// signs over these bytes); callers must not modify the returned slice.
func (e Envelope) RawRows() []byte { return e.rawRows }
