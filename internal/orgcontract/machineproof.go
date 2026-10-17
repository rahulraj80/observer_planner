package orgcontract

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Machine-bound credentials (T-656, ledger GRANT-15, docs/access-grants.md
// "Machine-scoped grants: what binds the machine").
//
// [HeaderMachineIdentity] is a CLAIM: the bearer and the per-push key name a
// member, not a machine, so any machine of a member could name another of that
// member's machines and receive its machine-scoped grants. A machine-bound
// credential closes that: each machine holds its own Ed25519 key (generated
// on the node, kept beside the enrolment secret, never in a database),
// registers the public half once (POST [MachineKeyPath]), and proves
// possession on every request that names the machine with a per-request
// signature ([HeaderMachineProofTimestamp] + [HeaderMachineProofSignature])
// over [MachineProofMessage].
//
// Compatibility, both directions:
//
//   - A released node sends no proof and never registers. A machine with no
//     registered key keeps the earlier behaviour (the T-389 attribution
//     check), unless the org turns on require_machine_proof (default off).
//   - A released server answers 404 on [MachineKeyPath] (the node then stops
//     asking for the process lifetime) and ignores the proof headers.
//   - Once a machine HAS a registered key, a claim for it without a valid
//     proof selects no machine-scoped grant (the sticky direction, like the
//     v2 policy binding): a grant is a weakening, so the closed answer is
//     "no grant", never "no document".

// MachineKeyPath is the node's registration route for its machine key.
const MachineKeyPath = "/api/agent/machine-key"

// AgentBudgetPath is the budget rail's route, the path a machine proof on it
// signs (the query never is).
const AgentBudgetPath = "/api/agent/budget"

// AgentLeavePath is the structured unenrol's leave notice (T-718). Since
// T-747 a managed node signs it with its machine key (the same proof headers
// as the grant and budget rails, path = this constant), and the server
// retires that member's key for the machine when the proof verifies, so a
// later re-enrolment of the machine registers a fresh key instead of a 409.
// A released node sends no proof and a released server ignores it.
const AgentLeavePath = "/api/agent/leave"

// MachineProofCapability is the token naming this protocol (machine key
// registration plus per-request machine proofs). The node sends it in
// [HeaderMachineProofCaps] on every proved request; it is how a log or a
// future server tells a proof-capable node from a released one.
const MachineProofCapability = "machine-proof-v1"

// Machine proof headers. Released servers ignore all three.
const (
	HeaderMachineProofCaps      = "X-SBO-Machine-Caps"
	HeaderMachineProofTimestamp = "X-SBO-Machine-Timestamp"
	HeaderMachineProofSignature = "X-SBO-Machine-Signature"
)

// MachineProofWindow is how far a proof's timestamp may sit from the server's
// clock in either direction.
const MachineProofWindow = 5 * time.Minute

const (
	machineProofDomain    = "sbo-machine-proof-v1"
	machineRegisterDomain = "sbo-machine-key-register-v1"
	machineRotateDomain   = "sbo-machine-key-rotate-v1"
)

// MachineKeyRegistration is the POST [MachineKeyPath] body. PublicKey is the
// base64 (std) Ed25519 public key; Signature is that key's signature over
// [MachineKeyRegistrationMessage] (proof of possession, bound to the org, the
// bearer's member, the machine, the key and the timestamp). RotationSignature
// is set only when replacing a registered key: the OLD key's signature over
// [MachineKeyRotationMessage], which is what lets a node rotate its own key
// without an admin.
type MachineKeyRegistration struct {
	MachineIdentity   string `json:"machine_identity"`
	PublicKey         string `json:"public_key"`
	Timestamp         int64  `json:"timestamp"`
	Signature         string `json:"signature"`
	RotationSignature string `json:"rotation_signature,omitempty"`
}

// MachineKeyRegistrationResponse answers a registration.
type MachineKeyRegistrationResponse struct {
	// Status is one of the MachineKey* statuses.
	Status string `json:"status"`
	// Fingerprint is the registered key's fingerprint ([MachineKeyFingerprint]).
	Fingerprint string `json:"fingerprint"`
}

// Registration statuses.
const (
	MachineKeyRegistered = "registered" // first key for this machine
	MachineKeyUnchanged  = "unchanged"  // the same key again
	MachineKeyRotated    = "rotated"    // replaced, proved by the old key
	MachineKeyReassigned = "reassigned" // replaced: the old holder is no active member
	// MachineKeyReplaced: replaced without the old key's hand-over because the
	// member completed a fresh admin-minted enrolment after the old key was
	// written (T-747, D-559). A released node treats any 200 as registered.
	MachineKeyReplaced = "replaced"
)

// MachineKeyFingerprint is the hex SHA-256 of the raw public key.
func MachineKeyFingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}

func machineMessage(domain string, fields ...string) []byte {
	var buf bytes.Buffer
	buf.WriteString(domain)
	for _, f := range fields {
		writeLPField(&buf, f)
	}
	return buf.Bytes()
}

// MachineKeyRegistrationMessage is what the new key signs to register.
func MachineKeyRegistrationMessage(orgID, subject, machine, publicKey string, ts int64) []byte {
	return machineMessage(machineRegisterDomain, orgID, subject, machine, publicKey, strconv.FormatInt(ts, 10))
}

// MachineKeyRotationMessage is what the OLD key signs to hand the machine to
// a new key.
func MachineKeyRotationMessage(orgID, subject, machine, newPublicKey string, ts int64) []byte {
	return machineMessage(machineRotateDomain, orgID, subject, machine, newPublicKey, strconv.FormatInt(ts, 10))
}

// MachineProofMessage is what the machine key signs on each request that names
// the machine: the org, the bearer's member, the machine, the HTTP method and
// path (no query), and the timestamp.
func MachineProofMessage(orgID, subject, machine, method, path string, ts int64) []byte {
	return machineMessage(machineProofDomain, orgID, subject, machine, method, path, strconv.FormatInt(ts, 10))
}

// SignMachineMessage signs msg with priv, base64 (std) encoded.
func SignMachineMessage(priv ed25519.PrivateKey, msg []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg))
}

// ErrMachineProof is the class of every machine proof refusal.
var ErrMachineProof = errors.New("machine proof refused")

// DecodeMachinePublicKey decodes a base64 (std) Ed25519 public key.
func DecodeMachinePublicKey(b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: not a base64 Ed25519 public key", ErrMachineProof)
	}
	return ed25519.PublicKey(raw), nil
}

// VerifyMachineMessage checks sig (base64 std) over msg with pub, and that ts
// lies within [MachineProofWindow] of now.
func VerifyMachineMessage(pub ed25519.PublicKey, msg []byte, sig string, ts int64, now time.Time) error {
	if d := now.Sub(time.Unix(ts, 0)); d > MachineProofWindow || d < -MachineProofWindow {
		return fmt.Errorf("%w: timestamp outside the %s window", ErrMachineProof, MachineProofWindow)
	}
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil || len(raw) != ed25519.SignatureSize {
		return fmt.Errorf("%w: malformed signature", ErrMachineProof)
	}
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, msg, raw) {
		return fmt.Errorf("%w: signature does not verify", ErrMachineProof)
	}
	return nil
}

// MachineProofHeaders returns the header set a node adds to a request naming
// machine, signed with priv for (orgID, subject, method, path) at now.
func MachineProofHeaders(priv ed25519.PrivateKey, orgID, subject, machine, method, path string, now time.Time) map[string]string {
	ts := now.Unix()
	return map[string]string{
		HeaderMachineProofCaps:      MachineProofCapability,
		HeaderMachineProofTimestamp: strconv.FormatInt(ts, 10),
		HeaderMachineProofSignature: SignMachineMessage(priv, MachineProofMessage(orgID, subject, machine, method, path, ts)),
	}
}
