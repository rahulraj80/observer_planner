package store

import (
	"context"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestScheduledPurgeEntriesRows pins the reduction of scheduled plans to the
// org-visible list (T-711), one case per rule.
func TestScheduledPurgeEntriesRows(t *testing.T) {
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	ts := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	day := 24 * time.Hour
	for _, tc := range []struct {
		name  string
		plans []ComponentPurgePlan
		want  []orgcontract.ComponentPurgeEntry
	}{
		{"no plans reports an empty list", nil, []orgcontract.ComponentPurgeEntry{}},
		{"interactive plans never leave", []ComponentPurgePlan{
			{ID: "a", Kind: PurgeInteractive, Component: "adapter.zed", State: PurgePreviewed, CreatedAt: now},
		}, []orgcontract.ComponentPurgeEntry{}},
		{"an open plan reports its due date", []ComponentPurgePlan{
			{ID: "a", Kind: PurgeScheduled, Component: "adapter.zed", State: PurgeGracePending, CreatedAt: now, GraceUntil: ts(9 * day)},
		}, []orgcontract.ComponentPurgeEntry{{ID: "adapter.zed", State: "grace_pending", DueOn: "2026-10-17"}}},
		{"the latest plan per component wins", []ComponentPurgePlan{
			{ID: "a", Kind: PurgeScheduled, Component: "adapter.zed", State: PurgeCancelled, CreatedAt: now.Add(-2 * day), GraceUntil: ts(5 * day)},
			{ID: "b", Kind: PurgeScheduled, Component: "adapter.zed", State: PurgeEligible, CreatedAt: now.Add(-day), GraceUntil: ts(-time.Hour)},
		}, []orgcontract.ComponentPurgeEntry{{ID: "adapter.zed", State: "eligible", DueOn: "2026-10-08"}}},
		{"a recently consumed plan stays", []ComponentPurgePlan{
			{ID: "a", Kind: PurgeScheduled, Component: "adapter.zed", State: PurgeConsumed, CreatedAt: now.Add(-60 * day), GraceUntil: ts(-50 * day), ConsumedAt: ts(-2 * day)},
		}, []orgcontract.ComponentPurgeEntry{{ID: "adapter.zed", State: "consumed", DueOn: "2026-08-19"}}},
		{"a finished plan past the window drops off", []ComponentPurgePlan{
			{ID: "a", Kind: PurgeScheduled, Component: "adapter.zed", State: PurgeConsumed, CreatedAt: now.Add(-60 * day), GraceUntil: ts(-50 * day), ConsumedAt: ts(-31 * day)},
			{ID: "b", Kind: PurgeScheduled, Component: "adapter.cline", State: PurgeExpired, CreatedAt: now.Add(-60 * day), GraceUntil: ts(-50 * day), ExpiresAt: now.Add(-49 * day)},
		}, []orgcontract.ComponentPurgeEntry{}},
		{"a recent cancellation stays, sorted by id", []ComponentPurgePlan{
			{ID: "a", Kind: PurgeScheduled, Component: "adapter.zed", State: PurgeCancelled, CreatedAt: now.Add(-day), GraceUntil: ts(6 * day)},
			{ID: "b", Kind: PurgeScheduled, Component: "adapter.cline", State: PurgeGracePending, CreatedAt: now, GraceUntil: ts(7 * day)},
		}, []orgcontract.ComponentPurgeEntry{
			{ID: "adapter.cline", State: "grace_pending", DueOn: "2026-10-15"},
			{ID: "adapter.zed", State: "cancelled", DueOn: "2026-10-14"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := scheduledPurgeEntries(tc.plans, now)
			if got == nil {
				t.Fatal("the list must never be nil")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %+v, want %+v", got, tc.want)
				}
			}
		})
	}
}

// TestComposeComponentPostureCarriesScheduledPurges pins the seam end to end
// on a real store (T-711): a scheduled plan reaches the posture row as an
// enum entry, a change of its state forces a push, and a store without a
// database reports nothing (nil, never an empty list).
func TestComposeComponentPostureCarriesScheduledPurges(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	s.SetComponentPostureProvider(func() (orgcontract.ComponentPostureRow, bool) {
		return orgcontract.ComponentPostureRow{Distro: "full", CatalogVersion: 1}, true
	})
	row, _ := s.composeComponentPosture(ctx)
	if row == nil || row.ScheduledPurges == nil || len(*row.ScheduledPurges) != 0 {
		t.Fatalf("no plan must report an empty, non-nil list: %+v", row)
	}
	s.commitComponentPostureDigest()
	plan, err := s.CreateComponentPurgePlan(ctx, ComponentPurgeSpec{Kind: PurgeScheduled, Component: "adapter.zed", PolicyVersion: "4"})
	if err != nil {
		t.Fatal(err)
	}
	row, changed := s.composeComponentPosture(ctx)
	if !changed || row.ScheduledPurges == nil || len(*row.ScheduledPurges) != 1 {
		t.Fatalf("a new plan must force a push: changed=%v row=%+v", changed, row)
	}
	if e := (*row.ScheduledPurges)[0]; e.ID != "adapter.zed" || e.State != "grace_pending" || e.DueOn != plan.GraceUntil.UTC().Format("2006-01-02") {
		t.Fatalf("entry = %+v", e)
	}
	s.commitComponentPostureDigest()
	if err := s.CancelComponentPurgePlan(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if row, changed := s.composeComponentPosture(ctx); !changed || (*row.ScheduledPurges)[0].State != "cancelled" {
		t.Fatalf("a cancellation must force a push: changed=%v row=%+v", changed, row)
	}

	bare := &Store{}
	bare.SetComponentPostureProvider(func() (orgcontract.ComponentPostureRow, bool) {
		return orgcontract.ComponentPostureRow{Distro: "full"}, true
	})
	if row, _ := bare.composeComponentPosture(ctx); row == nil || row.ScheduledPurges != nil {
		t.Fatalf("a store without a database must not report purges: %+v", row)
	}
}
