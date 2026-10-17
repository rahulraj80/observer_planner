package main

import (
	"log/slog"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/orgclient"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestLiveBudgetFailClosed pins the live fail_closed resolver (REVIEW-4
// P4-04): the node's own value, or the CURRENT governance pin, never a pin
// captured at daemon start. Not run in fast mode (D-507).
func TestLiveBudgetFailClosed(t *testing.T) {
	pin := func(m map[string]any) func() map[string]any { return func() map[string]any { return m } }
	cases := []struct {
		name   string
		own    bool
		pinned func() map[string]any
		want   bool
	}{
		{name: "own value on", own: true, pinned: pin(nil), want: true},
		{name: "no resolver", own: false, pinned: nil, want: false},
		{name: "no governance in force (after a leave)", own: false, pinned: pin(nil), want: false},
		{name: "current org pins it", own: false, pinned: pin(map[string]any{budgetFailClosedKey: true}), want: true},
		{name: "current org pins something else", own: false, pinned: pin(map[string]any{"guard.mode": "enforce"}), want: false},
	}
	for _, tc := range cases {
		if got := liveBudgetFailClosed(tc.own, tc.pinned)(); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestOrgBudgetHandleFailClosedFollowsLiveGovernance: the daemon was started
// under a left organization's fail_closed pin (local.FailClosed = true); once
// the governance pin is gone the next composition publishes fail_closed off,
// so the guard's replaced_fail_closed row cannot arm B-625 for the next org.
func TestOrgBudgetHandleFailClosedFollowsLiveGovernance(t *testing.T) {
	t.Parallel()
	cfg := config.Default().Guard
	cfg.Enabled, cfg.Mode = true, "enforce"
	gd, err := guard.New(guard.Options{Config: cfg, Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	pinned := map[string]any{budgetFailClosedKey: true}
	h := newOrgBudgetHandle(gd, config.GuardBudgetConfig{FromOrg: true, FailClosed: true}, nil, nil, slog.Default())
	h.failClosed = liveBudgetFailClosed(false, func() map[string]any { return pinned })
	h.apply(orgclient.BudgetFetchOutcome{State: orgcontract.BudgetFetchNotEnrolled})
	if !gd.EffectiveBudget().FailClosed {
		t.Fatal("the current governance pin was not applied")
	}
	pinned = nil // the leave removed the governance state
	h.apply(orgclient.BudgetFetchOutcome{State: orgcontract.BudgetFetchNotEnrolled})
	if gd.EffectiveBudget().FailClosed {
		t.Fatal("a left organization's fail_closed pin outlived its governance")
	}
}
