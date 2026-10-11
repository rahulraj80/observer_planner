package hook

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/policy"
)

// TestPromptHouseMessage_OverridableOrgDeny pins Sol A2-10 on the hook
// lane: an org-overridable deny tells the developer how to override it;
// an org-locked deny keeps the hard-block text. Same rendering as the
// proxy lane (guard.PromptOverrideHint).
func TestPromptHouseMessage_OverridableOrgDeny(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		overridable bool
		want, not   string
	}{
		{"overridable", true, "lets you override this rule: run `observer guard prompt allow github_pat --session s9`", "Edit it out"},
		{"org-locked", false, "Edit it out before sending", "lets you override"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pv := guard.PromptVerdict{
				Verdict:   policy.Verdict{RuleID: "R-172", Decision: policy.DecisionDeny, Reason: "secret in prompt", Overridable: tc.overridable},
				Outcome:   guard.PromptOutcomeBlocked,
				Detectors: "github_pat",
			}.WithAction(guard.PromptSeamCaps{Warn: true, Reenter: true, Block: true}, "hook")
			got := promptHouseMessage(pv, "s9")
			if !strings.Contains(got, tc.want) || strings.Contains(got, tc.not) {
				t.Errorf("message = %q", got)
			}
			if tc.overridable && got != "observer: secret in prompt"+guard.PromptOverrideHint(pv, "s9") {
				t.Errorf("hook lane does not use the shared rendering: %q", got)
			}
		})
	}
}
