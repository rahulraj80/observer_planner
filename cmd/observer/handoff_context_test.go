package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/models"
)

// Hook-parity Lane F (2026-10-03): session-start context injection by
// capability.

func TestJSONMergeWriter(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, first, want string }{
		{"empty object", "{}\n", `{"additional_context":"ctx"}` + "\n"},
		{"existing key kept", `{"additional_context":"mine","continue":true}` + "\n", `{"additional_context":"mine","continue":true}` + "\n"},
		{"not json passes through", "plain\n", "plain\n"},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		w := &jsonMergeWriter{w: &buf, fields: map[string]any{"additional_context": "ctx"}}
		if _, err := w.Write([]byte(tc.first)); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("tail"))
		if got := buf.String(); got != tc.want+"tail" {
			t.Errorf("%s: %q", tc.name, got)
		}
	}
	codex := sessionContextFields["codex"]("h")
	if hso, ok := codex["hookSpecificOutput"].(map[string]any); !ok || hso["additionalContext"] != "h" || hso["hookEventName"] != "SessionStart" {
		t.Fatalf("codex context fields %+v", codex)
	}
}

// TestSessionContextBindingGate: the injection only runs for a tool whose
// handoff capability offers the hook lane AND whose session-start
// context_inject binding has a reply row. Codex and Cursor have the rows
// but not (yet) the hook lane, so nothing is claimed for them; Claude Code
// keeps its own SessionStart builder.
func TestSessionContextBindingGate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tool, event string
		want        bool
	}{
		{models.ToolCodex, "SessionStart", hasInjectHookFor(models.ToolCodex)},
		{models.ToolCursor, "sessionStart", hasInjectHookFor(models.ToolCursor)},
		{models.ToolClaudeCode, "SessionStart", false},
		{models.ToolCodex, "Stop", false},
	} {
		if _, ok := sessionContextBinding(tc.tool, tc.event); ok != tc.want {
			t.Errorf("%s/%s: %v, want %v", tc.tool, tc.event, ok, tc.want)
		}
	}
	var buf bytes.Buffer
	if w := wrapSessionStartContext(context.Background(), &buf, models.ToolCodex, "Stop", []byte(`{}`), ""); w != &buf {
		t.Error("a non-session-start event must not be wrapped")
	}
}

func hasInjectHookFor(tool string) bool {
	c, ok := integration.For(tool)
	return ok && hasInjectHook(c.Handoff)
}
