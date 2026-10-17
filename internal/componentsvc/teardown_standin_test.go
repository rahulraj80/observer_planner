package componentsvc

import (
	"strings"
	"testing"
)

// TestTeardownNamesWhereToRunAVerbThisBuildLacks pins T-726: a manual
// teardown step whose component is compiled out of the running binary names
// a subcommand that is not registered here, so the preview and the outcome
// say to run it from a build that includes the component instead of telling
// the operator to run a verb this binary refuses as unknown. A step whose
// component is in the build keeps the plain "run X" line.
func TestTeardownNamesWhereToRunAVerbThisBuildLacks(t *testing.T) {
	const id = "integration.shell-wrap"
	orig := omittedComponents
	t.Cleanup(func() { omittedComponents = orig })

	for _, tc := range []struct {
		name     string
		omitted  []string
		wantNote bool
	}{
		{"component in the build", nil, false},
		{"component compiled out", []string{id}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			omittedComponents = func() []string { return tc.omitted }
			plan := teardownPlan([]string{id})
			if len(plan) != 1 || plan[0].Kind != StepManual || plan[0].Command == "" {
				t.Fatalf("plan = %+v, want one manual step with a command", plan)
			}
			hasNote := strings.Contains(plan[0].What, "this build does not include "+id)
			if hasNote != tc.wantNote {
				t.Errorf("preview note present = %v, want %v (What = %q)", hasNote, tc.wantNote, plan[0].What)
			}
			out := (&Service{}).runTeardown(plan, true)
			if len(out) != 1 || len(out[0].NeedsAttention) != 1 {
				t.Fatalf("outcome = %+v, want one step needing attention", out)
			}
			line := out[0].NeedsAttention[0]
			if tc.wantNote {
				if !strings.Contains(line, "is not available here") || !strings.Contains(line, "builds that include it") {
					t.Errorf("attention = %q, want the stand-in naming where to run it", line)
				}
			} else if line != "run `"+plan[0].Command+"` to remove it" {
				t.Errorf("attention = %q, want the plain run line", line)
			}
		})
	}
}
