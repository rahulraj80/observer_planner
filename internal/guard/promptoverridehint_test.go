package guard

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/policy"
)

// TestOverridableOrgPromptDenyRendersOverridePath pins Sol A2-10 on the
// proxy lane with a real org bundle: an overridable org rule under local
// mode=block is softened to re-enter/confirm, so the message must give
// the override path (approve, then resend), not "edit it out". An
// org-locked rule keeps the hard-block text.
func TestOverridableOrgPromptDenyRendersOverridePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		bundle       string
		overridable  bool
		wantContains string
		wantAbsent   string
	}{
		{
			"org overridable", "[[override]]\nrule = \"R-172\"\noverridable = true\n", true,
			"Your organization lets you override this rule: run `observer guard prompt allow", "Edit it out",
		},
		{
			"org locked", "[[override]]\nrule = \"R-172\"\nenforce = true\n", false,
			"Edit it out before sending", "lets you override",
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, pin := signedEnvelope(t, int64(500+i), tc.bundle)
			cfg := promptCfg("enforce", "block", nil)
			cfg.Rules.OrgBundle = guardCfgOrg().Rules.OrgBundle
			g, err := New(Options{
				Config: cfg, Home: "/home/u", KnownProjectRoots: []string{"/home/u/proj"},
				ReadFile: fsMap(map[string]string{orgBundlePath: env}), OrgKeyPinHash: pin,
			})
			if err != nil {
				t.Fatal(err)
			}
			g.SetPromptReconsiderStore(newMemPromptStore().funcs())
			secrets, pii, _ := g.BuildPromptFindings("push with " + fakeFinePAT + " please")
			pv := g.EvaluatePrompt(policy.Event{
				Kind: policy.KindUserPrompt, Tool: "claude-code", SessionID: "s-ovr",
				Secrets: secrets, PIIFindings: pii,
			})
			if pv.Verdict.RuleID != "R-172" || pv.Verdict.Decision != policy.DecisionDeny || pv.Verdict.Overridable != tc.overridable {
				t.Fatalf("verdict = %s %s overridable=%v", pv.Verdict.RuleID, pv.Verdict.Decision, pv.Verdict.Overridable)
			}
			pv = pv.WithAction(PromptSeamProxy, "proxy")
			msg := proxyPromptHouseMessage(pv, "s-ovr")
			if !strings.Contains(msg, tc.wantContains) || strings.Contains(msg, tc.wantAbsent) {
				t.Errorf("message = %q", msg)
			}
			if tc.overridable && !strings.Contains(msg, "--session s-ovr") {
				t.Errorf("override hint does not name the session: %q", msg)
			}
		})
	}
}

// TestPromptOverrideHint covers the helper's own table.
func TestPromptOverrideHint(t *testing.T) {
	t.Parallel()
	deny := func(overridable bool) PromptVerdict {
		return PromptVerdict{
			Verdict:   policy.Verdict{RuleID: "R-172", Decision: policy.DecisionDeny, Overridable: overridable},
			Outcome:   PromptOutcomeBlocked,
			Detectors: "github_pat,jwt",
		}
	}
	if got := PromptOverrideHint(deny(true), "s1"); !strings.Contains(got, "allow github_pat --session s1") {
		t.Errorf("overridable with session = %q", got)
	}
	if got := PromptOverrideHint(deny(true), ""); !strings.Contains(got, "allow github_pat --project") {
		t.Errorf("overridable without session = %q", got)
	}
	if got := PromptOverrideHint(deny(false), "s1"); got != "" {
		t.Errorf("locked deny = %q, want empty", got)
	}
	ask := deny(true)
	ask.Verdict.Decision = policy.DecisionAsk
	if got := PromptOverrideHint(ask, "s1"); got != "" {
		t.Errorf("ask = %q, want empty (the ask-once text already covers it)", got)
	}
	gerr := deny(true)
	gerr.GuardError = true
	if got := PromptOverrideHint(gerr, "s1"); got != "" {
		t.Errorf("guard error = %q, want empty", got)
	}
}
