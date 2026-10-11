package orgcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// Managed-node AI tool settings state (adapter config phase P7): what a
// MANAGED node did with each setting the org's policy set, one row per
// setting and target file, carried on the push envelope as
// PushEnvelope.AgentConfigState.
//
// What a row may carry is fixed here and pinned by
// tests/invariant/privacy_test.go: closed enums (state, mode, layer, scope),
// catalogue identifiers (tool, setting, file row id, never a path), value
// HASHES, counters and times. ObservedValue is the one value-bearing field: it
// carries the value found in the file when it differs from the org's (D-112:
// the org sees everything on a managed node, values included), and it ships
// ONLY when the node ships raw content (store.ShareOptions.shipsRawContent).
// The ruling allows scrubbing, not withholding (T-428): a value from a
// personal (local-only) file ships like any other, and a secret-shaped value
// ships in its SCRUBBED form (the raw secret never leaves the node; the
// server re-scrubs it as a backstop). An individual node never sends this
// field at all (the composer is installed on managed nodes only, and the
// server refuses it from a subject without a managed enrolment).
//
// COMPATIBILITY: an optional envelope field (omitempty). An older server's
// lenient push decode ignores it; an older node never sends it.

// AgentConfigStateMaxRows caps one report.
const AgentConfigStateMaxRows = 500

// AgentConfigObservedMaxBytes caps one observed value (its JSON encoding). A
// larger value keeps its hash but not its text. An older server stored at
// most 4096 bytes and drops a longer one (it never fails the report).
const AgentConfigObservedMaxBytes = 16 << 10

// AgentConfigStateReport is one managed node's current applied state.
type AgentConfigStateReport struct {
	// PolicyVersion is the version of the org policy the node holds (0 when
	// it holds none).
	PolicyVersion int64 `json:"policy_version"`
	// Digest is a hash of the rows without their timestamps, so the server
	// can skip an unchanged report.
	Digest string `json:"digest"`
	// Rows is the complete current state (a row missing from a report is no
	// longer a target: the server replaces the node's rows).
	Rows []AgentConfigStateRow `json:"rows"`
	// Truncated: more than AgentConfigStateMaxRows targets existed.
	Truncated bool `json:"truncated,omitempty"`
}

// AgentConfigStateRow is one setting on one target file.
type AgentConfigStateRow struct {
	Tool    string `json:"tool"`
	Setting string `json:"setting"`
	// FileID is the catalogue file row id (never a path).
	FileID string `json:"file_id"`
	// Scope is the file row's scope (user, project_shared, project_local).
	Scope string `json:"scope"`
	// ProjectHash is the project identity of a project target.
	ProjectHash string `json:"project_hash,omitempty"`
	Mode        string `json:"mode"`
	Layer       string `json:"layer"`
	// State is agentconfigorg.State (closed; unknown values are dropped by
	// the server's ingest).
	State string `json:"state"`
	// Reason is a closed short code (a sanity-check finding code) for a
	// refused or not-applied row.
	Reason       string `json:"reason,omitempty"`
	ExpectedHash string `json:"expected_hash"`
	ObservedHash string `json:"observed_hash,omitempty"`
	// ObservedValue is the JSON of the value found in the file when it
	// differs from the org's value; see the package note for when it ships.
	ObservedValue string `json:"observed_value,omitempty"`
	// DriftCount is the drift events within the back-off window (one hour).
	DriftCount  int    `json:"drift_count"`
	LastDriftAt string `json:"last_drift_at,omitempty"`
	ObservedAt  string `json:"observed_at"`
}

var reasonRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

// NormalizeAgentConfigReason keeps a reason only when it is a short closed
// code (a free-text message never reaches the wire).
func NormalizeAgentConfigReason(r string) string {
	if reasonRE.MatchString(r) {
		return r
	}
	return ""
}

// DigestAgentConfigState hashes the rows without ObservedAt, sorted, so an
// unchanged state yields the same digest on every push.
func DigestAgentConfigState(rows []AgentConfigStateRow) string {
	cp := make([]AgentConfigStateRow, len(rows))
	copy(cp, rows)
	for i := range cp {
		cp[i].ObservedAt = ""
	}
	sort.Slice(cp, func(i, j int) bool { return agentConfigRowKey(cp[i]) < agentConfigRowKey(cp[j]) })
	b, _ := json.Marshal(cp)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func agentConfigRowKey(r AgentConfigStateRow) string {
	return strings.Join([]string{r.Setting, r.FileID, r.ProjectHash}, "\x00")
}
