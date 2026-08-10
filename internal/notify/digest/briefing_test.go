package digest

import (
	"math"
	"strings"
	"testing"
)

func TestComposeBriefingContainsKeyLines(t *testing.T) {
	m := ComposeBriefing(BriefingData{
		Title:                 "Daily briefing",
		PeriodLabel:           "Sat, Aug 1, 2026",
		TotalUSD:              42.50,
		Sessions:              3,
		Turns:                 27,
		InputTokens:           1_200_000,
		OutputTokens:          340_000,
		CacheReadTokens:       2_000_000,
		CacheCreationTokens:   400_000,
		CacheHitRate:          0.62,
		ErrorTurns:            1,
		ActiveHours:           6.5,
		FastShare:             0.15,
		Tools:                 []LineItem{{Label: "claude-code", CostUSD: 42.50}},
		OpportunityCount:      2,
		OpportunitySavingsUSD: 3.20,
		Version:               "v1.19.0",
	})

	if !strings.Contains(m.Subject, "Daily briefing") || !strings.Contains(m.Subject, "Aug 1") {
		t.Errorf("subject missing title/period: %q", m.Subject)
	}
	wantText := []string{
		"Yesterday: 3 sessions, 27 turns, $42.50 observed spend.",
		"Sessions: 3",
		"Turns: 27",
		"Observed spend: $42.50",
		"Input tokens: 1.2M",
		"Output tokens: 340K",
		"Cache hit rate: 62%",
		"Error turns: 1",
		"Active hours: 6",
		"Fast turns: 15%",
		"Spend by tool",
		"claude-code: $42.50",
		"Opportunities",
		"Cards: 2",
		"Estimated savings: $3.20",
		"Sent by SuperBased Observer v1.19.0",
	}
	for _, w := range wantText {
		if !strings.Contains(m.Text, w) {
			t.Errorf("text body missing %q\n---\n%s", w, m.Text)
		}
	}
	for _, w := range []string{"Yesterday: 3 sessions", "$42.50", "Opportunities"} {
		if !strings.Contains(m.HTML, w) {
			t.Errorf("html body missing %q", w)
		}
	}
}

func TestComposeBriefingOmitsEmptySections(t *testing.T) {
	m := ComposeBriefing(BriefingData{
		Title:       "Daily briefing",
		PeriodLabel: "Sun, Aug 2, 2026",
	})
	if strings.Contains(m.Text, "Spend by tool") {
		t.Errorf("empty tools must not render a Spend by tool section\n---\n%s", m.Text)
	}
	if strings.Contains(m.Text, "Opportunities") {
		t.Errorf("zero opportunities must not render an Opportunities section\n---\n%s", m.Text)
	}
	if strings.Contains(m.Text, "Virtuous cycle") {
		t.Errorf("zero virtuous-cycle applies must not render the section\n---\n%s", m.Text)
	}
	if !strings.Contains(m.Text, "Yesterday: 0 sessions, 0 turns, $0.00 observed spend.") {
		t.Errorf("zero day must still render the heading\n---\n%s", m.Text)
	}
}

// TestComposeBriefingVirtuousCycle pins the Stage 3 exec-memo section: with
// applies present, the email renders the cumulative applies + projected +
// realized + realized/projected ratio + graded turns, in both text and HTML.
func TestComposeBriefingVirtuousCycle(t *testing.T) {
	m := ComposeBriefing(BriefingData{
		Title:                           "Daily briefing",
		PeriodLabel:                     "Mon, Aug 3, 2026",
		VirtuousCycleTotalApplies:       4,
		VirtuousCycleTotalProjectedUSD:  24.00,
		VirtuousCycleTotalRealizedUSD:   15.50,
		VirtuousCycleTotalRealizedTurns: 9,
	})
	wantText := []string{
		"Virtuous cycle",
		"CTA applies: 4",
		"Projected (monthly): $24.00",
		"Realized: $15.50",
		"Realized / projected: 65%",
		"Graded turns: 9",
	}
	for _, w := range wantText {
		if !strings.Contains(m.Text, w) {
			t.Errorf("text body missing %q\n---\n%s", w, m.Text)
		}
	}
	for _, w := range []string{"Virtuous cycle", "$15.50", "65%"} {
		if !strings.Contains(m.HTML, w) {
			t.Errorf("html body missing %q", w)
		}
	}
}

func TestCompactCounts(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1K"},
		{1200, "1K"},
		{2500, "2K"}, // Go's %.0f rounds half-to-even
		{1_200_000, "1.2M"},
		{3_400_000_000, "3.4B"},
	}
	for _, tt := range tests {
		if got := compact(tt.in); got != tt.want {
			t.Errorf("compact(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPercentClampsNaN(t *testing.T) {
	if got := percent(0.42); got != "42%" {
		t.Errorf("percent(0.42) = %q, want 42%%", got)
	}
	if got := percent(math.NaN()); got != "n/a" {
		t.Errorf("percent(NaN) = %q, want n/a", got)
	}
}
