package briefing

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
)

func failTurn(day string, tool, model, errMsg string, err bool) Turn {
	ts, perr := time.ParseInLocation("2006-01-02", day, time.Local)
	if perr != nil {
		panic(perr)
	}
	return Turn{
		SessionID:    "s-" + day,
		Tool:         tool,
		Model:        model,
		Timestamp:    ts,
		Tokens:       cost.TokenBundle{Input: 1000, Output: 500, CacheRead: 9000}, // healthy cache rate
		Error:        err,
		ErrorMessage: errMsg,
	}
}

func healthyTurn(day string) Turn {
	ts, _ := time.ParseInLocation("2006-01-02", day, time.Local)
	return Turn{
		SessionID: "h-" + day,
		Tool:      "claude",
		Model:     "opus",
		Timestamp: ts,
		Tokens:    cost.TokenBundle{Input: 1000, Output: 500, CacheRead: 9000},
		Error:     false,
	}
}

func failureFacts(turns []Turn) Facts {
	f := newTestFacts()
	f.WindowDays = 7
	f.Turns = turns
	return f
}

func TestDetectFailurePatterns_DupeAcrossSessions(t *testing.T) {
	var turns []Turn
	// Two sessions on different days, SAME error pattern (normalize to same)
	turns = append(turns, failTurn("2026-07-25", "claude", "opus", "permission denied on /etc/hosts", true))
	turns = append(turns, failTurn("2026-07-26", "claude", "opus", "permission denied on /etc/hosts", true))
	// Healthy days to suppress recurrence cache/error cards
	turns = append(turns, healthyTurn("2026-07-27"))
	turns = append(turns, healthyTurn("2026-07-28"))
	turns = append(turns, healthyTurn("2026-07-29"))
	turns = append(turns, healthyTurn("2026-07-30"))
	turns = append(turns, healthyTurn("2026-07-31"))

	b := Compose(failureFacts(turns))
	var found *Opportunity
	for i := range b.Opportunities {
		if b.Opportunities[i].Detector == "failure_pattern" {
			found = &b.Opportunities[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected failure_pattern card, got %+v", b.Opportunities)
	}
	if !found.Recurrence {
		t.Fatal("failure pattern must be flagged Recurrence")
	}
	if !strings.Contains(found.Title, "Permission errors recur") {
		t.Fatalf("title should mention permission: %q", found.Title)
	}
	if !strings.Contains(found.Nudge, "permissions and API credentials") {
		t.Fatalf("nudge should be categorized: %q", found.Nudge)
	}
	if len(found.Justification.Days) != 2 {
		t.Fatalf("justification days = %v, want 2 affected days", found.Justification.Days)
	}
}

func TestDetectFailurePatterns_SingleSessionMultipleDays(t *testing.T) {
	var turns []Turn
	// Same session (same SessionID), multiple days with same error
	// We need same SessionID but different days for this to work
	turns = append(turns, failTurn("2026-07-25", "claude", "opus", "build failed: undefined symbol foo", true))
	turns = append(turns, failTurn("2026-07-26", "claude", "opus", "build failed: undefined symbol foo", true))
	// Healthy days to suppress recurrence cards
	turns = append(turns, healthyTurn("2026-07-27"))
	turns = append(turns, healthyTurn("2026-07-28"))
	turns = append(turns, healthyTurn("2026-07-29"))
	turns = append(turns, healthyTurn("2026-07-30"))
	turns = append(turns, healthyTurn("2026-07-31"))

	b := Compose(failureFacts(turns))
	var found *Opportunity
	for i := range b.Opportunities {
		if b.Opportunities[i].Detector == "failure_pattern" {
			found = &b.Opportunities[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected failure_pattern card, got %+v", b.Opportunities)
	}
	if !strings.Contains(found.Title, "Compilation errors recur") {
		t.Fatalf("title should mention compilation: %q", found.Title)
	}
}

func TestDetectFailurePatterns_NoErrorMessage_NoSurface(t *testing.T) {
	var turns []Turn
	// Error turn with no error message - should not be categorized
	turns = append(turns, failTurn("2026-07-25", "claude", "opus", "", true))
	turns = append(turns, failTurn("2026-07-26", "claude", "opus", "", true))
	turns = append(turns, healthyTurn("2026-07-27"))
	turns = append(turns, healthyTurn("2026-07-28"))
	turns = append(turns, healthyTurn("2026-07-29"))
	turns = append(turns, healthyTurn("2026-07-30"))
	turns = append(turns, healthyTurn("2026-07-31"))

	b := Compose(failureFacts(turns))
	for _, o := range b.Opportunities {
		if o.Detector == "failure_pattern" {
			t.Fatalf("expected no failure_pattern for empty messages, got %q", o.Title)
		}
	}
}

func TestDetectFailurePatterns_DailyModeNoRecurrence(t *testing.T) {
	f := newTestFacts()
	f.WindowDays = 1
	f.Turns = []Turn{
		failTurn("2026-07-31", "claude", "opus", "permission denied", true),
		healthyTurn("2026-07-31"),
	}
	b := Compose(f)
	for _, o := range b.Opportunities {
		if o.Detector == "failure_pattern" {
			t.Fatalf("daily mode should not emit failure_pattern, got %q", o.Title)
		}
	}
}

func TestAggregateFailurePatterns(t *testing.T) {
	var turns []Turn
	// Permission: 2 occurrences across 2 sessions / 2 days.
	turns = append(turns, failTurn("2026-07-25", "claude", "opus", "permission denied on /etc/hosts", true))
	turns = append(turns, failTurn("2026-07-26", "claude", "opus", "permission denied  on  /etc/hosts", true))
	// Test failure: 2 occurrences across 2 sessions / 2 days (different tool/model).
	turns = append(turns, failTurn("2026-07-26", "codex", "sonnet", "--- FAIL: TestFoo want 1 got 0", true))
	turns = append(turns, failTurn("2026-07-27", "codex", "sonnet", "--- FAIL: TestFoo want 1 got 0", true))
	// Single session, single day → dropped (not a recurrence).
	turns = append(turns, failTurn("2026-07-28", "claude", "opus", "panic: nil pointer dereference", true))

	patterns := AggregateFailurePatterns(turns)
	if len(patterns) != 2 {
		t.Fatalf("got %d patterns, want 2: %+v", len(patterns), patterns)
	}
	// Equal counts → tie-broken by category label asc (permission < test_failure).
	if patterns[0].Category != "permission" || patterns[1].Category != "test_failure" {
		t.Fatalf("pattern order wrong: %s, %s", patterns[0].Category, patterns[1].Category)
	}
	p := patterns[0]
	if p.Count != 2 || len(p.Sessions) != 2 || len(p.Days) != 2 {
		t.Fatalf("permission pattern aggregate wrong: %+v", p)
	}
	// Whitespace runs collapsed by Normalize so both turns share one signature.
	if p.NormMsg != "permission denied on /etc/hosts" {
		t.Fatalf("NormMsg = %q", p.NormMsg)
	}
	if !sort.StringsAreSorted(p.Sessions) || !sort.StringsAreSorted(p.Days) {
		t.Fatalf("session/day slices must be sorted: %+v", p)
	}
	if !p.FirstSeen.Before(p.LastSeen) {
		t.Fatalf("first/last seen span wrong: %s .. %s", p.FirstSeen, p.LastSeen)
	}
}

func TestAggregateFailurePatternsEmpty(t *testing.T) {
	if got := AggregateFailurePatterns(nil); len(got) != 0 {
		t.Fatalf("nil input → %d patterns, want 0", len(got))
	}
	// Error turns without messages never form signatures.
	turns := []Turn{failTurn("2026-07-25", "claude", "opus", "", true), failTurn("2026-07-26", "claude", "opus", "", true)}
	if got := AggregateFailurePatterns(turns); len(got) != 0 {
		t.Fatalf("empty-message errors → %d patterns, want 0", len(got))
	}
}
