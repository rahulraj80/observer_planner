package dashapi

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/repricesvc"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// RepriceService is the dashboard's seam onto opt-in retroactive re-pricing
// (gap PRICE-REPRICE-1): cmd wires an *repricesvc.Service, the ONE owner of
// the plan / apply / revert flow shared with `observer reprice`. Nil = not
// wired (the routes answer 501).
type RepriceService interface {
	Pricing(ctx context.Context) repricesvc.PricingInfo
	Plan(ctx context.Context, f repricesvc.Filter) (repricesvc.Plan, error)
	Apply(ctx context.Context, req repricesvc.Request) (store.RepriceRun, error)
	Revert(ctx context.Context, runID int64, actor string) (store.RepriceRun, error)
	Runs(ctx context.Context, limit int) ([]store.RepriceRun, error)
}
