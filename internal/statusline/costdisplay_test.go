package statusline

import (
	"strings"
	"testing"
)

// TestRenderByCostDisplay pins the org's developer cost display on the line
// (operator ruling 2026-10-02): token mode shows token usage for the session
// and today segments and never a dollar figure, not even Claude Code's own
// stdin cost; dollar mode is the pre-change line.
func TestRenderByCostDisplay(t *testing.T) {
	stdinUSD, sessUSD, todayUSD := 0.91, 0.90, 0.90
	sessTok, todayTok := int64(180_400), int64(1_234_567)
	budgetUSD, budgetTok := "budget 120% of $0.75/day", "budget 120%"
	in := Input{Cost: &Cost{TotalCostUSD: &stdinUSD}}
	cases := []struct {
		name string
		in   Input
		tile DaemonTile
		want string
	}{
		{
			"dollars, stdin cost wins", in,
			DaemonTile{SessionUSD: &sessUSD, TodayUSD: &todayUSD, SessionTokens: &sessTok, TodayTokens: &todayTok, Budget: &budgetUSD},
			Wordmark + " · $0.91 session · $0.90 today · budget 120% of $0.75/day",
		},
		{
			"empty display is dollars",
			Input{},
			DaemonTile{CostDisplay: "", SessionUSD: &sessUSD, TodayUSD: &todayUSD},
			Wordmark + " · $0.90 session · $0.90 today",
		},
		{
			"tokens", in,
			DaemonTile{CostDisplay: CostDisplayTokens, SessionUSD: &sessUSD, TodayUSD: &todayUSD, SessionTokens: &sessTok, TodayTokens: &todayTok, Budget: &budgetTok},
			Wordmark + " · 180k tok session · 1.2M tok today · budget 120%",
		},
		{
			"tokens with no daemon figures omits the segments", in,
			DaemonTile{CostDisplay: CostDisplayTokens, Budget: &budgetTok},
			Wordmark + " · budget 120%",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Render(tc.in, tc.tile, RenderOptions{})
			if got != tc.want {
				t.Fatalf("line = %q\nwant   %q", got, tc.want)
			}
			if tc.tile.CostDisplay == CostDisplayTokens && strings.Contains(got, "$") {
				t.Fatalf("token display rendered a dollar figure: %q", got)
			}
		})
	}
}

func TestFormatTokens(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{-5, "0"},
		{0, "0"},
		{950, "950"},
		{999, "999"},
		{1000, "1.0k"},
		{1234, "1.2k"},
		{9_949, "9.9k"},
		{9_950, "10k"},
		{180_400, "180k"},
		{999_499, "999k"},
		{999_500, "1.0M"},
		{1_234_567, "1.2M"},
		{9_950_000, "10M"},
		{42_000_000, "42M"},
	}
	for _, c := range cases {
		if got := formatTokens(c.n); got != c.want {
			t.Errorf("formatTokens(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
