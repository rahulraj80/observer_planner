package dashboard

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/budgetwarnfile"
	"github.com/marmutapp/superbased-observer/internal/orgclient"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestRequestBudgetViews pins the picker's join (T-523): each listed budget
// carries its worst level this period, how far spend is past its cap (the
// suggested increase, rounded up), and when its period ends.
func TestRequestBudgetViews(t *testing.T) {
	now := time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC)
	budgets := []orgclient.RequestableBudget{
		{BudgetID: "b-day", Scope: "member:u1", Period: "calendar_day", CapUSD: 0.75, Grantable: true},
		{
			BudgetID: "b-codex", Scope: "member:u1", Period: "calendar_day", CapTokens: 1000, Grantable: true,
			Subject: &orgcontract.BudgetSubject{Kind: "tool", ID: "Codex"},
		},
		{BudgetID: "b-team", Scope: "team:eng", Period: "calendar_month", CapTokens: 9000},
		{BudgetID: "b-roll", Scope: "member:u1", Period: "rolling_30d", CapTokens: 5, Grantable: true},
	}
	windows := []budgetwarnfile.Window{
		{Window: "daily", Unit: "usd", Scope: "member:u1", Cap: 0.75, Spent: 1.261, Ratio: 1.68, Level: "exceeded"},
		{Window: "daily", Unit: "tokens", Scope: "member:u1", Subject: "tool:codex", Cap: 1000, Spent: 1200.2, Ratio: 1.2, Level: "exceeded"},
		{Window: "monthly", Unit: "tokens", Scope: "team:eng", Cap: 9000, Spent: 8000, Ratio: 0.89, Level: "warning"},
		// Another scope's window never lands on a budget.
		{Window: "daily", Unit: "usd", Scope: "member:u2", Cap: 0.75, Spent: 9, Ratio: 12, Level: "exceeded"},
	}
	got := requestBudgetViews(budgets, windows, now)
	byID := map[string]requestBudgetView{}
	for _, v := range got {
		byID[v.BudgetID] = v
	}
	if v := byID["b-day"]; v.Level != "exceeded" || v.OverUSD != 0.52 || v.OverTokens != 0 ||
		v.PeriodEndNs != time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC).UnixNano() {
		t.Fatalf("b-day = %+v", v)
	}
	if v := byID["b-codex"]; v.Level != "exceeded" || v.OverTokens != 201 {
		t.Fatalf("b-codex = %+v", v)
	}
	if v := byID["b-team"]; v.Level != "warning" || v.OverTokens != 0 ||
		v.PeriodEndNs != time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).UnixNano() {
		t.Fatalf("b-team = %+v", v)
	}
	// No warning window walks a rolling period, so nothing measured it here.
	if v := byID["b-roll"]; v.Level != "" || v.Ratio != 0 {
		t.Fatalf("b-roll = %+v", v)
	}
	if id := budgetIDForWindow(budgets, "member:u1", "daily", "tokens", "tool:codex"); id != "b-codex" {
		t.Fatalf("notice budget = %q", id)
	}
	if id := budgetIDForWindow(budgets, "member:u1", "daily", "tokens", ""); id != "" {
		t.Fatalf("a whole-caller window matched a per-tool budget: %q", id)
	}
}

// TestBudgetRequestWithoutAnIDPreviewsClean is the operator's report (D-334,
// T-523): a budget increase with no budget id (the developer cannot know
// one) previews and sends with no "a budget id is required" problem, and
// what is sent is exactly what the preview showed.
func TestBudgetRequestWithoutAnIDPreviewsClean(t *testing.T) {
	s, _ := newTestServer(t)
	fake := &fakeRequests{enrolled: true}
	s.opts.Requests = fake
	body := `{"request":{"request_type":"budget_increase","message":"over my cap mid-release","payload":{"delta_usd":5}}}`
	rr := doRequests(t, s, http.MethodPost, "/api/requests/preview", body)
	var pv requestPreview
	if err := json.Unmarshal(rr.Body.Bytes(), &pv); err != nil || rr.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rr.Code, rr.Body)
	}
	if len(pv.Problems) != 0 {
		t.Fatalf("preview problems = %+v", pv.Problems)
	}
	if rr := doRequests(t, s, http.MethodPost, "/api/requests", body); rr.Code != http.StatusCreated {
		t.Fatalf("send: %d %s", rr.Code, rr.Body)
	}
	sent, _ := json.Marshal(fake.sent[0])
	shown, _ := json.Marshal(pv.Request)
	if string(sent) != string(shown) {
		t.Fatalf("sent %s, previewed %s", sent, shown)
	}
}
