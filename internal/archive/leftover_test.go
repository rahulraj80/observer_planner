package archive

import (
	"reflect"
	"strings"
	"testing"
)

func TestMatchLeftovers(t *testing.T) {
	files := []LeftoverFile{
		{Name: "observer.db", Bytes: 100},
		{Name: "observer.db-wal", Bytes: 5},
		{Name: "observer.db.pre-reclaim-20260917T043021Z-wal", Bytes: 2},
		{Name: "observer.db.pre-reclaim-20260917T043021Z", Bytes: 300},
		{Name: "observer.db.pre-reclaim-20260917T043021Z-shm", Bytes: 1},
		{Name: "observer.db.pre-reclaim-20260101T000000Z", Bytes: 50},
		{Name: "observer.db.pre-reclaim-latest", Bytes: 999},
		{Name: "observer.db.pre-reclaim-20260917T043021Z.bak", Bytes: 999},
		{Name: "other.db.pre-reclaim-20260917T043021Z", Bytes: 999},
		{Name: "backups", Bytes: 0},
	}
	got, inProgress := MatchLeftovers("observer.db", files)
	if inProgress {
		t.Fatal("no reclaim copy is in the listing")
	}
	want := []ReclaimLeftover{
		{Stamp: "20260101T000000Z", Bytes: 50, Files: []LeftoverFile{{Name: "observer.db.pre-reclaim-20260101T000000Z", Bytes: 50}}},
		{Stamp: "20260917T043021Z", Bytes: 303, Files: []LeftoverFile{
			{Name: "observer.db.pre-reclaim-20260917T043021Z", Bytes: 300},
			{Name: "observer.db.pre-reclaim-20260917T043021Z-shm", Bytes: 1},
			{Name: "observer.db.pre-reclaim-20260917T043021Z-wal", Bytes: 2},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MatchLeftovers =\n%+v\nwant\n%+v", got, want)
	}
	if _, ip := MatchLeftovers("observer.db", append(files, LeftoverFile{Name: "observer.db.reclaim-20261007T101010Z"})); !ip {
		t.Fatal("a .reclaim-<stamp> copy must report a reclaim in progress")
	}
	if got, _ := MatchLeftovers("", files); got != nil {
		t.Fatalf("an empty db name matched %+v", got)
	}
}

// TestPlanLeftoverCleanup: one case per row of cleanupRules, in order, plus
// the unmeasured quick_check.
func TestPlanLeftoverCleanup(t *testing.T) {
	one := []ReclaimLeftover{{Stamp: "20260917T043021Z", Bytes: 300}}
	cases := []struct {
		name string
		in   CleanupInput
		want CleanupDecision
		has  string
	}{
		{"nothing", CleanupInput{LivePresent: true, LiveQuickCheck: "ok"}, CleanupNothing, "no reclaim leftovers"},
		{"reclaim in progress", CleanupInput{Leftovers: one, InProgress: true, LivePresent: true, LiveQuickCheck: "ok"}, CleanupInProgress, "still next to the database"},
		{"no live database", CleanupInput{Leftovers: one}, CleanupLiveMissing, "only copy"},
		{"live database damaged", CleanupInput{Leftovers: one, LivePresent: true, LiveQuickCheck: "*** in database main ***"}, CleanupLiveUnhealthy, "in database main"},
		{"live database not checked", CleanupInput{Leftovers: one, LivePresent: true}, CleanupLiveUnhealthy, "could not be run"},
		{"ready", CleanupInput{Leftovers: one, LivePresent: true, LiveQuickCheck: "ok"}, CleanupReady, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := PlanLeftoverCleanup(c.in)
			if p.Decision != c.want || !strings.Contains(p.Reason, c.has) {
				t.Fatalf("plan = %+v, want %s with %q", p, c.want, c.has)
			}
			if p.OK() != (c.want == CleanupReady) {
				t.Fatalf("OK() = %v", p.OK())
			}
			if len(c.in.Leftovers) > 0 && p.Bytes != 300 {
				t.Fatalf("bytes = %d", p.Bytes)
			}
		})
	}
	if len(cases) != len(cleanupRules)+1 {
		t.Fatalf("%d cases for %d rules (+1 for the unmeasured check)", len(cases), len(cleanupRules))
	}
}
