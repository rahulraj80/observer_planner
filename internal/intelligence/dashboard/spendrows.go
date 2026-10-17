package dashboard

import (
	"context"
	"net/http"
	"time"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
)

// scopeFromRequest reads the global filters (`tool`, `project`, `model`)
// from a dashboard request.
func scopeFromRequest(r *http.Request) dashapi.SpendScope {
	q := r.URL.Query()
	return dashapi.SpendScope{Tool: q.Get("tool"), Project: q.Get("project"), Model: modelParam(r)}
}

// spendTurns loads the deduped spend rows for [since, until) (a zero until
// is open-ended) scoped by the dashboard's global tool / project / model
// filters. sessionIDs, when non-empty, scopes to those sessions.
func (s *Server) spendTurns(ctx context.Context, since, until time.Time, scope dashapi.SpendScope, sessionIDs []string) ([]dashapi.SpendTurn, error) {
	return dashapi.LoadSpendTurns(ctx, s.db(), s.opts.CostEngine, since, until, scope, sessionIDs)
}

// turnCostAndStandard returns a turn's actual cost and what it would have
// cost at standard (non-long-context) rates - the pair the Analysis tab
// compares to flag a long-context turn. A recorded cost is ground truth and
// has no table counterfactual here (both equal it); an unpriced turn is 0/0.
func (s *Server) turnCostAndStandard(t dashapi.SpendTurn) (rowCost, rowStdCost float64) {
	if t.Recorded > 0 {
		return t.CostUSD, t.CostUSD
	}
	if p, ok := s.opts.CostEngine.LookupAt(t.Model, t.At); ok {
		return t.CostUSD, cost.Compute(stripLongContext(p), t.Bundle)
	}
	return 0, 0
}
