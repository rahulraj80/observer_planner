package guard

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestGateEnforcement pins Sol A2-01: the prompt gate is enforced by the
// prompt lane's own rule (independent prompt enforcement, the default),
// even in observe mode, and that never closes the tool gate. Both the
// config-only fallback and the live guard answer the same.
func TestGateEnforcement(t *testing.T) {
	base := config.Default().Guard
	base.Enabled = true
	for _, tc := range []struct {
		name string
		mut  func(g *config.GuardConfig)
		want GateEnforcement
	}{
		{"default observe: independent prompt enforcement", func(g *config.GuardConfig) { g.Mode = "observe" }, GateEnforcement{Prompt: true}},
		{"enforce: both", func(g *config.GuardConfig) { g.Mode = "enforce" }, GateEnforcement{Tool: true, Prompt: true}},
		{"observe, independence off", func(g *config.GuardConfig) {
			g.Mode = "observe"
			g.Prompt.EnforceIndependent = false
		}, GateEnforcement{}},
		{"enforce, independence off", func(g *config.GuardConfig) {
			g.Mode = "enforce"
			g.Prompt.EnforceIndependent = false
		}, GateEnforcement{Tool: true, Prompt: true}},
		{"prompt warn only", func(g *config.GuardConfig) {
			g.Mode = "observe"
			g.Prompt.Mode = "warn"
			g.Prompt.Detectors = nil
		}, GateEnforcement{}},
		{"prompt warn with a blocking detector override", func(g *config.GuardConfig) {
			g.Mode = "observe"
			g.Prompt.Mode = "warn"
			g.Prompt.Detectors = map[string]string{"github_pat": "block"}
		}, GateEnforcement{Prompt: true}},
		{"prompt off beats detector overrides", func(g *config.GuardConfig) {
			g.Mode = "enforce"
			g.Prompt.Mode = "off"
			g.Prompt.Detectors = map[string]string{"github_pat": "block"}
		}, GateEnforcement{Tool: true}},
		{"hook lane off", func(g *config.GuardConfig) {
			g.Mode = "observe"
			g.Prompt.HookLane = false
		}, GateEnforcement{}},
		{"guard mode off", func(g *config.GuardConfig) { g.Mode = "off" }, GateEnforcement{}},
		{"guard disabled", func(g *config.GuardConfig) {
			g.Mode = "enforce"
			g.Enabled = false
		}, GateEnforcement{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := base
			g.Prompt.Detectors = map[string]string{}
			for k, v := range base.Prompt.Detectors {
				g.Prompt.Detectors[k] = v
			}
			tc.mut(&g)
			if got := ConfigGateEnforcement(g); got != tc.want {
				t.Errorf("ConfigGateEnforcement = %+v, want %+v", got, tc.want)
			}
			if !g.Enabled {
				return
			}
			gd, err := New(Options{Config: g, Home: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if got := gd.GateEnforcement(); got != tc.want {
				t.Errorf("Guard.GateEnforcement = %+v, want %+v", got, tc.want)
			}
		})
	}
}
