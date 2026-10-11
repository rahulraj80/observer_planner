package integration

import "testing"

// TestLiveBlockingToolGateRule pins T-340's per-binding rule on synthetic
// rows, one case per rule condition: only a WIRED tool_gate binding whose
// reply can DENY and whose grounding is LIVE puts a row on hook_block. The
// mechanism plays no part, so Claude Code's own mechanism without such a
// binding falls through the ladder like any other row.
func TestLiveBlockingToolGateRule(t *testing.T) {
	live := Grounding{Kind: GroundingLive, Source: "test", Date: "2026-10-04"}
	doc := Grounding{Kind: GroundingDoc, Source: "test", Date: "2026-10-04"}
	gate := func(g Grounding, wired bool, decisions ...string) HookBinding {
		return HookBinding{Cap: HookCapToolGate, Phase: HookPhasePre, NativeEvent: "PreToolUse", Grounding: g, Wired: wired, Decisions: decisions}
	}
	launchable := HandoffCapability{Launch: &LaunchSpec{Subcommand: "x"}}
	for _, tc := range []struct {
		name string
		hook HookSpec
		want EnforcementChannel
	}{
		{"live wired deny gate", HookSpec{Mechanism: HookGrokHooksJSON, Bindings: []HookBinding{gate(live, true, HookDecisionDeny)}}, EnforceHookBlock},
		{"live wired deny gate on a plugin transport", HookSpec{Mechanism: HookPluginShimTS, Bindings: []HookBinding{gate(live, true, HookDecisionDeny, HookDecisionModify)}}, EnforceHookBlock},
		{"doc-grounded wired deny gate", HookSpec{Mechanism: HookGrokHooksJSON, Bindings: []HookBinding{gate(doc, true, HookDecisionDeny)}}, EnforceSandbox},
		{"live gate not wired", HookSpec{Mechanism: HookGrokHooksJSON, Bindings: []HookBinding{gate(live, false, HookDecisionDeny)}}, EnforceSandbox},
		{"live wired gate without deny", HookSpec{Mechanism: HookGrokHooksJSON, Bindings: []HookBinding{gate(live, true, HookDecisionModify)}}, EnforceSandbox},
		{"live wired prompt gate only", HookSpec{Mechanism: HookGrokHooksJSON, Bindings: []HookBinding{{Cap: HookCapPromptGate, Phase: HookPhasePre, NativeEvent: "UserPromptSubmit", Grounding: live, Wired: true, Decisions: []string{HookDecisionDeny}}}}, EnforceSandbox},
		{"claude mechanism with no live gate", HookSpec{Mechanism: HookClaudeSettings}, EnforceSandbox},
		{"cursor mechanism with no live gate", HookSpec{Mechanism: HookCursor, Bindings: []HookBinding{gate(doc, true, HookDecisionDeny)}}, EnforceSandbox},
	} {
		c := Capability{Tool: "synthetic", Hook: tc.hook, Handoff: launchable}
		if got := c.EnforcementChannel(); got != tc.want {
			t.Errorf("%s: EnforcementChannel() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestNodeEnforcementNeedsACurrentToolGate pins the node rule (T-340
// follow-up): hook_block holds only while this node's tool-gate hook is
// registered and current. Without it the tool falls back to the channel it
// would have with no hook, which the platform then degrades as usual. A row
// that was never hook_block ignores the gate flag.
func TestNodeEnforcementNeedsACurrentToolGate(t *testing.T) {
	live := Grounding{Kind: GroundingLive, Source: "test", Date: "2026-10-04"}
	gate := HookBinding{Cap: HookCapToolGate, Phase: HookPhasePre, NativeEvent: "PreToolUse", Grounding: live, Wired: true, Decisions: []string{HookDecisionDeny}}
	hooked := HookSpec{Mechanism: HookClaudeSettings, Bindings: []HookBinding{gate}}
	launchable := HandoffCapability{Launch: &LaunchSpec{Subcommand: "x"}}
	proxied := &ProxyRoute{}
	for _, tc := range []struct {
		name                 string
		c                    Capability
		gateCurrent, sandbox bool
		want                 EnforcementChannel
	}{
		{"hook registered and current", Capability{Hook: hooked, Handoff: launchable}, true, true, EnforceHookBlock},
		{"hook registered, no sandbox on this platform", Capability{Hook: hooked, Handoff: launchable}, true, false, EnforceHookBlock},
		{"hook missing or stale: sandbox", Capability{Hook: hooked, Handoff: launchable}, false, true, EnforceSandbox},
		{"hook missing, no sandbox: recorded acceptance", Capability{Hook: hooked, Handoff: launchable}, false, false, EnforceRecordedAcceptance},
		{"hook missing, not launchable, proxied: org disallow", Capability{Hook: hooked, Proxy: proxied}, false, true, EnforceOrgDisallow},
		{"never hook_block: gate flag ignored", Capability{Handoff: launchable}, true, true, EnforceSandbox},
	} {
		if got := tc.c.NodeEnforcement(tc.gateCurrent, tc.sandbox); got != tc.want {
			t.Errorf("%s: NodeEnforcement(%v, %v) = %q, want %q", tc.name, tc.gateCurrent, tc.sandbox, got, tc.want)
		}
	}
}

// TestEnforcementChannelFollowsLiveToolGateOnRegistry is the registry-wide
// form of the rule: every row with a live-verified blocking tool gate is
// hook_block, and no row without one is (T-340: Codex, Qoder, agy, Grok and
// the plugin-shim hosts showed sandbox_enforce while their live-verified
// PreToolUse deny already stopped the call).
func TestEnforcementChannelFollowsLiveToolGateOnRegistry(t *testing.T) {
	for _, c := range Capabilities() {
		live := false
		for _, b := range c.Hook.Bindings {
			if b.Cap == HookCapToolGate && b.Wired && b.Grounding.Kind == GroundingLive && b.HasDecision(HookDecisionDeny) {
				live = true
			}
		}
		got := c.EnforcementChannel()
		if live != (got == EnforceHookBlock) {
			t.Errorf("%s: live-verified blocking tool gate = %v but EnforcementChannel() = %q", c.Tool, live, got)
		}
	}
}
