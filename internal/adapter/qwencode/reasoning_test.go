package qwencode

import (
	"testing"
)

// TestTokenBundleReasoningConvention pins T-542, one row per rule of
// reasoningRules: output and reasoning must reach the TokenEvent DISJOINT,
// whichever content generator produced the turn.
func TestTokenBundleReasoningConvention(t *testing.T) {
	for _, tc := range []struct {
		name          string
		ev            rawUIEvent
		wantOutput    int64
		wantReasoning int64
	}{
		{
			// Live qwen-code 0.21.0 (OpenAI-compatible generator): the 16
			// thoughts sit inside the 56 output; 1234 + 56 == 1290.
			name:       "openai shape: total == input + output",
			ev:         rawUIEvent{AuthType: "openai", InputTokenCount: 1234, OutputTokenCount: 56, CachedTokenCount: 1000, ThoughtsTokens: 16, TotalTokenCount: 1290},
			wantOutput: 40, wantReasoning: 16,
		},
		{
			// Gemini generator: candidates exclude thoughts; the total adds
			// them on top. The arithmetic wins over the auth type.
			name:       "gemini shape: total == input + output + thoughts",
			ev:         rawUIEvent{AuthType: "openai", InputTokenCount: 1234, OutputTokenCount: 56, ThoughtsTokens: 16, TotalTokenCount: 1306},
			wantOutput: 56, wantReasoning: 16,
		},
		{
			name:       "no total, OpenAI-shaped auth",
			ev:         rawUIEvent{AuthType: "qwen-oauth", InputTokenCount: 100, OutputTokenCount: 50, ThoughtsTokens: 20},
			wantOutput: 30, wantReasoning: 20,
		},
		{
			name:       "no total, Gemini auth keeps disjoint",
			ev:         rawUIEvent{AuthType: "gemini-api-key", InputTokenCount: 100, OutputTokenCount: 50, ThoughtsTokens: 20},
			wantOutput: 50, wantReasoning: 20,
		},
		{
			name:       "no thoughts: output untouched",
			ev:         rawUIEvent{AuthType: "openai", InputTokenCount: 17883, OutputTokenCount: 81, TotalTokenCount: 17964},
			wantOutput: 81, wantReasoning: 0,
		},
		{
			name:       "thoughts larger than output never go negative",
			ev:         rawUIEvent{AuthType: "openai", InputTokenCount: 10, OutputTokenCount: 5, ThoughtsTokens: 5, TotalTokenCount: 15},
			wantOutput: 0, wantReasoning: 5,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tokenBundle(&tc.ev)
			if got.output != tc.wantOutput || got.reasoning != tc.wantReasoning {
				t.Fatalf("output=%d reasoning=%d, want %d / %d", got.output, got.reasoning, tc.wantOutput, tc.wantReasoning)
			}
		})
	}
}

// TestParseLiveSessionReasoningNotDoubled is the fixture half of T-542: the
// live 2026-07-09 session (glm-5.2 over the OpenAI-compatible generator)
// reports output 84 with 40 thoughts inside it (9786 + 84 == 9870), so the
// stored output is 44 and reasoning 40, not 84 + 40.
func TestParseLiveSessionReasoningNotDoubled(t *testing.T) {
	_, tokens, _ := parseFixture(t, "simple-session.jsonl")
	if len(tokens) != 1 {
		t.Fatalf("token events = %d, want 1", len(tokens))
	}
	if tokens[0].OutputTokens != 44 || tokens[0].ReasoningTokens != 40 {
		t.Fatalf("output=%d reasoning=%d, want 44 / 40 (reasoning counted once)", tokens[0].OutputTokens, tokens[0].ReasoningTokens)
	}
}
