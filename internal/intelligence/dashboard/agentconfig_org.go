package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/marmutapp/superbased-observer/internal/agentconfigorg"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// The Organization tab of the AI tool settings page (adapter config P6/P7):
// the org's verified AI tool policy as this node holds it, and what the node
// did with each directive on each file. Local, like the rest of the page:
// the rows carry this machine's project roots.

// agentConfigOrgDirective is one directive as the page shows it.
type agentConfigOrgDirective struct {
	Setting   string          `json:"setting"`
	Value     json.RawMessage `json:"value"`
	Mode      string          `json:"mode"`
	Layer     string          `json:"layer"`
	Policy    string          `json:"policy"`
	Common    string          `json:"common,omitempty"`
	Project   string          `json:"project,omitempty"`
	Placement string          `json:"placement,omitempty"`
}

// agentConfigOrgRow is one target's state.
type agentConfigOrgRow struct {
	Setting string `json:"setting"`
	Tool    string `json:"tool"`
	FileID  string `json:"file_id"`
	// Scope is the file row's scope; project_shared means the write can be
	// committed with the repository (T-431).
	Scope          string `json:"scope,omitempty"`
	ProjectRoot    string `json:"project_root,omitempty"`
	Mode           string `json:"mode"`
	Layer          string `json:"layer"`
	Policy         string `json:"policy"`
	State          string `json:"state"`
	Reason         string `json:"reason,omitempty"`
	ObservedValue  string `json:"observed_value,omitempty"`
	ObservedSecret bool   `json:"observed_secret,omitempty"`
	Personal       bool   `json:"personal,omitempty"`
	Drift          bool   `json:"drift"`
	DriftCount     int    `json:"drift_count"`
	LastAppliedAt  string `json:"last_applied_at,omitempty"`
	LastDriftAt    string `json:"last_drift_at,omitempty"`
	ObservedAt     string `json:"observed_at,omitempty"`
}

// agentConfigOrgView is GET /api/agent-config/org.
type agentConfigOrgView struct {
	// Managed: a managed enrolment with a live grant (the policy applies).
	Managed         bool                      `json:"managed"`
	HasPolicy       bool                      `json:"has_policy"`
	Version         int64                     `json:"version,omitempty"`
	ReceivedAt      string                    `json:"received_at,omitempty"`
	OnDrift         string                    `json:"on_drift,omitempty"`
	DeveloperAccess string                    `json:"developer_access,omitempty"`
	SecretsPosture  string                    `json:"secrets_posture,omitempty"`
	Grants          []string                  `json:"grants"`
	Policies        []string                  `json:"policies"`
	Directives      []agentConfigOrgDirective `json:"directives"`
	// Instructions are the org instruction blocks (D-107, T-426) this
	// machine adds to its AI tools' instruction files.
	Instructions []orgcontract.AgentConfigInstruction `json:"instructions"`
	Rows         []agentConfigOrgRow                  `json:"rows"`
	Drifted      int                                  `json:"drifted"`
}

func stampRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (s *Server) handleAgentConfigOrg(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	v := agentConfigOrgView{
		Grants: []string{}, Policies: []string{}, Directives: []agentConfigOrgDirective{},
		Instructions: []orgcontract.AgentConfigInstruction{}, Rows: []agentConfigOrgRow{},
	}
	if s.opts.Governance != nil {
		v.Managed = s.opts.Governance(ctx).ManagedDefaultsApply()
	}
	if s.db() == nil {
		writeJSON(w, v)
		return
	}
	st := store.New(s.db())
	if body, row, ok := st.OrgAgentConfigBody(ctx); ok {
		v.HasPolicy, v.Version, v.ReceivedAt = true, row.Version, stampRFC3339(row.ReceivedAt)
		v.OnDrift = orgcontract.NormalizeAgentConfigOnDrift(body.OnDrift)
		v.DeveloperAccess = orgcontract.NormalizeAgentConfigAccess(body.DeveloperAccess)
		v.SecretsPosture = orgcontract.NormalizeAgentConfigSecrets(body.SecretsPosture)
		for _, g := range body.Grants {
			v.Grants = append(v.Grants, g.Setting)
		}
		v.Policies = append(v.Policies, body.Policies...)
		for _, d := range body.Directives {
			v.Directives = append(v.Directives, agentConfigOrgDirective(d))
		}
		v.Instructions = append(v.Instructions, body.Instructions...)
	}
	rows, err := st.CurrentAgentConfigOrgState(ctx)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "state_unreadable", "message": "The org policy state could not be read."})
		return
	}
	for _, sr := range rows {
		drift := agentconfigorg.IsDrift(agentconfigorg.State(sr.State)) && sr.State != string(agentconfigorg.StateReasserted)
		if drift {
			v.Drifted++
		}
		v.Rows = append(v.Rows, agentConfigOrgRow{
			Setting: sr.SettingID, Tool: sr.Tool, FileID: sr.FileID, Scope: sr.Scope, ProjectRoot: sr.ProjectRoot, Mode: sr.Mode, Layer: sr.Layer,
			Policy: sr.PolicyName, State: sr.State, Reason: sr.Reason, ObservedValue: sr.ObservedValue, ObservedSecret: sr.ObservedSecret,
			Personal: sr.Personal, Drift: drift, DriftCount: len(sr.DriftTimes), LastAppliedAt: stampRFC3339(sr.LastAppliedAt),
			LastDriftAt: stampRFC3339(sr.LastDriftAt), ObservedAt: stampRFC3339(sr.ObservedAt),
		})
	}
	writeJSON(w, v)
}

// handleAgentConfigOrgApply applies the cached org policy now (the daemon
// also applies it on every verified change and once a minute).
func (s *Server) handleAgentConfigOrgApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !requireConfirmToken(w, r) {
		return
	}
	ctx := r.Context()
	if s.opts.Governance == nil || !s.opts.Governance(ctx).ManagedDefaultsApply() {
		writeJSONStatus(w, http.StatusConflict, map[string]string{
			"error": "not_managed", "message": "This machine is not managed by an organization, so there is no org AI tool policy to apply.",
		})
		return
	}
	if s.db() == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "no_database", "message": "Observer's database is not available."})
		return
	}
	res, err := s.agentConfigServiceFor("org").SyncOrg(ctx, store.New(s.db()))
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "apply_failed", "message": "The org policy could not be applied."})
		return
	}
	if !res.HasPolicy {
		writeJSONStatus(w, http.StatusConflict, map[string]string{
			"error": "no_policy", "message": "Your organization has not sent an AI tool policy to this machine yet.",
		})
		return
	}
	s.recordManageAudit(r, "agent_config_org_apply", fmt.Sprintf("version=%d writes=%d drift=%d", res.Version, res.Writes, res.DriftEvents))
	writeJSON(w, res)
}
