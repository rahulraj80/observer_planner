package policy

import (
	"strings"
	"testing"
)

// TestBudgetReasonWithoutDollars drives the REAL dollar matchers and then the
// token-display rewrite, so the template's writer and its reader cannot drift.
func TestBudgetReasonWithoutDollars(t *testing.T) {
	nodeWide := func(got, lim float64, hard bool) string {
		m := matchCostOver(
			func(*Event) float64 { return got },
			func(*Config) float64 { return lim },
			func(*Event) bool { return false },
			"daily")
		ok, reason := m(&MatchContext{Event: &Event{}, Cfg: &Config{BudgetHard: hard}})
		if !ok {
			t.Fatalf("matchCostOver(%v, %v) did not match", got, lim)
		}
		return reason
	}
	subject := func(got, lim float64) string {
		ok, reason := subjectCapBreach(&Event{},
			BudgetSubjectCap{Kind: "tool", ID: "claude-code", Window: BudgetWindowDaily, CapUSD: lim, Hard: true},
			BudgetWindowAmounts{DailyUSD: got}, false)
		if !ok {
			t.Fatalf("subjectCapBreach(%v, %v) did not match", got, lim)
		}
		return reason
	}
	cases := []struct {
		name   string
		reason string
		want   string
		wantOK bool
	}{
		{
			"node-wide over", nodeWide(0.90, 0.75, false),
			"daily spend is at 120% of the budget", true,
		},
		{
			"subject at the cap", subject(20, 20),
			"daily claude-code spend has reached 100% of the budget", true,
		},
		{
			"with the rule title and org label in front",
			"organization daily budget (you, warning only) reached: " + nodeWide(1.26, 0.75, false),
			"organization daily budget (you, warning only) reached: daily spend is at 168% of the budget", true,
		},
		{
			"cross-machine note kept",
			"daily spend $1.50 exceeds the $1.00 budget" + baselineNote(true),
			"daily spend is at 150% of the budget" + baselineNote(true), true,
		},
		{
			"token budget untouched",
			"daily usage 960000 tokens exceeds the 900000-token budget", "", false,
		},
		{
			"unavailable accounting untouched",
			"daily spend accounting is unavailable; cannot verify the configured budget", "", false,
		},
		{"unrelated rule untouched", "rm -rf on the home directory", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := BudgetReasonWithoutDollars(tc.reason)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (reason %q)", ok, tc.wantOK, tc.reason)
			}
			if !ok {
				if got != tc.reason {
					t.Fatalf("an unmatched reason must come back verbatim: %q", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
			if strings.Contains(got, "$") {
				t.Fatalf("token display kept a dollar figure: %q", got)
			}
		})
	}
}

// TestCostConditionReasonWithoutDollars pins T-204 for a user policy rule's
// session-spend condition: the writer's own output is rewritten into a share
// of the limit, the rest of a joined reason is kept, and an unrelated reason
// (including a shell `$1`) is left alone.
func TestCostConditionReasonWithoutDollars(t *testing.T) {
	rows := []struct {
		in   string
		want string
		ok   bool
	}{
		{SessionCostConditionReason(1.3, 1), "session spend is at 130% of the rule's limit", true},
		{
			"U-001: " + SessionCostConditionReason(2.5, 2) + "; tool matches Bash",
			"U-001: session spend is at 125% of the rule's limit; tool matches Bash",
			true,
		},
		{"command matches awk '{print $1}'", "command matches awk '{print $1}'", false},
		{"session tainted by web", "session tainted by web", false},
	}
	for _, r := range rows {
		got, ok := CostConditionReasonWithoutDollars(r.in)
		if got != r.want || ok != r.ok {
			t.Errorf("CostConditionReasonWithoutDollars(%q) = %q, %v; want %q, %v", r.in, got, ok, r.want, r.ok)
		}
		if r.ok && strings.Contains(got, "$") {
			t.Errorf("token rendering kept a dollar sign: %q", got)
		}
	}
	if got := SessionCostConditionReason(1.3, 1); got != "session spend $1.30 > $1.00" {
		t.Errorf("writer = %q", got)
	}
}
