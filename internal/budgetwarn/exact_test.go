package budgetwarn

import (
	"testing"
	"time"
)

// TestEvaluateExactDecimalBoundaries (Sol A5-05, D-016): spend that lands
// EXACTLY on a 75 / 90 / 100 percent rung must announce that rung. With the
// float form `spent/cap >= rung`, $0.075 of a $0.10 cap is 0.7499999999999999
// and $0.09 of $0.10 is 0.8999999999999999, so those rungs were missed until
// more spend arrived.
func TestEvaluateExactDecimalBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		unit  string
		cap   float64
		spent float64
		want  float64 // highest rung reached
	}{
		{"$0.075 of $0.10 is exactly 75%", "usd", 0.10, 0.075, 0.75},
		{"$0.09 of $0.10 is exactly 90%", "usd", 0.10, 0.09, 0.9},
		{"$0.10 of $0.10 is exactly 100%", "usd", 0.10, 0.10, 1},
		{"$0.30 of $0.40 is exactly 75%", "usd", 0.40, 0.30, 0.75},
		{"$0.63 of $0.70 is exactly 90%", "usd", 0.70, 0.63, 0.9},
		{"$7.50 of $10 is exactly 75%", "usd", 10, 7.5, 0.75},
		{"$0.074999 of $0.10 is below 75%", "usd", 0.10, 0.074999, 0},
		{"$0.089999 of $0.10 is still 75%", "usd", 0.10, 0.089999, 0.75},
		{"750 of 1000 tokens is exactly 75%", "tokens", 1000, 750, 0.75},
		{"749 of 1000 tokens is below 75%", "tokens", 1000, 749, 0},
		{"900 of 1000 tokens is exactly 90%", "tokens", 1000, 900, 0.9},
		{"a huge token cap does not overflow", "tokens", 9e15, 9e15, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, _ := Evaluate([]Window{{
				Window: "daily", Unit: tc.unit, Cap: tc.cap, Spent: tc.spent, Scope: "member:m1",
				Enforcement: "soft", Thresholds: []float64{0.75, 0.9, 1}, Timezone: "UTC",
			}}, State{}, now)
			if len(st.Windows) != 1 {
				t.Fatalf("windows = %+v", st.Windows)
			}
			if got := st.Windows[0].Crossed; got != tc.want {
				t.Errorf("crossed = %v, want %v", got, tc.want)
			}
			if tc.want > 0 && (len(st.New) != 1 || st.New[0].Threshold != tc.want) {
				t.Errorf("new = %+v, want one notice at %v", st.New, tc.want)
			}
		})
	}
}
