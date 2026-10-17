package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/orgclient"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestOrgBudgetWireIdentityReadFailureKeepsLastBudget is REVIEW-4 P4-01
// (HIGH), ruling D-521 "keep last budget": on an enrolled managed node whose
// verified org cap is exceeded, a FAILED enrolment identity read (here: the
// store is closed, standing in for a busy pool, a checkpoint or the 2 s
// deadline) must not lift the cap. Before the fix the failure collapsed into
// "no live enrolment" and the request was admitted with no budget check.
func TestOrgBudgetWireIdentityReadFailureKeepsLastBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, dbPath := writeManagedBudgetLaunchFixture(t, false, "enforce")
	database, err := dbtemplate.Open(ctx, db.Options{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = database.Close()
		}
	}()
	st := store.New(database)
	identity, active, err := orgclient.CurrentBudgetIdentity(ctx, st)
	if err != nil || !active {
		t.Fatalf("current budget identity: active=%v err=%v", active, err)
	}
	cfg := config.Default().Guard
	cfg.Enabled, cfg.Mode = true, "enforce"
	gd, err := guard.New(guard.Options{Config: cfg, Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	gd.SetBudgetLookup(func(string) (guard.BudgetSnapshot, bool) {
		return guard.BudgetSnapshot{MonthlyTokens: 150}, true
	})
	h := newOrgBudgetHandle(gd, config.GuardBudgetConfig{FromOrg: true},
		func() bool { return true }, st, slog.Default())
	current := budgetOutcome(100, orgcontract.BudgetPolicyEnforcementHard)
	current.Binding = identity.Binding
	h.apply(current)

	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	if d := gd.ScanProxyRequest("openai", []byte(`{"model":"test"}`), "", now); !d.Deny {
		t.Fatalf("precondition: an exceeded verified org cap was admitted while the identity read worked: %+v", d)
	}

	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	if d := gd.ScanProxyRequest("openai", []byte(`{"model":"test"}`), "", now); !d.Deny {
		t.Fatalf("a failed identity read lifted the exceeded org cap: %+v", d)
	}
}
