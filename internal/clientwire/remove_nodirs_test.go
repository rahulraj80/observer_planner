package clientwire

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestRemoveCreatesNothingOnACleanMachine pins T-742 / T-732 (D-551): a
// removal of every part of every registry tool (and its cross-OS target) on a
// machine with none of them installed writes nothing at all. Before the fix
// the settings lock created each tool's config directory, so a component
// teardown left ~20 empty vendor directories that the next
// `observer enroll --wire-clients` detected as installed tools and wired;
// and the plugin-shim hosts with no install location (deepseek, openclaw)
// reported an error for a removal that has nothing to do.
func TestRemoveCreatesNothingOnACleanMachine(t *testing.T) {
	home := t.TempDir()
	rm, err := NewRemover(RemoveOptions{
		BinaryPath: testBinary,
		HomeDir:    home,
		Getenv:     func(string) string { return "" },
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := []Part{PartHooks, PartMCP, PartRoute, PartTelemetry}
	for _, c := range integration.Capabilities() {
		for _, tool := range []string{c.Tool, c.Tool + WindowsSuffix} {
			m := rm.Remove(tool, parts...)
			if m.Failed() {
				t.Errorf("%s: removal on a clean machine failed: %+v", tool, removalErrors(m))
			}
		}
	}
	var created []string
	_ = filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err == nil && p != home {
			rel, _ := filepath.Rel(home, p)
			created = append(created, rel)
		}
		return nil
	})
	if len(created) > 0 {
		t.Fatalf("a removal on a clean machine created %d path(s): %s", len(created), strings.Join(created, ", "))
	}
}

func removalErrors(m Removal) []string {
	var out []string
	if m.RouteErr != nil {
		out = append(out, "route registrar: "+m.RouteErr.Error())
	}
	if m.Hooks != nil && m.Hooks.Error != nil {
		out = append(out, "hooks: "+m.Hooks.Error.Error())
	}
	if m.MCP != nil && m.MCP.Error != nil {
		out = append(out, "mcp: "+m.MCP.Error.Error())
	}
	if m.Route != nil && m.Route.Error != nil {
		out = append(out, "route: "+m.Route.Error.Error())
	}
	if m.Telemetry != nil && m.Telemetry.Error != nil {
		out = append(out, "telemetry: "+m.Telemetry.Error.Error())
	}
	return out
}
