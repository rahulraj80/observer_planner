package policy

import "testing"

// TestEngineReadsBudgetStamps pins T-418's capability: one row per configured
// fact that makes a budget, limit or subject row compare a stamp, plus the
// policy-file rule declaration, plus the all-zero engine that reads nothing.
func TestEngineReadsBudgetStamps(t *testing.T) {
	t.Parallel()
	spendRule := Rule{
		ID: "U-1", Category: CategoryBudget, Severity: SeverityHigh,
		AppliesTo: []EventKind{KindShellExec}, Observe: DecisionFlag, Enforce: DecisionFlag, Doc: "test rule",
		Match:             func(*MatchContext) (bool, string) { return false, "" },
		ReadsBudgetStamps: true,
	}
	plainRule := spendRule
	plainRule.ID, plainRule.ReadsBudgetStamps = "U-2", false
	for _, tc := range []struct {
		name string
		cfg  Config
		want bool
	}{
		{name: "nothing configured", cfg: Config{}, want: false},
		{name: "policy rule with no spend matcher", cfg: Config{ExtraRules: []Rule{plainRule}}, want: false},
		{name: "session usd", cfg: Config{BudgetSessionUSD: 1}, want: true},
		{name: "daily usd", cfg: Config{BudgetDailyUSD: 1}, want: true},
		{name: "weekly usd", cfg: Config{BudgetWeeklyUSD: 1}, want: true},
		{name: "monthly usd", cfg: Config{BudgetMonthlyUSD: 1}, want: true},
		{name: "session tokens", cfg: Config{BudgetSessionTokens: 1}, want: true},
		{name: "daily tokens", cfg: Config{BudgetDailyTokens: 1}, want: true},
		{name: "weekly tokens", cfg: Config{BudgetWeeklyTokens: 1}, want: true},
		{name: "monthly tokens", cfg: Config{BudgetMonthlyTokens: 1}, want: true},
		{name: "subject cap", cfg: Config{BudgetSubjectCaps: []BudgetSubjectCap{{Kind: BudgetSubjectKindTool, ID: "x", Window: "daily", CapUSD: 1}}}, want: true},
		{name: "5h warn", cfg: Config{LimitUtil5hWarn: 0.5}, want: true},
		{name: "5h deny", cfg: Config{LimitUtil5hDeny: 0.5}, want: true},
		{name: "weekly warn", cfg: Config{LimitUtilWeeklyWarn: 0.5}, want: true},
		{name: "weekly deny", cfg: Config{LimitUtilWeeklyDeny: 0.5}, want: true},
		{name: "org budget required", cfg: Config{BudgetRequired: true}, want: true},
		{name: "policy rule with a spend matcher", cfg: Config{ExtraRules: []Rule{spendRule}}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, err := New(tc.cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got := e.ReadsBudgetStamps(); got != tc.want {
				t.Fatalf("ReadsBudgetStamps = %v, want %v", got, tc.want)
			}
		})
	}
	if (*Engine)(nil).ReadsBudgetStamps() {
		t.Fatal("a nil engine reads no stamp")
	}
}
