package orgcontract

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Org DISPLAY POLICY rail (operator ruling 2026-10-02): the org admin's ONE
// cost-display choice — whether enrolled DEVELOPERS' node dashboards show
// dollar costs (the default) or token usage instead — delivered to nodes as a
// signed, versioned document over the enrolment bearer
// (GET /api/agent/display-policy). Admins always see dollars on the admin
// dashboard; nothing on this rail can change that.
//
// WHY ITS OWN SMALL DOCUMENT rather than a field on an existing rail:
//
//   - the PRICING rail (sbo-pricing-policy-v1) is applied only under
//     [guard.budget].from_org and its version moves with the price book, so a
//     display flip would re-download (and re-stamp) a whole price list, and an
//     individual node that never applies org prices would never see it;
//   - the BUDGET rail is resolved and signed PER CALLER (a subject is bound),
//     and a 404 there means "drop your cached cap" — the opposite of what a
//     display setting needs;
//   - node GOVERNANCE (node.governance via the policy-resource rail) is
//     intersected with a per-node enrolment GRANT and gated on authority
//     tokens, which a plain display preference has no reason to require.
//
// So this mirrors the ANNOUNCEMENT rail — the other fleet-wide, display-only
// document — down to the TOFU-pinned org key and the monotonic version, and
// additionally binds the ORG ID into the signed message the way the pricing
// rail does, so a document signed for one org cannot be replayed to a node
// enrolled with another org that happens to share a key.
//
// Nothing about this document is a security control. It changes what a
// dashboard renders; the node still computes and stores cost at capture.

// Developer cost-display vocabulary carried in [DisplayPolicyBody]. It is the
// org server's rollup.DeveloperCostDisplay* vocabulary, restated here because
// this package is the wire contract both sides import.
const (
	DisplayCostUSD    = "usd"
	DisplayCostTokens = "tokens"
)

// displayPolicySigningDomain domain-separates display-policy signatures from
// every other Ed25519 use in the protocol — including the announcement,
// routing and pricing rails, which the SAME org key signs.
const displayPolicySigningDomain = "sbo-display-policy-v1"

// DisplayPolicyBody is the signed body. Encoded with [EncodeDisplayPolicyBody]
// into [DisplayPolicyDoc].Body; the signature covers those exact bytes.
type DisplayPolicyBody struct {
	// DeveloperCostDisplay is "usd" or "tokens". A value this build does not
	// know resolves to token usage ([NormalizeDisplayCost]): only an explicit
	// "usd" shows dollars (D-013 fail-safe, Sol A4-03 class).
	//
	// COMPATIBILITY: nodes built before that rule mapped an unknown value to
	// dollars, and they cannot be changed. So a server must never put a NEW
	// value on this field: a future display mode rides a new field, and this
	// one keeps carrying "usd" or "tokens" as the fallback older nodes read
	// ("tokens" whenever the new mode hides dollars). The encoder enforces
	// the closed vocabulary on the way out.
	DeveloperCostDisplay string `json:"developer_cost_display"`
}

// DisplayPolicyDoc is the wire document of GET /api/agent/display-policy.
type DisplayPolicyDoc struct {
	// OrgID names the org the document was signed for. It is bound into the
	// signature; the node additionally refuses a document whose OrgID is not
	// its own enrolment's.
	OrgID string `json:"org_id"`
	// Version is monotonic per org (org_display_settings.
	// developer_display_version). 0 is the never-authored default document.
	Version int64 `json:"version"`
	// Body is the JSON-encoded [DisplayPolicyBody].
	Body string `json:"body"`
	// BodyHash is hex(SHA-256(Body)) — integrity/dedup only; it authorizes
	// nothing on its own (see Signature).
	BodyHash string `json:"body_hash"`
	// Signature is base64(Ed25519 over [DisplayPolicySigningMessage]).
	Signature string `json:"signature"`
	// PublicKey is base64(Ed25519 public key): the org distribution key the
	// node already pins across rails (orgclient/orgpin.go).
	PublicKey string `json:"public_key"`
}

// NormalizeDisplayCost maps any value onto the closed vocabulary. Only an
// exact "usd" is dollars; every other value (empty, a different spelling, a
// mode a newer server knows) is token usage, so an unreadable choice never
// exposes costs an org chose to hide (Sol A4-03 class).
func NormalizeDisplayCost(v string) string {
	if v == DisplayCostUSD {
		return DisplayCostUSD
	}
	return DisplayCostTokens
}

// EncodeDisplayPolicyBody returns the canonical body bytes (encoding/json of
// the normalized struct — one field, so the encoding is stable).
func EncodeDisplayPolicyBody(b DisplayPolicyBody) (string, error) {
	b.DeveloperCostDisplay = NormalizeDisplayCost(b.DeveloperCostDisplay)
	raw, err := json.Marshal(b)
	if err != nil {
		return "", fmt.Errorf("orgcontract.EncodeDisplayPolicyBody: %w", err)
	}
	return string(raw), nil
}

// DecodeDisplayPolicyBody parses a VERIFIED body. An unknown value is
// normalized to token usage rather than refused: the document is still the
// org's newest signed statement, and token usage is the reading of it that
// cannot expose a cost the org chose to hide.
func DecodeDisplayPolicyBody(body string) (DisplayPolicyBody, error) {
	var b DisplayPolicyBody
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		return DisplayPolicyBody{}, fmt.Errorf("orgcontract.DecodeDisplayPolicyBody: %w", err)
	}
	b.DeveloperCostDisplay = NormalizeDisplayCost(b.DeveloperCostDisplay)
	return b, nil
}

// DisplayPolicySigningMessage returns the canonical bytes signed over a
// display-policy document: SHA-256 over
//
//	domain || 0x00 || orgID || 0x00 || decimal(version) || 0x00 || body
//
// Binding the VERSION stops a captured document being replayed at an inflated
// version (which would freeze a node against every later genuine change via
// its monotonic short-circuit); binding the ORG stops cross-org replay; the
// domain tag stops a document signed on another rail by the same key from
// verifying here. The NUL separators keep the framing unambiguous.
func DisplayPolicySigningMessage(orgID string, version int64, body string) []byte {
	h := sha256.New()
	h.Write([]byte(displayPolicySigningDomain))
	h.Write([]byte{0})
	h.Write([]byte(orgID))
	h.Write([]byte{0})
	h.Write([]byte(strconv.FormatInt(version, 10)))
	h.Write([]byte{0})
	h.Write([]byte(body))
	return h.Sum(nil)
}

// SignDisplayPolicy builds and signs the document. The org server's agent
// handler is the only production caller.
func SignDisplayPolicy(priv ed25519.PrivateKey, orgID string, version int64, body DisplayPolicyBody) (DisplayPolicyDoc, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return DisplayPolicyDoc{}, errors.New("orgcontract.SignDisplayPolicy: bad private key")
	}
	encoded, err := EncodeDisplayPolicyBody(body)
	if err != nil {
		return DisplayPolicyDoc{}, err
	}
	sum := sha256.Sum256([]byte(encoded))
	pub, _ := priv.Public().(ed25519.PublicKey)
	return DisplayPolicyDoc{
		OrgID:     orgID,
		Version:   version,
		Body:      encoded,
		BodyHash:  hex.EncodeToString(sum[:]),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, DisplayPolicySigningMessage(orgID, version, encoded))),
		PublicKey: base64.StdEncoding.EncodeToString(pub),
	}, nil
}

// VerifyDisplayPolicy checks doc against the node's own enrolment org and the
// pinned org key. Every refusal is an error; the caller keeps its last
// verified value on any error (a refused document never clears local state).
func VerifyDisplayPolicy(doc DisplayPolicyDoc, expectOrgID, pinnedPubB64 string) error {
	if expectOrgID == "" || doc.OrgID != expectOrgID {
		return fmt.Errorf("orgcontract.VerifyDisplayPolicy: document is for org %q, this node is enrolled with %q", doc.OrgID, expectOrgID)
	}
	if doc.Version < 0 {
		return errors.New("orgcontract.VerifyDisplayPolicy: negative version")
	}
	pub, err := base64.StdEncoding.DecodeString(pinnedPubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("orgcontract.VerifyDisplayPolicy: bad public key")
	}
	if doc.Signature == "" {
		return errors.New("orgcontract.VerifyDisplayPolicy: unsigned document")
	}
	sig, err := base64.StdEncoding.DecodeString(doc.Signature)
	if err != nil {
		return errors.New("orgcontract.VerifyDisplayPolicy: bad signature encoding")
	}
	sum := sha256.Sum256([]byte(doc.Body))
	if hex.EncodeToString(sum[:]) != doc.BodyHash {
		return errors.New("orgcontract.VerifyDisplayPolicy: body hash mismatch")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), DisplayPolicySigningMessage(doc.OrgID, doc.Version, doc.Body), sig) {
		return errors.New("orgcontract.VerifyDisplayPolicy: signature invalid (it must cover this rail's domain tag, this org AND this version)")
	}
	return nil
}
