package orgclient

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

func TestRequestableBudgets(t *testing.T) {
	cases := []struct {
		name string
		caps []orgcontract.BudgetPolicyCap
		want []string
	}{
		{"nothing cached", nil, nil},
		{"an older server names no budget", []orgcontract.BudgetPolicyCap{{Period: "calendar_month", CapTokens: 5}}, nil},
		{
			"one per budget id, ordered by scope then period",
			[]orgcontract.BudgetPolicyCap{
				{Period: "calendar_day", ResolvedScope: "member:u1", BudgetID: "b-every", CapTokens: 3},
				{Period: "calendar_month", ResolvedScope: "member:u1", BudgetID: "b-u1", CapTokens: 5},
				{Period: "calendar_month", ResolvedScope: "member:u1", BudgetID: "b-u1", CapTokens: 5, Subject: &orgcontract.BudgetSubject{Kind: "tool", ID: "codex"}},
			},
			[]string{"b-every", "b-u1"},
		},
		{
			// C156-07: a grant for one member never raises a shared pool,
			// but the pool may be what ran out (D-334), so it is listed after
			// the member's own budgets, marked not grantable.
			"shared pools are listed last, not grantable",
			[]orgcontract.BudgetPolicyCap{
				{Period: "calendar_month", ResolvedScope: "team:eng", BudgetID: "b-eng", CapTokens: 9},
				{Period: "calendar_month", ResolvedScope: "org", BudgetID: "b-org", CapTokens: 9},
				{Period: "calendar_month", ResolvedScope: "tool:codex", BudgetID: "b-codex", CapTokens: 9},
				{Period: "calendar_month", ResolvedScope: "model:gpt-5", BudgetID: "b-gpt", CapTokens: 9},
				{Period: "calendar_month", ResolvedScope: "member:u1", BudgetID: "b-u1", CapTokens: 5},
			},
			[]string{"b-u1", "b-gpt", "b-org", "b-eng", "b-codex"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := requestableBudgets(orgcontract.BudgetPolicyBody{Caps: tc.caps})
			var ids []string
			for _, b := range got {
				ids = append(ids, b.BudgetID)
			}
			if len(ids) != len(tc.want) {
				t.Fatalf("ids = %v, want %v", ids, tc.want)
			}
			for i := range ids {
				if ids[i] != tc.want[i] {
					t.Fatalf("ids = %v, want %v", ids, tc.want)
				}
			}
		})
	}
	var nilClient *Client
	if nilClient.RequestableBudgets() != nil {
		t.Fatal("nil client lists budgets")
	}
}

// TestRequestableBudgetFacts pins what each listed budget says about itself
// (T-523): whether a grant can raise it, and only the caps of the row its id
// names (a unit another row supplied is not this budget's number).
func TestRequestableBudgetFacts(t *testing.T) {
	got := requestableBudgets(orgcontract.BudgetPolicyBody{Caps: []orgcontract.BudgetPolicyCap{
		{
			Period: "calendar_day", ResolvedScope: "member:u1", BudgetID: "b-u1", Timezone: "Europe/Paris", Enforcement: "hard",
			CapTokens: 5000, ScopeTokens: "member:u1", CapUSD: 50, ScopeUSD: "team:eng",
		},
		{Period: "calendar_month", ResolvedScope: "team:eng", BudgetID: "b-eng", CapUSD: 900},
	}})
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	own, team := got[0], got[1]
	if !own.Grantable || own.CapTokens != 5000 || own.CapUSD != 0 || own.Timezone != "Europe/Paris" || own.Enforcement != "hard" {
		t.Fatalf("own = %+v", own)
	}
	if team.Grantable || team.CapUSD != 900 {
		t.Fatalf("team = %+v", team)
	}
}
