package orgcontract

// Cloud Intelligence parity phase 2: the B1 rail (design
// docs/plans/cloud-intelligence-parity-design-2026-10-03.md sections 2.1 (d)
// and 4.1). An enrolled node builds the org-plane evidence envelope itself
// (cloudevidence, PlaneOrg) and submits its exact preview==upload bytes. The
// rail is authenticated like the consent POST: enrolment bearer + per-request
// Ed25519 proof over IntelRailSigningMessage with the body's SHA-256 bound in
// (IntelBodyCanonical).

// IntelEvidencePath is the submit rail.
const IntelEvidencePath = "/api/agent/intel/evidence"

// IntelEvidenceMaxBytes bounds one submission body (the envelope is bounded by
// cloudcontract's schema limits well below this).
const IntelEvidenceMaxBytes = 512 << 10

// Submission triggers.
const (
	// IntelTriggerBackground is the daemon's own submission of a quiet
	// org-plane session.
	IntelTriggerBackground = "background"
	// IntelTriggerDeveloper is a developer-started Enrich now / re-enrich
	// (counts against the org's per-developer daily cap).
	IntelTriggerDeveloper = "developer"
)

// IntelEvidenceSubmit is the POST /api/agent/intel/evidence body.
type IntelEvidenceSubmit struct {
	// SessionID is the node's real session id (the org already holds the row
	// the node pushed); the envelope itself carries only a pseudonym.
	SessionID string `json:"session_id"`
	// Purpose is the consent purpose the envelope was built under.
	Purpose string `json:"purpose"`
	// Trigger is IntelTriggerBackground or IntelTriggerDeveloper.
	Trigger string `json:"trigger"`
	// Envelope is the exact upload bytes (canonical JSON, as text).
	Envelope string `json:"envelope"`
	// EvidenceDigest and UploadDigest are the node's two digests; the server
	// recomputes both from Envelope and refuses a mismatch.
	EvidenceDigest string `json:"evidence_digest"`
	UploadDigest   string `json:"upload_digest"`
}

// IntelEvidenceReceipt is the 200 reply.
type IntelEvidenceReceipt struct {
	// JobID is the org job carrying the evidence.
	JobID int64 `json:"job_id"`
	// Outcome is admitted | duplicate | already_enriched.
	Outcome string `json:"outcome"`
	// State is the job's state (queued for a new job).
	State string `json:"state,omitempty"`
	// EvidenceLevel is the level the SERVER computed from the envelope.
	EvidenceLevel string `json:"evidence_level"`
	// ExpiresAt is when the server deletes the bytes (RFC3339).
	ExpiresAt string `json:"expires_at,omitempty"`
}

// IntelEvidenceRefusal is the error body of a refused submission: a closed
// code and a sentence the node shows as is.
type IntelEvidenceRefusal struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	// Retryable tells the node whether the same bytes may succeed later
	// (consent may be accepted, the session may finish pushing, the daily cap
	// rolls over).
	Retryable bool `json:"retryable"`
}

// Refusal codes of the submit rail.
const (
	IntelEvidenceErrInvalid          = "invalid_evidence"
	IntelEvidenceErrPurpose          = "purpose_not_allowed"
	IntelEvidenceErrLevel            = "level_not_allowed"
	IntelEvidenceErrNotAdmitted      = "not_admitted"
	IntelEvidenceErrDeveloperActions = "developer_actions_off"
	IntelEvidenceErrDeveloperCap     = "developer_cap_reached"
	IntelEvidenceErrSessionUnknown   = "session_unknown"
	// IntelEvidenceErrBackgroundOff refuses a background submission when the
	// member's effective settings turned automatic enrichment off (phase 4).
	IntelEvidenceErrBackgroundOff = "background_off"
	// IntelEvidenceErrSessionPurged refuses a submission for a session the
	// organization purged: a purged session stays purged until the member
	// re-consents (individual tenancy) or an admin re-enriches it. Not
	// retryable for the same bytes.
	IntelEvidenceErrSessionPurged = "session_purged"
)
