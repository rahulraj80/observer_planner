package orgclient

import (
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// RequestableBudget is one org budget this node is governed by, as a
// developer picks it on a budget-increase request (orchestrator ruling on the
// request/grant lane's Q4, D-334). It comes from the verified budget rail the
// node already holds, so it names only budgets that apply to this caller; the
// org server re-checks that on receipt (T-523). The approver can still change
// the budget on the grant.
type RequestableBudget struct {
	BudgetID string                     `json:"budget_id"`
	Scope    string                     `json:"scope"`
	Period   string                     `json:"period"`
	Subject  *orgcontract.BudgetSubject `json:"subject,omitempty"`
	// CapTokens / CapUSD are the caps of the row BudgetID names, in the
	// units that row authored (a unit another row supplied is left out: it
	// is not this budget's number).
	CapTokens int64   `json:"cap_tokens,omitempty"`
	CapUSD    float64 `json:"cap_usd,omitempty"`
	// Timezone anchors the period (IANA; "" = UTC); Enforcement is the org
	// mode (report | soft | hard).
	Timezone    string `json:"timezone,omitempty"`
	Enforcement string `json:"enforcement,omitempty"`
	// Grantable is false for a pool shared with others (org, team, an
	// org-wide tool or model): a grant for one member never raises it
	// (C156-07), so the request reaches the approver as an ask to raise the
	// pool for everyone. It is listed because it may be the budget that ran
	// out (D-334).
	Grantable bool `json:"grantable"`
}

// RequestableBudgets lists the budgets named on the cached budget rail body,
// one per budget id, the ones a grant can raise for this member first.
// Empty when nothing is cached or the org server predates budget ids on the
// rail; the request then goes without one and the org fills it in or the
// approver picks it.
func (c *Client) RequestableBudgets() []RequestableBudget {
	if c == nil {
		return nil
	}
	_, body, have, _, _, _ := c.budget.snapshot()
	if !have {
		return nil
	}
	return requestableBudgets(body)
}

// requestableBudgets is RequestableBudgets over a body (pure, for tests).
func requestableBudgets(body orgcontract.BudgetPolicyBody) []RequestableBudget {
	seen := map[string]bool{}
	var out []RequestableBudget
	for _, cp := range body.Caps {
		if cp.BudgetID == "" || seen[cp.BudgetID] {
			continue
		}
		seen[cp.BudgetID] = true
		b := RequestableBudget{
			BudgetID: cp.BudgetID, Scope: cp.ResolvedScope, Period: cp.Period, Subject: cp.Subject,
			Timezone: cp.Timezone, Enforcement: cp.Enforcement,
			Grantable: strings.HasPrefix(cp.ResolvedScope, orgcontract.BudgetGrantableScopePrefix),
		}
		// BudgetID names the row behind ResolvedScope; a unit whose number a
		// different row supplied (ScopeTokens / ScopeUSD) is not its cap.
		if cp.ScopeTokens == "" || cp.ScopeTokens == cp.ResolvedScope {
			b.CapTokens = cp.CapTokens
		}
		if cp.ScopeUSD == "" || cp.ScopeUSD == cp.ResolvedScope {
			b.CapUSD = cp.CapUSD
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Grantable != out[j].Grantable {
			return out[i].Grantable
		}
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Period < out[j].Period
	})
	return out
}
