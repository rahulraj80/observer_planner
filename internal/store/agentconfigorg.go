package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/scrub"
)

// The org AI TOOL POLICY on a managed node (adapter config phases P6 and P7,
// agent migration 157). ONE OWNER: both tables are written only here.
// NODE-LOCAL, pinned by tests/invariant/privacy_test.go's forbidden-table
// sentinel; orgpush.go never names them. What reaches the org is the
// envelope-level AgentConfigState report this file composes from closed
// fields (composeAgentConfigState), gated to managed enrolments.
//
//   - org_agent_config_policy: the last VERIFIED signed document of
//     GET /api/agent/agent-config-policy (one row). Written only by
//     orgclient.FetchAgentConfigPolicy after the signature, org, member, key
//     pin and monotonic version checks. A 404 never clears it.
//   - agent_config_org_state: what the node did with each directive on each
//     target file (agentconfigsvc.ApplyOrg is the one caller of the writer).

// OrgAgentConfigPolicyRow is the cached, VERIFIED document.
type OrgAgentConfigPolicyRow struct {
	OrgID        string
	Subject      string
	Version      int64
	Body         string
	BodyHash     string
	Signature    string
	ServerPubkey string
	ReceivedAt   time.Time
}

// UpsertOrgAgentConfigPolicy replaces the single-row cache.
func (s *Store) UpsertOrgAgentConfigPolicy(ctx context.Context, row OrgAgentConfigPolicyRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO org_agent_config_policy (id, org_id, subject, version, body, body_hash, signature, server_pubkey, received_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  org_id = excluded.org_id, subject = excluded.subject, version = excluded.version,
		  body = excluded.body, body_hash = excluded.body_hash, signature = excluded.signature,
		  server_pubkey = excluded.server_pubkey, received_at = excluded.received_at`,
		row.OrgID, row.Subject, row.Version, row.Body, row.BodyHash, row.Signature, row.ServerPubkey, timestamp(row.ReceivedAt))
	if err != nil {
		return fmt.Errorf("store.UpsertOrgAgentConfigPolicy: %w", err)
	}
	return nil
}

// GetOrgAgentConfigPolicy returns the cached document (ok=false when absent).
func (s *Store) GetOrgAgentConfigPolicy(ctx context.Context) (OrgAgentConfigPolicyRow, bool, error) {
	var row OrgAgentConfigPolicyRow
	var ts string
	err := s.db.QueryRowContext(ctx, `
		SELECT org_id, subject, version, body, body_hash, signature, server_pubkey, received_at
		FROM org_agent_config_policy WHERE id = 1`).
		Scan(&row.OrgID, &row.Subject, &row.Version, &row.Body, &row.BodyHash, &row.Signature, &row.ServerPubkey, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return OrgAgentConfigPolicyRow{}, false, nil
	}
	if err != nil {
		return OrgAgentConfigPolicyRow{}, false, fmt.Errorf("store.GetOrgAgentConfigPolicy: %w", err)
	}
	row.ReceivedAt = parseStamp(ts)
	return row, true, nil
}

// CurrentOrgAgentConfigPolicy is the ENROLMENT-BOUND read of the cache
// (review M3-01): the cached document only when it was signed for the org
// and the member this node is enrolled as NOW. A row left by an earlier
// enrolment (another org, or another member of the same org) reads as
// absent, so it can neither be applied, nor lock a setting, nor be reported
// to the current org. Every consumer outside the rail's own fetch path (which
// needs the raw row to judge a re-pin) reads through this.
func (s *Store) CurrentOrgAgentConfigPolicy(ctx context.Context) (OrgAgentConfigPolicyRow, bool, error) {
	enr, err := s.LoadEnrolment(ctx)
	if err != nil {
		return OrgAgentConfigPolicyRow{}, false, fmt.Errorf("store.CurrentOrgAgentConfigPolicy: %w", err)
	}
	if enr == nil {
		return OrgAgentConfigPolicyRow{}, false, nil
	}
	row, ok, err := s.GetOrgAgentConfigPolicy(ctx)
	if err != nil || !ok {
		return OrgAgentConfigPolicyRow{}, false, err
	}
	if row.OrgID != enr.OrgID || row.Subject != enr.UserID {
		return OrgAgentConfigPolicyRow{}, false, nil
	}
	return row, true, nil
}

// OrgAgentConfigBody decodes the cached verified body of the CURRENT
// enrolment. ok is false when no document is cached for it or it does not
// decode (an editor then treats the node as holding no policy: read-only on
// a managed node, D-102).
func (s *Store) OrgAgentConfigBody(ctx context.Context) (orgcontract.AgentConfigPolicyBody, OrgAgentConfigPolicyRow, bool) {
	row, ok, err := s.CurrentOrgAgentConfigPolicy(ctx)
	if err != nil || !ok {
		return orgcontract.AgentConfigPolicyBody{}, OrgAgentConfigPolicyRow{}, false
	}
	body, err := orgcontract.DecodeAgentConfigPolicyBody(row.Body)
	if err != nil {
		return orgcontract.AgentConfigPolicyBody{}, row, false
	}
	return body, row, true
}

// CurrentAgentConfigOrgState returns the applied state rows only while a
// policy of the CURRENT enrolment is held (nil otherwise): state written
// under an earlier enrolment is never shown or counted as this org's drift.
func (s *Store) CurrentAgentConfigOrgState(ctx context.Context) ([]AgentConfigOrgStateRow, error) {
	if _, ok, err := s.CurrentOrgAgentConfigPolicy(ctx); err != nil || !ok {
		return nil, err
	}
	return s.LoadAgentConfigOrgState(ctx)
}

// ClearOrgAgentConfig drops the cached document and the applied state. The
// unenrol path and a re-enrolment that changes the org or the member call it:
// a departed org's policy must not keep locking settings, and its state must
// not be reported to the next org.
func (s *Store) ClearOrgAgentConfig(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM org_agent_config_policy`); err != nil {
		return fmt.Errorf("store.ClearOrgAgentConfig: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM agent_config_org_state`); err != nil {
		return fmt.Errorf("store.ClearOrgAgentConfig: state: %w", err)
	}
	return nil
}

// AgentConfigOrgStateRow is one directive on one target file.
type AgentConfigOrgStateRow struct {
	SettingID string
	FileID    string
	// ProjectRoot is the local project directory ("" for a user file); it
	// never leaves the node.
	ProjectRoot string
	ProjectHash string
	Tool        string
	Scope       string
	Mode        string
	Layer       string
	PolicyName  string
	State       string
	Reason      string
	// ExpectedHash / ObservedHash are value fingerprints.
	ExpectedHash string
	ObservedHash string
	// ObservedValue is the JSON of the value found when it differs from the
	// org's ("" when none). A secret-shaped value is held SCRUBBED (the
	// writer, agentconfigsvc.observe, never hands this store a raw secret).
	ObservedValue string
	// ObservedSecret: the observed value looked like a credential; the
	// value held is its scrubbed form.
	ObservedSecret bool
	// Personal: the target is a personal (local-only) file. On a managed
	// node its value ships like any other (D-112, T-428).
	Personal      bool
	DriftTimes    []time.Time
	LastAppliedAt time.Time
	LastDriftAt   time.Time
	PolicyVersion int64
	ObservedAt    time.Time
}

// Key identifies the row's target.
func (r AgentConfigOrgStateRow) Key() string {
	return r.SettingID + "\x00" + r.FileID + "\x00" + r.ProjectRoot
}

// LoadAgentConfigOrgState returns every stored row.
func (s *Store) LoadAgentConfigOrgState(ctx context.Context) ([]AgentConfigOrgStateRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT setting_id, file_id, project_root, project_hash, tool, scope, mode, layer, policy_name, state, reason,
		       expected_hash, observed_hash, COALESCE(observed_value, ''), observed_secret, drift_times,
		       last_applied_at, last_drift_at, policy_version, observed_at
		FROM agent_config_org_state ORDER BY setting_id, file_id, project_root`)
	if err != nil {
		return nil, fmt.Errorf("store.LoadAgentConfigOrgState: %w", err)
	}
	defer rows.Close()
	var out []AgentConfigOrgStateRow
	for rows.Next() {
		var r AgentConfigOrgStateRow
		var secret int
		var drift, applied, last, observed string
		if err := rows.Scan(&r.SettingID, &r.FileID, &r.ProjectRoot, &r.ProjectHash, &r.Tool, &r.Scope, &r.Mode, &r.Layer,
			&r.PolicyName, &r.State, &r.Reason, &r.ExpectedHash, &r.ObservedHash, &r.ObservedValue, &secret, &drift,
			&applied, &last, &r.PolicyVersion, &observed); err != nil {
			return nil, fmt.Errorf("store.LoadAgentConfigOrgState: scan: %w", err)
		}
		r.ObservedSecret = secret != 0
		var stamps []string
		if json.Unmarshal([]byte(drift), &stamps) == nil {
			for _, st := range stamps {
				if t := parseStamp(st); !t.IsZero() {
					r.DriftTimes = append(r.DriftTimes, t)
				}
			}
		}
		r.LastAppliedAt, r.LastDriftAt, r.ObservedAt = parseStamp(applied), parseStamp(last), parseStamp(observed)
		r.Personal = r.Scope == "project_local"
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReplaceAgentConfigOrgState stores the complete current state (one
// transaction; a target missing from rows is no longer a target).
func (s *Store) ReplaceAgentConfigOrgState(ctx context.Context, rows []AgentConfigOrgStateRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store.ReplaceAgentConfigOrgState: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_config_org_state`); err != nil {
		return fmt.Errorf("store.ReplaceAgentConfigOrgState: clear: %w", err)
	}
	for _, r := range rows {
		stamps := make([]string, 0, len(r.DriftTimes))
		for _, t := range r.DriftTimes {
			stamps = append(stamps, timestamp(t))
		}
		drift, _ := json.Marshal(stamps)
		var observed any
		if r.ObservedValue != "" {
			observed = r.ObservedValue
		}
		secret := 0
		if r.ObservedSecret {
			secret = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agent_config_org_state (setting_id, file_id, project_root, project_hash, tool, scope, mode, layer,
			  policy_name, state, reason, expected_hash, observed_hash, observed_value, observed_secret, drift_times,
			  last_applied_at, last_drift_at, policy_version, observed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.SettingID, r.FileID, r.ProjectRoot, r.ProjectHash, r.Tool, r.Scope, r.Mode, r.Layer,
			r.PolicyName, r.State, r.Reason, r.ExpectedHash, r.ObservedHash, observed, secret, string(drift),
			stampOrEmpty(r.LastAppliedAt), stampOrEmpty(r.LastDriftAt), r.PolicyVersion, timestamp(r.ObservedAt)); err != nil {
			return fmt.Errorf("store.ReplaceAgentConfigOrgState: insert: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store.ReplaceAgentConfigOrgState: commit: %w", err)
	}
	return nil
}

func stampOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return timestamp(t)
}

// ProjectRootsByRemoteHash maps each git remote hash (the project identity
// an org project directive targets) to the local roots that carry it.
func (s *Store) ProjectRootsByRemoteHash(ctx context.Context) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT git_remote_hash, root_path FROM projects
		WHERE git_remote_hash IS NOT NULL AND git_remote_hash <> '' AND root_path <> ''
		ORDER BY root_path`)
	if err != nil {
		return nil, fmt.Errorf("store.ProjectRootsByRemoteHash: %w", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var h, root string
		if err := rows.Scan(&h, &root); err != nil {
			return nil, fmt.Errorf("store.ProjectRootsByRemoteHash: scan: %w", err)
		}
		out[h] = append(out[h], root)
	}
	return out, rows.Err()
}

// SetAgentConfigStateGate wires whether this node reports the org AI tool
// policy state on the push envelope: true only for a MANAGED enrolment
// holding a live grant (cmd/observer decides). nil = never report.
func (s *Store) SetAgentConfigStateGate(gate func(ctx context.Context) bool) {
	s.agentConfigStateGate = gate
}

// composeAgentConfigState builds the envelope report from the stored rows,
// through a func seam so orgpush.go names neither table. nil on an
// individual node, on a managed node that holds no policy, and when the
// gate is unset. includeValues is ShareOptions.shipsRawContent(): only then
// does an observed value ship (a personal file's value included, a
// secret-shaped one only scrubbed: D-112, T-428).
func (s *Store) composeAgentConfigState(ctx context.Context, includeValues bool) *orgcontract.AgentConfigStateReport {
	if s.agentConfigStateGate == nil || !s.agentConfigStateGate(ctx) {
		return nil
	}
	pol, ok, err := s.CurrentOrgAgentConfigPolicy(ctx)
	if err != nil || !ok {
		return nil
	}
	rows, err := s.LoadAgentConfigOrgState(ctx)
	if err != nil {
		return nil
	}
	return BuildAgentConfigStateReport(pol.Version, rows, includeValues)
}

// noteAgentConfigDigest records the report's digest as PENDING and reports
// whether it differs from the last one the org server accepted. The pending
// digest becomes the delivered one only in commitAgentConfigDigest, which
// CommitPushedSnapshots runs after an accepted push (review C156-39): a push
// that fails keeps the change "changed", so a drift-only state is re-forced on
// the next cycle instead of waiting for unrelated telemetry or a restart.
func (s *Store) noteAgentConfigDigest(rep *orgcontract.AgentConfigStateReport) bool {
	if rep == nil {
		return false
	}
	s.agentConfigDigestMu.Lock()
	defer s.agentConfigDigestMu.Unlock()
	s.agentConfigDigestPending = rep.Digest
	return rep.Digest != s.agentConfigDigest
}

// commitAgentConfigDigest promotes the pending digest to delivered.
func (s *Store) commitAgentConfigDigest() {
	s.agentConfigDigestMu.Lock()
	defer s.agentConfigDigestMu.Unlock()
	if s.agentConfigDigestPending != "" {
		s.agentConfigDigest = s.agentConfigDigestPending
	}
}

// BuildAgentConfigStateReport is the pure row mapping composeAgentConfigState
// uses (exported for the dashboard's "what the org sees" view).
func BuildAgentConfigStateReport(version int64, rows []AgentConfigOrgStateRow, includeValues bool) *orgcontract.AgentConfigStateReport {
	rep := &orgcontract.AgentConfigStateReport{PolicyVersion: version, Rows: []orgcontract.AgentConfigStateRow{}}
	// Several local clones of one project collapse onto one wire row per
	// (setting, file, project); the worst state wins so drift is never hidden.
	byKey := map[string]int{}
	for _, r := range rows {
		w := orgcontract.AgentConfigStateRow{
			Tool: r.Tool, Setting: r.SettingID, FileID: r.FileID, Scope: r.Scope, ProjectHash: r.ProjectHash,
			Mode: r.Mode, Layer: r.Layer, State: r.State, Reason: orgcontract.NormalizeAgentConfigReason(r.Reason),
			ExpectedHash: r.ExpectedHash, ObservedHash: r.ObservedHash, DriftCount: len(r.DriftTimes),
			LastDriftAt: stampOrEmpty(r.LastDriftAt), ObservedAt: stampOrEmpty(r.ObservedAt),
		}
		if includeValues && len(r.ObservedValue) <= orgcontract.AgentConfigObservedMaxBytes {
			w.ObservedValue = wireObservedValue(r.ObservedValue)
		}
		k := w.Setting + "\x00" + w.FileID + "\x00" + w.ProjectHash
		if i, seen := byKey[k]; seen {
			if stateRank(w.State) > stateRank(rep.Rows[i].State) {
				rep.Rows[i] = w
			}
			continue
		}
		byKey[k] = len(rep.Rows)
		rep.Rows = append(rep.Rows, w)
	}
	sort.Slice(rep.Rows, func(i, j int) bool {
		a, b := rep.Rows[i], rep.Rows[j]
		if a.Setting != b.Setting {
			return a.Setting < b.Setting
		}
		if a.FileID != b.FileID {
			return a.FileID < b.FileID
		}
		return a.ProjectHash < b.ProjectHash
	})
	if len(rep.Rows) > orgcontract.AgentConfigStateMaxRows {
		rep.Rows, rep.Truncated = rep.Rows[:orgcontract.AgentConfigStateMaxRows], true
	}
	rep.Digest = orgcontract.DigestAgentConfigState(rep.Rows)
	return rep
}

// wireObservedValue is the value as it may leave the node: re-scrubbed
// through the JSON-safe scrubber (a backstop; the writer already scrubbed
// it), and dropped when a pattern-certain secret still survives.
func wireObservedValue(v string) string {
	if v == "" {
		return ""
	}
	out := string(agentConfigWireScrubber.ScrubForward([]byte(v)))
	if certainSecret(out) {
		out, _ = scrub.MaskSecrets(out, func(f scrub.TypedFinding) bool { return f.Certain })
		if certainSecret(out) {
			return ""
		}
	}
	return out
}

func certainSecret(v string) bool {
	for _, f := range scrub.DetectSecrets(v) {
		if f.Certain {
			return true
		}
	}
	return false
}

var agentConfigWireScrubber = scrub.New()

// stateRank orders states by how much the org needs to see them.
func stateRank(s string) int {
	switch s {
	case "backed_off":
		return 6
	case "drifted":
		return 5
	case "reasserted":
		return 4
	case "refused_by_hygiene":
		return 3
	case "developer_value":
		return 2
	case "applied":
		return 1
	}
	return 0
}
