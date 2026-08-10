package turnkind

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		name     string
		in       ClassifierInput
		want     Kind
		wantRule string
	}{
		{
			name:     "long_context_band",
			in:       ClassifierInput{PromptTokens: 250_000},
			want:     KindLongContext,
			wantRule: "long_context_band",
		},
		{
			name:     "routed_kind_trusted",
			in:       ClassifierInput{PromptTokens: 1000, WasRoutedKind: "edit"},
			want:     KindEdit,
			wantRule: "routed_kind",
		},
		{
			name:     "routed_overrides_long_context",
			in:       ClassifierInput{PromptTokens: 250_000, WasRoutedKind: "plan"},
			want:     KindPlan,
			wantRule: "routed_kind",
		},
		{
			name:     "tool_use_heavy_output",
			in:       ClassifierInput{PromptTokens: 1000, ResponseTokens: 2000, HasToolUse: true},
			want:     KindEdit,
			wantRule: "tool_use_heavy_output",
		},
		{
			name:     "tool_use_early_turn",
			in:       ClassifierInput{PromptTokens: 1000, ResponseTokens: 100, HasToolUse: true, SessionTurnIndex: 1},
			want:     KindPlan,
			wantRule: "tool_use_early_turn",
		},
		{
			name:     "tool_use_general",
			in:       ClassifierInput{PromptTokens: 1000, ResponseTokens: 100, HasToolUse: true, SessionTurnIndex: 5},
			want:     KindEdit,
			wantRule: "tool_use_general",
		},
		{
			name:     "first_turn_small_response_plan",
			in:       ClassifierInput{PromptTokens: 1000, ResponseTokens: 200, SessionTurnIndex: 0},
			want:     KindPlan,
			wantRule: "first_turn_small_response",
		},
		{
			name:     "first_turn_exploration",
			in:       ClassifierInput{PromptTokens: 1000, ResponseTokens: 800, SessionTurnIndex: 0},
			want:     KindReadOnly,
			wantRule: "first_turn_exploration",
		},
		{
			name:     "small_response_mid_session",
			in:       ClassifierInput{PromptTokens: 1000, ResponseTokens: 100, SessionTurnIndex: 3},
			want:     KindReadOnly,
			wantRule: "small_response_mid_session",
		},
		{
			name:     "large_response_likely_test",
			in:       ClassifierInput{PromptTokens: 500, ResponseTokens: 1500, SessionTurnIndex: 2},
			want:     KindTestRun,
			wantRule: "large_response_likely_test",
		},
		{
			name:     "no_response",
			in:       ClassifierInput{PromptTokens: 1000, ResponseTokens: 0},
			want:     KindUnknown,
			wantRule: "no_response",
		},
		{
			name:     "heuristic_unknown",
			in:       ClassifierInput{PromptTokens: 1000, ResponseTokens: 500, SessionTurnIndex: 2},
			want:     KindUnknown,
			wantRule: "heuristic_unknown",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotRule := Classify(tc.in)
			if got != tc.want {
				t.Fatalf("Classify() = %q, want %q", got, tc.want)
			}
			if gotRule != tc.wantRule {
				t.Fatalf("Classify() rule = %q, want %q", gotRule, tc.wantRule)
			}
		})
	}
}

func TestAllKinds(t *testing.T) {
	kinds := AllKinds()
	if len(kinds) != 8 {
		t.Fatalf("AllKinds() = %d, want 8", len(kinds))
	}
	seen := make(map[Kind]bool)
	for _, k := range kinds {
		if seen[k] {
			t.Fatalf("duplicate kind %q", k)
		}
		seen[k] = true
	}
	for _, k := range []Kind{KindPlan, KindReadOnly, KindEdit, KindTestRun, KindHousekeeping, KindSubagent, KindLongContext, KindUnknown} {
		if !seen[k] {
			t.Fatalf("missing kind %q", k)
		}
	}
}
