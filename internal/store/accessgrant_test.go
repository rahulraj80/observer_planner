package store

import (
	"context"
	"testing"
)

// TestGuardEventGrantIDShipsOnThePush: the one grant fact that leaves the
// node is guard_events.grant_id, as metadata on the existing row (zero, and so
// omitted on the wire, when no grant covered the event).
func TestGuardEventGrantIDShipsOnThePush(t *testing.T) {
	t.Parallel()
	s, db := newTestStore(t)
	ctx := context.Background()
	if _, err := s.InsertGuardEvents(ctx, []GuardEventRow{
		guardEventFixture("sA", "R-171", t0Guard),
		guardEventFixture("sA", "R-172", t0Guard),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE guard_events SET grant_id = 12 WHERE rule_id = 'R-171'`); err != nil {
		t.Fatal(err)
	}
	batch, err := s.SelectUnpushedSince(ctx, PushCursor{}, 1<<20, "org-1", "dev@acme.example", ShareOptions{}, ScopeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, g := range batch.GuardEvents {
		got[g.RuleID] = g.GrantID
	}
	if got["R-171"] != 12 || got["R-172"] != 0 {
		t.Fatalf("grant ids on the wire = %v", got)
	}
}
