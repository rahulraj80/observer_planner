package hook

import (
	"sort"
	"strings"
	"testing"
)

// TestHookTargetWritersTable pins the registrar table that replaced the
// Register / Unregister switches (lane MP-ADP): the same 20 targets in the
// same table, every row carrying both directions, and no target in both
// tables (the tool-gate-only table is consulted first, so an overlap would
// silently shadow a row).
func TestHookTargetWritersTable(t *testing.T) {
	want := []string{
		"antigravity-cli", "claude-code", "claude-code-windows", "codex", "codex-windows",
		"command-code", "command-code-windows", "cursor", "cursor-windows", "devin",
		"droid", "droid-windows", "gemini-cli", "gemini-cli-windows", "poolside",
		"poolside-windows", "qoder", "qoder-windows", "qwen-code", "qwen-code-windows",
	}
	var got []string
	for k, w := range hookTargetWriters {
		got = append(got, k)
		if w.register == nil || w.unregister == nil {
			t.Errorf("%s: a row without both directions", k)
		}
		if _, dup := toolGateWriters[k]; dup {
			t.Errorf("%s is in both writer tables", k)
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("targets:\n got %v\nwant %v", got, want)
	}
}

// TestUnknownHookTargetIsRefused keeps the fall-through: a target in neither
// table and with no plugin-shim capability is refused in both directions.
func TestUnknownHookTargetIsRefused(t *testing.T) {
	r, err := NewRegistry(Options{HomeDir: t.TempDir(), BinaryPath: "/usr/local/bin/observer", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res := r.Register("no-such-tool"); res.Error == nil || !strings.Contains(res.Error.Error(), "not supported") {
		t.Fatalf("register: %+v", res)
	}
	if res := r.Unregister("no-such-tool"); res.Error == nil || !strings.Contains(res.Error.Error(), "not supported") {
		t.Fatalf("unregister: %+v", res)
	}
}
