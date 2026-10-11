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
	"strings"
)

// The signed PURGE TOMBSTONE set of the org-served Cloud Intelligence result
// rail (parity follow-up ruling 1, the D-192 withdrawal precedent). When an
// admin or member purge removes a session's enrichment on the org server, the
// node that owns the session still holds the result in its local cache
// (org_intel_cache) and renders its title. The result rail therefore carries,
// beside each page, ONE signed set naming every purged session of the calling
// node's subject. The node clears a cached result only on a set it VERIFIED:
// the org key it pinned from the signed requirement document, its own org, and
// its own subject. An unsigned, foreign or unverifiable set keeps the cache,
// and a node that never sees a set keeps what it has but receives nothing new
// for a purged session (the org no longer serves it).

// intelTombstoneSigningDomain domain-separates tombstone-set signatures from
// every other document the same org key signs.
const intelTombstoneSigningDomain = "sbo-intel-tombstone-v1"

// IntelTombstone is one purged session. JobID is the highest purged job id for
// it, as a decimal string like IntelResultRow.JobID: the node clears every
// cached result of the session whose job id is at or below it, so a result a
// later admin Re-enrich produced (a higher job id) survives.
type IntelTombstone struct {
	SessionID string `json:"session_id"`
	JobID     string `json:"job_id"`
	PurgedAt  string `json:"purged_at"`
}

// IntelTombstoneBody is the signed body: whose sessions, when, and which.
type IntelTombstoneBody struct {
	// Subject is the node enrolment subject the set addresses. The node
	// refuses a set for any other subject, so one node's set replayed to
	// another clears nothing.
	Subject  string           `json:"subject"`
	IssuedAt string           `json:"issued_at"`
	Entries  []IntelTombstone `json:"entries"`
	// Cursor is a SETTLED position in the owner's purge order (see
	// IntelTombstoneCursor): sets page oldest purge first, and the node sends
	// the cursor of the last set it applied back in HeaderIntelTombstonesAfter,
	// so a backlog larger than one set drains page by page instead of only the
	// newest page ever arriving (review C156-28). The node keeps it as a
	// durable floor and refuses a verified set whose cursor is below it, so an
	// older signed set replayed after a restart is never applied (review
	// C156-29). Entries past Cursor (purges too recent to have settled) are
	// served again on the next set, which is harmless: clearing is idempotent.
	Cursor string `json:"cursor,omitempty"`
	// More reports that purges exist past this set (the node asks again).
	More bool `json:"more,omitempty"`
}

// HeaderIntelTombstonesAfter carries the cursor of the last tombstone set the
// node applied on a result-rail GET. It is a position hint, not an
// authorization: the set that comes back is signed and floor-checked by the
// node, so a forged or stale header can only make the server serve a set the
// node then refuses or has already applied.
const HeaderIntelTombstonesAfter = "X-Observer-Intel-Tombstones-After"

// IntelTombstoneCursor renders the total-order position of one purge: its
// purge time (fixed-width RFC3339 UTC) then its session id, separated by one
// space (an RFC3339 instant never holds one).
func IntelTombstoneCursor(purgedAt, sessionID string) string {
	return purgedAt + " " + sessionID
}

// SplitIntelTombstoneCursor is IntelTombstoneCursor's inverse. ok is false for
// an empty or malformed cursor.
func SplitIntelTombstoneCursor(c string) (purgedAt, sessionID string, ok bool) {
	at, sess, found := strings.Cut(c, " ")
	if !found || at == "" || sess == "" {
		return "", "", false
	}
	return at, sess, true
}

// CompareIntelTombstoneCursor orders two cursors (-1, 0, +1) by purge time,
// then session id. An empty or malformed cursor sorts before every valid one.
func CompareIntelTombstoneCursor(a, b string) int {
	aAt, aSess, aOK := SplitIntelTombstoneCursor(a)
	bAt, bSess, bOK := SplitIntelTombstoneCursor(b)
	switch {
	case !aOK && !bOK:
		return 0
	case !aOK:
		return -1
	case !bOK:
		return 1
	}
	if c := strings.Compare(aAt, bAt); c != 0 {
		return c
	}
	return strings.Compare(aSess, bSess)
}

// IntelTombstoneDoc is the wire document carried on IntelResultsResponse.
type IntelTombstoneDoc struct {
	OrgID     string `json:"org_id"`
	Body      string `json:"body"`
	BodyHash  string `json:"body_hash"`
	Signature string `json:"signature"`
	PublicKey string `json:"public_key"`
}

// IntelTombstoneSigningMessage returns the canonical signed bytes: SHA-256
// over domain || 0 || orgID || 0 || subject || 0 || body.
func IntelTombstoneSigningMessage(orgID, subject, body string) []byte {
	h := sha256.New()
	h.Write([]byte(intelTombstoneSigningDomain))
	h.Write([]byte{0})
	h.Write([]byte(orgID))
	h.Write([]byte{0})
	h.Write([]byte(subject))
	h.Write([]byte{0})
	h.Write([]byte(body))
	return h.Sum(nil)
}

// SignIntelTombstones builds and signs the set (the org server's result
// reader is the only production caller).
func SignIntelTombstones(priv ed25519.PrivateKey, orgID string, body IntelTombstoneBody) (IntelTombstoneDoc, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return IntelTombstoneDoc{}, errors.New("orgcontract.SignIntelTombstones: bad private key")
	}
	if orgID == "" || body.Subject == "" {
		return IntelTombstoneDoc{}, errors.New("orgcontract.SignIntelTombstones: org and subject are required")
	}
	if body.Entries == nil {
		body.Entries = []IntelTombstone{}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return IntelTombstoneDoc{}, fmt.Errorf("orgcontract.SignIntelTombstones: %w", err)
	}
	encoded := string(raw)
	sum := sha256.Sum256(raw)
	pub, _ := priv.Public().(ed25519.PublicKey)
	return IntelTombstoneDoc{
		OrgID:     orgID,
		Body:      encoded,
		BodyHash:  hex.EncodeToString(sum[:]),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, IntelTombstoneSigningMessage(orgID, body.Subject, encoded))),
		PublicKey: base64.StdEncoding.EncodeToString(pub),
	}, nil
}

// VerifyIntelTombstones checks doc against the node's own org, its own
// enrolment subject and the org key it pinned, and returns the verified body.
// Every refusal is an error; the caller clears nothing on any error. An entry
// without a session id or with a job id that is not a positive decimal makes
// the whole set unusable (a set the org signed is never partly applied).
func VerifyIntelTombstones(doc IntelTombstoneDoc, expectOrgID, expectSubject, pinnedPubB64 string) (IntelTombstoneBody, error) {
	if expectOrgID == "" || doc.OrgID != expectOrgID {
		return IntelTombstoneBody{}, fmt.Errorf("orgcontract.VerifyIntelTombstones: set is for org %q, this node is enrolled with %q", doc.OrgID, expectOrgID)
	}
	pub, err := base64.StdEncoding.DecodeString(pinnedPubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return IntelTombstoneBody{}, errors.New("orgcontract.VerifyIntelTombstones: no pinned org key")
	}
	if doc.Signature == "" {
		return IntelTombstoneBody{}, errors.New("orgcontract.VerifyIntelTombstones: unsigned set")
	}
	sig, err := base64.StdEncoding.DecodeString(doc.Signature)
	if err != nil {
		return IntelTombstoneBody{}, errors.New("orgcontract.VerifyIntelTombstones: bad signature encoding")
	}
	sum := sha256.Sum256([]byte(doc.Body))
	if hex.EncodeToString(sum[:]) != doc.BodyHash {
		return IntelTombstoneBody{}, errors.New("orgcontract.VerifyIntelTombstones: body hash mismatch")
	}
	var body IntelTombstoneBody
	if err := json.Unmarshal([]byte(doc.Body), &body); err != nil {
		return IntelTombstoneBody{}, fmt.Errorf("orgcontract.VerifyIntelTombstones: %w", err)
	}
	if expectSubject == "" || body.Subject != expectSubject {
		return IntelTombstoneBody{}, fmt.Errorf("orgcontract.VerifyIntelTombstones: set addresses subject %q, this node is %q", body.Subject, expectSubject)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), IntelTombstoneSigningMessage(doc.OrgID, body.Subject, doc.Body), sig) {
		return IntelTombstoneBody{}, errors.New("orgcontract.VerifyIntelTombstones: signature invalid (it must cover this rail's domain tag, this org AND this subject)")
	}
	for _, e := range body.Entries {
		if e.SessionID == "" {
			return IntelTombstoneBody{}, errors.New("orgcontract.VerifyIntelTombstones: an entry has no session id")
		}
		if n, perr := strconv.ParseInt(e.JobID, 10, 64); perr != nil || n <= 0 {
			return IntelTombstoneBody{}, fmt.Errorf("orgcontract.VerifyIntelTombstones: entry %q has job id %q", e.SessionID, e.JobID)
		}
	}
	return body, nil
}

// IntelTombstoneCursorOlder reports whether cursor c lies at an EARLIER purge
// time than floor: the node's replay check (review C156-29). It compares the
// purge-time part only, which is fixed-width RFC3339 and so orders the same
// under every collation; the session-id tie-break is ordered by the server's
// own database collation (PostgreSQL's may not be byte order), so a set at the
// floor's own second is never refused (re-applying it is idempotent). An
// empty or malformed c is older than any valid floor.
func IntelTombstoneCursorOlder(c, floor string) bool {
	fAt, _, fOK := SplitIntelTombstoneCursor(floor)
	if !fOK {
		return false
	}
	cAt, _, cOK := SplitIntelTombstoneCursor(c)
	return !cOK || cAt < fAt
}
