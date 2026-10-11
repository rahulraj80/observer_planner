package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegisterAntigravityCLIOccupiedKey pins Sol A2-09: an existing value
// under observer's hook name that is not positively observer's is a
// conflict without --force (file untouched), replaced with a .bak under
// --force, and never deleted by unregister. A stale observer entry is
// refreshed without --force.
func TestRegisterAntigravityCLIOccupiedKey(t *testing.T) {
	t.Parallel()
	const foreign = `{"superbased-observer":{"userOwnedHandler":"custom-command"}}`
	const foreignCmd = `{"superbased-observer":{"enabled":true,"PreInvocation":[{"type":"command","command":"/opt/acme/audit --note hook antigravity-cli"}]}}`
	const staleOurs = `{"superbased-observer":{"enabled":true,"PreInvocation":[{"type":"command","command":"/old/path/observer hook antigravity-cli PreInvocation","timeout":30}]}}`

	setup := func(t *testing.T, body string, force bool) (*Registry, string) {
		t.Helper()
		r := genericSettingsRegistry(t, filepath.Join(".gemini", "config"))
		r.opts.Force = force
		path := filepath.Join(r.opts.HomeDir, ".gemini", "config", "hooks.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return r, path
	}

	for name, body := range map[string]string{"foreign object": foreign, "foreign command": foreignCmd} {
		t.Run("conflict without force/"+name, func(t *testing.T) {
			r, path := setup(t, body, false)
			res := r.Register("antigravity-cli")
			if res.Error == nil || !strings.Contains(res.Error.Error(), "--force") {
				t.Fatalf("Register = %+v, want a --force conflict", res)
			}
			if raw, _ := os.ReadFile(path); string(raw) != body {
				t.Fatalf("file changed on conflict: %s", raw)
			}
			un := r.Unregister("antigravity-cli")
			if un.Error != nil || len(un.HooksRemoved) != 0 {
				t.Fatalf("Unregister = %+v, want nothing removed", un)
			}
			if raw, _ := os.ReadFile(path); string(raw) != body {
				t.Fatalf("unregister deleted a foreign entry: %s", raw)
			}
		})
	}

	t.Run("force replaces with backup", func(t *testing.T) {
		r, path := setup(t, foreign, true)
		if res := r.Register("antigravity-cli"); res.Error != nil || len(res.HooksAdded) != 2 {
			t.Fatalf("Register = %+v", res)
		}
		if bak, err := os.ReadFile(path + ".bak"); err != nil || string(bak) != foreign {
			t.Fatalf("backup = %q, %v", bak, err)
		}
		raw, _ := os.ReadFile(path)
		if !strings.Contains(string(raw), "hook antigravity-cli PreToolUse") {
			t.Fatalf("not registered: %s", raw)
		}
	})

	t.Run("stale observer entry refreshes without force", func(t *testing.T) {
		r, path := setup(t, staleOurs, false)
		if res := r.Register("antigravity-cli"); res.Error != nil || len(res.HooksAdded) != 2 {
			t.Fatalf("Register = %+v", res)
		}
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), "/old/path") {
			t.Fatalf("stale entry kept: %s", raw)
		}
		if _, err := os.Stat(path + ".bak"); err == nil {
			t.Error("a refresh of observer's own entry wrote a backup")
		}
	})
}
