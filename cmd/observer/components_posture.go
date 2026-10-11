package main

import (
	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// components_posture.go composes the node's component posture for the org
// wire (modular packaging plan 6.4, T-682). The posture is resolved the same
// way every other component surface is (componentSnapshotForConfig: the
// governed resolver over the signed inputs), so the org sees exactly what
// `observer components status` says. Only enums leave: see
// orgcontract.ComponentPostureRow.

// componentPostureProvider returns the store seam. Each push re-reads the
// config (falling back to the start-time copy when it does not load) and
// re-resolves, so a [components] edit or a policy, grant or ceiling change
// reaches the org on the next push.
func componentPostureProvider(configPath string, fallback config.Config) store.ComponentPostureProvider {
	return func() (orgcontract.ComponentPostureRow, bool) {
		cfg := fallback
		if loaded, err := config.Load(config.LoadOptions{GlobalPath: configPath}); err == nil {
			cfg = loaded
		}
		return componentPostureRow(componentSnapshotForConfig(cfg)), true
	}
}

// componentPostureRow maps a resolved snapshot onto the wire row (pure).
func componentPostureRow(s componentstate.Snapshot) orgcontract.ComponentPostureRow {
	active := s.ActiveIDs()
	row := orgcontract.ComponentPostureRow{
		Distro:           string(components.DistroOf(s.Compiled)),
		CatalogVersion:   components.CatalogVersion,
		ComponentsDigest: components.Digest(s.Compiled),
		ActiveDigest:     components.Digest(active),
		CompiledCount:    len(s.Compiled),
		ActiveCount:      len(active),
		GovernanceSource: s.Governed.Source,
		OrgManaged:       s.Governed.OrgManaged,
		CeilingPinned:    s.Governed.Governance != nil && s.Governed.Governance.CeilingPinned,
	}
	for _, d := range s.Result.Decisions {
		if d.State == components.StateActive {
			continue
		}
		row.Inactive = append(row.Inactive, orgcontract.ComponentStateEntry{
			ID: d.ID, State: string(d.State), Reason: string(d.Reason),
		})
	}
	return orgcontract.NormalizeComponentPosture(row, func(id string) bool {
		c, ok := components.Get(id)
		return ok && c.Binary == components.BinaryNode
	})
}
