package proxy

// Node-activity and budget-warning seams (2026-10-02 budget-latency arc).
//
// The proxy sees every proxied request first, so it is the cheapest place to
// say "the developer is working" (the policy-freshness checker's trigger) and
// "a priced turn just committed" (the budget-warning ladder's and the
// activity-driven push's trigger). Both are ONE optional callback each; the
// proxy neither knows nor cares who listens (CLAUDE.md #2). A nil callback is
// the byte-identical pre-arc path.

// HeaderBudgetWarning is the soft-budget warning header the node proxy stamps
// on a proxied response, in the AI gateway's spelling
// (internal/aigateway/gwhttp.HeaderBudgetWarning): "<enforcement>/<scope>
// <pct>%" per window at or past a warn rung, semicolon-separated.
const HeaderBudgetWarning = "X-SBO-Budget-Warning"

// Activity kinds passed to Options.Activity.
const (
	// ActivityRequest is a proxied request arriving (before routing).
	ActivityRequest = "request"
	// ActivityTurn is an api_turn committed to the sink.
	ActivityTurn = "turn"
)

// noteActivity reports one activity event. The callback must not block.
func (p *Proxy) noteActivity(kind string) {
	if p == nil || p.activity == nil {
		return
	}
	p.activity(kind)
}

// budgetWarningValue returns the header value to stamp ("" = none).
func (p *Proxy) budgetWarningValue() string {
	if p == nil || p.budgetWarning == nil {
		return ""
	}
	return p.budgetWarning()
}
