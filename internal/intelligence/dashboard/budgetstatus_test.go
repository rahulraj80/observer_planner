package dashboard

import (
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/budgetwarnfile"
	"github.com/marmutapp/superbased-observer/internal/orgclient"
)

// TestBudgetStatusPayload pins the API shape the dashboard banner reads:
// no file is available=false (never "under budget"), a file is passed through,
// and each active notice names the budget it measures when the rail names one
// (T-522: the banner opens a request for exactly that budget).
func TestBudgetStatusPayload(t *testing.T) {
	p := filepath.Join(t.TempDir(), budgetwarnfile.StatusFilename)
	got := budgetStatusPayload(p, nil)
	if got["available"] != false {
		t.Fatalf("missing file = %v", got)
	}
	if err := budgetwarnfile.Write(p, budgetwarnfile.File{
		Windows: []budgetwarnfile.Window{{Window: "daily", Unit: "usd", Cap: 0.75, Ratio: 1.68, Level: "exceeded", Crossed: 1}},
		Active: []budgetwarnfile.Notice{
			{ID: "x", Message: "m", Threshold: 1, Window: "daily", Unit: "usd", Scope: "member:u1"},
			{ID: "y", Message: "m", Threshold: 1, Window: "monthly", Unit: "tokens", Scope: "team:eng"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	got = budgetStatusPayload(p, nil)
	if got["available"] != true {
		t.Fatalf("payload = %v", got)
	}
	if ws, ok := got["windows"].([]budgetwarnfile.Window); !ok || len(ws) != 1 {
		t.Fatalf("windows = %#v", got["windows"])
	}
	if as, ok := got["active"].([]budgetNoticeView); !ok || len(as) != 2 || as[0].BudgetID != "" {
		t.Fatalf("active without a rail = %#v", got["active"])
	}
	budgets := []orgclient.RequestableBudget{
		{BudgetID: "b-u1", Scope: "member:u1", Period: "calendar_day", CapUSD: 0.75, Grantable: true},
		{BudgetID: "b-eng", Scope: "team:eng", Period: "calendar_month", CapTokens: 9000},
	}
	got = budgetStatusPayload(p, budgets)
	as, _ := got["active"].([]budgetNoticeView)
	if len(as) != 2 || as[0].BudgetID != "b-u1" || as[1].BudgetID != "b-eng" {
		t.Fatalf("active with a rail = %#v", got["active"])
	}
}
