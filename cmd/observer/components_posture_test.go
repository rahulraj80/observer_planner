package main

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestComponentPostureRowFromSnapshot pins the node half of the component
// posture (T-682): the row carries the compiled and active digests of the
// resolved snapshot, and every non-active component with its state and
// reason, and nothing else.
func TestComponentPostureRowFromSnapshot(t *testing.T) {
	requireCompiled(t, "remote")
	snap := componentstate.Resolve(config.ComponentsConfig{Remove: []string{"remote"}}, nil)
	row := componentPostureRow(snap)
	if row.CatalogVersion != components.CatalogVersion || row.ComponentsDigest != components.Digest(snap.Compiled) {
		t.Fatalf("header = %+v", row)
	}
	if row.ActiveDigest != components.Digest(snap.ActiveIDs()) || row.ActiveCount != len(snap.ActiveIDs()) {
		t.Fatalf("active digest/count mismatch: %+v", row)
	}
	var found bool
	for _, e := range row.Inactive {
		if e.ID == "remote" {
			found = e.State == string(components.StateBlocked) || e.State == string(components.StateAvailable)
		}
		if e.ID == "core" {
			t.Fatal("core is always active and never listed")
		}
	}
	if !found {
		t.Fatalf("removed remote not listed inactive: %+v", row.Inactive)
	}
	if row.OrgManaged || row.GovernanceSource != "" {
		t.Fatalf("an ungoverned snapshot reports no governance: %+v", row)
	}
}
