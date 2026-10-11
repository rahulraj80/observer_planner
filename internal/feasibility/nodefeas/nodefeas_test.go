package nodefeas

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/feasibility"
)

// TestComponentReasonRows: one case per componentReasons row, plus the
// feasible fall-through.
func TestComponentReasonRows(t *testing.T) {
	cases := []struct {
		d          components.Decision
		selected   bool
		feasible   bool
		effect     feasibility.Effect
		reasonPart string
	}{
		{components.Decision{ID: "codeintel", State: components.StateActive, Reason: components.ReasonSelected}, true, true, feasibility.EffectNone, ""},
		{components.Decision{ID: "codeintel", State: components.StateAbsent, Reason: components.ReasonNotCompiled}, true, false, feasibility.EffectOff, "not in this build"},
		{components.Decision{ID: "codeintel", State: components.StateAbsent, Reason: components.ReasonNotCompiled}, false, false, feasibility.EffectUnavailable, "builds that include it"},
		{components.Decision{ID: "terminals.ssh", State: components.StateBlocked, Reason: components.ReasonRequiresNotCompiled, Because: "terminals"}, false, false, feasibility.EffectUnavailable, "needs terminals"},
		{components.Decision{ID: "codeintel", State: components.StateBlocked, Reason: components.ReasonDenied}, false, false, feasibility.EffectUnavailable, "[components].remove"},
		{components.Decision{ID: "terminals.ssh", State: components.StateBlocked, Reason: components.ReasonDeniedDependency, Because: "terminals"}, false, false, feasibility.EffectUnavailable, "switched off"},
		{components.Decision{ID: "codeintel", State: components.StateBlocked, Reason: components.ReasonAboveCeiling}, false, false, feasibility.EffectUnavailable, "ceiling"},
		{components.Decision{ID: "codeintel", State: components.StateBlocked, Reason: components.ReasonPolicyDenied}, false, false, feasibility.EffectUnavailable, "denied by your organization"},
		{components.Decision{ID: "codeintel", State: components.StateBlocked, Reason: components.ReasonPolicyUnlisted}, false, false, feasibility.EffectUnavailable, "not allowed"},
		{components.Decision{ID: "codeintel", State: components.StateRequiredMissing, Reason: components.ReasonPolicyRequired}, false, false, feasibility.EffectUnavailable, "required by your organization"},
	}
	for _, tc := range cases {
		t.Run(string(tc.d.Reason)+"/"+string(tc.d.State), func(t *testing.T) {
			var sel []string
			if tc.selected {
				sel = []string{tc.d.ID}
			}
			v := ComponentVerdicts([]components.Decision{tc.d}, sel)[0]
			if v.Feasible != tc.feasible || v.Effect != tc.effect {
				t.Fatalf("got %+v", v)
			}
			if tc.reasonPart != "" && !strings.Contains(v.Reason, tc.reasonPart) {
				t.Fatalf("reason %q lacks %q", v.Reason, tc.reasonPart)
			}
			if !v.Feasible && (v.Fix == "" || v.FixStep == "") {
				t.Fatalf("an infeasible option needs a fix and a step: %+v", v)
			}
		})
	}
}

// TestSettingsStartOff: a setting that needs an inactive or absent
// component, or a host fact that does not hold, is "off" with the reason;
// never a refusal (no node setting is unsafe to run without).
func TestSettingsStartOff(t *testing.T) {
	decisions := []components.Decision{
		{ID: "remote", State: components.StateAvailable, Reason: components.ReasonNotSelected},
		{ID: "terminals.sandbox", State: components.StateActive, Reason: components.ReasonSelected},
		{ID: "obs", State: components.StateAbsent, Reason: components.ReasonNotCompiled},
	}
	on := map[string]bool{"dashboard.addr.nonloopback": true, "terminal.sandbox.enabled": true, "observability.admission.enabled": true}
	rep := Settings(on, decisions, map[string]string{FactSandboxReady: "bubblewrap is not installed"})
	for id, part := range map[string]string{
		"dashboard.addr.nonloopback":      "remote is installed but not active",
		"terminal.sandbox.enabled":        "bubblewrap is not installed",
		"observability.admission.enabled": "not in this build",
	} {
		v, ok := rep.Get(id)
		if !ok || v.Effect != feasibility.EffectOff || !strings.Contains(v.Reason, part) {
			t.Errorf("%s = %+v, want off with %q", id, v, part)
		}
	}
	if len(rep.Refusals()) != 0 {
		t.Fatalf("no node setting may refuse the start: %+v", rep.Refusals())
	}
}

// TestSettingRulesNameRealComponents: every component a rule needs is a
// node catalog row (a stale id would make the rule silently feasible).
func TestSettingRulesNameRealComponents(t *testing.T) {
	for _, r := range SettingRules {
		for _, n := range r.Needs {
			if strings.HasPrefix(n, "host.") {
				continue
			}
			if c, ok := components.Get(n); !ok || c.Binary != components.BinaryNode {
				t.Errorf("%s needs %s, which is not a node component", r.ID, n)
			}
		}
	}
}
