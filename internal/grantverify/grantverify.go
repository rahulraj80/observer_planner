package grantverify

import (
	"crypto/ed25519"
	"errors"

	"github.com/marmutapp/superbased-observer/internal/govern"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// ConsentModeForTenancy maps the enrolment tenancy onto the govern consent
// mode recorded on the grant. Managed tenancy is the signal the resolver
// branches on to honour the managed-only authorities; individual enrolments
// stay interactive so those authorities are inert.
//
// This is the LOCAL answer: what the node concludes when the organization
// said nothing about how consent was obtained (the token rail). It is also
// the only consent mode the unsigned candidate below admits.
func ConsentModeForTenancy(tenancy string) string {
	if tenancy == orgcontract.TenancyManaged {
		return govern.ConsentManaged
	}
	return govern.ConsentInteractive
}

// candidate is one row of the signed-consent table: the consent fields to
// verify against, and what a verification with them proves.
type candidate struct {
	mode, actor string
	evidence    govern.ConsentEvidence
}

// candidates is the ordered table of consent evidence a stored grant's
// signature may have covered. The stored row records the consent the NODE
// resolved, which is not always what the organization SIGNED:
//
//   - the IdP rail (and a grant replacement) signs mode + actor, stored
//     verbatim;
//   - a server that declares managed consent with no named actor (the
//     enterprise-posture token mint) signs mode with an EMPTY actor, while the
//     node records its local username as the actor;
//   - the plain token rail signs NO consent at all (the pre-P6c message), and
//     the node derives the mode from the enrolment's tenancy.
//
// The unsigned candidate is admitted only when the stored mode is exactly
// what the enrolment's tenancy implies, so flipping an individual grant's
// mode to managed in the database is still detected. The exact candidate is
// tried first, so the verifying row names what the signature covers: a
// fallback means the stored actor (and, for the unsigned row, the mode) is a
// local annotation a database edit could rewrite (Sol A3-03).
func candidates(mode, actor, tenancy string) []candidate {
	out := []candidate{{mode, actor, govern.ConsentEvidenceModeAndActor}}
	if mode != "" && actor != "" {
		out = append(out, candidate{mode, "", govern.ConsentEvidenceMode})
	}
	if mode == ConsentModeForTenancy(tenancy) {
		out = append(out, candidate{"", "", govern.ConsentEvidenceNone})
	}
	return out
}

// Verify re-verifies a stored grant document against the org distribution
// public key recorded at enrolment. doc carries the consent the node
// RECORDED; tenancy is the enrolment's tenancy. It returns Valid with the
// consent evidence of the candidate that verified, Unchecked when there is no
// usable key material (never Invalid: a node that legitimately holds no key
// must not be reported as tampered with), and Invalid otherwise.
func Verify(doc orgcontract.StoredGrantDocument, tenancy string, pub ed25519.PublicKey) (govern.GrantIntegrity, govern.ConsentEvidence) {
	for _, c := range candidates(doc.ConsentMode, doc.ConsentActor, tenancy) {
		try := doc
		try.ConsentMode, try.ConsentActor = c.mode, c.actor
		err := orgcontract.VerifyStoredGrant(try, pub)
		switch {
		case err == nil:
			return govern.GrantIntegrityValid, c.evidence
		case errors.Is(err, orgcontract.ErrStoredGrantUncheckable):
			return govern.GrantIntegrityUnchecked, govern.ConsentEvidenceUnverified
		}
	}
	return govern.GrantIntegrityInvalid, govern.ConsentEvidenceUnverified
}
