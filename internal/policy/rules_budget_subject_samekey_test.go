package policy

import (
	"testing"
)

// TestSubjectBudgetRules_SameSubjectTwoCapsAreIndependent pins the node
// contract the compound budget scopes rely on
// (docs/plans/compound-budget-scopes-plan-2026-10-01.md §3.1): the org server
// may ship TWO caps with the identical (kind, id, window) - an org-wide
// per-tool cap and a per-developer per-tool cap - each carrying its OWN
// cross-machine baseline, and the node must evaluate each one independently.
// Merging them, or keeping only the first, would drop a ceiling the org
// authored.
func TestSubjectBudgetRules_SameSubjectTwoCapsAreIndependent(t *testing.T) {
	t.Parallel()
	cfg := Config{Mode: ModeEnforce, BudgetSubjectCaps: []BudgetSubjectCap{
		// org-wide cursor cap: $100/day, the rest of the org already spent 99.8
		{Kind: BudgetSubjectKindTool, ID: "cursor", Window: BudgetWindowDaily, CapUSD: 100, BaselineUSD: 99.8, Hard: true},
		// this developer's own cursor cap: $0.50/day, nothing spent elsewhere
		{Kind: BudgetSubjectKindTool, ID: "cursor", Window: BudgetWindowDaily, CapUSD: 0.5, Hard: true},
	}}
	eng, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cases := []struct {
		name     string
		local    float64
		wantDeny bool
	}{
		{name: "under both", local: 0.1},
		{name: "the org-wide cap binds through its baseline", local: 0.25, wantDeny: true},
		{name: "the per-developer cap binds on local spend alone", local: 0.6, wantDeny: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ev := subjectTestEvent("cursor", "")
			ev.ToolUsage = BudgetWindowAmounts{DailyUSD: tc.local}
			v := eng.Evaluate(ev)
			denied := v.RuleID == "B-626" && v.Decision == DecisionDeny
			if denied != tc.wantDeny {
				t.Fatalf("local %.2f: verdict %s/%s (%s), want deny=%v", tc.local, v.RuleID, v.Decision, v.Reason, tc.wantDeny)
			}
		})
	}
}
