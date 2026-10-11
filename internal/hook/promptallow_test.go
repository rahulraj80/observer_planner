package hook

import (
	"bytes"
	"strings"
	"testing"
)

// TestHandlePromptAllow pins the dialect-correct "nothing to say" reply
// (node 1 live test, 2026-10-03): Antigravity CLI rejected
// {"decision":"approve"} on every PreInvocation ("unknown field
// \"decision\""); its contract accepts only injectSteps or an empty
// object. Every dialect without its own allow shape keeps HandleApprove's
// reply byte for byte.
func TestHandlePromptAllow(t *testing.T) {
	t.Parallel()
	var approve bytes.Buffer
	HandleApprove("x", strings.NewReader("{}"), &approve, &bytes.Buffer{})
	cases := []struct {
		dialect string
		want    string
	}{
		{PromptDialectAntigravityPreInvocation, "{}\n"},
		{PromptDialectClaudeCode, approve.String()},
		{PromptDialectCursor, approve.String()},
		{PromptDialectGemini, approve.String()},
		{"", approve.String()},
		{"no-such-dialect", approve.String()},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		HandlePromptAllow(tc.dialect, "x", strings.NewReader(`{"conversationId":"c1"}`), &out, &bytes.Buffer{})
		if out.String() != tc.want {
			t.Errorf("HandlePromptAllow(%q) = %q, want %q", tc.dialect, out.String(), tc.want)
		}
	}
}

// TestPromptDialectAllowRepliesNeverCarryDecision: no dialect that
// declares its own allow shape may fall back to the generic decision
// field its host rejects.
func TestPromptDialectAllowRepliesNeverCarryDecision(t *testing.T) {
	t.Parallel()
	for name, d := range promptDialects {
		if d.allowReply == nil {
			continue
		}
		var out bytes.Buffer
		HandlePromptAllow(name, "x", strings.NewReader(""), &out, &bytes.Buffer{})
		if strings.Contains(out.String(), "decision") {
			t.Errorf("%s allow reply %q carries a decision field", name, out.String())
		}
	}
}
