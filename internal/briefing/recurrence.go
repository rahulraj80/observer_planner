package briefing

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// recurrence.go holds the multi-day ("smarter opportunities") detectors:
// cross-day correlation of the single-day signals — recurring cache misses,
// recurring error days, and the overlap between them. These only run in
// multi-day mode (Facts.WindowDays > 1), where "this pattern recurs every
// low-cache day" is a real, reproducible claim. Pure and deterministic like
// the rest of the composer; the day-boundary arithmetic uses the machine's
// local zone, matching the briefing's day-resolution decision.

// DayKPI is the per-local-day aggregate the recurrence detectors reason over.
type DayKPI struct {
	Date                string
	Turns               int
	SpendUSD            float64
	InputTokens         int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	ErrorTurns          int
}

// dayKPIsFromTurns groups the window's turns into per-local-day aggregates.
func dayKPIsFromTurns(turns []Turn) []DayKPI {
	by := map[string]*DayKPI{}
	var order []string
	for _, t := range turns {
		d := t.Timestamp.In(time.Local).Format("2006-01-02")
		k, ok := by[d]
		if !ok {
			k = &DayKPI{Date: d}
			by[d] = k
			order = append(order, d)
		}
		k.Turns++
		k.SpendUSD += t.CostUSD
		k.InputTokens += t.Tokens.Input
		k.CacheReadTokens += t.Tokens.CacheRead
		k.CacheCreationTokens += t.Tokens.CacheCreation + t.Tokens.CacheCreation1h
		if t.Error {
			k.ErrorTurns++
		}
	}
	out := make([]DayKPI, 0, len(order))
	for _, d := range order {
		out = append(out, *by[d])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// dayCacheRate is the effective cache hit rate for one day.
func dayCacheRate(k DayKPI) float64 {
	total := k.InputTokens + k.CacheReadTokens + k.CacheCreationTokens
	if total == 0 {
		return 0
	}
	return float64(k.CacheReadTokens) / float64(total)
}

// detectRecurrence is the multi-day entry point. It fires no cards in daily
// mode and when fewer than two days have activity.
func detectRecurrence(f Facts) []Opportunity {
	if f.WindowDays <= 1 {
		return nil
	}
	days := dayKPIsFromTurns(f.Turns)
	if len(days) < 2 {
		return nil
	}
	var opps []Opportunity
	opps = append(opps, detectRecurringCacheMiss(f, days)...)
	opps = append(opps, detectRecurringErrors(f, days)...)
	opps = append(opps, detectDayCorrelation(f, days)...)
	return opps
}

// detectRecurringCacheMiss fires when cache hit rate was below the healthy
// threshold on a meaningful fraction of the window's days. It prices the
// would-be-warm reads (same per-day math as the single-day cache card,
// summed over the affected days) so the card carries a reproducible dollar
// value, and it names the recurring weekday when the low days cluster on one
// — the "recurs every low-cache day" reasoning.
func detectRecurringCacheMiss(f Facts, days []DayKPI) []Opportunity {
	th := f.Thresholds
	var affected []string
	var rates []float64
	var dayTokens []float64
	for _, d := range days {
		r := dayCacheRate(d)
		if r >= th.HealthyCacheHitRate {
			continue
		}
		affected = append(affected, d.Date)
		rates = append(rates, r)
		dayTokens = append(dayTokens, float64(d.InputTokens+d.CacheReadTokens+d.CacheCreationTokens))
	}
	if len(affected) < 2 {
		return nil
	}
	// Guard against isolated dips: the cold days must cover a meaningful share
	// of the window (about one day in seven, floor 2) to count as a pattern.
	minAffected := 2
	if n := f.WindowDays / 7; n > minAffected {
		minAffected = n
	}
	if len(affected) < minAffected {
		return nil
	}

	var sum float64
	for _, r := range rates {
		sum += r
	}
	avg := sum / float64(len(rates))
	blend := blendedInputRate(f)
	savings := 0.0
	for i, r := range rates {
		if gap := th.HealthyCacheHitRate - r; gap > 0 {
			savings += gap * dayTokens[i] * blend * 0.5
		}
	}
	savings = round2(savings)

	weekday := recurringWeekday(affected)
	recurring := weekday != ""
	title := fmt.Sprintf("Cache was below %.0f%% on %d of the last %d days (avg %.0f%%)",
		th.HealthyCacheHitRate*100, len(affected), f.WindowDays, avg*100)
	nudge := "Keep the stable parts of your prompt prefix identical across sessions and avoid restarting mid-task — cache reuse depends on it."
	if recurring {
		title = fmt.Sprintf("Cache misses recur every %s — %d of the last %d days under %.0f%%",
			weekday, len(affected), f.WindowDays, th.HealthyCacheHitRate*100)
		nudge = fmt.Sprintf("The pattern is recurring, not noise — on %ss keep the prompt prefix stable and avoid churning session state.", weekday)
	}

	j := Justify(Claim{
		Detector:       "recurring_cache_miss",
		Title:          title,
		Nudge:          nudge,
		Source:         "token_usage",
		GroupBy:        "day",
		SavingsUSD:     savings,
		SavingsMin:     savings,
		Confidence:     0.7,
		FlipConditions: "If the low-cache days were genuinely novel work, the pattern is coincidence rather than a habit worth fixing.",
		Evidence: Evidence{
			Numbers: map[string]float64{
				"low_cache_days":     float64(len(affected)),
				"total_days":         float64(len(days)),
				"avg_rate":           round3(avg),
				"healthy_rate":       th.HealthyCacheHitRate,
				"savings_usd":        savings,
				"recurring_weekdays": boolToFloat(recurring),
			},
			Math: fmt.Sprintf("low-cache days [%s]; mean rate %.3f vs healthy %.2f; savings = sum over days of gap*tokens*blend*0.5 = %.2f; recurring weekday: %s",
				joinBounded(affected), avg, th.HealthyCacheHitRate, savings, orNone(weekday)),
		},
	}, f)
	j.Days = affected
	return []Opportunity{{
		DedupKey:      fmt.Sprintf("recurring_cache_%dd", f.WindowDays),
		Detector:      "recurring_cache_miss",
		Category:      "cache",
		Scope:         "multi-day",
		Severity:      "info",
		Title:         title,
		Nudge:         nudge,
		SavingsUSD:    savings,
		SavingsMin:    savings,
		Confidence:    j.Confidence,
		Recurrence:    true,
		Justification: j,
	}}
}

// detectRecurringErrors fires when error turns spread across several days of
// a long window — a repeating failure, not a one-off.
func detectRecurringErrors(f Facts, days []DayKPI) []Opportunity {
	if f.WindowDays < 7 {
		return nil
	}
	var errorDays []string
	var errorTurns int
	for _, d := range days {
		if d.ErrorTurns > 0 {
			errorDays = append(errorDays, d.Date)
			errorTurns += d.ErrorTurns
		}
	}
	if len(errorDays) < 2 {
		return nil
	}
	title := fmt.Sprintf("Errors recurred on %d of the last %d days (%d error turns)", len(errorDays), f.WindowDays, errorTurns)
	nudge := "The same failure is probably being retried across sessions — dedupe the recurring error before the next briefing (ties into failure analysis)."
	j := Justify(Claim{
		Detector:       "recurring_errors",
		Title:          title,
		Nudge:          nudge,
		Source:         "api_turns",
		GroupBy:        "day",
		SavingsUSD:     0,
		SavingsMin:     0,
		Confidence:     0.6,
		FlipConditions: "If each error day is a different unrelated failure, this is noise, not a recurring problem.",
		Evidence: Evidence{
			Numbers: map[string]float64{
				"error_days":         float64(len(errorDays)),
				"total_days":         float64(len(days)),
				"error_turns":        float64(errorTurns),
				"avg_errors_per_day": round2(float64(errorTurns) / float64(len(days))),
			},
			Math: fmt.Sprintf("error days [%s]; %d error turns across %d days", joinBounded(errorDays), errorTurns, len(days)),
		},
	}, f)
	j.Days = errorDays
	return []Opportunity{{
		DedupKey:      fmt.Sprintf("recurring_errors_%dd", f.WindowDays),
		Detector:      "recurring_errors",
		Category:      "reliability",
		Scope:         "multi-day",
		Severity:      "warning",
		Title:         title,
		Nudge:         nudge,
		SavingsUSD:    0,
		SavingsMin:    0,
		Confidence:    j.Confidence,
		Recurrence:    true,
		Justification: j,
	}}
}

// detectDayCorrelation names the overlap between the recurring signals: days
// that were BOTH low-cache and error-laden point at a shared cause (session
// churn / long restarts). Fires only when both patterns are already present.
func detectDayCorrelation(f Facts, days []DayKPI) []Opportunity {
	low := map[string]bool{}
	errs := map[string]bool{}
	var lowList, errList []string
	for _, d := range days {
		if dayCacheRate(d) < f.Thresholds.HealthyCacheHitRate {
			low[d.Date] = true
			lowList = append(lowList, d.Date)
		}
		if d.ErrorTurns > 0 {
			errs[d.Date] = true
			errList = append(errList, d.Date)
		}
	}
	if len(lowList) < 2 || len(errList) < 2 {
		return nil
	}
	var overlap []string
	for _, d := range lowList {
		if errs[d] {
			overlap = append(overlap, d)
		}
	}
	if len(overlap) == 0 {
		return nil
	}
	title := fmt.Sprintf("Low-cache and error days overlap on %d of %d days", len(overlap), len(days))
	nudge := "A shared cause (frequent restarts / short-lived sessions) may be behind both — check session length and restart cadence on those days."
	j := Justify(Claim{
		Detector:       "day_correlation",
		Title:          title,
		Nudge:          nudge,
		Source:         "api_turns",
		GroupBy:        "day",
		SavingsUSD:     0,
		SavingsMin:     0,
		Confidence:     0.5,
		FlipConditions: "Co-occurrence is not causation; the overlap may be two independent habits landing on the same weekdays.",
		Evidence: Evidence{
			Numbers: map[string]float64{
				"overlap_days":   float64(len(overlap)),
				"low_cache_days": float64(len(lowList)),
				"error_days":     float64(len(errList)),
				"total_days":     float64(len(days)),
			},
			Math: fmt.Sprintf("overlap [%s]; low-cache days [%s]; error days [%s]", joinBounded(overlap), joinBounded(lowList), joinBounded(errList)),
		},
	}, f)
	j.Days = overlap
	return []Opportunity{{
		DedupKey:      fmt.Sprintf("day_correlation_%dd", f.WindowDays),
		Detector:      "day_correlation",
		Category:      "reliability",
		Scope:         "multi-day",
		Severity:      "info",
		Title:         title,
		Nudge:         nudge,
		SavingsUSD:    0,
		SavingsMin:    0,
		Confidence:    j.Confidence,
		Recurrence:    true,
		Justification: j,
	}}
}

// recurringWeekday returns the weekday name when at least ceil(2/3) of the
// affected days share one (and there are at least 2), else "".
func recurringWeekday(dates []string) string {
	if len(dates) < 2 {
		return ""
	}
	counts := map[time.Weekday]int{}
	for _, d := range dates {
		if t, err := time.ParseInLocation("2006-01-02", d, time.Local); err == nil {
			counts[t.Weekday()]++
		}
	}
	need := int(math.Ceil(2.0 / 3.0 * float64(len(dates))))
	for wd, n := range counts {
		if n >= need {
			return wd.String()
		}
	}
	return ""
}

func joinBounded(items []string) string {
	const max = 10
	if len(items) <= max {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:max], ", ") + fmt.Sprintf(" (+%d more)", len(items)-max)
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
