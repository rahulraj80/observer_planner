package policy

import (
	"math"
	"regexp"
	"strconv"
)

// The DOLLAR budget reason, and its token-display rendering (operator ruling
// 2026-10-02: an org admin chooses whether developers see dollar costs or only
// token usage). This file is the ONE owner of the dollar reason template: the
// two USD matchers (matchCostOver, subjectCapBreach) render through
// usdBudgetReason, and BudgetReasonWithoutDollars parses exactly that shape,
// so the writer and the reader cannot drift (TestBudgetReasonWithoutDollars
// drives the real matchers).

// usdBudgetReason renders "<label> spend $X exceeds|has reached the $Y
// budget<note>".
func usdBudgetReason(label string, got, lim float64, note string) string {
	comparison := " exceeds the $"
	if got == lim {
		comparison = " has reached the $"
	}
	// no-bare-usd:allow (the dollar templates' one owner; the WithoutDollars readers render the token view)
	return label + " spend $" + strconv.FormatFloat(got, 'f', 2, 64) +
		comparison + strconv.FormatFloat(lim, 'f', 2, 64) + " budget" + note
}

// usdBudgetReasonRE matches usdBudgetReason's output anywhere at the END of a
// reason (a verdict reason is "<rule title>: <detail>", and the org label
// rewrites the title, so the template is matched as a suffix).
var usdBudgetReasonRE = regexp.MustCompile(`^(.*) spend \$(-?[0-9]+\.[0-9]+) (exceeds|has reached) the \$([0-9]+\.[0-9]+) budget(.*)$`)

// BudgetReasonWithoutDollars rewrites a dollar budget verdict reason for a
// developer whose org chose token display: the amounts become the share of the
// budget used, e.g.
//
//	"daily spend $0.90 exceeds the $0.75 budget"   -> "daily spend is at 120% of the budget"
//	"daily spend $0.75 has reached the $0.75 budget" -> "daily spend has reached 100% of the budget"
//
// Everything around the template (the rule title, the org label, the
// cross-machine note) is kept. ok=false when reason carries no dollar budget
// template (a token budget, an accounting-unavailable reason, any other rule):
// the caller keeps it verbatim. A dollar amount is never converted into
// tokens - that would fabricate a number.
func BudgetReasonWithoutDollars(reason string) (string, bool) {
	m := usdBudgetReasonRE.FindStringSubmatch(reason)
	if m == nil {
		return reason, false
	}
	got, err1 := strconv.ParseFloat(m[2], 64)
	lim, err2 := strconv.ParseFloat(m[4], 64)
	if err1 != nil || err2 != nil || lim <= 0 {
		return reason, false
	}
	pct := strconv.Itoa(int(math.Round(got / lim * 100)))
	if m[3] == "has reached" {
		return m[1] + " spend has reached " + pct + "% of the budget" + m[5], true
	}
	return m[1] + " spend is at " + pct + "% of the budget" + m[5], true
}

// SessionCostConditionReason renders a user policy rule's session-spend
// condition (guard policy file `match.session_cost_usd_gt`) as
// "session spend $X > $Y". It is the ONE owner of that template, so
// CostConditionReasonWithoutDollars can parse exactly what it writes.
func SessionCostConditionReason(got, lim float64) string {
	return "session spend $" + strconv.FormatFloat(got, 'f', 2, 64) +
		" > $" + strconv.FormatFloat(lim, 'f', 2, 64)
}

// sessionCostConditionRE matches SessionCostConditionReason's output anywhere
// in a verdict reason (a rule with several conditions joins their reasons).
var sessionCostConditionRE = regexp.MustCompile(`session spend \$(-?[0-9]+\.[0-9]+) > \$([0-9]+\.[0-9]+)`)

// CostConditionReasonWithoutDollars rewrites every session-spend condition in
// a verdict reason for a developer whose org chose token display (T-204):
//
//	"session spend $1.30 > $1.00" -> "session spend is at 130% of the rule's limit"
//
// The rest of the reason is kept. ok=false when the reason carries no such
// condition. Like BudgetReasonWithoutDollars it never converts dollars into
// tokens.
func CostConditionReasonWithoutDollars(reason string) (string, bool) {
	changed := false
	out := sessionCostConditionRE.ReplaceAllStringFunc(reason, func(m string) string {
		sub := sessionCostConditionRE.FindStringSubmatch(m)
		got, err1 := strconv.ParseFloat(sub[1], 64)
		lim, err2 := strconv.ParseFloat(sub[2], 64)
		if err1 != nil || err2 != nil || lim <= 0 {
			return "session spend is over the rule's limit"
		}
		changed = true
		return "session spend is at " + strconv.Itoa(int(math.Round(got/lim*100))) + "% of the rule's limit"
	})
	return out, changed || out != reason
}
