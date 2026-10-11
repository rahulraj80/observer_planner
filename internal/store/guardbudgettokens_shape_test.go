package store

import (
	"strings"
	"testing"
)

// TestGuardBudgetTokensAdvisorySQLSkipsManagedValidation pins T-418's query
// reshape: the advisory statement carries none of the managed-only timestamp
// validation (SQLite evaluates both operands of AND in a value context, so a
// bound `managed` flag did not stop it running per row), and the managed
// statement still carries all of it.
func TestGuardBudgetTokensAdvisorySQLSkipsManagedValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		managed bool
		want    bool
	}{
		{managed: false, want: false},
		{managed: true, want: true},
	} {
		q := guardBudgetTokensSQL(tc.managed)
		for _, marker := range []string{"julianday(", "strftime("} {
			if got := strings.Contains(q, marker); got != tc.want {
				t.Fatalf("managed=%v: statement contains %q = %v, want %v", tc.managed, marker, got, tc.want)
			}
		}
		if strings.Contains(q, "params") {
			t.Fatalf("managed=%v: statement still binds a params CTE", tc.managed)
		}
	}
}
