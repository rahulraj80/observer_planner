package integration

import "testing"

func TestWantsRegistration(t *testing.T) {
	prompt := HookBinding{Cap: HookCapPromptGate, Phase: HookPhasePre, Registered: true, Wired: true}
	life := HookBinding{Cap: HookCapSessionLifecycle, Phase: HookPhaseEnd, Registered: true, Wired: true}
	tool := HookBinding{Cap: HookCapToolGate, Phase: HookPhasePre, Registered: true, Wired: true}
	vendorOnly := HookBinding{Cap: HookCapSessionLifecycle, Phase: HookPhaseEnd}
	for _, tc := range []struct {
		name string
		b    []HookBinding
		f    HookFeatures
		want bool
	}{
		{"prompt only, lane on", []HookBinding{prompt}, HookFeatures{PromptGate: true}, true},
		{"prompt only, lane off", []HookBinding{prompt}, HookFeatures{}, false},
		{"prompt plus lifecycle, lane off", []HookBinding{prompt, life}, HookFeatures{}, true},
		{"tool gate, gate off", []HookBinding{tool}, HookFeatures{}, false},
		{"tool gate, gate on", []HookBinding{tool}, HookFeatures{ToolGate: true}, true},
		{"vendor-only lifecycle", []HookBinding{prompt, vendorOnly}, HookFeatures{}, false},
		{"nothing", nil, HookFeatures{PromptGate: true, ToolGate: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (HookSpec{Bindings: tc.b}).WantsRegistration(tc.f); got != tc.want {
				t.Fatalf("WantsRegistration = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPromptLaneOnlyMatchesWiredBindings keeps the legacy PromptLaneOnly
// flag honest now that the capability-shaped gate decides (Lane C2): a row
// flagged PromptLaneOnly must have only prompt-family wired bindings, and a
// row whose wired bindings are all prompt-family must carry the flag. The
// one exception is a row with NO wired binding at all (Antigravity's
// PreInvocation is registered by init only, unwired), where the flag is
// the only statement of intent.
func TestPromptLaneOnlyMatchesWiredBindings(t *testing.T) {
	for _, c := range Capabilities() {
		h := c.Hook
		anyWired := false
		for _, b := range h.Bindings {
			anyWired = anyWired || b.Wired
		}
		if !anyWired {
			continue
		}
		if h.PromptLaneOnly != h.WiredPromptOnly() {
			t.Errorf("%s: PromptLaneOnly = %v but WiredPromptOnly() = %v", c.Tool, h.PromptLaneOnly, h.WiredPromptOnly())
		}
	}
}
