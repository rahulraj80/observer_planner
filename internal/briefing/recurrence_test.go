package briefing

import (
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
)

func recTurn(day string, input, cacheRead, cacheCreation int64, err bool) Turn {
	ts, perr := time.ParseInLocation("2006-01-02", day, time.Local)
	if perr != nil {
		panic(perr)
	}
	return Turn{
		SessionID: "s-" + day,
		Tool:      "claude",
		Model:     "claude-opus-4",
		Timestamp: ts,
		Tokens: cost.TokenBundle{
			Input:         input,
			CacheRead:     cacheRead,
			CacheCreation: cacheCreation,
		},
		Error: err,
	}
}

func recurrenceFacts(turns []Turn, days int) Facts {
	f := newTestFacts()
	f.WindowDays = days
	f.Turns = turns
	return f
}

func findOpp(opps []Opportunity, detector string) *Opportunity {
	for i := range opps {
		if opps[i].Detector == detector {
			return &opps[i]
		}
	}
	return nil
}

// TestDayKPIsFromTurns checks the per-local-day grouping the recurrence
// detectors reason over.
func TestDayKPIsFromTurns(t *testing.T) {
	turns := []Turn{
		recTurn("2026-07-25", 1000, 9000, 500, true),
		recTurn("2026-07-25", 2000, 0, 0, false),
		recTurn("2026-07-26", 3000, 0, 700, false),
	}
	days := dayKPIsFromTurns(turns)
	if len(days) != 2 {
		t.Fatalf("want 2 days, got %d: %+v", len(days), days)
	}
	if days[0].Date != "2026-07-25" || days[1].Date != "2026-07-26" {
		t.Fatalf("days must be date-ascending, got %s then %s", days[0].Date, days[1].Date)
	}
	d := days[0]
	if d.Turns != 2 || d.InputTokens != 3000 || d.CacheReadTokens != 9000 || d.CacheCreationTokens != 500 || d.ErrorTurns != 1 {
		t.Fatalf("day[0] aggregation wrong: %+v", d)
	}
	if days[1].ErrorTurns != 0 {
		t.Fatalf("day[1] must have no errors: %+v", days[1])
	}
	// day[0] has two turns (9000 cacheRead of 12500 total tokens).
	if rate := dayCacheRate(days[0]); rate != 0.72 {
		t.Fatalf("cache rate of day[0] = %.2f, want 0.72", rate)
	}
}

// TestDetectRecurrenceDailyMode pins the guard: recurrence reasoning never
// runs (or reports) in single-day mode, even when the day is cache-cold.
func TestDetectRecurrenceDailyMode(t *testing.T) {
	f := recurrenceFacts([]Turn{
		recTurn("2026-07-31", 1000, 0, 0, false),
		recTurn("2026-07-31", 1000, 0, 0, true),
	}, 1)
	opps := detectRecurrence(f)
	if len(opps) != 0 {
		t.Fatalf("daily mode must not emit recurrence cards, got %d: %+v", len(opps), opps)
	}
	b := Compose(f)
	if o := findOpp(b.Opportunities, "recurring_cache_miss"); o != nil {
		t.Fatalf("daily briefing must not contain a recurring_cache_miss card")
	}
}

// TestDetectRecurringCacheMiss_WeekdayPattern is the headline recurrence case:
// cache is cold every Wednesday across a 28-day window, the card names the
// weekday, carries a reproducible dollar value, and survives the dollar
// filter purely on its Recurrence flag (savings 0.02 < 0.05 threshold).
func TestDetectRecurringCacheMiss_WeekdayPattern(t *testing.T) {
	var turns []Turn
	// 2026-07-08, 15, 22, 29 are all Wednesdays.
	for _, day := range []string{"2026-07-08", "2026-07-15", "2026-07-22", "2026-07-29"} {
		turns = append(turns, recTurn(day, 1000, 0, 0, false))
	}
	b := Compose(recurrenceFacts(turns, 28))
	o := findOpp(b.Opportunities, "recurring_cache_miss")
	if o == nil {
		t.Fatalf("expected a recurring_cache_miss card, got %+v", b.Opportunities)
	}
	if !o.Recurrence {
		t.Fatalf("recurring card must be flagged Recurrence")
	}
	if !strings.Contains(o.Title, "Wednesday") {
		t.Fatalf("title should name the recurring weekday, got %q", o.Title)
	}
	if o.SavingsUSD != 0.02 {
		t.Fatalf("savings = %.4f, want 0.02 (4 low days x $0.0045)", o.SavingsUSD)
	}
	if len(o.Justification.Days) != 4 {
		t.Fatalf("justification days = %v, want the 4 affected days", o.Justification.Days)
	}
	if got := o.Justification.Evidence.Numbers["low_cache_days"]; got != 4 {
		t.Fatalf("low_cache_days = %v, want 4", got)
	}
	if len(b.Opportunities) != 1 {
		t.Fatalf("expected only the recurrence card (single-day cache card is below the $0.05 filter), got %d", len(b.Opportunities))
	}
}

// TestDetectRecurringCacheMiss_NotEnoughDays pins the fraction guard: two
// cold days in a 28-day window are an isolated dip, not a pattern.
func TestDetectRecurringCacheMiss_NotEnoughDays(t *testing.T) {
	var turns []Turn
	for _, day := range []string{"2026-07-08", "2026-07-15"} {
		turns = append(turns, recTurn(day, 1000, 0, 0, false))
	}
	b := Compose(recurrenceFacts(turns, 28))
	if len(b.Opportunities) != 0 {
		t.Fatalf("2 cold days of 28 must not fire a recurrence card, got %+v", b.Opportunities)
	}
}

// TestDetectRecurringErrors pins the reliability card and its window gate.
func TestDetectRecurringErrors(t *testing.T) {
	var turns []Turn
	for _, day := range []string{"2026-07-25", "2026-07-26", "2026-07-27", "2026-07-28", "2026-07-29", "2026-07-30", "2026-07-31"} {
		turns = append(turns, recTurn(day, 1000, 9000, 0, day == "2026-07-25" || day == "2026-07-27" || day == "2026-07-29"))
	}
	b := Compose(recurrenceFacts(turns, 7))
	o := findOpp(b.Opportunities, "recurring_errors")
	if o == nil {
		t.Fatalf("expected a recurring_errors card, got %+v", b.Opportunities)
	}
	if !o.Recurrence {
		t.Fatalf("recurring_errors must be flagged Recurrence")
	}
	if o.SavingsUSD != 0 {
		t.Fatalf("errors card has no dollar estimate by design")
	}
	if got := o.Justification.Evidence.Numbers["error_days"]; got != 3 {
		t.Fatalf("error_days = %v, want 3", got)
	}

	// Same data in a 6-day window must not fire (WindowDays < 7 gate).
	short := recurrenceFacts(turns[:6], 6)
	if len(detectRecurringErrors(short, dayKPIsFromTurns(short.Turns))) != 0 {
		t.Fatalf("6-day window must not fire recurring_errors")
	}
}

// TestDetectDayCorrelation fires only when BOTH patterns are present and the
// same day carries both signals.
func TestDetectDayCorrelation(t *testing.T) {
	var turns []Turn
	// Days 25/27/29: low-cache AND error. Days 26/28: healthy, no errors.
	for _, day := range []string{"2026-07-25", "2026-07-26", "2026-07-27", "2026-07-28", "2026-07-29"} {
		bad := day == "2026-07-25" || day == "2026-07-27" || day == "2026-07-29"
		if bad {
			turns = append(turns, recTurn(day, 1000, 0, 0, true))
		} else {
			turns = append(turns, recTurn(day, 1000, 9000, 0, false))
		}
	}
	f := recurrenceFacts(turns, 7)
	opps := detectDayCorrelation(f, dayKPIsFromTurns(turns))
	if len(opps) != 1 {
		t.Fatalf("want one day_correlation card, got %d", len(opps))
	}
	o := opps[0]
	if !o.Recurrence || o.SavingsUSD != 0 {
		t.Fatalf("correlation card shape wrong: %+v", o)
	}
	if got := o.Justification.Evidence.Numbers["overlap_days"]; got != 3 {
		t.Fatalf("overlap_days = %v, want 3", got)
	}

	// No error days → no correlation card.
	clean := recurrenceFacts([]Turn{
		recTurn("2026-07-25", 1000, 0, 0, false),
		recTurn("2026-07-26", 1000, 0, 0, false),
	}, 7)
	if len(detectDayCorrelation(clean, dayKPIsFromTurns(clean.Turns))) != 0 {
		t.Fatalf("low-cache without errors must not fire day_correlation")
	}
}

// TestRecurringWeekday is the pure weekday-clustering helper.
func TestRecurringWeekday(t *testing.T) {
	cases := []struct {
		days []string
		want string
	}{
		{[]string{"2026-07-08", "2026-07-15", "2026-07-22", "2026-07-29"}, "Wednesday"},
		{[]string{"2026-07-09", "2026-07-16", "2026-07-22"}, "Thursday"}, // 2/3 share Thursday
		{[]string{"2026-07-08", "2026-07-13"}, ""},                       // 1 each, no cluster
		{[]string{"2026-07-08"}, ""},                                     // too few
		{nil, ""},
	}
	for _, tc := range cases {
		if got := recurringWeekday(tc.days); got != tc.want {
			t.Errorf("recurringWeekday(%v) = %q, want %q", tc.days, got, tc.want)
		}
	}
}
