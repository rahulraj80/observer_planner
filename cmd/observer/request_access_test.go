package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/accessgrant"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestResolveAccessDraft pins `observer request access`'s draft (request-
// grants G4). The guard's deny line offers `observer request access --rule
// R "<reason>"`, so a rule alone must yield a request the local pre-check
// accepts: rule-wide unless --breadth says otherwise (an exact request needs
// an operand, which only an event carries). --session resolves to that
// session's newest event for the rule, the dashboard deep link's own rule.
func TestResolveAccessDraft(t *testing.T) {
	ctx := context.Background()
	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "o.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	st := store.New(database)
	if _, err := st.InsertGuardEvents(ctx, []store.GuardEventRow{{
		TS: time.Now().UTC().Add(-time.Minute), SessionID: "sA", Tool: "codex", EventKind: "shell_exec", RuleID: "R-171",
		Category: "exfil", Severity: "high", Decision: "deny", Enforced: true, Source: "org", TargetHash: "h1",
	}}); err != nil {
		t.Fatal(err)
	}
	var evID int64
	if err := database.QueryRow(`SELECT id FROM guard_events`).Scan(&evID); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name                       string
		event                      int64
		rule, session, breadth     string
		breadthSet                 bool
		wantBreadth, wantOp, wantR string
		wantErr                    bool
	}{
		{name: "rule alone is rule-wide", rule: "R-171", wantBreadth: accessgrant.BreadthRule, wantR: "R-171"},
		{name: "rule and session take that session's event", rule: "R-171", session: "sA", wantBreadth: accessgrant.BreadthExact, wantOp: "h1", wantR: "R-171"},
		{name: "a session with no event stays rule-wide", rule: "R-171", session: "sZ", wantBreadth: accessgrant.BreadthRule, wantR: "R-171"},
		{name: "an explicit breadth wins", rule: "R-171", session: "sA", breadth: accessgrant.BreadthRule, breadthSet: true, wantBreadth: accessgrant.BreadthRule, wantR: "R-171"},
		{name: "an event is exact by default", event: evID, wantBreadth: accessgrant.BreadthExact, wantOp: "h1", wantR: "R-171"},
		{name: "an unknown event is an error", event: evID + 99, wantErr: true},
		{name: "nothing named is an error", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := resolveAccessDraft(ctx, st, tc.event, tc.rule, tc.session, tc.breadth, tc.breadthSet)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %+v", d.Payload)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if d.RequestType != accessgrant.TypeGuardExemption || d.Payload.RuleID != tc.wantR ||
				d.Payload.Breadth != tc.wantBreadth || d.Payload.OperandHash != tc.wantOp {
				t.Fatalf("draft %+v", d.Payload)
			}
		})
	}
}
