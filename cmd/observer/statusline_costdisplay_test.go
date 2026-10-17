package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/statusline"
)

// TestStatuslineJSONByCostDisplay pins `observer statusline --json` in both
// developer cost displays: dollar mode keeps the pre-change shape (no
// cost_display / token keys), token mode nulls every dollar field (including
// Claude Code's stdin session cost) and carries the token figures instead.
func TestStatuslineJSONByCostDisplay(t *testing.T) {
	stdinUSD, sessUSD, todayUSD := 0.91, 0.90, 0.90
	sessTok, todayTok := int64(180_000), int64(180_000)
	in := statusline.Input{Cost: &statusline.Cost{TotalCostUSD: &stdinUSD}}
	cases := []struct {
		name      string
		display   string
		wantKeys  map[string]any
		forbidden []string
	}{
		{
			"dollars", "usd",
			map[string]any{"today_usd": 0.90, "session_usd": 0.91},
			[]string{"cost_display", "today_tokens", "session_tokens"},
		},
		{
			"tokens", statusline.CostDisplayTokens,
			map[string]any{"cost_display": "tokens", "today_tokens": 180000.0, "session_tokens": 180000.0, "today_usd": nil, "session_usd": nil},
			nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tile := statusline.DaemonTile{
				CostDisplay: c.display, SessionUSD: &sessUSD, TodayUSD: &todayUSD,
				SessionTokens: &sessTok, TodayTokens: &todayTok,
			}
			var buf strings.Builder
			if err := writeStatuslineJSON(&buf, "line", tile, in, "daemon"); err != nil {
				t.Fatal(err)
			}
			var raw map[string]any
			if err := json.Unmarshal([]byte(buf.String()), &raw); err != nil {
				t.Fatalf("decode: %v", err)
			}
			for k, want := range c.wantKeys {
				got, ok := raw[k]
				if !ok || got != want {
					t.Errorf("%s = %v (present %v), want %v", k, got, ok, want)
				}
			}
			for _, k := range c.forbidden {
				if _, ok := raw[k]; ok {
					t.Errorf("dollar mode must keep the pre-change shape; found %q", k)
				}
			}
		})
	}
}
