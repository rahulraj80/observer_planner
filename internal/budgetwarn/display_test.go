package budgetwarn

import (
	"strings"
	"testing"
	"time"
)

// TestRenderMessageByDisplay pins the developer-facing sentence in both cost
// displays (operator ruling 2026-10-02: the org chooses whether developers see
// dollar costs or only token usage). One row per (unit, display, enforcement)
// shape.
func TestRenderMessageByDisplay(t *testing.T) {
	usdSoft := Notice{Window: "daily", Unit: "usd", Scope: "member:dana", Enforcement: "soft", Threshold: 1, Ratio: 1.2, Spent: 0.9, Cap: 0.75}
	usdHard := Notice{Window: "monthly", Unit: "usd", Scope: "team:web", Enforcement: "hard", Threshold: 1, Ratio: 1.05, Spent: 21, Cap: 20}
	usdHardWarn := Notice{Window: "daily", Unit: "usd", Scope: "org", Enforcement: "hard", Threshold: 0.75, Ratio: 0.8, Spent: 8, Cap: 10}
	tokSoft := Notice{Window: "daily", Unit: "tokens", Scope: "member:dana", Enforcement: "report", Threshold: 0.75, Ratio: 0.8, Spent: 960000, Cap: 1200000}
	cases := []struct {
		name    string
		n       Notice
		display string
		want    string
	}{
		{
			"usd cap, dollar display", usdSoft, DisplayUSD,
			"Org daily budget (you, warning only): 120% used - $0.90 of $0.75. Requests are not blocked.",
		},
		{
			"usd cap, token display", usdSoft, DisplayTokens,
			"Org daily budget (you, warning only): 120% used of the budget. Requests are not blocked.",
		},
		{
			"usd hard exhausted, token display", usdHard, DisplayTokens,
			"Org monthly budget (your team, blocking): 105% used of the budget. Further proxied requests, and the next prompt or tool call in tools with a pre-send hook, are blocked until the period resets or an admin raises the cap.",
		},
		{
			"usd hard below 100, token display", usdHardWarn, DisplayTokens,
			"Org daily budget (your organization, blocking): 80% used of the budget. Requests will be blocked at 100%.",
		},
		{
			"token cap, dollar display", tokSoft, DisplayUSD,
			"Org daily budget (you, report only): 80% used - 960,000 tokens of 1,200,000 tokens. Requests are not blocked.",
		},
		{
			"token cap, token display", tokSoft, DisplayTokens,
			"Org daily budget (you, report only): 80% used - 960,000 tokens of 1,200,000 tokens. Requests are not blocked.",
		},
		{
			"unknown display falls back to dollars", usdSoft, "",
			"Org daily budget (you, warning only): 120% used - $0.90 of $0.75. Requests are not blocked.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderMessage(tc.n, tc.display)
			if got != tc.want {
				t.Fatalf("RenderMessage = %q\nwant            %q", got, tc.want)
			}
			if tc.display == DisplayTokens && strings.Contains(got, "$") {
				t.Fatalf("token display rendered a dollar figure: %q", got)
			}
		})
	}
}

// TestNoticeForRendersDollars keeps the evaluator's own message (the
// pre-display baseline every surface re-renders from) in dollars.
func TestNoticeForRendersDollars(t *testing.T) {
	w := Window{Window: "daily", Unit: "usd", Cap: 0.75, Spent: 0.72, Scope: "member:x", Enforcement: "soft"}
	n := noticeFor(w, "2026-10-02", 0.9, 0.96, time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC))
	if want := RenderMessage(n, DisplayUSD); n.Message != want {
		t.Fatalf("noticeFor message %q, want %q", n.Message, want)
	}
}

// TestStatusLabelByDisplay pins the statusline's short budget form.
func TestStatusLabelByDisplay(t *testing.T) {
	cases := []struct {
		name                        string
		window, unit, enforce, disp string
		capV, ratio, crossed        float64
		want                        string
	}{
		{"usd daily, dollars", "daily", "usd", "soft", DisplayUSD, 0.75, 1.68, 1, "budget 168% of $0.75/day"},
		{"usd daily, tokens", "daily", "usd", "soft", DisplayTokens, 0.75, 1.68, 1, "budget 168%"},
		{"usd monthly hard blocked, tokens", "monthly", "usd", "hard", DisplayTokens, 20, 1.05, 1, "budget 105% (blocked)"},
		{"usd monthly hard blocked, dollars", "monthly", "usd", "hard", DisplayUSD, 20, 1.05, 1, "budget 105% of $20.00/mo (blocked)"},
		{"token cap, tokens", "daily", "tokens", "report", DisplayTokens, 1200000, 0.8, 0.75, "budget 80% of 1,200,000 tokens/day"},
		{"token cap, dollars", "daily", "tokens", "report", DisplayUSD, 1200000, 0.8, 0.75, "budget 80% of 1,200,000 tokens/day"},
		{"hard below 100 is not blocked", "daily", "usd", "hard", DisplayUSD, 10, 0.9, 0.9, "budget 90% of $10.00/day"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StatusLabel(tc.window, tc.unit, tc.capV, tc.ratio, tc.enforce, tc.crossed, tc.disp); got != tc.want {
				t.Fatalf("StatusLabel = %q, want %q", got, tc.want)
			}
		})
	}
}
