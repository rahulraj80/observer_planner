package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestHookComponentDecisionTable pins one case per row of the ordered
// decision table.
func TestHookComponentDecisionTable(t *testing.T) {
	for _, r := range hookComponentRows {
		if got := hookComponentDecision(r.adapterActive, r.planFailsClose); got != r.verdict {
			t.Errorf("%s: got %v want %v", r.name, got, r.verdict)
		}
	}
}

// TestLeftoverHookOfInactiveAdapterRecordsNothing is the hook-runtime half
// of the L2 adapter gate (MODPKG-14 residual 3): a Claude Code hook that
// outlived `remove adapter.claude-code` answers the host's allow and writes
// no row; the same invocation with the adapter active records the event.
func TestLeftoverHookOfInactiveAdapterRecordsNothing(t *testing.T) {
	requireAdapter(t, "claude-code")
	const body = `{"session_id":"s-inert","hook_event_name":"PostToolUseFailure","tool_name":"Bash","tool_input":{"command":"ls"},"error":"boom","cwd":"/w"}`
	for _, tc := range []struct {
		name     string
		block    string
		wantRows int
	}{
		{name: "adapter active", block: "", wantRows: 1},
		{name: "adapter removed", block: "[components]\nremove = [\"adapter.claude-code\"]\n", wantRows: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantRows > 0 {
				requireActiveAdapters(t, "claude-code")
			}
			e := newBootstrapEnvMode(t, "observe", tc.block)
			h := newFailureHarness(t, hookFailurePlan{})
			prev := hookPostureFailsClosedFn
			hookPostureFailsClosedFn = func(string, string, integration.HookCap) hookPostureAnswer { return hookPostureAnswer{} }
			t.Cleanup(func() { hookPostureFailsClosedFn = prev })
			t.Setenv("OBSERVER_HOOK_MAX_RUNTIME", "")
			withStdinT(t, body, func() {
				runHookInvocation(context.Background(), []string{"claude-code", "post-tool-failure"}, e.cp, 0, e.exit)
			})
			if out := h.stdout.String(); !strings.Contains(out, "approve") {
				t.Fatalf("host reply is not the allow: %q", out)
			}
			if got := countSessionActions(t, filepath.Join(filepath.Dir(e.cp), "o.db"), "s-inert"); got != tc.wantRows {
				t.Fatalf("actions rows for the session: got %d want %d", got, tc.wantRows)
			}
			if tc.wantRows == 0 {
				if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".observer", "hook-events.jsonl")); err == nil {
					t.Fatal("an inactive adapter's hook left a hook-events.jsonl receipt")
				}
			}
		})
	}
}

// countSessionActions counts a session's action rows, 0 when the DB was
// never created.
func countSessionActions(t *testing.T, path, session string) int {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro", path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM actions WHERE session_id = ?`, session).Scan(&n); err != nil {
		if strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "unable to open") || strings.Contains(err.Error(), "out of memory") {
			return 0
		}
		t.Fatal(err)
	}
	return n
}
