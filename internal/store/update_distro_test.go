package store

import (
	"context"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/policyfam/nodecomponents"
	"github.com/marmutapp/superbased-observer/internal/update"
)

// update_distro_test.go pins agent migration 161 (lane MP-REL2, T-687): the
// legacy defaults every pre-modular node reads, the round trip the apply
// path depends on, and that a row built from scratch never resets them.

func TestMigration161LegacyDefaults(t *testing.T) {
	s := updateTestStore(t)
	row, err := s.LoadUpdateState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	legacy := nodecomponents.LegacyDefaults()
	if row.CurrentDistro != string(legacy.Distro) || row.DistroEpoch(update.ChannelStable) != legacy.DistroEpoch {
		t.Fatalf("fresh node: distro %q epoch %d, want the legacy defaults %+v", row.CurrentDistro, row.DistroEpoch(update.ChannelStable), legacy)
	}
	if row.CurrentDistro != string(components.DistroFull) || row.ComponentsDigest != "" || len(row.DistroEpochs) != 0 {
		t.Fatalf("fresh node: %+v", row)
	}
}

func TestDistroStateRoundTripsAndSurvivesAScratchRow(t *testing.T) {
	s := updateTestStore(t)
	ctx := context.Background()
	err := s.TransitionUpdateState(ctx, update.StateAvailable, func(r *UpdateStateRow) {
		r.CurrentDistro = "minimal"
		r.ComponentsDigest = "abc"
		r.DistroEpochs = map[string]int64{"stable": 3, "edge": 1}
	}, UpdateEventRow{})
	if err != nil {
		t.Fatal(err)
	}
	row, _ := s.LoadUpdateState(ctx)
	if row.CurrentDistro != "minimal" || row.ComponentsDigest != "abc" || row.DistroEpoch(update.ChannelStable) != 3 || row.DistroEpoch(update.ChannelLTS) != 0 {
		t.Fatalf("round trip: %+v", row)
	}
	// A caller that builds a row from scratch (no distro fields) must not
	// reset what the last apply recorded.
	if err := s.SaveUpdateState(ctx, UpdateStateRow{NodeState: update.NodeState{State: update.StateIdle}}); err != nil {
		t.Fatal(err)
	}
	row, _ = s.LoadUpdateState(ctx)
	if row.CurrentDistro != "minimal" || row.DistroEpoch(update.ChannelEdge) != 1 {
		t.Fatalf("a scratch row reset the distro state: %+v", row)
	}
}

func TestDistroStateToleratesAPre161Database(t *testing.T) {
	s := updateTestStore(t)
	ctx := context.Background()
	for _, c := range []string{"current_distro", "components_digest", "distro_epochs"} {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE update_state DROP COLUMN `+c); err != nil {
			t.Fatal(err)
		}
	}
	row, err := s.LoadUpdateState(ctx)
	if err != nil || row.State != update.StateIdle || row.CurrentDistro != "full" {
		t.Fatalf("pre-161 load: %+v %v", row, err)
	}
	row.CurrentDistro = "minimal"
	if err := s.SaveUpdateState(ctx, row); err != nil {
		t.Fatalf("pre-161 save must not fail on the missing columns: %v", err)
	}
}
