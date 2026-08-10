package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/marmutapp/superbased-observer/internal/configapply"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// virtuouscycleorg.go is the node side of the Virtuous-Cycle trend push (Stage 3
// of docs/design/standup-briefing.md). It lives OUTSIDE orgpush.go on
// purpose: the privacy sentinel (tests/invariant/privacy_test.go) forbids
// orgpush.go from naming any node-local table, and this file is the
// single place the node-local read for the virtuous cycle aggregate is owned.
// The same file-boundary rule that keeps the obs_* reads inside internal/obs
// and the briefing_aggregates read in briefingorg.go.
//
// The aggregate is CONTENT-FREE: per (week_start) cumulative applies +
// projected + realized (broad filter from earliest apply to now), composed from
// the CTA ledger (briefing-cta-ledger.jsonl) + router_decisions realized_delta_usd.
// It never reads the briefing_snapshots, CTA ledger content, or raw router_decisions rows.
// Ships only under the node-side [org_client.share] virtuous_cycle opt-in.

// SelectVirtuousCycle returns the cumulative virtuous-cycle trend buckets,
// grouped by week (Monday-start). Each row represents one week with applies +
// projected monthly savings; the MOST RECENT week additionally carries the
// cumulative realized savings measured from the earliest apply to now (broad
// filter — no mode/policy filter to avoid double-count). windowDays is
// reserved for future windowing; today the trend spans ALL applies (the
// cumulative figure is what makes it a cycle). Requires s.dbPath to be set
// (via SetDBPath) to locate the ledger file.
func (s *Store) SelectVirtuousCycle(ctx context.Context, windowDays int) ([]orgcontract.VirtuousCycleRow, error) {
	_ = windowDays // reserved: trend spans all applies today
	if s.dbPath == "" {
		return nil, fmt.Errorf("store.SelectVirtuousCycle: dbPath not set")
	}

	// Read the CTA ledger file
	ledgerPath := filepath.Join(filepath.Dir(s.dbPath), "briefing-cta-ledger.jsonl")
	events, _, err := configapply.NewLedger(ledgerPath).Read()
	if err != nil {
		return nil, fmt.Errorf("store.SelectVirtuousCycle: read ledger: %w", err)
	}

	// Collect all routing applies (not just in-window)
	var applies []configapply.LedgerEvent
	for _, ev := range events {
		if ev.Kind == "apply" && ev.Section == "routing" {
			applies = append(applies, ev)
		}
	}
	if len(applies) == 0 {
		return nil, nil
	}

	// Find earliest apply timestamp for the cumulative realized filter
	earliest := applies[0].Time
	for _, ev := range applies[1:] {
		if ev.Time.Before(earliest) {
			earliest = ev.Time
		}
	}

	// Query cumulative realized from earliest apply to now (broad filter:
	// all routing modes/policies since the first apply to now, to avoid double-count).
	q := RealizedRoutingQuery{Since: earliest.UTC(), Until: time.Now().UTC()}
	realized, qerr := s.SelectRealizedRoutingDelta(ctx, q)
	if qerr != nil {
		return nil, fmt.Errorf("store.SelectVirtuousCycle: realized delta: %w", qerr)
	}

	// Bucket each apply by week for the trend (applies + projected only).
	// RealizedUSD is NOT attributed per week (that would double-count the
	// broad cumulative query) — instead the MOST RECENT week carries the
	// single cumulative figure from earliest apply to now (set after sort
	// below). The orgserver digest sums across rows, so a single carrier
	// keeps the sum exact.
	byWeek := map[string]*orgcontract.VirtuousCycleRow{}
	for _, ev := range applies {
		weekStart := weekStart(ev.Time)
		w := byWeek[weekStart]
		if w == nil {
			w = &orgcontract.VirtuousCycleRow{WeekStart: weekStart}
			byWeek[weekStart] = w
		}
		w.Applies++
		w.ProjectedUSD += ev.ProjectedUSD
	}

	// Sort weeks chronologically, then stamp the cumulative realized figure
	// onto the MOST RECENT week only.
	weeks := make([]orgcontract.VirtuousCycleRow, 0, len(byWeek))
	for _, w := range byWeek {
		weeks = append(weeks, *w)
	}
	sort.Slice(weeks, func(i, j int) bool { return weeks[i].WeekStart < weeks[j].WeekStart })
	if len(weeks) > 0 {
		weeks[len(weeks)-1].RealizedUSD = realized.DeltaUSD
		weeks[len(weeks)-1].RealizedTurns = int64(realized.GradedTurns)
	}

	return weeks, nil
}

// weekStart returns the Monday of the week containing t, formatted YYYY-MM-DD.
func weekStart(t time.Time) string {
	// Adjust to Monday (0 = Sunday in Go, so Monday = 1)
	daysToMonday := int(t.Weekday() - time.Monday)
	if daysToMonday < 0 {
		daysToMonday += 7
	}
	monday := t.AddDate(0, 0, -daysToMonday)
	return monday.Format("2006-01-02")
}
