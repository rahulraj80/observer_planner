package main

import (
	"context"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/hook"
	"github.com/marmutapp/superbased-observer/internal/models"
)

// Hook-parity Lane B (2026-10-03): binding-resolved prompt dialects.

// TestPromptOnlyReceiverResolvesBindingDialect: a second prompt event on a
// tool is answered in ITS binding's dialect (Devin CLI's UserPromptSubmit
// next to Desktop Cascade's pre_user_prompt); Kimi Code's prompt binding is
// served by the binding fallback with an EMPTY allow.
func TestPromptOnlyReceiverResolvesBindingDialect(t *testing.T) {
	ctx := context.Background()
	env := newObserveTestEnv(t, nil)
	out := captureStdio(t, `{"session_id":"d","prompt":"hi"}`, func() {
		hookReceivers["devin"](ctx, "UserPromptSubmit", env.cfgPath)
	})
	if strings.TrimSpace(out) != `{}` {
		t.Fatalf("devin UserPromptSubmit (devin-cli dialect allow) = %q", out)
	}
	out = captureStdio(t, `{"trajectory_id":"t","tool_info":{"user_prompt":"hi"}}`, func() {
		hookReceivers["devin"](ctx, "pre_user_prompt", env.cfgPath)
	})
	if strings.TrimSpace(out) != `{"decision":"approve"}` {
		t.Fatalf("devin pre_user_prompt (cascade allow) = %q", out)
	}
	b, ok := resolveHookBinding(models.ToolKimiCode, "UserPromptSubmit")
	if !ok || b.Dialect != hook.PromptDialectKimiCode {
		t.Fatalf("kimi prompt binding %+v ok=%v", b, ok)
	}
	out = captureStdio(t, `{"session_id":"k","prompt":"hi"}`, func() {
		if !dispatchBindingReceiver(ctx, models.ToolKimiCode, "UserPromptSubmit", env.cfgPath, b) {
			t.Error("kimi prompt binding must dispatch")
		}
	})
	if out != "" {
		t.Fatalf("kimi allow must be an empty stdout, got %q", out)
	}
	// A binding with no prompt dialect keeps the generic fallback.
	gb, ok := resolveHookBinding(models.ToolGoose, "UserPromptSubmit")
	if !ok || dispatchBindingReceiver(ctx, models.ToolGoose, "UserPromptSubmit", env.cfgPath, gb) {
		t.Fatal("goose has no prompt dialect: the generic fallback answers")
	}
}
