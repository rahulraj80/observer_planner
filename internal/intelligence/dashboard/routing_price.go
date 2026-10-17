package dashboard

import (
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
	"github.com/marmutapp/superbased-observer/internal/routingcore"
)

// enginePriceFn adapts the dashboard's cost engine to the routing
// vocabulary's injected pricing seam (the same adaptation the CLI's
// routingPriceFn makes). Nil engine gives nil, so callers keep the facts'
// default pricing. It speaks only the core internal/routingcore, so the
// model value report keeps it in a build without proxy.routing (lane
// MP-L1I, T-715).
func enginePriceFn(engine *cost.Engine) routingcore.PriceFn {
	if engine == nil {
		return nil
	}
	return func(model string, u routingcore.PromptUsage) (float64, bool) {
		return engine.Compute(model, cost.TokenBundle{
			Input: u.Input, Output: u.Output, CacheRead: u.CacheRead,
			CacheCreation: u.CacheCreation, CacheCreation1h: u.CacheCreation1h,
			Reasoning: u.Reasoning, WebSearchRequests: u.WebSearchRequests, Fast: u.Fast,
		})
	}
}
