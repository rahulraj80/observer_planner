package guard

import (
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/policy"
)

// TestRetiredOrReplacedEnrolmentKeepsLocalCaps is REVIEW-4 P4-02: when the
// enrolment an org-composed engine belongs to is confirmed gone (retired) or
// replaced with no fail-closed opt-in, the proxy admission fails open onto the
// node's OWN [guard.budget] caps (D-502), never onto no caps at all. Here the
// org composition raised the monthly token ceiling to 1000 for enrolment b1;
// the node's own ceiling is 100 and 150 tokens are spent, so the local cap
// must deny once b1 is no longer the live enrolment. While b1 is live the
// composed 1000 governs and the request is admitted.
func TestRetiredOrReplacedEnrolmentKeepsLocalCaps(t *testing.T) {
	cases := []struct {
		name     string
		lookup   func() (string, bool)
		wantDeny bool
	}{
		{name: "same_enrolment_uses_composed_engine", lookup: func() (string, bool) { return "b1", true }, wantDeny: false},
		{name: "retired_uses_local_caps", lookup: func() (string, bool) { return "", false }, wantDeny: true},
		{name: "replaced_open_uses_local_caps", lookup: func() (string, bool) { return "b2", true }, wantDeny: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := guardCfg()
			cfg.Mode = "enforce"
			cfg.Budget = config.GuardBudgetConfig{MonthlyTokens: 100, Hard: true}
			g := newTestGuard(t, cfg, nil)
			g.SetBudgetLookup(func(string) (BudgetSnapshot, bool) {
				return BudgetSnapshot{MonthlyTokens: 150}, true
			})
			if err := g.ApplyOrgBudget(config.GuardBudgetConfig{MonthlyTokens: 1000, Hard: true}, nil, false,
				policy.BudgetProtection{}, "b1"); err != nil {
				t.Fatal(err)
			}
			g.SetBudgetBindingLookup(tc.lookup)
			var res ProxyRequestResult
			g.scanBudget(g.set.Load(), &res, "s1", "https://api.anthropic.com/v1/messages", time.Now())
			if res.Deny != tc.wantDeny {
				t.Fatalf("deny=%v (%s %s), want %v", res.Deny, res.DenyRuleID, res.DenyReason, tc.wantDeny)
			}
			if tc.wantDeny && res.DenyRuleID != "B-623" {
				t.Fatalf("deny rule = %s, want the local monthly token cap B-623", res.DenyRuleID)
			}
		})
	}
}
