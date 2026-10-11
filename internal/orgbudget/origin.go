package orgbudget

import "github.com/marmutapp/superbased-observer/internal/orgcontract"

// Origin is the PROVENANCE of one effective node-window ceiling that the
// organization authored (2026-10-02 budget-latency arc). Compose emits one row
// per ADOPTED (window, unit) pair — an org cap that lost to a tighter local
// ceiling governs nothing and has no origin row, exactly as it carries no
// calendar and no enforcement mode.
//
// Two consumers read it and neither may branch on where a number came from in
// any other way (CLAUDE.md #3):
//
//   - the guard renders a breach of an org-sourced ceiling in the org's terms
//     ("organization daily budget (you, soft)") instead of citing a local
//     [guard.budget] key the developer never set;
//   - the budget-warning service walks the org's own warn ladder
//     (Thresholds, e.g. 0.75 / 0.9 / 1) against the same ceiling.
type Origin struct {
	// Window is "daily" or "monthly" (the only node windows the org's period
	// vocabulary reaches).
	Window string
	// Unit is "usd" or "tokens".
	Unit string
	// Cap is the ORG's authored number for this unit (not the composed one:
	// the warn ladder is a fraction of what the admin wrote).
	Cap float64
	// Scope is the org scope that supplied this unit's number
	// ("member:<id>", "team:<id>", "org", ...).
	Scope string
	// Period is the org period ("calendar_day", "calendar_month",
	// "rolling_30d"); Timezone its calendar anchor.
	Period   string
	Timezone string
	// Enforcement is the org row's mode: report | soft | hard.
	Enforcement string
	// Thresholds is the org's warn ladder (fractions of Cap), as authored.
	Thresholds []float64
}

// originsOf builds the origin rows for one adopted cap.
func originsOf(w window, entry orgcontract.BudgetPolicyCap, adopted adoptedUnits) []Origin {
	var out []Origin
	base := Origin{
		Window: string(w), Period: entry.Period, Timezone: entry.Timezone,
		Enforcement: entry.Enforcement,
		Thresholds:  append([]float64(nil), entry.Thresholds...),
	}
	if adopted.usd {
		o := base
		o.Unit, o.Cap = "usd", entry.CapUSD
		o.Scope = firstNonEmpty(entry.ScopeUSD, entry.ResolvedScope)
		out = append(out, o)
	}
	if adopted.tokens {
		o := base
		o.Unit, o.Cap = "tokens", float64(entry.CapTokens)
		o.Scope = firstNonEmpty(entry.ScopeTokens, entry.ResolvedScope)
		out = append(out, o)
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
