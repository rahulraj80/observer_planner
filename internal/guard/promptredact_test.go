package guard

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/policy"
)

// Hook-parity Lane B (2026-10-03): the redact action on a seam that can
// rewrite the prompt (PromptSeamCaps.Redact).

// TestResolvePromptActionRedactLadder: redact sits after block on the ask
// and deny ladders, never on flag's, and only on a seam that can redact.
func TestResolvePromptActionRedactLadder(t *testing.T) {
	t.Parallel()
	redactOnly := PromptSeamCaps{Redact: true}
	cases := []struct {
		name     string
		decision policy.Decision
		caps     PromptSeamCaps
		want     PromptAction
		wanted   PromptAction
	}{
		{"ask on redact-only seam", policy.DecisionAsk, redactOnly, PromptActionRedact, PromptActionConfirm},
		{"deny on redact-only seam", policy.DecisionDeny, redactOnly, PromptActionRedact, PromptActionBlock},
		{"flag never redacts", policy.DecisionFlag, redactOnly, PromptActionFlagAfter, PromptActionWarn},
		{"block beats redact on deny", policy.DecisionDeny, PromptSeamCaps{Block: true, Redact: true}, PromptActionBlock, ""},
		{"reenter beats redact on ask", policy.DecisionAsk, PromptSeamCaps{Reenter: true, Block: true, Redact: true}, PromptActionReenter, PromptActionConfirm},
		{"redact beats warn on deny", policy.DecisionDeny, PromptSeamCaps{Warn: true, Redact: true}, PromptActionRedact, PromptActionBlock},
	}
	for _, tc := range cases {
		got, wanted := ResolvePromptAction(tc.decision, tc.caps)
		if got != tc.want || wanted != tc.wanted {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", tc.name, got, wanted, tc.want, tc.wanted)
		}
		if tc.decision == policy.DecisionDeny && (got == PromptActionConfirm || got == PromptActionReenter) {
			t.Errorf("%s: a deny became approvable", tc.name)
		}
	}
	if PromptActionRedact.Stops() || PromptActionRedact.SuppressesToast() {
		t.Error("a redact forwards the prompt and still toasts")
	}
}

// TestWithActionRedactWanted walks the redactWanted table: requested
// redact mode masks a fresh interrupt and a confirmed resend on any
// redact-capable seam; a seam that cannot stop masks a confirmed resend
// (the developer was never told how to confirm); grants, guard errors and
// seams without Redact never mask.
func TestWithActionRedactWanted(t *testing.T) {
	t.Parallel()
	stopsOrRedacts := PromptSeamCaps{Reenter: true, Block: true, Redact: true}
	redactOnly := PromptSeamCaps{Redact: true}
	pv := func(o PromptOutcome, d policy.Decision, mode PromptMode) PromptVerdict {
		return PromptVerdict{Verdict: policy.Verdict{Decision: d, RuleID: "R-172"}, Outcome: o, RequestedMode: mode}
	}
	cases := []struct {
		name string
		pv   PromptVerdict
		caps PromptSeamCaps
		want PromptAction
	}{
		{"redact mode, fresh, stop-capable seam", pv(PromptOutcomeBlocked, policy.DecisionAsk, PromptModeRedact), stopsOrRedacts, PromptActionRedact},
		{"redact mode, confirmed resend", pv(PromptOutcomeConfirmed, policy.DecisionFlag, PromptModeRedact), stopsOrRedacts, PromptActionRedact},
		{"redact mode, no_session deny", pv(PromptOutcomeBlocked, policy.DecisionDeny, PromptModeRedact), stopsOrRedacts, PromptActionRedact},
		{"redact mode, seam cannot redact", pv(PromptOutcomeBlocked, policy.DecisionAsk, PromptModeRedact), PromptSeamCaps{Reenter: true, Block: true}, PromptActionReenter},
		{"ask-once mode, stop-capable seam", pv(PromptOutcomeBlocked, policy.DecisionAsk, ""), stopsOrRedacts, PromptActionReenter},
		{"ask-once mode, confirmed resend, stop-capable seam", pv(PromptOutcomeConfirmed, policy.DecisionFlag, ""), stopsOrRedacts, PromptActionNone},
		{"ask-once mode, redact-only seam, fresh", pv(PromptOutcomeBlocked, policy.DecisionAsk, ""), redactOnly, PromptActionRedact},
		{"ask-once mode, redact-only seam, confirmed", pv(PromptOutcomeConfirmed, policy.DecisionFlag, ""), redactOnly, PromptActionRedact},
		{"operator grant never masks", pv(PromptOutcomeApproved, policy.DecisionFlag, PromptModeRedact), stopsOrRedacts, PromptActionNone},
		{"warn mode never masks", pv(PromptOutcomeWarned, policy.DecisionFlag, ""), redactOnly, PromptActionFlagAfter},
	}
	for _, tc := range cases {
		got := tc.pv.WithAction(tc.caps, "hook")
		if got.Action != tc.want {
			t.Errorf("%s: action %q, want %q", tc.name, got.Action, tc.want)
		}
		if got.Action == PromptActionRedact && got.Outcome != PromptOutcomeWarned {
			t.Errorf("%s: a redacted prompt went through; outcome %q", tc.name, got.Outcome)
		}
	}
	guardErr := pv(PromptOutcomeBlocked, policy.DecisionAsk, PromptModeRedact)
	guardErr.GuardError = true
	if got := guardErr.WithAction(stopsOrRedacts, "hook"); got.Action == PromptActionRedact {
		t.Error("a guard-error verdict must never be answered by a redact")
	}
}

// TestPromptEmissionAndBridgeForRedact: a redact is a forwarded, enforced
// emission and persists as a non-blocking flag that still toasts.
func TestPromptEmissionAndBridgeForRedact(t *testing.T) {
	t.Parallel()
	g := newTestGuard(t, promptProxyCfg("redact", false), nil)
	pv := PromptVerdict{
		Verdict: policy.Verdict{Decision: policy.DecisionAsk, RuleID: "R-172", Reason: "secret"},
		Outcome: PromptOutcomeBlocked, RequestedMode: PromptModeRedact, Fingerprint: "fp",
	}.WithAction(PromptSeamCaps{Reenter: true, Block: true, Redact: true}, "hook")
	em := PromptEmission(pv)
	if em.Permission != "allow" || !em.Enforced {
		t.Fatalf("emission %+v, want an enforced allow", em)
	}
	av := g.ActionVerdictFromPrompt(pv, em, ActionInput{SessionID: "s", Tool: "poolside"})
	if av.Verdict.Decision != policy.DecisionFlag || av.SuppressAlert || !strings.Contains(av.Verdict.Reason, "[action=redact]") {
		t.Fatalf("bridge %+v", av)
	}
}

// TestRedactPromptText: a certain secret is masked and the masked text
// rescans clean; PII, an allow-listed value or a clean prompt are never
// offered as a redact.
func TestRedactPromptText(t *testing.T) {
	t.Parallel()
	g := newTestGuard(t, promptProxyCfg("redact", false), nil)
	masked, ok := g.RedactPromptText("use this token " + promptTestSecret + " please")
	if !ok || strings.Contains(masked, promptTestSecret) || !strings.Contains(masked, "[REDACTED:") {
		t.Fatalf("masked=%q ok=%v", masked, ok)
	}
	if _, ok := g.RedactPromptText("card " + promptTestPAN + " and " + promptTestSecret); ok {
		t.Error("a prompt that still carries PII after masking must not be offered as a redact")
	}
	if _, ok := g.RedactPromptText("nothing to see"); ok {
		t.Error("a clean prompt is not a redact")
	}
}
