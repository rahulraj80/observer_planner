package guard

import (
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/policy"
)

// TestOrgBudgetLivenessTable pins one case per budgetLivenessRules row
// (T-718, the B-625 fail-safe).
func TestOrgBudgetLivenessTable(t *testing.T) {
	const (
		unknown  = BudgetIdentityUnknown
		enrolled = BudgetIdentityEnrolled
		gone     = BudgetIdentityNotEnrolled
	)
	cases := []struct {
		row                    string
		hasLookup              bool
		state                  BudgetIdentityState
		current, engineBinding string
		failClosed             bool
		want                   budgetLiveness
	}{
		{row: "unbound_without_lookup", want: budgetLiveMatch},
		{row: "identity_unknown", hasLookup: true, state: unknown, engineBinding: "b1", want: budgetLiveMatch},
		{row: "identity_unknown (fail_closed does not matter)", hasLookup: true, state: unknown, engineBinding: "b1", failClosed: true, want: budgetLiveMatch},
		{row: "no_live_enrolment", hasLookup: true, state: gone, engineBinding: "b1", want: budgetLiveRetired},
		{row: "no_live_enrolment (fail_closed does not matter)", hasLookup: true, state: gone, engineBinding: "b1", failClosed: true, want: budgetLiveRetired},
		{row: "same_enrolment", hasLookup: true, state: enrolled, current: "b1", engineBinding: "b1", want: budgetLiveMatch},
		{row: "replaced_fail_closed", hasLookup: true, state: enrolled, current: "b2", engineBinding: "b1", failClosed: true, want: budgetLiveReplacedClosed},
		{row: "replaced_fail_open", hasLookup: true, state: enrolled, current: "b2", engineBinding: "b1", want: budgetLiveReplacedOpen},
		{row: "replaced_fail_open (no lookup, bound engine)", engineBinding: "b1", want: budgetLiveReplacedOpen},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		got := orgBudgetLiveness(tc.hasLookup, tc.state, tc.current, tc.engineBinding, tc.failClosed)
		if got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.row, got, tc.want)
		}
		seen[tc.row] = true
	}
	for _, r := range budgetLivenessRules {
		if !seen[r.name] {
			t.Errorf("rule %q has no test case", r.name)
		}
	}
}

// armedManagedGuard returns an enforcing guard whose org-composed engine is
// the fail-closed B-625 posture bound to enrolment "b1".
func armedManagedGuard(t *testing.T, failClosed bool) *Guard {
	t.Helper()
	cfg := guardCfg()
	cfg.Mode = "enforce"
	g := newTestGuard(t, cfg, nil)
	if err := g.ApplyOrgBudget(config.GuardBudgetConfig{Hard: true, FailClosed: failClosed}, nil, true, policy.BudgetProtection{}, "b1"); err != nil {
		t.Fatal(err)
	}
	return g
}

// TestB625AdmitsAfterUnenrol is the operator's 2026-10-09 report: after
// `observer unenroll` the daemon's engine still carried the managed
// requirement and refused every request with B-625. With no live enrolment
// the requirement is retired, fail_closed or not.
func TestB625AdmitsAfterUnenrol(t *testing.T) {
	for _, failClosed := range []bool{false, true} {
		g := armedManagedGuard(t, failClosed)
		g.SetBudgetBindingLookup(func() (string, bool) { return "", false })
		var res ProxyRequestResult
		g.scanBudget(g.set.Load(), &res, "s1", "https://api.anthropic.com/v1/messages", time.Now())
		if res.Deny {
			t.Fatalf("fail_closed=%v: unenrolled node refused a request: %s %s", failClosed, res.DenyRuleID, res.DenyReason)
		}
		d := g.CheckInterventionBudget(InterventionBudgetInput{SourceReady: true, Now: time.Now()})
		if d.Deny {
			t.Fatalf("fail_closed=%v: unenrolled node published a stop: %+v", failClosed, d)
		}
	}
}

// TestB625LiveEnrolmentArmedStillBlocks keeps the opted-in posture: the same
// enrolment, fail_closed, no body -> B-625.
func TestB625LiveEnrolmentArmedStillBlocks(t *testing.T) {
	g := armedManagedGuard(t, true)
	g.SetBudgetBindingLookup(func() (string, bool) { return "b1", true })
	var res ProxyRequestResult
	g.scanBudget(g.set.Load(), &res, "s1", "https://api.anthropic.com/v1/messages", time.Now())
	if !res.Deny || res.DenyRuleID != "B-625" {
		t.Fatalf("armed fail-closed posture did not block: %+v", res)
	}
}

// TestIdentityUnknownKeepsLastComposedEngine (REVIEW-4 P4-01, ruling D-521):
// a failed identity read is "unknown", and unknown keeps enforcing the last
// composed engine - the armed B-625 here, fail_closed or not - in the proxy
// admission AND the intervention pass. Only a confirmed unenrol lifts it.
func TestIdentityUnknownKeepsLastComposedEngine(t *testing.T) {
	for _, failClosed := range []bool{false, true} {
		g := armedManagedGuard(t, failClosed)
		g.SetBudgetIdentityLookup(func() (string, BudgetIdentityState) { return "", BudgetIdentityUnknown })
		var res ProxyRequestResult
		g.scanBudget(g.set.Load(), &res, "s1", "https://api.anthropic.com/v1/messages", time.Now())
		if !res.Deny || res.DenyRuleID != "B-625" {
			t.Fatalf("fail_closed=%v: an unknown identity lifted the composed engine: %+v", failClosed, res)
		}
	}
}

// TestB625ReplacedEnrolmentFailsOpenUnlessOptedIn: a re-enrol (a different
// live binding) with no verified body yet admits by default (D-502) and
// refuses only under fail_closed.
func TestB625ReplacedEnrolmentFailsOpenUnlessOptedIn(t *testing.T) {
	for _, tc := range []struct {
		failClosed bool
		wantDeny   bool
	}{{false, false}, {true, true}} {
		g := armedManagedGuard(t, tc.failClosed)
		g.SetBudgetBindingLookup(func() (string, bool) { return "b2", true })
		var res ProxyRequestResult
		g.scanBudget(g.set.Load(), &res, "s1", "https://api.anthropic.com/v1/messages", time.Now())
		if res.Deny != tc.wantDeny {
			t.Fatalf("fail_closed=%v: deny=%v (%s), want %v", tc.failClosed, res.Deny, res.DenyRuleID, tc.wantDeny)
		}
	}
}
