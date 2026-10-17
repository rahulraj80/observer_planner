package orgcontract

import (
	"bytes"
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

// Org AI TOOL SETTINGS rail (adapter config phase P6, backlog item 8;
// docs/agent-config.md "Organization policy"): the org admin's settings for
// the developer's AI coding tools (Claude Code settings.json, Codex
// config.toml, ...), composed PER CALLER from the org-wide, team and member
// documents the admin authored, signed, and delivered to MANAGED nodes over
// the enrolment bearer (GET /api/agent/agent-config-policy).
//
// WHY ITS OWN RAIL rather than a unified policy-resource family or a table on
// an existing rail (the compat invariant, D-233 / D-234):
//
//   - a new policy-resource family is closed in five places (policyfam, the
//     config accept-list validator, the server's family enum, the govern
//     authority-to-family table and the policy-state point lists). A released
//     agent's config.Load REFUSES an unknown family named in
//     [org_client.policy].accept_families, and an already-enrolled managed
//     node never has a new family added to that list, so a family would need
//     a new enforce token and a re-enrolment to reach the fleet. A new rail is
//     a URL an older agent never calls, so it cannot disturb that agent's
//     other policies, and a newer node polling an older server reads a 404 as
//     "not supported" (a family would get 400 unknown_family every cycle);
//   - the body is resolved per caller (team and member layers); the resource
//     rail signs one body per family at publish time and only node.governance
//     has a serve-time re-sign special case. The budget and display rails are
//     the precedents for a body signed at serve time and bound to a subject;
//   - node.governance rejects unknown fields on released agents, and the
//     guard bundle is TOML the hook reads on every tool call: neither should
//     carry a few hundred vendor setting values.
//
// The signature covers the BODY BYTES exactly as the server encoded them (the
// display-policy rule), so a newer server may add body fields without an
// older node's re-encode breaking verification: the node verifies the bytes,
// then decodes leniently.
//
// INDIVIDUAL NODES NEVER RECEIVE IT: the server answers a non-managed subject
// 404 not_managed, and the node fetches and applies it only while its own
// enrolment is managed with a live grant. Secret values are never carried:
// the publish lint refuses a setting the catalogue marks secret and a value
// that looks like a credential.

// agentConfigPolicySigningDomain domain-separates this rail's signatures from
// every other Ed25519 use of the same org key.
const agentConfigPolicySigningDomain = "sbo-agent-config-policy-v1"

// RailAgentConfig is the rail key of the policy-version vector and the push
// acknowledgment (PushResponse.RailVersions). An older node ignores the
// unknown key; an older server never sends it.
const RailAgentConfig = "agent_config"

// AgentConfigPolicyPath is the agent GET.
const AgentConfigPolicyPath = "/api/agent/agent-config-policy"

// AgentConfigPolicySchema is the body schema this build writes and reads.
const AgentConfigPolicySchema = 1

// Closed vocabularies carried in the body. A value a node does not know is
// handled per field (see each Normalize* function): never by widening what a
// developer may do.
const (
	// Modes of one directive (plan section 3.2).
	AgentConfigModeEnforced = "enforced"
	AgentConfigModeDefault  = "default"

	// Org layers a directive came from (agentconfig.LayerO1..O3).
	AgentConfigLayerOrg    = "o1_org"
	AgentConfigLayerTeam   = "o2_team"
	AgentConfigLayerMember = "o3_member"

	// Placement of a project directive (plan section 6.3).
	AgentConfigPlacementLocalOnly = "local_only"
	AgentConfigPlacementShared    = "shared"

	// What the node does when a value the org set drifts.
	AgentConfigOnDriftReassert = "reassert"
	AgentConfigOnDriftReport   = "report"

	// The developer's default access on a managed node (D-102).
	AgentConfigAccessReadOnly = "read_only"
	AgentConfigAccessEditable = "editable"

	// The plaintext-secret posture (D-104).
	AgentConfigSecretsWarn   = "warn"
	AgentConfigSecretsRefuse = "refuse"
	AgentConfigSecretsAllow  = "allow"
)

// AgentConfigDirective is one setting the org sets for this node.
type AgentConfigDirective struct {
	// Setting is a catalogue id ("claude-code/model"). A common-vocabulary
	// value is expanded by the server into one directive per native setting
	// (Common then names the key it came from).
	Setting string `json:"setting"`
	// Value is the native value to write (JSON).
	Value json.RawMessage `json:"value"`
	// Mode is enforced or default.
	Mode string `json:"mode"`
	// Layer is the org layer that won for this setting.
	Layer string `json:"layer"`
	// Policy is the authoring document's NAME, shown to the developer
	// ("Set by your organization's policy <name>").
	Policy string `json:"policy"`
	// Common is the common-vocabulary key the value came from, if any.
	Common string `json:"common,omitempty"`
	// Project is a project identity hash (internal/git.ResolveIdentity);
	// empty means the tool's user-scope file.
	Project string `json:"project,omitempty"`
	// Placement is local_only or shared for a project directive.
	Placement string `json:"placement,omitempty"`
}

// AgentConfigInstruction is one org instruction (D-107): text the node adds
// as a marked org block ("<!-- observer:org-begin ... -->" ... end marker)
// to each installed AI tool's own instruction file, at the user level
// (Project empty) or in every local clone of one project. The block is the
// org's; the developer's text around it is never touched.
type AgentConfigInstruction struct {
	// Project is a project identity hash; empty means the user-level
	// instruction file of every installed tool.
	Project string `json:"project,omitempty"`
	// Placement is local_only (the tool's personal, git-excluded
	// instruction file) or shared (the file a commit carries) for a project
	// instruction.
	Placement string `json:"placement,omitempty"`
	// Text is the block's content (Markdown, no Observer markers).
	Text string `json:"text"`
	// Policy names the authoring document(s); Layer is the most specific org
	// layer that contributed.
	Policy string `json:"policy"`
	Layer  string `json:"layer"`
}

// AgentConfigInstructionMaxBytes caps one instruction's text.
const AgentConfigInstructionMaxBytes = 32 << 10

// AgentConfigInstructionSuffix is the setting part of the pseudo setting id
// an org instruction's state row carries ("<tool>/@org-instructions"): the
// state wire stays one shape for settings and instruction blocks, and a
// server that does not know it stores it as any other row.
const AgentConfigInstructionSuffix = "@org-instructions"

// AgentConfigInstructionSetting is the state-row setting id of one tool's
// org instruction block.
func AgentConfigInstructionSetting(tool string) string {
	return tool + "/" + AgentConfigInstructionSuffix
}

// IsAgentConfigInstructionSetting reports an org instruction state row.
func IsAgentConfigInstructionSetting(setting string) bool {
	return strings.HasSuffix(setting, "/"+AgentConfigInstructionSuffix)
}

// AgentConfigGrant lets the developer edit a setting on a managed node (D-102
// and D-122: a grant also lets the developer's own value beat an org default).
// Setting is a catalogue id, or "<tool>/*" for every setting of one tool.
type AgentConfigGrant struct {
	Setting string `json:"setting"`
}

// AgentConfigPolicyBody is the signed body, JSON-encoded into
// AgentConfigPolicyDoc.Body.
type AgentConfigPolicyBody struct {
	Schema          int                    `json:"schema"`
	SecretsPosture  string                 `json:"secrets_posture"`
	OnDrift         string                 `json:"on_drift"`
	DeveloperAccess string                 `json:"developer_access"`
	Grants          []AgentConfigGrant     `json:"grants,omitempty"`
	Directives      []AgentConfigDirective `json:"directives,omitempty"`
	// Instructions are the org instruction blocks (D-107, T-426). An older
	// node decodes the body leniently and ignores them.
	Instructions []AgentConfigInstruction `json:"instructions,omitempty"`
	// Policies names every document that contributed (display only).
	Policies []string `json:"policies,omitempty"`
}

// Empty reports a body that sets nothing: a SIGNED empty body is the only
// thing that withdraws the org's settings from a node (D-016).
func (b AgentConfigPolicyBody) Empty() bool {
	return len(b.Directives) == 0 && len(b.Grants) == 0 && len(b.Instructions) == 0
}

// AgentConfigPolicyDoc is the wire document of GET /api/agent/agent-config-policy.
type AgentConfigPolicyDoc struct {
	OrgID string `json:"org_id"`
	// Subject is the org member the body was composed for (the enrolment's
	// user id), bound into the signature.
	Subject string `json:"subject"`
	// Version is monotonic per org: the document sequence plus the roster
	// epoch plus the composition format, so a team change or a publish both
	// move it.
	Version   int64  `json:"version"`
	Body      string `json:"body"`
	BodyHash  string `json:"body_hash"`
	Signature string `json:"signature"`
	PublicKey string `json:"public_key"`
}

// NormalizeAgentConfigSecrets maps an unknown posture onto warn, the
// individual-node default (a newer server's stricter mode reads as warn on an
// older node, never as allow).
func NormalizeAgentConfigSecrets(v string) string {
	switch v {
	case AgentConfigSecretsRefuse, AgentConfigSecretsAllow:
		return v
	}
	return AgentConfigSecretsWarn
}

// NormalizeAgentConfigOnDrift maps an unknown drift response onto reassert
// (the org's value is kept).
func NormalizeAgentConfigOnDrift(v string) string {
	if v == AgentConfigOnDriftReport {
		return v
	}
	return AgentConfigOnDriftReassert
}

// NormalizeAgentConfigAccess maps an unknown access default onto read_only:
// only an explicit "editable" opens the page (D-102 fail-safe).
func NormalizeAgentConfigAccess(v string) string {
	if v == AgentConfigAccessEditable {
		return v
	}
	return AgentConfigAccessReadOnly
}

// EncodeAgentConfigPolicyBody returns the canonical body bytes. Directives
// are emitted in the order given (the composer sorts them); values are
// re-encoded compactly so two equal bodies are byte-identical.
func EncodeAgentConfigPolicyBody(b AgentConfigPolicyBody) (string, error) {
	b.Schema = AgentConfigPolicySchema
	b.SecretsPosture = NormalizeAgentConfigSecrets(b.SecretsPosture)
	b.OnDrift = NormalizeAgentConfigOnDrift(b.OnDrift)
	b.DeveloperAccess = NormalizeAgentConfigAccess(b.DeveloperAccess)
	for i := range b.Directives {
		var buf bytes.Buffer
		if err := json.Compact(&buf, b.Directives[i].Value); err != nil {
			return "", fmt.Errorf("orgcontract.EncodeAgentConfigPolicyBody: directive %s: %w", b.Directives[i].Setting, err)
		}
		b.Directives[i].Value = buf.Bytes()
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return "", fmt.Errorf("orgcontract.EncodeAgentConfigPolicyBody: %w", err)
	}
	return string(raw), nil
}

// DecodeAgentConfigPolicyBody parses a VERIFIED body leniently (an unknown
// field a newer server added is ignored) and normalizes the closed fields.
// A schema newer than this build is refused: its meaning cannot be assumed.
func DecodeAgentConfigPolicyBody(body string) (AgentConfigPolicyBody, error) {
	var b AgentConfigPolicyBody
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		return AgentConfigPolicyBody{}, fmt.Errorf("orgcontract.DecodeAgentConfigPolicyBody: %w", err)
	}
	if b.Schema > AgentConfigPolicySchema {
		return AgentConfigPolicyBody{}, fmt.Errorf("orgcontract.DecodeAgentConfigPolicyBody: schema %d is newer than this build (%d)", b.Schema, AgentConfigPolicySchema)
	}
	b.SecretsPosture = NormalizeAgentConfigSecrets(b.SecretsPosture)
	b.OnDrift = NormalizeAgentConfigOnDrift(b.OnDrift)
	b.DeveloperAccess = NormalizeAgentConfigAccess(b.DeveloperAccess)
	return b, nil
}

// AgentConfigPolicySigningMessage is SHA-256 over
//
//	domain || 0x00 || orgID || 0x00 || subject || 0x00 || decimal(version) || 0x00 || body
//
// Binding the subject stops a body composed for one member being replayed to
// another; the org stops cross-org replay; the version stops a captured body
// being replayed at an inflated version.
func AgentConfigPolicySigningMessage(orgID, subject string, version int64, body string) []byte {
	h := sha256.New()
	h.Write([]byte(agentConfigPolicySigningDomain))
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

// SignAgentConfigPolicy builds and signs the document for one caller.
func SignAgentConfigPolicy(priv ed25519.PrivateKey, orgID, subject string, version int64, body AgentConfigPolicyBody) (AgentConfigPolicyDoc, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return AgentConfigPolicyDoc{}, errors.New("orgcontract.SignAgentConfigPolicy: bad private key")
	}
	if strings.TrimSpace(subject) == "" {
		return AgentConfigPolicyDoc{}, errors.New("orgcontract.SignAgentConfigPolicy: empty subject")
	}
	encoded, err := EncodeAgentConfigPolicyBody(body)
	if err != nil {
		return AgentConfigPolicyDoc{}, err
	}
	sum := sha256.Sum256([]byte(encoded))
	pub, _ := priv.Public().(ed25519.PublicKey)
	return AgentConfigPolicyDoc{
		OrgID: orgID, Subject: subject, Version: version, Body: encoded,
		BodyHash:  hex.EncodeToString(sum[:]),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, AgentConfigPolicySigningMessage(orgID, subject, version, encoded))),
		PublicKey: base64.StdEncoding.EncodeToString(pub),
	}, nil
}

// VerifyAgentConfigPolicy checks doc against the node's own enrolment (org and
// subject) and the pinned org key. Every refusal is an error; the caller keeps
// its last verified body on any error (a refused document never clears).
func VerifyAgentConfigPolicy(doc AgentConfigPolicyDoc, expectOrgID, expectSubject, pinnedPubB64 string) error {
	if expectOrgID == "" || doc.OrgID != expectOrgID {
		return fmt.Errorf("orgcontract.VerifyAgentConfigPolicy: document is for org %q, this node is enrolled with %q", doc.OrgID, expectOrgID)
	}
	if expectSubject == "" || doc.Subject != expectSubject {
		return fmt.Errorf("orgcontract.VerifyAgentConfigPolicy: document is for member %q, this node is enrolled as %q", doc.Subject, expectSubject)
	}
	if doc.Version < 0 {
		return errors.New("orgcontract.VerifyAgentConfigPolicy: negative version")
	}
	pub, err := base64.StdEncoding.DecodeString(pinnedPubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("orgcontract.VerifyAgentConfigPolicy: bad public key")
	}
	if doc.Signature == "" {
		return errors.New("orgcontract.VerifyAgentConfigPolicy: unsigned document")
	}
	sig, err := base64.StdEncoding.DecodeString(doc.Signature)
	if err != nil {
		return errors.New("orgcontract.VerifyAgentConfigPolicy: bad signature encoding")
	}
	sum := sha256.Sum256([]byte(doc.Body))
	if hex.EncodeToString(sum[:]) != doc.BodyHash {
		return errors.New("orgcontract.VerifyAgentConfigPolicy: body hash mismatch")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), AgentConfigPolicySigningMessage(doc.OrgID, doc.Subject, doc.Version, doc.Body), sig) {
		return errors.New("orgcontract.VerifyAgentConfigPolicy: signature invalid (it must cover this rail's domain tag, this org, this member AND this version)")
	}
	return nil
}
