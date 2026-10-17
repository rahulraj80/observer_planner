package orgbudget

import (
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestComposeSubjectCaps_SameSubjectTwoAddressees pins that two signed caps
// sharing (period, subject) - the org-wide per-tool cap and the caller's own
// per-developer per-tool cap (compound budget scopes, plan §3.1) - BOTH compose
// onto the node, each with its own baseline. The node keeps a LIST of subject
// caps; nothing here may de-duplicate them by key.
func TestComposeSubjectCaps_SameSubjectTwoAddressees(t *testing.T) {
	t.Parallel()
	now := testNow(t)
	fresh := now.Add(-time.Minute).UTC().Format(time.RFC3339)
	day := orgcontract.BudgetPolicyPeriodCalendarDay
	hard := orgcontract.BudgetPolicyEnforcementHard
	body := orgcontract.BudgetPolicyBody{Version: 9, Caps: []orgcontract.BudgetPolicyCap{
		{
			Period: day, CapUSD: 100, Enforcement: hard, ResolvedScope: "tool:cursor",
			Subject:  &orgcontract.BudgetSubject{Kind: "tool", ID: "cursor"},
			SpentUSD: 99.8, SpentAsOf: fresh, SpentExcludesMachine: true,
		},
		{
			Period: day, CapUSD: 0.5, Enforcement: hard, ResolvedScope: "member:u1",
			Subject:  &orgcontract.BudgetSubject{Kind: "tool", ID: "cursor"},
			SpentUSD: 0.1, SpentAsOf: fresh, SpentExcludesMachine: true,
		},
	}}
	eff, _ := Compose(Thresholds{}, body, Capabilities{
		FromOrg: true, HaveBody: true, FetchState: orgcontract.BudgetFetchOK,
		GuardMode: "enforce", OrgAuthoritative: true, RequireOrgBudget: true, Now: now,
	})
	if len(eff.Subjects) != 2 {
		t.Fatalf("Subjects = %+v, want both same-subject caps", eff.Subjects)
	}
	if eff.Subjects[0].CapUSD != 100 || eff.Subjects[0].BaselineUSD != 99.8 {
		t.Errorf("org-wide cap = %+v", eff.Subjects[0])
	}
	if eff.Subjects[1].CapUSD != 0.5 || eff.Subjects[1].BaselineUSD != 0.1 {
		t.Errorf("per-developer cap = %+v", eff.Subjects[1])
	}
}
