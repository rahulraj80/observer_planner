package agentconfigsvc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// OrgStore is the node store seam SyncOrg reads and writes: the cached
// verified policy of the CURRENT enrolment (a row signed for an earlier org
// or member reads as absent, review M3-01), the per-target state (one owner:
// store/agentconfigorg.go) and the project identities of the local clones.
type OrgStore interface {
	CurrentOrgAgentConfigPolicy(ctx context.Context) (store.OrgAgentConfigPolicyRow, bool, error)
	LoadAgentConfigOrgState(ctx context.Context) ([]store.AgentConfigOrgStateRow, error)
	ProjectRootsByRemoteHash(ctx context.Context) (map[string][]string, error)
	ReplaceAgentConfigOrgState(ctx context.Context, rows []store.AgentConfigOrgStateRow) error
}

// OrgSyncResult is what one sync did.
type OrgSyncResult struct {
	// HasPolicy is false when no verified policy is cached (nothing ran).
	HasPolicy   bool  `json:"has_policy"`
	Version     int64 `json:"version"`
	Writes      int   `json:"writes"`
	DriftEvents int   `json:"drift_events"`
	Targets     int   `json:"targets"`
	// Rows is the new complete state (node-local; carries project roots).
	Rows []store.AgentConfigOrgStateRow `json:"-"`
}

// orgSyncMu serializes the sync inside one process (the daemon's sweep and
// the dashboard's "apply now" share it). Across processes each file write
// still takes the vendor-file lock, and the state table is replaced whole.
var orgSyncMu sync.Mutex

// SyncOrg applies the cached org policy and records the state: the ONE
// entry the daemon's sweep, the dashboard and the CLI call. The caller has
// already decided the node is managed with a live grant. A signed EMPTY body
// (the withdrawal) yields no targets, so the state empties and every lock
// lifts; the values the org wrote stay in the files, now the developer's.
func (s *Service) SyncOrg(ctx context.Context, st OrgStore) (OrgSyncResult, error) {
	orgSyncMu.Lock()
	defer orgSyncMu.Unlock()
	pol, ok, err := st.CurrentOrgAgentConfigPolicy(ctx)
	if err != nil {
		return OrgSyncResult{}, fmt.Errorf("agentconfigsvc.SyncOrg: policy: %w", err)
	}
	if !ok {
		return OrgSyncResult{}, nil
	}
	body, err := orgcontract.DecodeAgentConfigPolicyBody(pol.Body)
	if err != nil {
		return OrgSyncResult{}, fmt.Errorf("agentconfigsvc.SyncOrg: body: %w", err)
	}
	prior, err := st.LoadAgentConfigOrgState(ctx)
	if err != nil {
		return OrgSyncResult{}, fmt.Errorf("agentconfigsvc.SyncOrg: state: %w", err)
	}
	roots, err := st.ProjectRootsByRemoteHash(ctx)
	if err != nil {
		return OrgSyncResult{}, fmt.Errorf("agentconfigsvc.SyncOrg: projects: %w", err)
	}
	now := s.o.Now().UTC().Truncate(time.Second)
	var grants []string
	if s.o.EditGrants != nil {
		grants = s.o.EditGrants(ctx, now)
	}
	res := s.ApplyOrg(ctx, OrgApplyInput{Body: body, Version: pol.Version, Prior: prior, ProjectRoots: roots, EditGrants: grants, Now: now})
	if err := st.ReplaceAgentConfigOrgState(ctx, res.Rows); err != nil {
		return OrgSyncResult{}, fmt.Errorf("agentconfigsvc.SyncOrg: record: %w", err)
	}
	return OrgSyncResult{
		HasPolicy: true, Version: pol.Version, Writes: res.Writes, DriftEvents: res.DriftEvents,
		Targets: len(res.Rows), Rows: res.Rows,
	}, nil
}
