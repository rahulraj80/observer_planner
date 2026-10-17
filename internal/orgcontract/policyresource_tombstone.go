package orgcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// Signed policy-resource WITHDRAWAL (D-016 follow-up to Sol A5-01).
//
// An unsigned 404 never clears a node's installed org layer: a proxy error or
// a route mismatch must not fail open a guardrail. That leaves one honest way
// for the org to take a layer back from a node its targeting no longer
// addresses: a SIGNED tombstone. It is an ordinary SignedPolicyResource,
// signed by the same key over the same PolicyResourceSigningMessage, with a
// reserved CompilerVersion and a closed body bound to ONE subject:
//
//	{"withdrawn":true,"subject":"<hex SHA-256 of domain || 0x00 || user id>"}
//
// The subject binding is what makes the tombstone safe to accept: one
// node's tombstone replayed to another node fails that node's subject check.
// The reserved compiler is what keeps it safe for OLDER agents: they demand
// compiler "v1" and reject this envelope as a closed-envelope violation, so
// they keep their layer exactly as they did before.

// PolicyResourceTombstoneCompiler is the reserved CompilerVersion of a signed
// withdrawal. No family compiler ever produces it.
const PolicyResourceTombstoneCompiler = "tombstone.v1"

// policyResourceTombstoneSubjectDomain separates the subject digest from
// every other SHA-256 in the protocol.
const policyResourceTombstoneSubjectDomain = "sbo-policy-resource-tombstone-subject-v1"

// policyResourceTombstoneBody is the closed tombstone body. Field order is
// the canonical encoding (encoding/json writes struct fields in order).
type policyResourceTombstoneBody struct {
	Withdrawn bool   `json:"withdrawn"`
	Subject   string `json:"subject"`
}

// PolicyResourceTombstoneSubject is the digest a tombstone binds its subject
// (the enrolment's user id, the bearer subject on the server) by. The raw id
// never rides the body.
func PolicyResourceTombstoneSubject(subject string) string {
	sum := sha256.Sum256([]byte(policyResourceTombstoneSubjectDomain + "\x00" + subject))
	return hex.EncodeToString(sum[:])
}

// PolicyResourceTombstoneBody is the canonical tombstone body for subject.
func PolicyResourceTombstoneBody(subject string) string {
	b, err := json.Marshal(policyResourceTombstoneBody{Withdrawn: true, Subject: PolicyResourceTombstoneSubject(subject)})
	if err != nil {
		// A two-field struct of a bool and a hex string cannot fail to
		// encode; an empty body then fails every check below.
		return ""
	}
	return string(b)
}

// NewPolicyResourceTombstone returns the UNSIGNED tombstone withdrawing
// family from subject at version: match-all selectors, no capabilities, the
// canonical body and its hash. The caller signs it with SignPolicyResource
// and sets PublicKey / Signature / SignedAt / Description.
func NewPolicyResourceTombstone(id, family, subject string, version int64) SignedPolicyResource {
	body := PolicyResourceTombstoneBody(subject)
	sum := sha256.Sum256([]byte(body))
	return SignedPolicyResource{
		ID:              id,
		Version:         version,
		Family:          family,
		CompilerVersion: PolicyResourceTombstoneCompiler,
		Body:            body,
		BodyHash:        hex.EncodeToString(sum[:]),
		SelectorsJSON:   "{}",
	}
}

// IsPolicyResourceTombstone reports whether r claims to be a withdrawal. A
// claim is not a verdict: CheckPolicyResourceTombstone (plus the signature
// and key-pin gates) decides whether to honour it.
func IsPolicyResourceTombstone(r SignedPolicyResource) bool {
	return r.CompilerVersion == PolicyResourceTombstoneCompiler
}

// ErrPolicyResourceTombstoneInvalid wraps every closed-envelope failure of a
// tombstone.
var ErrPolicyResourceTombstoneInvalid = errors.New("orgcontract: invalid policy-resource tombstone")

// CheckPolicyResourceTombstone validates the closed tombstone envelope for
// one node: the expected id and family, a positive version, match-all
// selectors, no capabilities, and the canonical body bound to THIS subject.
// It does not verify the signature (VerifyPolicyResource) or decide whether
// the signing key is trusted (the key pin); the caller runs both first.
func CheckPolicyResourceTombstone(r SignedPolicyResource, id, family, subject string) error {
	switch {
	case !IsPolicyResourceTombstone(r):
		return fmt.Errorf("%w: compiler %q is not %q", ErrPolicyResourceTombstoneInvalid, r.CompilerVersion, PolicyResourceTombstoneCompiler)
	case r.ID != id || r.Family != family || r.Version <= 0:
		return fmt.Errorf("%w: id=%q family=%q version=%d (want id=%q family=%q version>0)",
			ErrPolicyResourceTombstoneInvalid, r.ID, r.Family, r.Version, id, family)
	case r.SelectorsJSON != "{}" || len(r.RequiredCapabilities) != 0:
		return fmt.Errorf("%w: a withdrawal carries no selectors or capabilities", ErrPolicyResourceTombstoneInvalid)
	case subject == "":
		return fmt.Errorf("%w: this node has no enrolment subject to match", ErrPolicyResourceTombstoneInvalid)
	case r.Body != PolicyResourceTombstoneBody(subject):
		return fmt.Errorf("%w: the withdrawal is addressed to a different subject", ErrPolicyResourceTombstoneInvalid)
	}
	return nil
}
