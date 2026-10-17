package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/orgclient"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// setBudgetLaunchFailClosed turns on [guard.budget].fail_closed in a fixture
// config written by writeManagedBudgetLaunchFixture (whose last section is
// [guard.budget]).
func setBudgetLaunchFailClosed(t *testing.T, cfgPath string) {
	t.Helper()
	f, err := os.OpenFile(cfgPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open fixture config: %v", err)
	}
	if _, err := f.WriteString("fail_closed = true\n"); err != nil {
		t.Fatalf("append fail_closed: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close fixture config: %v", err)
	}
}

// TestBudgetLaunchFencedLeaveIsNotEnrolled is REVIEW-4 P4-03 (first half): a
// leave that fenced the generation and then failed a later Required step
// leaves the managed enrolment row with a tombstoned generation and no grant.
// The proxy already reads that as "not enrolled"; the launcher must agree and
// start the tool instead of refusing every launch with "cannot verify the
// managed-budget launch state".
func TestBudgetLaunchFencedLeaveIsNotEnrolled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfgPath, dbPath := writeManagedBudgetLaunchFixture(t, true, "enforce")
	database, err := dbtemplate.Open(ctx, db.Options{Path: dbPath})
	if err != nil {
		t.Fatalf("reopen DB: %v", err)
	}
	st := store.New(database)
	orgKey := orgclient.OrgKey("https://org.example.test", "org-budget-launch-test")
	if _, err := st.BumpEnrolmentGeneration(ctx, orgKey, true); err != nil {
		t.Fatalf("tombstone generation: %v", err)
	}
	if err := st.DeleteAllEnrolmentGrants(ctx); err != nil {
		t.Fatalf("DeleteAllEnrolmentGrants: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}
	if err := enforceBudgetControlledLaunch(ctx, cfgPath, "muse",
		budgetLaunchEvidence{Route: budgetLaunchRouteDirect}); err != nil {
		t.Fatalf("a fenced leave refused the launch: %v", err)
	}
}

// TestBudgetLaunchExpiredManagedGrantFollowsFailClosed is REVIEW-4 P4-03
// (second half, ruling D-521 "follow fail_closed"): an expired managed grant
// is a reason outside the request. Without fail_closed the tool starts on the
// node's own caps; with it the launch is refused with the safe-state error.
func TestBudgetLaunchExpiredManagedGrantFollowsFailClosed(t *testing.T) {
	t.Parallel()
	for _, failClosed := range []bool{false, true} {
		cfgPath, dbPath := writeManagedBudgetLaunchFixture(t, true, "enforce")
		if failClosed {
			setBudgetLaunchFailClosed(t, cfgPath)
		}
		rewriteBudgetLaunchGrant(t, dbPath, func(grant *store.EnrolmentGrant) {
			grant.ExpiresAt = time.Now().UTC().Add(-time.Hour)
			grant.SignedExpiresAt = grant.ExpiresAt
		})
		err := enforceBudgetControlledLaunch(context.Background(), cfgPath, "muse",
			budgetLaunchEvidence{Route: budgetLaunchRouteDirect})
		if !failClosed {
			if err != nil {
				t.Fatalf("fail_closed=false: an expired managed grant refused the launch: %v", err)
			}
			continue
		}
		if err == nil || errors.Is(err, errBudgetLaunchUncontrolled) ||
			!strings.Contains(err.Error(), "managed enrolment grant is expired") {
			t.Fatalf("fail_closed=true: expired managed grant error = %v, want the safe-state refusal", err)
		}
	}
}
