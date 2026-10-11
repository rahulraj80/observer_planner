package main

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/budgetwarnfile"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/orgbudget"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/policy"
)

// TestBudgetWarnServiceWarnsOnSubjectCaps (Sol A5-04, D-016): per-tool and
// per-model caps are enforced by the guard, so the developer must be warned at
// the org's 75 / 90 / 100 rungs on them too, each cap on its own ladder, read
// through the guard's accounting (ByTool / ByModel) plus the subject baseline,
// in the composed calendars. An unavailable subject read keeps the last
// evaluation (the A5-07 rule).
func TestBudgetWarnServiceWarnsOnSubjectCaps(t *testing.T) {
	db := filepath.Join(t.TempDir(), "observer.db")
	var mu sync.Mutex
	available := true
	toolUSD, modelTokens := 7.0, int64(700)
	var gotCal guard.BudgetCalendars
	subjectSpend := func(cal guard.BudgetCalendars) (guard.BudgetSnapshot, bool) {
		mu.Lock()
		defer mu.Unlock()
		gotCal = cal
		return guard.BudgetSnapshot{
			ByTool:  map[string]policy.BudgetWindowAmounts{"claude-code": {DailyUSD: toolUSD}, "codex": {DailyUSD: 99}},
			ByModel: map[string]policy.BudgetWindowAmounts{"gpt-5": {MonthlyTokens: modelTokens}},
		}, available
	}
	type notice struct{ title, body string }
	var notices []notice
	svc := newBudgetWarnService(db, func() (guard.BudgetWindowSpend, bool) {
		return guard.BudgetWindowSpend{DailyUSD: 1}, true
	}, func(title, body string) { notices = append(notices, notice{title, body}) }, nil)
	svc.SetSubjectSpend(subjectSpend)
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	ladder := []float64{0.75, 0.9, 1}
	svc.SetComposition([]orgbudget.Origin{{
		Window: "daily", Unit: "usd", Cap: 100, Scope: "member:m1", Period: "calendar_day",
		Timezone: "UTC", Enforcement: "soft", Thresholds: ladder,
	}}, []orgbudget.SubjectCap{
		{
			Kind: orgcontract.BudgetSubjectTool, ID: "claude-code", Window: "daily", CapUSD: 10, BaselineUSD: 0.5,
			ScopeUSD: "member:m1", Enforcement: "hard", Thresholds: ladder, Timezone: "UTC",
		},
		{
			Kind: orgcontract.BudgetSubjectModel, ID: "gpt-5", Window: "monthly", CapTokens: 1000,
			ScopeTokens: "org", Enforcement: "soft", Thresholds: ladder, Timezone: "UTC",
		},
	}, guard.BudgetCalendars{DailyTimezone: "Asia/Kolkata"})

	step := func(tool float64, model int64) []notice {
		mu.Lock()
		toolUSD, modelTokens = tool, model
		mu.Unlock()
		before := len(notices)
		now = now.Add(time.Minute)
		svc.evaluate()
		return notices[before:]
	}
	expect := func(got []notice, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("notices = %+v, want %d", got, len(want))
		}
		for i, w := range want {
			if !strings.Contains(got[i].body, w) {
				t.Errorf("notice %d body = %q, want it to mention %q", i, got[i].body, w)
			}
		}
	}

	// 7.0 local + 0.5 baseline = 75% of the $10 tool cap exactly; the model is
	// at 70% and the whole caller at 1%: one notice, the tool's.
	first := step(7.0, 700)
	expect(first, "budget for tool claude-code")
	if gotCal.DailyTimezone != "Asia/Kolkata" || gotCal.MonthlyTimezone != "UTC" {
		t.Errorf("subject read calendars = %+v, want the composed engine's", gotCal)
	}
	if !strings.Contains(first[0].title, "75% reached") {
		t.Errorf("title = %q", first[0].title)
	}
	// The model reaches 75% (tokens); the tool holds: only the model speaks.
	expect(step(7.0, 750), "budget for model gpt-5")
	// The tool reaches 90%, then exactly 100% (9.5 + 0.5 baseline).
	expect(step(8.5, 750), "tool claude-code")
	expect(step(9.5, 750), "Further proxied requests, and the next prompt or tool call in tools with a pre-send hook, are blocked")
	// Nothing new: no rung is announced twice.
	expect(step(9.5, 760))

	file, err := budgetwarnfile.Read(budgetwarnfile.StatusPath(db))
	if err != nil || file == nil {
		t.Fatalf("status file: %v", err)
	}
	subjects := map[string]bool{}
	for _, a := range file.Active {
		subjects[a.Subject] = true
	}
	if !subjects["tool:claude-code"] || !subjects["model:gpt-5"] {
		t.Errorf("status file Active = %+v, want both subject warnings", file.Active)
	}

	// An unavailable subject read keeps the file and the dedup state.
	mu.Lock()
	available = false
	mu.Unlock()
	now = now.Add(time.Minute)
	svc.evaluate()
	file2, err := budgetwarnfile.Read(budgetwarnfile.StatusPath(db))
	if err != nil || file2 == nil || len(file2.Active) != len(file.Active) {
		t.Fatalf("after an unavailable subject read the file = %+v err=%v", file2, err)
	}
	mu.Lock()
	available = true
	mu.Unlock()
	expect(step(9.5, 760))
}

// TestProcessBudgetSubjectSpendReadsTheRegisteredAccounting pins the one
// accounting owner: the warning reader calls exactly the lookup guardwire
// registered, node-wide, with the composed calendars.
func TestProcessBudgetSubjectSpendReadsTheRegisteredAccounting(t *testing.T) {
	db := filepath.Join(t.TempDir(), "observer.db")
	read := processBudgetSubjectSpend(db)
	if _, ok := read(guard.BudgetCalendars{}); ok {
		t.Fatal("no registered accounting must read unavailable, never zero")
	}
	var gotSession string
	var gotCal guard.BudgetCalendars
	registerProcessBudgetAccounting(db, func(sessionID string, a guard.BudgetAccountingContext) (guard.BudgetSnapshot, bool) {
		gotSession, gotCal = sessionID, a.Calendars
		return guard.BudgetSnapshot{ByTool: map[string]policy.BudgetWindowAmounts{"codex": {DailyUSD: 2}}}, true
	})
	t.Cleanup(func() { registerProcessBudgetAccounting(db, nil) })
	snap, ok := read(guard.BudgetCalendars{DailyTimezone: "UTC", MonthlyTimezone: "Europe/Berlin"})
	if !ok || snap.ByTool["codex"].DailyUSD != 2 {
		t.Fatalf("snap = %+v ok=%v", snap, ok)
	}
	if gotSession != "" || gotCal.MonthlyTimezone != "Europe/Berlin" {
		t.Errorf("lookup called with session %q calendars %+v", gotSession, gotCal)
	}
}
