package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/budgetwarnfile"
	"github.com/marmutapp/superbased-observer/internal/hook"
)

// TestBudgetNoticeOnNextPrompt pins the in-band warning contract: Claude
// Code's allow reply gains a top-level systemMessage carrying the active
// notice, exactly once node-wide; a dialect with no documented user-visible
// field is left untouched; an existing systemMessage is never overwritten.
func TestBudgetNoticeOnNextPrompt(t *testing.T) {
	db := filepath.Join(t.TempDir(), "observer.db")
	now := time.Date(2026, 10, 2, 5, 19, 0, 0, time.UTC)
	if err := budgetwarnfile.Write(budgetwarnfile.StatusPath(db), budgetwarnfile.File{
		Active: []budgetwarnfile.Notice{{
			ID: "daily-usd-2026-10-02-1", Threshold: 1, CrossedAt: now.Add(-time.Minute),
			Message: "Org daily budget (you, warning only): 168% used - $1.26 of $0.75. Requests are not blocked.",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	reply := func(dialect string, v any) map[string]any {
		var buf bytes.Buffer
		w := wrapBudgetNotice(&buf, dialect, db, now)
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatalf("reply is not JSON: %q", buf.String())
		}
		return m
	}

	// A dialect without a user-visible field: untouched, notice stays pending.
	if m := reply(hook.PromptDialectTopLevelBlock, map[string]any{}); m["systemMessage"] != nil {
		t.Fatalf("top-level-block dialect got a notice: %v", m)
	}
	// An existing systemMessage is never overwritten (and stays pending).
	if m := reply(hook.PromptDialectClaudeCode, map[string]any{"systemMessage": "guard"}); m["systemMessage"] != "guard" {
		t.Fatalf("existing systemMessage overwritten: %v", m)
	}
	// Claude Code: the notice rides the allow reply.
	m := reply(hook.PromptDialectClaudeCode, hook.ClaudeCodePromptApproveReply())
	msg, _ := m["systemMessage"].(string)
	if msg == "" || m["hookSpecificOutput"] == nil {
		t.Fatalf("claude-code reply = %v", m)
	}
	if want := "Observer: Org daily budget (you, warning only)"; len(msg) < len(want) || msg[:len(want)] != want {
		t.Fatalf("systemMessage = %q", msg)
	}
	// Once per period, node-wide: the next prompt (any session) is clean.
	if m := reply(hook.PromptDialectClaudeCode, hook.ClaudeCodePromptApproveReply()); m["systemMessage"] != nil {
		t.Fatalf("notice shown twice: %v", m)
	}
	// Gemini has the same documented field.
	_ = budgetwarnfile.Write(budgetwarnfile.StatusPath(db), budgetwarnfile.File{
		Active: []budgetwarnfile.Notice{{ID: "monthly-usd-2026-10-0.9", Threshold: 0.9, CrossedAt: now, Message: "m"}},
	})
	if m := reply(hook.PromptDialectGemini, map[string]any{}); m["systemMessage"] != "Observer: m" {
		t.Fatalf("gemini reply = %v", m)
	}
}
