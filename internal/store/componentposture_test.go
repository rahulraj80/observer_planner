package store

import (
	"context"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestComposeComponentPostureChangeTracking pins the component posture seam
// (T-682): no provider composes nothing (byte-identical envelope); a posture
// forces a push until it is delivered; an unchanged one rides along without
// forcing; a changed one forces again; values outside the vocabulary are
// normalized away before the wire.
func TestComposeComponentPostureChangeTracking(t *testing.T) {
	s := &Store{}
	if row, changed := s.composeComponentPosture(context.Background()); row != nil || changed {
		t.Fatal("no provider must compose nothing")
	}
	posture := orgcontract.ComponentPostureRow{
		Distro: "full", CatalogVersion: 1, GovernanceSource: "bogus",
		Inactive: []orgcontract.ComponentStateEntry{{ID: "remote", State: "available", Reason: "not_selected"}},
	}
	s.SetComponentPostureProvider(func() (orgcontract.ComponentPostureRow, bool) { return posture, true })
	row, changed := s.composeComponentPosture(context.Background())
	if row == nil || !changed || row.GovernanceSource != "" || len(row.Inactive) != 1 {
		t.Fatalf("first compose: row=%+v changed=%v", row, changed)
	}
	if _, changed := s.composeComponentPosture(context.Background()); !changed {
		t.Fatal("an undelivered posture must keep forcing a push")
	}
	s.commitComponentPostureDigest()
	if row, changed := s.composeComponentPosture(context.Background()); row == nil || changed {
		t.Fatalf("a delivered, unchanged posture rides along without forcing: row=%v changed=%v", row, changed)
	}
	// A delivered posture older than the resend bound forces again (a
	// re-enrolment to another org converges).
	s.componentPostureCommittedAt = time.Now().Add(-componentPostureResendEvery)
	if _, changed := s.composeComponentPosture(context.Background()); !changed {
		t.Fatal("a posture delivered longer ago than the resend bound must force a push")
	}
	s.commitComponentPostureDigest()
	posture.Distro = "developer"
	if _, changed := s.composeComponentPosture(context.Background()); !changed {
		t.Fatal("a changed posture must force a push")
	}
	s.SetComponentPostureProvider(func() (orgcontract.ComponentPostureRow, bool) { return orgcontract.ComponentPostureRow{}, false })
	if row, changed := s.composeComponentPosture(context.Background()); row != nil || changed {
		t.Fatal("ok=false must compose nothing")
	}
}
