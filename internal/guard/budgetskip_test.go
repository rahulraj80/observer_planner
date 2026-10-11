package guard

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestBudgetLookupSkippedWhenNothingReadsIt pins T-418: with no budget cap of
// any kind configured, neither the watcher-ingest lane nor the proxy
// admission lane runs the node-wide accounting lookup, however many sessions
// a batch touches. One row per configuration; the last two are the controls
// that must keep the lookup on.
func TestBudgetLookupSkippedWhenNothingReadsIt(t *testing.T) {
	t.Parallel()
	costRule := map[string]string{
		"/home/u/.observer/guard-policy.toml": "[[rule]]\n" +
			"id = 'U-700'\ncategory = 'budget'\ndecision = 'flag'\n" +
			"severity = 'high'\napplies_to = ['shell_exec']\n" +
			"match.session_cost_usd_gt = 2.5\n",
	}
	for _, tc := range []struct {
		name       string
		cfg        func() config.GuardConfig
		files      map[string]string
		wantLookup bool
	}{
		{name: "no cap configured", cfg: guardCfg, wantLookup: false},
		{name: "a local dollar cap", cfg: func() config.GuardConfig { return budgetCfg("observe", false) }, wantLookup: true},
		{name: "a local token cap", cfg: func() config.GuardConfig {
			c := guardCfg()
			c.Budget.DailyTokens = 1000
			return c
		}, wantLookup: true},
		{name: "a usage-window threshold", cfg: func() config.GuardConfig {
			c := guardCfg()
			c.Budget.Window.Util5hWarn = 0.8
			return c
		}, wantLookup: true},
		{name: "a policy-file spend matcher", cfg: func() config.GuardConfig {
			c := guardCfg()
			c.Rules.UserPolicy = "/home/u/.observer/guard-policy.toml"
			return c
		}, files: costRule, wantLookup: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := newTestGuard(t, tc.cfg(), tc.files)
			if issues := g.LoadIssues(); len(issues) != 0 {
				t.Fatalf("load issues: %v", issues)
			}
			var calls atomic.Int64
			g.SetBudgetLookup(func(string) (BudgetSnapshot, bool) { calls.Add(1); return BudgetSnapshot{}, true })
			now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
			// A backlog replay across ten sessions: before the fix, ten
			// node-wide reads with nothing to compare them against.
			var in []ActionInput
			for i := 0; i < 10; i++ {
				in = append(in, budgetIn(fmt.Sprintf("s%d", i), "echo hi", now))
			}
			g.EvaluateActions(in)
			var res ProxyRequestResult
			g.scanBudget(g.set.Load(), &res, "proxy-s", "api.anthropic.com", now)
			if got := calls.Load(); (got > 0) != tc.wantLookup {
				t.Fatalf("budget lookups = %d, want lookup=%v", got, tc.wantLookup)
			}
		})
	}
}
