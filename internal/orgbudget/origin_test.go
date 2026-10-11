package orgbudget

import (
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestComposeEmitsOriginsForAdoptedWindowsOnly pins the provenance Compose
// hands the guard (verdict text) and the warning service (warn ladder): one
// row per ADOPTED (window, unit), carrying the org's own cap, scope,
// enforcement and thresholds - and no row for a cap that lost to a tighter
// local ceiling or for a period the node has no window for.
func TestComposeEmitsOriginsForAdoptedWindowsOnly(t *testing.T) {
	body := orgcontract.BudgetPolicyBody{
		Caps: []orgcontract.BudgetPolicyCap{
			{
				Period: orgcontract.BudgetPolicyPeriodCalendarDay, CapUSD: 0.75, Enforcement: "soft",
				Thresholds: []float64{0.75, 0.9, 1}, ResolvedScope: "member:m1", ScopeUSD: "member:m1",
			},
			{
				Period: orgcontract.BudgetPolicyPeriodCalendarMonth, CapTokens: 1_000_000, Enforcement: "soft",
				Thresholds: []float64{0.75, 0.9, 1}, ResolvedScope: "team:t1", ScopeTokens: "team:t1",
			},
			{Period: orgcontract.BudgetPolicyPeriodRolling30d, CapUSD: 5, Enforcement: "report", ResolvedScope: "org"},
		},
	}
	caps := Capabilities{FromOrg: true, HaveBody: true, FetchState: orgcontract.BudgetFetchOK, GuardMode: "enforce", Now: time.Now()}

	eff, _ := Compose(Thresholds{}, body, caps)
	if len(eff.Origins) != 2 {
		t.Fatalf("origins = %+v, want the daily USD and monthly tokens rows", eff.Origins)
	}
	d := eff.Origins[0]
	if d.Window != "daily" || d.Unit != "usd" || d.Cap != 0.75 || d.Scope != "member:m1" || d.Enforcement != "soft" || len(d.Thresholds) != 3 {
		t.Fatalf("daily origin = %+v", d)
	}
	m := eff.Origins[1]
	if m.Window != "monthly" || m.Unit != "tokens" || m.Cap != 1_000_000 || m.Scope != "team:t1" {
		t.Fatalf("monthly origin = %+v", m)
	}

	// A tighter LOCAL daily ceiling on an individual node wins: the org's
	// daily number governs nothing, so it has no origin row.
	eff, _ = Compose(Thresholds{DailyUSD: 0.5}, body, caps)
	for _, o := range eff.Origins {
		if o.Window == "daily" && o.Unit == "usd" {
			t.Fatalf("a cap that lost to a tighter local ceiling must not carry an origin: %+v", o)
		}
	}

	// No body: nothing is the org's.
	eff, _ = Compose(Thresholds{DailyUSD: 2}, orgcontract.BudgetPolicyBody{}, Capabilities{FromOrg: true})
	if len(eff.Origins) != 0 {
		t.Fatalf("no body must yield no origins: %+v", eff.Origins)
	}
}
