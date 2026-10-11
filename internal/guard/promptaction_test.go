package guard

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/policy"
)

// TestResolvePromptAction pins the one decision x seam-capability table
// (operator ruling 2026-10-02): one row per (decision, capability set).
func TestResolvePromptAction(t *testing.T) {
	t.Parallel()
	full := PromptSeamCaps{Warn: true, Confirm: true, Reenter: true, Block: true}
	hookWarns := PromptSeamCaps{Warn: true, Reenter: true, Block: true}
	hookStops := PromptSeamCaps{Reenter: true, Block: true}
	blockOnly := PromptSeamCaps{Block: true}
	cases := []struct {
		name     string
		decision policy.Decision
		caps     PromptSeamCaps
		want     PromptAction
		wanted   PromptAction
	}{
		{"allow on any seam", policy.DecisionAllow, full, PromptActionNone, ""},

		{"flag, seam can warn", policy.DecisionFlag, hookWarns, PromptActionWarn, ""},
		{"flag, hook without a notice field (Cursor)", policy.DecisionFlag, hookStops, PromptActionFlagAfter, PromptActionWarn},
		{"flag, proxy", policy.DecisionFlag, PromptSeamProxy, PromptActionFlagAfter, PromptActionWarn},
		{"flag, warn-only near-pre-send hook", policy.DecisionFlag, PromptSeamWarnOnly, PromptActionWarn, ""},
		{"flag, capture lane", policy.DecisionFlag, PromptSeamCapture, PromptActionFlagAfter, PromptActionWarn},
		{"flag never escalates on a block-only seam", policy.DecisionFlag, blockOnly, PromptActionFlagAfter, PromptActionWarn},

		{"ask, native confirm", policy.DecisionAsk, full, PromptActionConfirm, ""},
		{"ask, hook re-enter", policy.DecisionAsk, hookWarns, PromptActionReenter, PromptActionConfirm},
		{"ask, proxy re-enter", policy.DecisionAsk, PromptSeamProxy, PromptActionReenter, PromptActionConfirm},
		{"ask, block-only seam stays at least as strong", policy.DecisionAsk, blockOnly, PromptActionBlock, PromptActionConfirm},
		{"ask, warn-only seam", policy.DecisionAsk, PromptSeamWarnOnly, PromptActionWarn, PromptActionConfirm},
		{"ask, capture lane", policy.DecisionAsk, PromptSeamCapture, PromptActionFlagAfter, PromptActionConfirm},

		{"deny, hook", policy.DecisionDeny, hookStops, PromptActionBlock, ""},
		{"deny, proxy", policy.DecisionDeny, PromptSeamProxy, PromptActionBlock, ""},
		{"deny, warn-only seam", policy.DecisionDeny, PromptSeamWarnOnly, PromptActionWarn, PromptActionBlock},
		{"deny, capture lane", policy.DecisionDeny, PromptSeamCapture, PromptActionFlagAfter, PromptActionBlock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, wanted := ResolvePromptAction(tc.decision, tc.caps)
			if got != tc.want || wanted != tc.wanted {
				t.Fatalf("ResolvePromptAction(%s, %+v) = (%q, %q), want (%q, %q)", tc.decision, tc.caps, got, wanted, tc.want, tc.wanted)
			}
		})
	}
}

// TestPromptEmissionFollowsAction pins that the persisted permission and
// enforced flag are what the developer actually got.
func TestPromptEmissionFollowsAction(t *testing.T) {
	t.Parallel()
	cases := []struct {
		action   PromptAction
		perm     string
		enforced bool
		toast    bool
	}{
		{PromptActionNone, "allow", false, true},
		{PromptActionWarn, "allow", false, false},
		{PromptActionFlagAfter, "allow", false, true},
		{PromptActionReenter, "ask", true, false},
		{PromptActionConfirm, "ask", true, false},
		{PromptActionBlock, "deny", true, true},
	}
	for _, tc := range cases {
		em := PromptEmission(PromptVerdict{Action: tc.action})
		if em.Permission != tc.perm || em.Enforced != tc.enforced {
			t.Errorf("%q: emission = %+v, want (%s, enforced=%v)", tc.action, em, tc.perm, tc.enforced)
		}
		if tc.action != PromptActionNone && tc.action.SuppressesToast() == tc.toast {
			t.Errorf("%q: SuppressesToast = %v, want toast=%v", tc.action, tc.action.SuppressesToast(), tc.toast)
		}
	}
}

const orgPromptEnforceOffTOML = `
[[override]]
  enforce = false
  overridable = true
  rule = "R-172"
`

const orgPromptEnforceOnTOML = `
[[override]]
  enforce = true
  overridable = true
  rule = "R-172"
`

const orgPromptNoEnforceKeyTOML = `
[[override]]
  overridable = true
  rule = "R-172"
`

// TestEvaluatePrompt_OrgEnforceOffCapsAtFlag is the demo-node-1 bug:
// the org's active bundle wrote R-172 with enforce=false (the policy
// form's "Enforce" checkbox OFF), the node runs guard mode=enforce with
// the local prompt mode ask-once, and the hook lanes still asked once.
// The ruling: enforce=false caps a prompt rule at flag (warn only).
func TestEvaluatePrompt_OrgEnforceOffCapsAtFlag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		bundle     string
		wantDec    policy.Decision
		wantCapped bool
	}{
		{"enforce = false caps at flag", orgPromptEnforceOffTOML, policy.DecisionFlag, true},
		{"enforce = true keeps ask-once", orgPromptEnforceOnTOML, policy.DecisionAsk, false},
		{"no enforce key keeps ask-once", orgPromptNoEnforceKeyTOML, policy.DecisionAsk, false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, pin := signedEnvelope(t, int64(40+i), tc.bundle)
			cfg := promptCfg("enforce", "ask-once", nil)
			cfg.Rules.OrgBundle = guardCfgOrg().Rules.OrgBundle
			g, err := New(Options{
				Config: cfg, Home: "/home/u", KnownProjectRoots: []string{"/home/u/proj"},
				ReadFile: fsMap(map[string]string{orgBundlePath: env}), OrgKeyPinHash: pin,
			})
			if err != nil {
				t.Fatalf("guard.New: %v", err)
			}
			g.SetPromptReconsiderStore(newMemPromptStore().funcs())
			secrets, pii, _ := g.BuildPromptFindings("push with " + fakeFinePAT + " please")
			pv := g.EvaluatePrompt(policy.Event{
				Kind: policy.KindUserPrompt, Tool: "claude-code", SessionID: "s-org",
				Secrets: secrets, PIIFindings: pii,
			})
			if pv.Verdict.RuleID != "R-172" || pv.Verdict.Decision != tc.wantDec || pv.OrgCapped != tc.wantCapped {
				t.Fatalf("verdict = (%s %s capped=%v), want (R-172 %s capped=%v)",
					pv.Verdict.RuleID, pv.Verdict.Decision, pv.OrgCapped, tc.wantDec, tc.wantCapped)
			}
			// Every lane resolves the capped decision to a warning where
			// it can show one, never to a confirm or a block.
			if tc.wantCapped {
				for _, caps := range []PromptSeamCaps{{Warn: true, Reenter: true, Block: true}, PromptSeamProxy, PromptSeamCapture} {
					if a := pv.WithAction(caps, "x").Action; a.Stops() {
						t.Errorf("capped verdict stops the prompt on %+v (action %q)", caps, a)
					}
				}
			}
		})
	}
}

// TestActionVerdictFromPrompt_RecordsActionAndLane pins the reason
// markers the reporting surfaces read.
func TestActionVerdictFromPrompt_RecordsActionAndLane(t *testing.T) {
	t.Parallel()
	g := newTestGuard(t, guardCfg(), nil)
	pv := PromptVerdict{
		Verdict: policy.Verdict{Decision: policy.DecisionFlag, RuleID: "R-172", Reason: "r"},
		Outcome: PromptOutcomeWarned,
	}.WithAction(PromptSeamProxy, "proxy")
	av := g.ActionVerdictFromPrompt(pv, PromptEmission(pv), ActionInput{SessionID: "s"})
	for _, want := range []string{"[outcome=warned]", "[action=flag-after]", "[wanted=warn]", "[lane=proxy]"} {
		if !strings.Contains(av.Verdict.Reason, want) {
			t.Errorf("reason %q missing %s", av.Verdict.Reason, want)
		}
	}
	if av.SuppressAlert {
		t.Error("a flag-after must toast: the developer saw nothing before send")
	}
}

// TestPromptRulesWarnOnly pins the helper the org preview uses: only an
// explicit enforce=false on a prompt rule counts.
func TestPromptRulesWarnOnly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body string
		want       []string
	}{
		{"enforce off on R-172", orgPromptEnforceOffTOML, []string{"R-172"}},
		{"enforce on", orgPromptEnforceOnTOML, []string{}},
		{"no enforce key", orgPromptNoEnforceKeyTOML, []string{}},
		{"tool-call rule is not a prompt rule", "[[override]]\nrule = \"R-110\"\nenforce = false\n", []string{}},
		{"unparseable", "[[override", nil},
	}
	for _, tc := range cases {
		got := PromptRulesWarnOnly(tc.body)
		if len(got) != len(tc.want) || (tc.want == nil) != (got == nil) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		}
	}
}

// TestWithAction_OutcomeFollowsAction pins that a fresh ask ("blocked" by
// the reconsider-once machine) on a seam that cannot stop the prompt is
// recorded as "warned": the prompt went through.
func TestWithAction_OutcomeFollowsAction(t *testing.T) {
	ask := PromptVerdict{Verdict: policy.Verdict{Decision: policy.DecisionAsk, RuleID: "R-172"}, Outcome: PromptOutcomeBlocked}
	cases := []struct {
		name string
		caps PromptSeamCaps
		want PromptOutcome
	}{
		{"warn-only hook", PromptSeamWarnOnly, PromptOutcomeWarned},
		{"capture", PromptSeamCapture, PromptOutcomeWarned},
		{"proxy re-enter", PromptSeamProxy, PromptOutcomeBlocked},
		{"confirm hook", PromptSeamCaps{Warn: true, Confirm: true}, PromptOutcomeBlocked},
	}
	for _, tc := range cases {
		if got := ask.WithAction(tc.caps, "hook").Outcome; got != tc.want {
			t.Errorf("%s: outcome %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestResolvePromptActionExhaustive walks every decision against all 16
// combinations of the four capability bits and checks each answer
// against an independent oracle of the D-030 semantics (Sol A2-11): a
// deny never becomes a confirm or a re-enter, a flag never stops, an ask
// stops whenever the seam can stop, and the action is always one the
// seam can do (or flag-after).
func TestResolvePromptActionExhaustive(t *testing.T) {
	t.Parallel()
	oracle := func(d policy.Decision, c PromptSeamCaps) PromptAction {
		pick := func(ladder ...PromptAction) PromptAction {
			for _, a := range ladder {
				if c.can(a) {
					return a
				}
			}
			return PromptActionFlagAfter
		}
		switch d {
		case policy.DecisionFlag:
			return pick(PromptActionWarn)
		case policy.DecisionAsk:
			return pick(PromptActionConfirm, PromptActionReenter, PromptActionBlock, PromptActionWarn)
		case policy.DecisionDeny:
			return pick(PromptActionBlock, PromptActionWarn)
		default:
			return PromptActionNone
		}
	}
	for _, d := range []policy.Decision{policy.DecisionAllow, policy.DecisionFlag, policy.DecisionAsk, policy.DecisionDeny} {
		for bits := 0; bits < 16; bits++ {
			c := PromptSeamCaps{Warn: bits&1 != 0, Confirm: bits&2 != 0, Reenter: bits&4 != 0, Block: bits&8 != 0}
			got, _ := ResolvePromptAction(d, c)
			if want := oracle(d, c); got != want {
				t.Errorf("ResolvePromptAction(%s, %+v) = %q, want %q", d, c, got, want)
			}
			if d == policy.DecisionDeny && (got == PromptActionConfirm || got == PromptActionReenter) {
				t.Errorf("deny became approvable on %+v: %q", c, got)
			}
			if d == policy.DecisionFlag && got.Stops() {
				t.Errorf("flag stopped the prompt on %+v: %q", c, got)
			}
			if d == policy.DecisionAsk && (c.Confirm || c.Reenter || c.Block) && !got.Stops() {
				t.Errorf("ask did not stop on a seam that can on %+v: %q", c, got)
			}
			if got != PromptActionNone && got != PromptActionFlagAfter && !c.can(got) {
				t.Errorf("action %q not supported by %+v", got, c)
			}
		}
	}
}
