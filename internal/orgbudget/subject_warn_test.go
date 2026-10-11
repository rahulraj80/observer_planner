package orgbudget

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestComposeSubjectCapsCarryTheWarnLadder (Sol A5-04, D-016): a subject cap
// carries the same warn-ladder provenance an Origin does (scope per unit,
// mode, the org's thresholds, the cap's own timezone), so the warning service
// can walk 75 / 90 / 100 on it.
func TestComposeSubjectCapsCarryTheWarnLadder(t *testing.T) {
	t.Parallel()
	now := testNow(t)
	body := orgcontract.BudgetPolicyBody{Version: 5, Caps: []orgcontract.BudgetPolicyCap{{
		Period: orgcontract.BudgetPolicyPeriodCalendarDay, CapUSD: 10, CapTokens: 5000,
		Enforcement:   orgcontract.BudgetPolicyEnforcementSoft,
		Subject:       &orgcontract.BudgetSubject{Kind: "tool", ID: "codex"},
		ResolvedScope: "team:t1", ScopeTokens: "member:m1",
		Thresholds: []float64{0.75, 0.9, 1}, Timezone: "Asia/Kolkata",
	}}}
	eff, _ := Compose(Thresholds{}, body, Capabilities{
		FromOrg: true, HaveBody: true, FetchState: orgcontract.BudgetFetchOK,
		GuardMode: "enforce", OrgAuthoritative: true, RequireOrgBudget: true, Now: now,
	})
	if len(eff.Subjects) != 1 {
		t.Fatalf("Subjects = %+v", eff.Subjects)
	}
	sc := eff.Subjects[0]
	if sc.ScopeUSD != "team:t1" || sc.ScopeTokens != "member:m1" || sc.Enforcement != "soft" ||
		sc.Timezone != "Asia/Kolkata" || len(sc.Thresholds) != 3 || sc.Thresholds[1] != 0.9 {
		t.Errorf("subject cap warn provenance = %+v", sc)
	}
	body.Caps[0].Thresholds[1] = 0.5
	if sc.Thresholds[1] != 0.9 {
		t.Error("Thresholds aliases the body's slice")
	}
}
