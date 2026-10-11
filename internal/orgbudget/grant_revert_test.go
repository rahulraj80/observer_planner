package orgbudget

import (
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestGrantedCapRevertsOfflineAtExpiry: a cap raised by an org access grant
// is enforced at the granted number until the grant's expiry, then at the
// base number, with no fetch in between (request-grants design §3.3).
func TestGrantedCapRevertsOfflineAtExpiry(t *testing.T) {
	t.Parallel()
	expiry := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	cp := orgcontract.BudgetPolicyCap{
		Period: orgcontract.BudgetPolicyPeriodCalendarMonth, CapTokens: 1_200_000,
		Enforcement: orgcontract.BudgetPolicyEnforcementHard, ResolvedScope: "member:alice",
		GrantID: 12, BaseCapTokens: 1_000_000, GrantExpiresAtNs: expiry.UnixNano(),
	}
	body := orgcontract.BudgetPolicyBody{
		Version: 3, ResolvedScope: cp.ResolvedScope, CapTokens: cp.CapTokens, Period: cp.Period,
		Enforcement: cp.Enforcement, Caps: []orgcontract.BudgetPolicyCap{cp},
	}
	for _, tc := range []struct {
		name string
		now  time.Time
		want int64
	}{
		{"before expiry the granted cap holds", expiry.Add(-time.Hour), 1_200_000},
		{"at expiry the base cap returns", expiry, 1_000_000},
		{"after expiry the base cap holds", expiry.Add(48 * time.Hour), 1_000_000},
	} {
		got, _ := Compose(Thresholds{}, body, Capabilities{
			FromOrg: true, HaveBody: true, OrgAuthoritative: true,
			FetchState: orgcontract.BudgetFetchOK, GuardMode: "enforce", Now: tc.now,
		})
		if got.MonthlyTokens != tc.want {
			t.Errorf("%s: monthly_tokens = %d, want %d", tc.name, got.MonthlyTokens, tc.want)
		}
	}
	perpetual := cp
	perpetual.GrantExpiresAtNs = 0
	if c := perpetual.RevertExpiredGrant(expiry.Add(1000 * time.Hour)); c.CapTokens != 1_200_000 {
		t.Fatalf("a perpetual grant reverted: %+v", c)
	}
}
