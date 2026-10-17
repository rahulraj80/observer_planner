package main

import (
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/guardposture"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestGuardHookFailureSeam pins T-346: the Security page's hook failure mode
// is the hook's own per-family answer, whatever [guard].strict says.
func TestGuardHookFailureSeam(t *testing.T) {
	for _, tc := range []struct {
		name         string
		config       string
		posture      *guardposture.Posture
		tool, prompt bool
	}{
		{
			name:    "enforcing posture, not strict: both families fail closed",
			config:  "[guard]\nenabled = true\nmode = \"enforce\"\nstrict = false\n",
			posture: &enforcedPosture,
			tool:    true, prompt: true,
		},
		{
			name:    "tool family only",
			config:  "[guard]\nenabled = true\nmode = \"enforce\"\n",
			posture: &guardposture.Posture{Enforced: true, Source: guardposture.SourceLocal, GateCaps: []string{string(integration.HookCapToolGate)}},
			tool:    true, prompt: false,
		},
		{
			name:    "strict, observe mode, posture not enforced: fail open",
			config:  "[guard]\nenabled = true\nmode = \"observe\"\nstrict = true\n",
			posture: &guardposture.Posture{Source: guardposture.SourceLocal},
			tool:    false, prompt: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cp := filepath.Join(dir, "config.toml")
			writeFileT(t, cp, "[observer]\ndb_path = \""+filepath.ToSlash(filepath.Join(dir, "o.db"))+"\"\n"+tc.config)
			if tc.posture != nil {
				writePosture(t, cp, *tc.posture)
			}
			got := guardHookFailureSeam(cp)()
			if got.ToolGateClosed != tc.tool || got.PromptGateClosed != tc.prompt {
				t.Errorf("seam = %+v, want tool=%v prompt=%v", got, tc.tool, tc.prompt)
			}
		})
	}
}
