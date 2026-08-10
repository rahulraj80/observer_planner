package digest

import (
	"fmt"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/notify/email"
)

// BriefingData is the fully-assembled, content-free input to ComposeBriefing:
// one completed local day's rollup. It mirrors the briefing at_glance shape
// (docs/design/standup-briefing.md §2.1) but stays content-free — no prompt
// text, no paths, no project names — so it may ride the same [email] channel
// as the cost digest. Every figure is OBSERVED data.
type BriefingData struct {
	// Title is the digest headline, e.g. "Daily briefing".
	Title string
	// PeriodLabel is the human window label, e.g. "Sat, Aug 1, 2026".
	PeriodLabel string
	// TotalUSD is the observed spend over the day.
	TotalUSD float64
	// Sessions / Turns are the session + api-turn counts for the day.
	Sessions int
	Turns    int
	// InputTokens / OutputTokens / CacheReadTokens / CacheCreationTokens are
	// the day's token totals in the four billable buckets.
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	// CacheHitRate is cache_read/(input+cache_read+cache_created), 0..1.
	CacheHitRate float64
	// ErrorTurns is the count of failed / error-class turns.
	ErrorTurns int
	// ActiveHours is the span of distinct hours with activity, 0..24.
	ActiveHours float64
	// FastShare is the fraction of turns served by fast/cheap models, 0..1.
	FastShare float64
	// Tools are the per-tool spend lines (already cost-sorted by the caller).
	Tools []LineItem
	// OpportunityCount is the number of actionable cards composed for the day.
	OpportunityCount int
	// OpportunitySavingsUSD is the sum of the cards' estimated savings; only
	// rendered when OpportunityCount > 0.
	OpportunitySavingsUSD float64
	// VirtuousCycleTotalApplies is the cumulative number of CTA applies
	// across the tracked period (from pushed virtuous_cycle rows).
	VirtuousCycleTotalApplies int64
	// VirtuousCycleTotalProjectedUSD is the sum of monthly projected savings
	// across all recorded CTA applies.
	VirtuousCycleTotalProjectedUSD float64
	// VirtuousCycleTotalRealizedUSD is the cumulative realized savings
	// measured from the earliest apply to now.
	VirtuousCycleTotalRealizedUSD float64
	// VirtuousCycleTotalRealizedTurns is the number of graded turns
	// contributing to the realized figure.
	VirtuousCycleTotalRealizedTurns int64
	// Version stamps the email footer.
	Version string
	// To is the recipient override; empty leaves Message.To nil so the
	// Notifier applies the configured [email].to defaults.
	To []string
}

// ComposeBriefing renders one day's briefing into an email.Message through the
// shared composer. Pure: no I/O. The subject follows the "[Observer] …"
// convention; the heading frames the day as sessions/turns/spend.
func ComposeBriefing(d BriefingData) email.Message {
	subject := fmt.Sprintf("[Observer] %s — %s", d.Title, d.PeriodLabel)

	heading := fmt.Sprintf("Yesterday: %d sessions, %d turns, %s observed spend.",
		d.Sessions, d.Turns, money(d.TotalUSD))

	fields := []email.Field{
		{Label: "Period", Value: d.PeriodLabel},
		{Label: "Observed spend", Value: money(d.TotalUSD)},
		{Label: "Sessions", Value: fmt.Sprintf("%d", d.Sessions)},
		{Label: "Turns", Value: fmt.Sprintf("%d", d.Turns)},
		{Label: "Input tokens", Value: compact(d.InputTokens)},
		{Label: "Output tokens", Value: compact(d.OutputTokens)},
		{Label: "Cache read", Value: compact(d.CacheReadTokens)},
		{Label: "Cache created", Value: compact(d.CacheCreationTokens)},
		{Label: "Cache hit rate", Value: percent(d.CacheHitRate)},
		{Label: "Error turns", Value: fmt.Sprintf("%d", d.ErrorTurns)},
		{Label: "Active hours", Value: fmt.Sprintf("%.0f", d.ActiveHours)},
		{Label: "Fast turns", Value: percent(d.FastShare)},
	}

	var sections []email.Section
	if len(d.Tools) > 0 {
		sec := email.Section{Title: "Spend by tool"}
		for _, it := range d.Tools {
			sec.Fields = append(sec.Fields, email.Field{Label: labelOrNone(it.Label), Value: money(it.CostUSD)})
		}
		sections = append(sections, sec)
	}
	if d.OpportunityCount > 0 {
		sec := email.Section{Title: "Opportunities"}
		sec.Fields = append(sec.Fields,
			email.Field{Label: "Cards", Value: fmt.Sprintf("%d", d.OpportunityCount)},
			email.Field{Label: "Estimated savings", Value: money(d.OpportunitySavingsUSD)})
		sections = append(sections, sec)
	}
	if d.VirtuousCycleTotalApplies > 0 {
		realizedPct := 0.0
		if d.VirtuousCycleTotalProjectedUSD > 0 {
			realizedPct = d.VirtuousCycleTotalRealizedUSD / d.VirtuousCycleTotalProjectedUSD * 100
		}
		sec := email.Section{Title: "Virtuous cycle"}
		sec.Fields = append(sec.Fields,
			email.Field{Label: "CTA applies", Value: fmt.Sprintf("%d", d.VirtuousCycleTotalApplies)},
			email.Field{Label: "Projected (monthly)", Value: money(d.VirtuousCycleTotalProjectedUSD)},
			email.Field{Label: "Realized", Value: money(d.VirtuousCycleTotalRealizedUSD)},
			email.Field{Label: "Realized / projected", Value: fmt.Sprintf("%.0f%%", realizedPct)},
			email.Field{Label: "Graded turns", Value: fmt.Sprintf("%d", d.VirtuousCycleTotalRealizedTurns)})
		sections = append(sections, sec)
	}

	return email.Compose(email.ComposeParams{
		Subject:  subject,
		Heading:  heading,
		Fields:   fields,
		Sections: sections,
		Version:  d.Version,
		To:       d.To,
	})
}

// compact renders a token count as a short human figure (12K, 3.4M, 1.2B).
func compact(n int64) string {
	abs := n
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case abs >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case abs >= 1_000:
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// percent renders a 0..1 fraction as "42%".
func percent(v float64) string {
	pct := v * 100
	s := fmt.Sprintf("%.0f%%", pct)
	// A present-but-absurd input (e.g. a NaN ratio) must never produce "NaN%".
	if strings.Contains(s, "NaN") || strings.Contains(s, "%!") {
		return "n/a"
	}
	return s
}
