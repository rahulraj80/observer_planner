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

	"github.com/marmutapp/superbased-observer/internal/cloudcontract"
)

// Cloud Intelligence parity, phase 1: the org-plane CONSENT model (design
// docs/plans/cloud-intelligence-parity-design-2026-10-03.md section 3.7,
// operator rulings R1/R2). Two agent rails, both on the org server the node is
// already enrolled with:
//
//   - GET  /api/agent/intel/requirement: the org's org-plane posture as a
//     SIGNED, VERSIONED document (the display-policy shape: TOFU-pinned org
//     key, org id and version bound into the signature, monotonic). It tells
//     the node whether the org runs session enrichment, with which provider,
//     model, evidence level and purposes, and whether the org has made the
//     individual-tenancy opt-in a condition of membership. It is NOT a grant
//     replacement and it is never auto-accepted: on an individual-tenancy node
//     a requirement the developer has not accepted is stored as PENDING and
//     changes nothing until the developer accepts it interactively (INV-13).
//
//   - POST /api/agent/intel/consent: the node's OWN report of its consent
//     posture (enum + digest only, no content), authenticated like the result
//     rail. The server stores it keyed on the bearer subject. It is the only
//     way the server learns that an individual member opted in; the server
//     never writes a member's consent on the member's behalf.

// Agent rail paths. The signer and the verifier bind these CONSTANTS (never
// the received URL path) into the per-request proof.
const (
	IntelRequirementPath = "/api/agent/intel/requirement"
	IntelConsentPath     = "/api/agent/intel/consent"
)

// IntelBodyCanonical is the canonical "query" a POST on the intel rails binds
// into IntelRailSigningMessage: the hex SHA-256 of the exact request body. The
// rail's domain tag, method and path already separate it from the result GET;
// binding the body hash makes a captured proof useless for any other body.
func IntelBodyCanonical(body []byte) string {
	sum := sha256.Sum256(body)
	return "body_sha256=" + hex.EncodeToString(sum[:])
}

// intelRequirementSigningDomain domain-separates requirement-document
// signatures from every other rail the same org key signs.
const intelRequirementSigningDomain = "sbo-intel-requirement-v1"

// IntelRequirementBody is the signed body of the requirement document.
type IntelRequirementBody struct {
	// OrgName is the org's display name (D-144: the disclosure names the org).
	OrgName string `json:"org_name"`
	// Enabled is org_intel_settings.enabled.
	Enabled bool `json:"enabled"`
	// ProviderLabel names the org's configured provider ('' = none configured)
	// and Model the configured model ('' = the provider's default).
	ProviderLabel string `json:"provider_label"`
	Model         string `json:"model"`
	// EvidenceLevel is the org's level (cloudcontract.EvidenceLevel).
	EvidenceLevel string `json:"evidence_level"`
	// Purposes is the org's allowed purpose set.
	Purposes []string `json:"purposes"`
	// DeveloperActions reports whether developers may start Enrich now.
	DeveloperActions bool `json:"developer_actions"`
	// PerDeveloperDailyCap is the per-developer daily submission cap.
	PerDeveloperDailyCap int `json:"per_developer_daily_cap"`
	// RequireIndividualConsent: the org has made the individual-tenancy opt-in
	// a condition of membership (ruling R1).
	RequireIndividualConsent bool `json:"require_individual_consent"`
	// RequirementVersion is the monotonic consent requirement version.
	RequirementVersion int64 `json:"requirement_version"`
	// RequiredSince is when the current requirement version was set.
	RequiredSince string `json:"required_since,omitempty"`
	// DeadlineDays is the optional deadline before an unaccepted individual
	// member's pushes are refused (0 = none).
	DeadlineDays int `json:"deadline_days"`

	// Parity phase 4: the rest of the org's control set, as it applies to THIS
	// member (the server resolves team and member overrides before signing).
	// Every field is omitempty and additive: a server that predates them
	// leaves them absent and the node keeps its own defaults.
	//
	// EvidenceSettings is the selection the node builds org envelopes with
	// (nil = the contract's defaults).
	EvidenceSettings *cloudcontract.EvidenceSettings `json:"evidence_settings,omitempty"`
	// Background false stops the node's background submission (nil = on).
	Background *bool `json:"background,omitempty"`
	// PerDeveloperMonthlyCap is the per-developer monthly submission cap
	// (0 = none).
	PerDeveloperMonthlyCap int `json:"per_developer_monthly_cap,omitempty"`
	// QuietMinutes, MinActions and MaxAgeDays choose which sessions the node
	// submits in the background (0 = the node's defaults).
	QuietMinutes int `json:"quiet_minutes,omitempty"`
	MinActions   int `json:"min_actions,omitempty"`
	MaxAgeDays   int `json:"max_age_days,omitempty"`
}

// IntelRequirementDoc is the wire document of GET /api/agent/intel/requirement.
type IntelRequirementDoc struct {
	OrgID     string `json:"org_id"`
	Version   int64  `json:"version"`
	Body      string `json:"body"`
	BodyHash  string `json:"body_hash"`
	Signature string `json:"signature"`
	PublicKey string `json:"public_key"`
}

// EncodeIntelRequirementBody returns the canonical body bytes. Purposes are
// copied so a nil list encodes as [] (one canonical form).
func EncodeIntelRequirementBody(b IntelRequirementBody) (string, error) {
	if b.Purposes == nil {
		b.Purposes = []string{}
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return "", fmt.Errorf("orgcontract.EncodeIntelRequirementBody: %w", err)
	}
	return string(raw), nil
}

// DecodeIntelRequirementBody parses a VERIFIED body.
func DecodeIntelRequirementBody(body string) (IntelRequirementBody, error) {
	var b IntelRequirementBody
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		return IntelRequirementBody{}, fmt.Errorf("orgcontract.DecodeIntelRequirementBody: %w", err)
	}
	return b, nil
}

// IntelRequirementSigningMessage returns the canonical signed bytes: SHA-256
// over domain || 0 || orgID || 0 || subject || 0 || decimal(version) || 0 ||
// body.
//
// The document carries ONE member's effective settings (team and member
// overrides resolved), so it is bound to that member's subject (the bearer's
// user id) as well as the org (C156-16): another member's document, even one
// with a valid org signature, never verifies on this node. The rail was
// unreleased when the subject was added, so there is no unbound v1 to keep.
// The NUL separators make the encoding unambiguous: neither id contains NUL.
func IntelRequirementSigningMessage(orgID, subject string, version int64, body string) []byte {
	h := sha256.New()
	h.Write([]byte(intelRequirementSigningDomain))
	h.Write([]byte{0})
	h.Write([]byte(orgID))
	h.Write([]byte{0})
	h.Write([]byte(subject))
	h.Write([]byte{0})
	h.Write([]byte(strconv.FormatInt(version, 10)))
	h.Write([]byte{0})
	h.Write([]byte(body))
	return h.Sum(nil)
}

// SignIntelRequirement builds and signs the document for one member (subject
// is the verified bearer's user id; the org server's agent handler is the only
// production caller).
func SignIntelRequirement(priv ed25519.PrivateKey, orgID, subject string, version int64, body IntelRequirementBody) (IntelRequirementDoc, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return IntelRequirementDoc{}, errors.New("orgcontract.SignIntelRequirement: bad private key")
	}
	encoded, err := EncodeIntelRequirementBody(body)
	if err != nil {
		return IntelRequirementDoc{}, err
	}
	sum := sha256.Sum256([]byte(encoded))
	pub, _ := priv.Public().(ed25519.PublicKey)
	return IntelRequirementDoc{
		OrgID:     orgID,
		Version:   version,
		Body:      encoded,
		BodyHash:  hex.EncodeToString(sum[:]),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, IntelRequirementSigningMessage(orgID, subject, version, encoded))),
		PublicKey: base64.StdEncoding.EncodeToString(pub),
	}, nil
}

// VerifyIntelRequirement checks doc against the node's own enrolment org and
// member (expectSubject, the enrolment's user id) and the pinned org key.
// Every refusal is an error; the caller keeps its last verified document on
// any error.
func VerifyIntelRequirement(doc IntelRequirementDoc, expectOrgID, expectSubject, pinnedPubB64 string) error {
	if expectOrgID == "" || doc.OrgID != expectOrgID {
		return fmt.Errorf("orgcontract.VerifyIntelRequirement: document is for org %q, this node is enrolled with %q", doc.OrgID, expectOrgID)
	}
	if expectSubject == "" {
		return errors.New("orgcontract.VerifyIntelRequirement: this node has no enrolment subject to bind the document to")
	}
	if doc.Version < 0 {
		return errors.New("orgcontract.VerifyIntelRequirement: negative version")
	}
	pub, err := base64.StdEncoding.DecodeString(pinnedPubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("orgcontract.VerifyIntelRequirement: bad public key")
	}
	if doc.Signature == "" {
		return errors.New("orgcontract.VerifyIntelRequirement: unsigned document")
	}
	sig, err := base64.StdEncoding.DecodeString(doc.Signature)
	if err != nil {
		return errors.New("orgcontract.VerifyIntelRequirement: bad signature encoding")
	}
	sum := sha256.Sum256([]byte(doc.Body))
	if hex.EncodeToString(sum[:]) != doc.BodyHash {
		return errors.New("orgcontract.VerifyIntelRequirement: body hash mismatch")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), IntelRequirementSigningMessage(doc.OrgID, expectSubject, doc.Version, doc.Body), sig) {
		return errors.New("orgcontract.VerifyIntelRequirement: signature invalid (it must cover this rail's domain tag, this org, this member AND this version)")
	}
	return nil
}

// intelLevelDescriptions is the plain-language line per evidence level used by
// the one disclosure below (table-driven; an unknown level reads as the
// narrowest description, never a wider one).
var intelLevelDescriptions = map[string]string{
	"structural": "session metadata only: tool, model family, timing, token and cost metrics, and a summary of actions. No text from your prompts or the assistant's replies.",
	"titles":     "session metadata plus your first prompt in each session (scrubbed for secrets).",
	"excerpts":   "session metadata plus bounded, scrubbed excerpts: some of your prompts, recent assistant messages and failure categories.",
}

// IntelDisclosure renders THE consent disclosure for a requirement body: the
// one text the dashboard consent screen, the enrolment consent section and
// `observer intel accept` all show, and whose SHA-256 the node records as the
// disclosure digest of what the developer accepted. One source of truth means
// the CLI and the dashboard can never describe the same consent differently.
//
// required selects the "how to stop" line: when the org has made the consent a
// condition of membership the way out is leaving the organization; otherwise it
// is turning the switch off.
func IntelDisclosure(b IntelRequirementBody) (text, digest string) {
	org := strings.TrimSpace(b.OrgName)
	if org == "" {
		org = "Your organization"
	}
	provider := strings.TrimSpace(b.ProviderLabel)
	if provider == "" {
		provider = "no provider configured yet"
	}
	model := strings.TrimSpace(b.Model)
	if model == "" {
		model = "the provider's default model"
	}
	level := intelLevelDescriptions[b.EvidenceLevel]
	if level == "" {
		level = intelLevelDescriptions["structural"]
	}
	ids := append([]string(nil), b.Purposes...)
	if len(ids) == 0 {
		ids = []string{string(cloudcontract.PurposeStructuralInsights)}
	}
	// Friendly label first, the id after it (ruling Q4): a developer reads the
	// name, and the id stays so the text still matches what the org configured.
	purposes := make([]string, 0, len(ids))
	for _, id := range ids {
		purposes = append(purposes, cloudcontract.Purpose(id).Label()+" ("+id+")")
	}
	lines := []string{
		org + " runs Cloud Intelligence (session enrichment) on sessions captured while this machine is enrolled.",
		"Who receives the evidence: " + org + ", on its own organization server. Nothing goes to SuperBased.",
		"Provider and model: " + provider + ", " + model + ".",
		"What is sent per session: " + level,
		"Purposes: " + strings.Join(purposes, ", ") + ".",
		"Who can read the results: you, and members of " + org + " who can already see the session.",
	}
	if b.RequireIndividualConsent {
		lines = append(lines, org+" requires this consent to stay a member. To stop, leave the organization (Settings, Organization, Leave, or `observer unenroll`).")
	} else {
		lines = append(lines, "To stop, turn it off in Settings, Cloud Intelligence, or run `observer intel disable`.")
	}
	text = strings.Join(lines, "\n")
	sum := sha256.Sum256([]byte(text))
	return text, hex.EncodeToString(sum[:])
}

// IntelConsentRequiredError is the typed error code of the 409 the push rail
// answers once an org's consent deadline has passed for an individual-tenancy
// member who has not accepted the current requirement (design section 3.7
// step 4). The node shows the server's message verbatim.
const IntelConsentRequiredError = "consent_required"

// IntelConsentReport is the POST /api/agent/intel/consent body: the node's own
// consent posture. Enum, version and digest only: no content.
type IntelConsentReport struct {
	// OptIn is the node's effective opt-in (its config key AND a live consent
	// record). false withdraws.
	OptIn bool `json:"opt_in"`
	// AcceptedRequirementVersion is the requirement version the developer
	// accepted (0 when the org has no requirement and the developer opted in
	// from the switch).
	AcceptedRequirementVersion int64 `json:"accepted_requirement_version"`
	// AcceptedLevel and AcceptedPurposes are the coverage the developer
	// accepted; the server never enriches above it.
	AcceptedLevel    string   `json:"accepted_level"`
	AcceptedPurposes []string `json:"accepted_purposes"`
	// DisclosureDigest is the SHA-256 of the IntelDisclosure text the
	// developer was shown.
	DisclosureDigest string `json:"disclosure_digest"`
}

// IntelConsentReply is the 200 body of the consent rail: what the server now
// holds for the calling member.
type IntelConsentReply struct {
	Recorded                   bool   `json:"recorded"`
	OptIn                      bool   `json:"opt_in"`
	AcceptedRequirementVersion int64  `json:"accepted_requirement_version"`
	ConsentSince               string `json:"consent_since,omitempty"`
}
