package orgcontract

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// storedgrant.go — RUNTIME re-verification of the grant document a node has
// on disk (Track C item 1,
// docs/plans/org-guardrail-control-wave-2026-09-21.md).
//
// Until this file, a grant's signature was verified exactly ONCE, at the
// moment the org offered it (orgclient.evaluateGrantOffer), and thereafter
// treated as evidence rather than as a check: internal/govern.Resolve never
// looked at Grant.Signature again. That left one cheap evasion open — edit
// the stored row (authority_json is a plain JSON array in a node-local
// SQLite file) and the resolver would honour the rewritten authority set for
// the life of the grant, because every OTHER gate it applies (org id, server
// URL, key pin, generation, TTL) is a field of that same rewritten row.
//
// Re-verification closes it: the stored fields are re-canonicalized into the
// document the organization actually signed and checked against the org
// distribution key the node recorded at enrolment. A rewritten authority
// list no longer verifies, and the resolver reports a loud state instead of
// obeying it.
//
// HONESTY ABOUT WHAT THIS BUYS (the same posture as grant.go's own note).
// This is DETECTION on a machine the developer owns, not prevention. Someone
// who can edit the grant row can also delete the key-material row, which
// yields "unchecked" rather than "invalid" — loud in `observer org status`
// and `observer doctor`, and visible to the org as a governance lapse (the
// fleethealth `governance_lapsed` finding), but not a lock. The lock is
// OS-level ownership / MDM; see docs/teams-operations.md.

// StoredGrantDocument is the node-local shape of a stored grant, flattened
// to exactly the fields the signature covers.
//
// It exists so the node's store row (internal/store.EnrolmentGrant) never
// has to be shaped like a wire type, and so the two document shapes a stored
// grant can have — the enrolment grant and an out-of-band grant REPLACEMENT,
// which is signed over a different domain with the replacement generation
// bound in — are chosen by a DATA property of the row
// (ReplacementGeneration > 0), never by a caller-side branch.
//
// The JSON tags exist for one reader: a KEPT initial set's proof (see
// InitialProof) is stored as this document inside the node's
// initial_components_json column (EncodeStoredInitialComponents).
type StoredGrantDocument struct {
	OrgID        string   `json:"org_id"`
	OrgServerURL string   `json:"org_server_url"`
	KeyPinSHA256 string   `json:"key_pin_sha256"`
	Authority    []string `json:"authority"`
	// GrantedAt / ExpiresAt are RFC3339 as the signer wrote them. ExpiresAt
	// MUST be the SIGNED expiry (store.EnrolmentGrant.SignedExpiresAt), not
	// the working clock: RenewEnrolmentGrant moves the working expiry
	// forward inside the signed window and deliberately leaves the signature
	// and the signed window untouched, precisely so the row keeps verifying.
	GrantedAt string `json:"granted_at"`
	ExpiresAt string `json:"expires_at"`

	ConsentMode  string `json:"consent_mode,omitempty"`
	ConsentActor string `json:"consent_actor,omitempty"`

	// InitialComponents is the grant's signed initial component set, nil
	// when the grant carried none. A node that stores a grant carrying it
	// must store it too, or the stored row stops verifying.
	InitialComponents *InitialComponentSet `json:"initial_components,omitempty"`

	// Signature is the base64url Ed25519 signature stored with the row.
	Signature string `json:"signature"`

	// ReplacementGeneration is 0 for an enrolment grant and > 0 for a grant
	// this node accepted through the out-of-band replacement rail. It both
	// SELECTS the document shape and is BOUND INTO the replacement's signing
	// message, so it cannot be flipped to dodge the check: flipping it makes
	// the other document shape's message, which does not verify either.
	ReplacementGeneration int64 `json:"replacement_generation,omitempty"`

	// InitialProof is set only on a replacement row whose initial set was
	// KEPT from an earlier grant (a set-less replacement, from an older
	// server or a fleet document that did not carry components.govern). It
	// is the earlier document, exactly as that grant was stored, whose own
	// signature covers the set. The row's signature then verifies over the
	// set-less replacement message and the set verifies under the proof;
	// neither half alone is accepted (ledger MODPKG-17 (1), REVIEW-4 P4-10).
	// It is never signed itself and never nested.
	InitialProof *StoredGrantDocument `json:"-"`
}

// ErrStoredGrantUncheckable reports that no verification could be performed
// because the node holds no org distribution key material. It is NOT a
// tamper signal: a pre-2026-09-13 enrolment, or an org server that delivered
// no policy key, legitimately produces it. Callers map it to "unchecked" and
// keep today's behaviour.
var ErrStoredGrantUncheckable = errors.New("orgcontract: no org distribution key material to re-verify the stored grant against")

// VerifyStoredGrant re-verifies a stored grant document under pub.
//
// It returns nil when the document verifies, ErrStoredGrantUncheckable when
// pub is absent/malformed (the honest "we could not check" answer), and a
// named error when the document did NOT verify — which is the tamper signal.
func VerifyStoredGrant(d StoredGrantDocument, pub ed25519.PublicKey) error {
	if len(pub) != ed25519.PublicKeySize {
		return ErrStoredGrantUncheckable
	}
	if d.InitialProof != nil {
		return verifyKeptInitialSet(d, pub)
	}
	// No set-less fallback (MODPKG-17 (1), REVIEW-4 P4-10): a row that holds
	// a set either carries the signature that covers it, or carries the
	// proof of the earlier grant that signed it. Accepting a set-less
	// message here let anyone holding the replacement's primary signature
	// (it rides the node's own push response) store any set beside it.
	return verifyStoredSignature(d, pub)
}

// errStoredGrantDoesNotVerify is the tamper verdict.
var errStoredGrantDoesNotVerify = errors.New("orgcontract.VerifyStoredGrant: the stored grant does not verify under this organization's signing key")

// verifyStoredSignature checks d's own signature over its own message.
func verifyStoredSignature(d StoredGrantDocument, pub ed25519.PublicKey) error {
	if d.Signature == "" {
		return errors.New("orgcontract.VerifyStoredGrant: the stored grant carries no signature")
	}
	sig, err := base64.RawURLEncoding.DecodeString(d.Signature)
	if err != nil {
		return fmt.Errorf("orgcontract.VerifyStoredGrant: decode signature: %w", err)
	}
	if !ed25519.Verify(pub, StoredGrantSigningMessage(d), sig) {
		return errStoredGrantDoesNotVerify
	}
	return nil
}

// verifyKeptInitialSet verifies a replacement row whose initial set was kept
// from an earlier grant: the row's own signature over the SET-LESS
// replacement message (what that replacement signed), and the set under the
// earlier grant's own signature (the proof). Every binding a swap could
// exploit is checked: the proof names the same org, server and key pin, is
// strictly older than the row, carries exactly the row's set, and is not
// itself a kept set.
//
// The proof's consent fields are a local record of that earlier row, which
// is not always what the organization signed (the candidate table in
// internal/grantverify), so its verification tries the same three readings.
// That is safe here because the proof vouches for the SET only; the row's
// consent and authority are checked by the row's own signature above.
func verifyKeptInitialSet(d StoredGrantDocument, pub ed25519.PublicKey) error {
	p := *d.InitialProof
	switch {
	case d.ReplacementGeneration <= 0:
		return errors.New("orgcontract.VerifyStoredGrant: a kept initial set is only valid on a replacement row")
	case d.InitialComponents == nil || p.InitialComponents == nil:
		return errors.New("orgcontract.VerifyStoredGrant: a kept initial set proof without a set")
	case p.InitialProof != nil:
		return errors.New("orgcontract.VerifyStoredGrant: a kept initial set proof must not itself be kept")
	case p.OrgID != d.OrgID || p.OrgServerURL != d.OrgServerURL || p.KeyPinSHA256 != d.KeyPinSHA256:
		return errors.New("orgcontract.VerifyStoredGrant: the kept initial set proof names another organization")
	case p.ReplacementGeneration >= d.ReplacementGeneration:
		return errors.New("orgcontract.VerifyStoredGrant: the kept initial set proof is not older than the row")
	case !SameInitialComponentSet(p.InitialComponents, d.InitialComponents):
		return errStoredGrantDoesNotVerify
	}
	row := d
	row.InitialComponents, row.InitialProof = nil, nil
	if err := verifyStoredSignature(row, pub); err != nil {
		return err
	}
	readings := [][2]string{{p.ConsentMode, p.ConsentActor}, {p.ConsentMode, ""}, {"", ""}}
	var last error
	for _, r := range readings {
		try := p
		try.ConsentMode, try.ConsentActor = r[0], r[1]
		if last = verifyStoredSignature(try, pub); last == nil {
			return nil
		}
	}
	return last
}

// SameInitialComponentSet reports whether two sets are the same signed set:
// the same catalog version and the same canonical ids (the form the signing
// message binds).
func SameInitialComponentSet(a, b *InitialComponentSet) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.CatalogVersion != b.CatalogVersion {
		return false
	}
	x, y := CanonicalAuthority(a.IDs), CanonicalAuthority(b.IDs)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// StoredInitialComponents is the node-local value of the
// initial_components_json column: the set, plus, for a kept set, the earlier
// grant document that signed it (StoredGrantDocument.InitialProof). The
// proof rides in the same column so a set and its evidence can never be
// written apart, and an older reader that decodes only the set ignores it.
type StoredInitialComponents struct {
	CatalogVersion int                  `json:"catalog_version"`
	IDs            []string             `json:"ids"`
	SignedBy       *StoredGrantDocument `json:"signed_by,omitempty"`
}

// EncodeStoredInitialComponents renders the column value; ok is false for a
// nil set (the column is NULL).
func EncodeStoredInitialComponents(set *InitialComponentSet, proof *StoredGrantDocument) (string, bool, error) {
	if set == nil {
		return "", false, nil
	}
	ids := set.IDs
	if ids == nil {
		ids = []string{}
	}
	raw, err := json.Marshal(StoredInitialComponents{CatalogVersion: set.CatalogVersion, IDs: ids, SignedBy: proof})
	if err != nil {
		return "", false, fmt.Errorf("orgcontract.EncodeStoredInitialComponents: %w", err)
	}
	return string(raw), true, nil
}

// DecodeStoredInitialComponents reads the column value. A corrupt value
// decodes to an EMPTY set (core only) with no proof: the fail-safe direction
// for a set the organization signed is less, never the whole compiled set,
// and a replacement row then fails re-verification loudly.
func DecodeStoredInitialComponents(raw string) (*InitialComponentSet, *StoredGrantDocument) {
	var s StoredInitialComponents
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return &InitialComponentSet{IDs: []string{}}, nil
	}
	if s.IDs == nil {
		s.IDs = []string{}
	}
	return &InitialComponentSet{CatalogVersion: s.CatalogVersion, IDs: s.IDs}, s.SignedBy
}

// StoredGrantSigningMessage rebuilds the canonical signing message for a
// stored document by re-projecting it onto whichever wire type actually
// signed it. There is deliberately no third algorithm here: both branches
// call the SAME functions the signer and the enrol-time verifier call, so a
// future change to either signing message can never silently desynchronize
// the runtime check.
func StoredGrantSigningMessage(d StoredGrantDocument) []byte {
	if d.ReplacementGeneration > 0 {
		r := GrantReplacement{
			OrgID:        d.OrgID,
			OrgServerURL: d.OrgServerURL,
			KeyPinSHA256: d.KeyPinSHA256,
			Generation:   d.ReplacementGeneration,
			Authority:    d.Authority,
			GrantedAt:    d.GrantedAt,
			ExpiresAt:    d.ExpiresAt,
			ConsentMode:  d.ConsentMode,
			ConsentActor: d.ConsentActor,
		}
		// A replacement accepted WITH an initial set is stored with its
		// InitialSignature (orgclient.AcceptGrantReplacement), which covers
		// the set; one without is stored with the primary signature, and a
		// set it kept is verified under its InitialProof instead
		// (verifyKeptInitialSet).
		if d.InitialComponents != nil {
			r.InitialComponents = d.InitialComponents
			return GrantReplacementInitialSigningMessage(r)
		}
		return GrantReplacementSigningMessage(r)
	}
	return EnrolmentGrantSigningMessage(EnrolmentGrant{
		OrgID:             d.OrgID,
		OrgServerURL:      d.OrgServerURL,
		KeyPinSHA256:      d.KeyPinSHA256,
		Authority:         d.Authority,
		GrantedAt:         d.GrantedAt,
		ExpiresAt:         d.ExpiresAt,
		ConsentMode:       d.ConsentMode,
		ConsentActor:      d.ConsentActor,
		InitialComponents: d.InitialComponents,
	})
}
