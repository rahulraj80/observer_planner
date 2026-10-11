package otlpvendor

import "testing"

// TestOutputExcludesReasoningByTotal pins T-542's per-record rule: under
// ReasoningByTotal the record's own total decides whether the gross output
// adds reasoning back; the static flag is only the fallback.
func TestOutputExcludesReasoningByTotal(t *testing.T) {
	byTotal := Normalize{ReasoningByTotal: true}
	byTotalFallbackExcl := Normalize{ReasoningByTotal: true, OutputExcludesReasoning: true}
	for _, tc := range []struct {
		name                            string
		n                               Normalize
		input, output, total, reasoning int64
		want                            bool
	}{
		{"openai shape: reasoning inside output", byTotal, 1234, 56, 1290, 16, false},
		{"openai shape beats an excluding fallback", byTotalFallbackExcl, 1234, 56, 1290, 16, false},
		{"gemini shape: reasoning beside output", byTotal, 1234, 56, 1306, 16, true},
		{"no total: fallback false", byTotal, 1234, 56, 0, 16, false},
		{"no total: fallback true", byTotalFallbackExcl, 1234, 56, 0, 16, true},
		{"inconsistent total: fallback", byTotalFallbackExcl, 1234, 56, 9999, 16, true},
		{"no reasoning: fallback", byTotal, 1234, 56, 1290, 0, false},
		{"flag off ignores total", Normalize{OutputExcludesReasoning: true}, 1234, 56, 1290, 16, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := outputExcludesReasoning(tc.n, tc.input, tc.output, tc.total, tc.reasoning); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
