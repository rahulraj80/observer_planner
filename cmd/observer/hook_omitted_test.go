//go:build sbo_omit_adapter_codex || sbo_omit_adapter_open_interpreter || sbo_omit_adapter_cursor

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
)

// TestOmittedAdapterGate pins the gate-only receiver a build registers for
// a compiled-out adapter whose hook mechanism serves its own gates (lane
// MP-L1D, T-598; hook_omitted.go): the prompt gate still decides (a secret
// in the prompt is blocked in the host's own dialect), and nothing is
// captured (no actions row). The L2 adapter gate routes a leftover hook here
// only when its plan fails closed; this calls the receiver directly.
func TestOmittedAdapterGate(t *testing.T) {
	const secret = "my key is sk-ant-api03-1234567890ABCDEFGhijKLmnopqRSTuvwxyz"
	cases := []struct {
		tool, event, body string
		blocked           func(map[string]any) bool
	}{
		{
			tool: "codex", event: "UserPromptSubmit",
			body:    `{"session_id":"s1","turn_id":"t1","cwd":"/r","hook_event_name":"UserPromptSubmit","prompt":"` + secret + `"}`,
			blocked: func(r map[string]any) bool { d, _ := r["decision"].(string); return d == "block" },
		},
		{
			tool: "cursor", event: "beforeSubmitPrompt",
			body:    `{"conversation_id":"c1","generation_id":"g1","hook_event_name":"beforeSubmitPrompt","workspace_roots":["/r"],"prompt":"` + secret + `","attachments":[]}`,
			blocked: func(r map[string]any) bool { c, ok := r["continue"].(bool); return ok && !c },
		},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			if !adapterCompiledOut(c.tool) {
				t.Skipf("adapter.%s is compiled into this build", c.tool)
			}
			// The prompt-gate dialect may also signal a block by exit code
			// (codex: exit 2 beside the JSON block, which Codex honours).
			prevExit := hookOSExit
			var exitCodes []int
			hookOSExit = func(code int) { exitCodes = append(exitCodes, code) }
			t.Cleanup(func() { hookOSExit = prevExit })
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "observer.db")
			configPath := writePromptGuardConfig(t, dbPath)
			reply := runPromptSubmitHookOnce(t, c.tool, c.event, configPath, c.body)
			if !c.blocked(reply) {
				t.Fatalf("the prompt gate did not block in the %s dialect: %+v (exit codes %v)", c.tool, reply, exitCodes)
			}
			database, err := dbtemplate.Open(context.Background(), db.Options{Path: dbPath})
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			var n int
			if err := database.QueryRow(`SELECT COUNT(*) FROM actions`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Fatalf("the gate-only receiver captured %d actions rows; want none", n)
			}
		})
	}
}
