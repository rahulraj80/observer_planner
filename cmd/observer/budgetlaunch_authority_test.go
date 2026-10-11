package main

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/govern"
)

// TestBudgetLaunchAuthorityRules pins one case per budgetLaunchAuthorityRules
// row (REVIEW-4 P4-03). Not run in fast mode (D-507); listed in the fold4
// walkthrough part.
func TestBudgetLaunchAuthorityRules(t *testing.T) {
	cases := []struct {
		row  string
		in   budgetLaunchAuthorityInput
		want budgetLaunchAuthority
	}{
		{row: "managed_no_grant", in: budgetLaunchAuthorityInput{managed: true, noGrant: true}, want: budgetAuthorityUnverifiable},
		{row: "individual_no_grant", in: budgetLaunchAuthorityInput{noGrant: true}, want: budgetAuthorityNone},
		{row: "managed_grant_expired", in: budgetLaunchAuthorityInput{managed: true, state: govern.StateGrantExpired}, want: budgetAuthorityUnverifiable},
		{row: "managed_identity_unverifiable", in: budgetLaunchAuthorityInput{managed: true, state: govern.StateKeyPinMismatch}, want: budgetAuthorityUnverifiable},
		{row: "individual_grant_unusable", in: budgetLaunchAuthorityInput{state: govern.StateGrantExpired}, want: budgetAuthorityNone},
		{row: "verified_grant", in: budgetLaunchAuthorityInput{managed: true}, want: budgetAuthorityFromGrant},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		got := budgetAuthorityFromGrant
		for _, r := range budgetLaunchAuthorityRules {
			if r.when(tc.in) {
				if r.name != tc.row {
					t.Errorf("%s: matched row %q first", tc.row, r.name)
				}
				got = r.out
				break
			}
		}
		if got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.row, got, tc.want)
		}
		seen[tc.row] = true
	}
	for _, r := range budgetLaunchAuthorityRules {
		if !seen[r.name] {
			t.Errorf("rule %q has no test case", r.name)
		}
	}
}
