package integration

import (
	"testing"
	"time"
)

// eachBinding walks every binding of every registry row.
func eachBinding(t *testing.T, fn func(c Capability, b HookBinding)) {
	t.Helper()
	for _, c := range Capabilities() {
		for _, b := range c.Hook.Bindings {
			fn(c, b)
		}
	}
}

// TestHookCapVocabularyIsClosed pins the capability vocabulary itself: the
// eighteen capabilities of the hook-parity audit section 2 (StatusDisplay is
// not a hook on any vendor but Claude Code's statusLine, so it is not one).
func TestHookCapVocabularyIsClosed(t *testing.T) {
	caps := HookCaps()
	if len(caps) != 18 {
		t.Fatalf("HookCaps() = %d capabilities, want 18: %v", len(caps), caps)
	}
	for i := 1; i < len(caps); i++ {
		if caps[i-1] >= caps[i] {
			t.Errorf("HookCaps() not sorted at %d: %v", i, caps)
		}
	}
	for _, c := range caps {
		if len(hookCapPhases[c]) == 0 {
			t.Errorf("%s admits no phase", c)
		}
	}
	gates := map[HookCap]bool{
		HookCapToolGate: true, HookCapToolRewrite: true, HookCapPromptGate: true,
		HookCapPermissionGate: true, HookCapStopGate: true,
	}
	for _, c := range caps {
		if c.IsGate() != gates[c] {
			t.Errorf("%s.IsGate() = %v, want %v", c, c.IsGate(), gates[c])
		}
	}
	if HookCap("status_display").Known() {
		t.Error("status_display must not be a hook capability")
	}
}

// TestEveryBindingUsesClosedVocabularies: capability, phase, decisions,
// grounding kind, process relation and host failure policy all come from
// their closed sets, and every binding names its evidence.
func TestEveryBindingUsesClosedVocabularies(t *testing.T) {
	eachBinding(t, func(c Capability, b HookBinding) {
		id := c.Tool + "/" + b.NativeEvent
		if b.NativeEvent == "" {
			t.Errorf("%s: binding with no NativeEvent", c.Tool)
		}
		if !b.Cap.Known() {
			t.Errorf("%s: unknown capability %q", id, b.Cap)
		}
		if !b.Cap.ValidPhase(b.Phase) {
			t.Errorf("%s: phase %q not in %s's closed set %v", id, b.Phase, b.Cap, hookCapPhases[b.Cap])
		}
		for _, d := range b.Decisions {
			if !KnownHookDecision(d) {
				t.Errorf("%s: unknown decision %q", id, d)
			}
		}
		if !b.Grounding.Kind.Known() {
			t.Errorf("%s: unknown grounding kind %q", id, b.Grounding.Kind)
		}
		if b.Grounding.Source == "" || b.Grounding.Date == "" {
			t.Errorf("%s: grounding must name a Source and a Date, got %+v", id, b.Grounding)
		}
		if !b.ProcRel.Known() {
			t.Errorf("%s: unknown process relation %q", id, b.ProcRel)
		}
		if !b.HostOnFailure.Known() {
			t.Errorf("%s: unknown host failure policy %q", id, b.HostOnFailure)
		}
		if b.HostTimeout < 0 {
			t.Errorf("%s: negative host timeout", id)
		}
	})
}

// TestWiredBindingImpliesRegistered: an Observer feature can only act on an
// event Observer registers, except on a plugin / tailer transport whose
// registration is not one config entry per event.
func TestWiredBindingImpliesRegistered(t *testing.T) {
	eachBinding(t, func(c Capability, b HookBinding) {
		// AutoWired:false is a manual registration (zcode: the receiver
		// exists, the operator wires the config by hand). A binding reached
		// through another tool's replayed config (ReplayedVia) is registered
		// there, not in this row's own file.
		// A binding served by another transport (a log tailer, ServedElsewhere)
		// takes no config entry of its own either.
		if b.Wired && !b.Registered && b.ReplayedVia == "" && b.NeedsEventConfig(c.Hook.Mechanism) && c.Hook.AutoWired {
			t.Errorf("%s/%s: Wired without Registered on per-event-config mechanism %q", c.Tool, b.NativeEvent, c.Hook.Mechanism)
		}
		if b.Registered && c.Hook.Mechanism == HookNone {
			t.Errorf("%s/%s: Registered on a row with no hook mechanism", c.Tool, b.NativeEvent)
		}
	})
}

// TestWiredGateBindingHasDialectAndDecisions: a gate Observer acts on must
// name the reply dialect it speaks and what that reply can express.
func TestWiredGateBindingHasDialectAndDecisions(t *testing.T) {
	eachBinding(t, func(c Capability, b HookBinding) {
		if !b.Cap.IsGate() || !b.Wired {
			return
		}
		if b.Dialect == "" {
			t.Errorf("%s/%s: wired gate binding has no Dialect", c.Tool, b.NativeEvent)
		}
		if len(b.Decisions) == 0 {
			t.Errorf("%s/%s: wired gate binding has no Decisions", c.Tool, b.NativeEvent)
		}
	})
}

// TestHookSpecAccessors pins Provides / WiredFor / Binding /
// RegisteredEvents on a synthetic spec.
func TestHookSpecAccessors(t *testing.T) {
	h := HookSpec{Mechanism: HookClaudeSettings, Bindings: []HookBinding{
		{Cap: HookCapSessionLifecycle, Phase: HookPhaseStart, NativeEvent: "SessionStart", Registered: true, Wired: true},
		{Cap: HookCapContextInject, Phase: HookPhaseStart, NativeEvent: "SessionStart", Registered: true},
		{
			Cap: HookCapToolGate, Phase: HookPhasePre, NativeEvent: "PreToolUse", Registered: true, Wired: true,
			Decisions: []string{HookDecisionAllow, HookDecisionDeny}, HostTimeout: 60 * time.Second,
		},
		{Cap: HookCapCompactLifecycle, Phase: HookPhasePre, NativeEvent: "PreCompact"},
	}}
	if !h.Provides(HookCapCompactLifecycle) || h.Provides(HookCapStopGate) {
		t.Error("Provides wrong")
	}
	if !h.WiredFor(HookCapToolGate) || h.WiredFor(HookCapContextInject) || h.WiredFor(HookCapCompactLifecycle) {
		t.Error("WiredFor wrong")
	}
	b, ok := h.Binding("SessionStart")
	if !ok || b.Cap != HookCapSessionLifecycle {
		t.Errorf("Binding(SessionStart) = %+v, %v; want the primary session_lifecycle binding", b, ok)
	}
	if _, ok := h.Binding("Nope"); ok {
		t.Error("Binding(Nope) found")
	}
	got := h.RegisteredEvents()
	if len(got) != 2 || got[0] != "SessionStart" || got[1] != "PreToolUse" {
		t.Errorf("RegisteredEvents = %v", got)
	}
	gate, _ := h.Binding("PreToolUse")
	if !gate.HasDecision(HookDecisionDeny) || gate.HasDecision(HookDecisionAsk) {
		t.Error("HasDecision wrong")
	}
	if !HookClaudeSettings.PerEventConfig() || HookHermesPlugin.PerEventConfig() || HookNone.PerEventConfig() {
		t.Error("PerEventConfig wrong")
	}
}

// TestRegisteredAllLive: true only when there is a registered binding and
// every registered binding is live; vendor-only bindings do not count.
func TestRegisteredAllLive(t *testing.T) {
	live := Grounding{Kind: GroundingLive, Source: "x", Date: "d"}
	doc := Grounding{Kind: GroundingDoc, Source: "x", Date: "d"}
	cases := []struct {
		name string
		bs   []HookBinding
		want bool
	}{
		{"none", nil, false},
		{"vendor only", []HookBinding{{Grounding: live}}, false},
		{"all live", []HookBinding{{Registered: true, Grounding: live}, {Grounding: doc}}, true},
		{"one doc", []HookBinding{{Registered: true, Grounding: live}, {Registered: true, Grounding: doc}}, false},
	}
	for _, tc := range cases {
		if got := (HookSpec{Bindings: tc.bs}).RegisteredAllLive(); got != tc.want {
			t.Errorf("%s: RegisteredAllLive = %v, want %v", tc.name, got, tc.want)
		}
	}
}
