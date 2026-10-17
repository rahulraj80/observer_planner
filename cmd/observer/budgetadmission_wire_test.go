package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/budgetwarnfile"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/orgbudget"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// fakeHookBudgetChecker answers CheckInterventionBudget from a per-tool table.
type fakeHookBudgetChecker struct {
	decisions map[string]guard.InterventionBudgetDecision
	asked     []string
}

func (f *fakeHookBudgetChecker) CheckInterventionBudget(in guard.InterventionBudgetInput) guard.InterventionBudgetDecision {
	f.asked = append(f.asked, in.Tool)
	if !in.SourceReady || in.Now.IsZero() {
		return guard.InterventionBudgetDecision{Deny: true, Reason: "fixture: bad input"}
	}
	return f.decisions[in.Tool]
}

func (f *fakeHookBudgetChecker) RenderBudgetReason(ruleID, reason string) string {
	return "rendered " + ruleID + ": " + reason
}

// TestT519HookBudgetStopsTable pins the publisher: the node-wide decision
// first (one stop covers every tool), then one decision per HARD per-tool cap;
// soft caps and model caps publish nothing, and a rule-less deny is published
// under the managed fail-closed row B-625.
func TestT519HookBudgetStopsTable(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 5, 20, 27, 0, 0, time.UTC)
	midnight := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	toolCap := func(id string, hard bool) orgbudget.SubjectCap {
		return orgbudget.SubjectCap{Kind: orgcontract.BudgetSubjectTool, ID: id, Window: "daily", CapUSD: 1, Hard: hard}
	}
	modelCap := orgbudget.SubjectCap{Kind: orgcontract.BudgetSubjectModel, ID: "gpt-5.4", Window: "daily", CapUSD: 1, Hard: true}
	cases := []struct {
		name      string
		decisions map[string]guard.InterventionBudgetDecision
		subjects  []orgbudget.SubjectCap
		want      []budgetwarnfile.Stop
	}{
		{
			name:      "node-wide exhausted cap stops every tool",
			decisions: map[string]guard.InterventionBudgetDecision{"": {Deny: true, RuleID: "B-602", Reason: "spent", ValidUntil: midnight}},
			subjects:  []orgbudget.SubjectCap{toolCap("cursor", true)},
			want:      []budgetwarnfile.Stop{{RuleID: "B-602", Reason: "rendered B-602: spent", ValidUntil: midnight}},
		},
		{
			name: "under the node-wide cap: only the exhausted hard tool cap stops",
			decisions: map[string]guard.InterventionBudgetDecision{
				"cursor": {Deny: true, RuleID: "B-626", Reason: "cursor spent"},
				"codex":  {},
			},
			subjects: []orgbudget.SubjectCap{toolCap("Cursor", true), toolCap("codex", true), toolCap("cursor", true)},
			want:     []budgetwarnfile.Stop{{Tool: "cursor", RuleID: "B-626", Reason: "rendered B-626: cursor spent"}},
		},
		{
			name:      "soft tool caps and model caps publish nothing",
			decisions: map[string]guard.InterventionBudgetDecision{"cursor": {Deny: true, RuleID: "B-626"}, "gpt-5.4": {Deny: true, RuleID: "B-628"}},
			subjects:  []orgbudget.SubjectCap{toolCap("cursor", false), modelCap},
			want:      nil,
		},
		{
			name:      "a rule-less deny fails closed under B-625",
			decisions: map[string]guard.InterventionBudgetDecision{"": {Deny: true, Reason: "managed budget evaluation failed"}},
			want:      []budgetwarnfile.Stop{{RuleID: "B-625", Reason: "rendered B-625: managed budget evaluation failed"}},
		},
		{
			name:      "nothing exhausted: nothing published",
			decisions: map[string]guard.InterventionBudgetDecision{"": {Available: true}},
			want:      nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := hookBudgetStops(&fakeHookBudgetChecker{decisions: tc.decisions}, tc.subjects, now)
			if !budgetwarnfile.SameStops(got, tc.want) {
				t.Fatalf("stops = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestT519PublishAdmission pins the daemon's write discipline: no file on a
// node that never had a stop, one write per changed decision, an unavailable
// decision keeps the last file, and a stale file from an earlier daemon is
// cleared on the first pass.
func TestT519PublishAdmission(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "observer.db")
	path := budgetwarnfile.AdmissionPath(dbPath)
	now := time.Date(2026, 10, 5, 20, 27, 0, 0, time.UTC)
	var stops []budgetwarnfile.Stop
	ok := true
	calls := 0
	svc := newBudgetWarnService(dbPath, nil, nil, nil)
	svc.now = func() time.Time { return now }
	svc.SetAdmission(func(time.Time, []orgbudget.SubjectCap) ([]budgetwarnfile.Stop, bool) {
		calls++
		return stops, ok
	})

	svc.publishAdmission()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a node with no stop got an admission file (err %v)", err)
	}

	stops = []budgetwarnfile.Stop{{RuleID: "B-602", Reason: "spent"}}
	svc.publishAdmission()
	a, err := budgetwarnfile.ReadAdmission(path)
	if err != nil || a == nil || len(a.Stops) != 1 {
		t.Fatalf("admission after the crossing = %+v, %v", a, err)
	}
	info1, _ := os.Stat(path)

	now = now.Add(5 * time.Second)
	svc.publishAdmission()
	info2, _ := os.Stat(path)
	if !info2.ModTime().Equal(info1.ModTime()) {
		t.Fatal("an unchanged decision rewrote the file")
	}

	ok, stops = false, nil
	svc.publishAdmission()
	if a, _ := budgetwarnfile.ReadAdmission(path); a == nil || len(a.Stops) != 1 {
		t.Fatal("an unavailable decision dropped a published stop")
	}

	ok = true
	svc.publishAdmission()
	if a, _ := budgetwarnfile.ReadAdmission(path); a == nil || len(a.Stops) != 0 {
		t.Fatalf("a cleared decision kept the stop: %+v", a)
	}

	// A restarted daemon with nothing to stop clears a stale file at once.
	if err := budgetwarnfile.WriteAdmission(path, budgetwarnfile.Admission{Stops: []budgetwarnfile.Stop{{RuleID: "B-602"}}}); err != nil {
		t.Fatal(err)
	}
	restarted := newBudgetWarnService(dbPath, nil, nil, nil)
	restarted.SetAdmission(func(time.Time, []orgbudget.SubjectCap) ([]budgetwarnfile.Stop, bool) { return nil, true })
	restarted.publishAdmission()
	if a, _ := budgetwarnfile.ReadAdmission(path); a == nil || len(a.Stops) != 0 {
		t.Fatalf("a restarted daemon kept a stale stop: %+v", a)
	}
	if calls == 0 {
		t.Fatal("publisher never asked")
	}
}

// TestT519HookStopIsTheManagedDecision is the spend-parity pin: the stop the
// hook path enforces is the SAME CheckInterventionBudget the launch gate and
// the process controller take, over a managed node whose only spend is a
// direct-vendor tool's HOOK-captured usage (Cursor's shape on node 1: source
// "hook", reliability "accurate"). The warning number every surface shows
// counts the same rows.
func TestT519HookStopIsTheManagedDecision(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		spend    bool
		wantRule string
	}{
		{"cursor's hook-captured spend exhausts the cap", true, "B-623"},
		{"under the cap nothing stops", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfgPath, dbPath := writeManagedBudgetLaunchFixture(t, true, "enforce")
			if tc.spend {
				database, err := dbtemplate.Open(ctx, db.Options{Path: dbPath})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.New(database).IngestBudgetUsage(ctx, nil, []models.TokenEvent{{
					SessionID: "cursor-t519", SourceEventID: "t519-1", Tool: models.ToolCursor,
					Model: "cursor-grok-4.5-high-fast", ProjectRoot: t.TempDir(), SourceFile: "hook",
					Timestamp:   time.Now().UTC(),
					InputTokens: managedBudgetLaunchFixtureCapTokens, OutputTokens: 1,
					Source: "hook", Reliability: "accurate",
				}}, nil); err != nil {
					t.Fatalf("record cursor usage: %v", err)
				}
				_ = database.Close()
			}
			cfg, err := config.Load(config.LoadOptions{GlobalPath: cfgPath})
			if err != nil {
				t.Fatal(err)
			}
			database, err := dbtemplate.Open(ctx, db.Options{Path: dbPath})
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			state, err := resolveManagedBudgetLaunchState(ctx, cfg, database)
			if err != nil {
				t.Fatal(err)
			}
			gd := budgetLaunchSpendGuard(ctx, cfg, cfgPath, state)
			if gd == nil {
				t.Fatal("no budget guard")
			}
			now := time.Now().UTC()
			launch := gd.CheckInterventionBudget(guard.InterventionBudgetInput{Tool: models.ToolCursor, SourceReady: true, Now: now})
			stops := hookBudgetStops(gd, nil, now)
			if tc.wantRule == "" {
				if launch.Deny || len(stops) != 0 {
					t.Fatalf("under cap: launch=%+v stops=%+v, want neither to stop", launch, stops)
				}
				return
			}
			if !launch.Deny || launch.RuleID != tc.wantRule {
				t.Fatalf("process-controller decision = %+v, want a %s deny", launch, tc.wantRule)
			}
			if len(stops) != 1 || stops[0].RuleID != launch.RuleID || stops[0].Tool != "" {
				t.Fatalf("hook stops = %+v, want the same %s node-wide decision", stops, launch.RuleID)
			}
			if !strings.Contains(stops[0].Reason, strconv.FormatInt(managedBudgetLaunchFixtureCapTokens, 10)) {
				t.Fatalf("stop reason %q does not carry the cap it compared", stops[0].Reason)
			}
			if sp, ok := gd.BudgetWindowSpendNow(); !ok || sp.MonthlyTokens < managedBudgetLaunchFixtureCapTokens {
				t.Fatalf("warning-surface spend = %+v ok=%v, want it to count cursor's hook-captured tokens", sp, ok)
			}
		})
	}
}
