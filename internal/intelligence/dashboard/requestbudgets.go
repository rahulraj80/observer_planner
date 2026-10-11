package dashboard

import (
	"math"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/accessgrant"
	"github.com/marmutapp/superbased-observer/internal/budgetwarnfile"
	"github.com/marmutapp/superbased-observer/internal/orgclient"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// requestbudgets.go - the budget picker on a budget-increase request (T-523,
// D-334) and the banner's way into it (T-522). A developer on an org-managed
// node never types a budget id: the picker lists the budgets the node's
// verified budget rail names, each joined with what the node's budget-warning
// service measured against it (budget-status.json), so the dialog can default
// to the one that ran out and suggest how much more it needs. The join is
// this file's one rule; the banner's notices name their budget through the
// same rule (budgetIDForWindow).

// requestBudgetView is one budget the request dialog offers.
type requestBudgetView struct {
	orgclient.RequestableBudget
	// Level is the budget's worst warning level this period ("ok",
	// "warning", "exceeded"); "" when nothing measured it here.
	Level string `json:"level,omitempty"`
	// Ratio is the highest spent/cap measured against it this period.
	Ratio float64 `json:"ratio,omitempty"`
	// OverTokens / OverUSD are how far spend is past the cap in each unit
	// (rounded up), the dialog's suggested increase.
	OverTokens int64   `json:"over_tokens,omitempty"`
	OverUSD    float64 `json:"over_usd,omitempty"`
	// PeriodEndNs is when the budget's current period ends: a budget
	// increase lasts at most until then (D-081).
	PeriodEndNs int64 `json:"period_end_ns,omitempty"`
}

// periodWindows maps an org budget period onto the warning service's window
// name; a period the service does not warn on (rolling_30d) has none.
var periodWindows = map[string]string{"calendar_day": "daily", "calendar_month": "monthly"}

// levelRank orders warning levels.
var levelRank = map[string]int{"ok": 1, "warning": 2, "exceeded": 3}

// budgetUnitCapped says whether b authored a cap in unit.
var budgetUnitCapped = map[string]func(b orgclient.RequestableBudget) bool{
	"tokens": func(b orgclient.RequestableBudget) bool { return b.CapTokens > 0 },
	"usd":    func(b orgclient.RequestableBudget) bool { return b.CapUSD > 0 },
}

// budgetSubjectKey renders a budget's subject the way the warning service
// keys a window ("tool:<id>", "model:<id>"; "" for the whole caller).
func budgetSubjectKey(s *orgcontract.BudgetSubject) string {
	kind, id, ok := orgcontract.BudgetPolicyCap{Subject: s}.SubjectKey()
	if !ok {
		return ""
	}
	return kind + ":" + id
}

// budgetMatchesWindow reports whether a measured warning window is this
// budget's: the same scope, period, subject, and a cap in the window's unit.
func budgetMatchesWindow(b orgclient.RequestableBudget, scope, window, unit, subject string) bool {
	capped, ok := budgetUnitCapped[unit]
	return ok && capped(b) && b.Scope == scope && periodWindows[b.Period] == window &&
		budgetSubjectKey(b.Subject) == strings.ToLower(strings.TrimSpace(subject))
}

// budgetIDForWindow names the budget a warning window or notice measures,
// "" when no listed budget matches exactly one way.
func budgetIDForWindow(budgets []orgclient.RequestableBudget, scope, window, unit, subject string) string {
	id := ""
	for _, b := range budgets {
		if !budgetMatchesWindow(b, scope, window, unit, subject) {
			continue
		}
		if id != "" && id != b.BudgetID {
			return ""
		}
		id = b.BudgetID
	}
	return id
}

// requestBudgetViews joins the listed budgets with the measured windows.
func requestBudgetViews(budgets []orgclient.RequestableBudget, windows []budgetwarnfile.Window, now time.Time) []requestBudgetView {
	out := make([]requestBudgetView, 0, len(budgets))
	for _, b := range budgets {
		v := requestBudgetView{RequestableBudget: b}
		if end := accessgrant.PeriodEnd(b.Period, b.Timezone, now); !end.IsZero() {
			v.PeriodEndNs = end.UnixNano()
		}
		for _, w := range windows {
			if !budgetMatchesWindow(b, w.Scope, w.Window, w.Unit, w.Subject) {
				continue
			}
			if levelRank[w.Level] > levelRank[v.Level] {
				v.Level = w.Level
			}
			v.Ratio = math.Max(v.Ratio, w.Ratio)
			over := w.Spent - w.Cap
			if !(over > 0) || math.IsInf(over, 0) {
				continue
			}
			if w.Unit == "tokens" {
				v.OverTokens = max(v.OverTokens, int64(math.Ceil(over)))
			} else {
				v.OverUSD = math.Max(v.OverUSD, math.Ceil(over*100)/100)
			}
		}
		out = append(out, v)
	}
	return out
}
