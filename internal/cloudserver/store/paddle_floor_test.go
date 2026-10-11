package store

import "testing"

// TestPaddlePlanNotOlderThan pins the D-299 floor the Paddle activation path
// applies before assigning a plan: a price (or stored subscription row) that
// names an OLDER version of the plan the account already holds is raised to
// the current version, so a renewal can never schedule a migrated subscriber
// back onto the stale allowance. Everything else passes through unchanged.
func TestPaddlePlanNotOlderThan(t *testing.T) {
	tests := []struct {
		name    string
		mapped  PaddlePlan
		current Plan
		want    int
	}{
		{"same plan, older mapped version is raised", PaddlePlan{Name: PlanPlusBeta, Version: 2, Interval: "month"}, Plan{Name: PlanPlusBeta, Version: 3}, 3},
		{"same plan, v1 legacy price is raised", PaddlePlan{Name: PlanPlusBeta, Version: 1}, Plan{Name: PlanPlusBeta, Version: 3}, 3},
		{"same plan, same version unchanged", PaddlePlan{Name: PlanPlusBeta, Version: 3}, Plan{Name: PlanPlusBeta, Version: 3}, 3},
		{"same plan, newer mapped version kept", PaddlePlan{Name: PlanPlusBeta, Version: 4}, Plan{Name: PlanPlusBeta, Version: 3}, 4},
		{"different plan (activation from free) unchanged", PaddlePlan{Name: PlanPlusBeta, Version: 2}, Plan{Name: PlanFree, Version: 3}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := paddlePlanNotOlderThan(tt.mapped, tt.current)
			if got.Version != tt.want || got.Name != tt.mapped.Name ||
				got.Interval != tt.mapped.Interval || got.Display != tt.mapped.Display {
				t.Fatalf("paddlePlanNotOlderThan(%+v, %s v%d) = %+v, want version %d and every other field unchanged",
					tt.mapped, tt.current.Name, tt.current.Version, got, tt.want)
			}
		})
	}
}
