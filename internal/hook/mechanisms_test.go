package hook

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestRegistryWritesMatchesDispatch keeps RegistryWrites equal to what
// Registry.Register actually dispatches (T-400): for every integration
// registry row, a dry-run Register must find a writer exactly when the row's
// mechanism is in the table. A writer added without a row (or a row without a
// writer) fails here instead of silently mis-reporting the Connected-tools
// hooks cell or `observer init`'s hook step.
func TestRegistryWritesMatchesDispatch(t *testing.T) {
	for _, c := range integration.Capabilities() {
		c := c
		t.Run(c.Tool, func(t *testing.T) {
			reg, err := NewRegistry(Options{BinaryPath: "/opt/observer/bin/observer", HomeDir: t.TempDir(), DryRun: true})
			if err != nil {
				t.Fatal(err)
			}
			// register, not Register: the exported wrapper rewrites the
			// error for a tool whose config holds credentials (hermes), and
			// only the dispatch itself is under test here.
			res := reg.register(c.Tool)
			unsupported := res.Error != nil && strings.Contains(res.Error.Error(), "not supported for hook registration")
			if want := RegistryWrites(c.Hook.Mechanism); want == unsupported {
				t.Fatalf("mechanism %q: RegistryWrites=%v but Register error = %v", c.Hook.Mechanism, want, res.Error)
			}
		})
	}
}
