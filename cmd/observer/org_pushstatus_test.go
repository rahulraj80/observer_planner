package main

import (
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/store"
)

func TestEligibleAndHistoricalTotals(t *testing.T) {
	maxIDs := store.PushCursor{Sessions: 10, Actions: 100, APITurns: 20, TokenUsage: 30, GuardEvents: 5}
	counts := store.PushCursor{Sessions: 0, Actions: 10, APITurns: 0, TokenUsage: 0, GuardEvents: 0}
	if got := eligibleTotal(counts); got != 10 {
		t.Errorf("eligibleTotal = %d, want 10", got)
	}
	if got := historicalTotal(maxIDs); got != 165 {
		t.Errorf("historicalTotal = %d, want 165", got)
	}
}

func TestPushEligibilityReason(t *testing.T) {
	// eligible > 0 → no reason.
	if r := pushEligibilityReason(5, 100, nil); r != "" {
		t.Errorf("eligible>0 should give no reason, got %q", r)
	}
	// no history at all.
	if r := pushEligibilityReason(0, 0, nil); !strings.Contains(r, "no captured activity") {
		t.Errorf("zero-history reason = %q", r)
	}
	// history but no push yet → predates-enrolment.
	if r := pushEligibilityReason(0, 250, nil); !strings.Contains(r, "predate enrolment") {
		t.Errorf("predates-enrolment reason = %q", r)
	}
	// history + a prior push → up-to-date.
	lp := &store.PushLogEntry{PushedAt: "2026-06-25T10:00:00Z", Status: "ok"}
	if r := pushEligibilityReason(0, 250, lp); !strings.Contains(r, "up to date") {
		t.Errorf("up-to-date reason = %q", r)
	}
}

func TestNextAttempt(t *testing.T) {
	if got := nextAttempt(false, nil, 900); !strings.Contains(got, "not running") {
		t.Errorf("not-running = %q", got)
	}
	if got := nextAttempt(true, nil, 900); !strings.Contains(got, "no push yet") {
		t.Errorf("no-push = %q", got)
	}
	lp := &store.PushLogEntry{PushedAt: "2026-06-25T10:00:00Z"}
	if got := nextAttempt(true, lp, 900); got != "2026-06-25T10:15:00Z" {
		t.Errorf("next from last+interval = %q, want 2026-06-25T10:15:00Z", got)
	}
	// unparseable timestamp → graceful fallback.
	bad := &store.PushLogEntry{PushedAt: "not-a-time"}
	if got := nextAttempt(true, bad, 900); !strings.Contains(got, "after the last attempt") {
		t.Errorf("bad-timestamp fallback = %q", got)
	}
}

// TestPushStampLocalNamesTheZone: the last-push stamp is SQLite UTC with no
// zone; every surface renders it in local time with the zone named.
func TestPushStampLocalNamesTheZone(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	cases := []struct {
		in   string
		loc  *time.Location
		want string
	}{
		{"2026-10-03 04:59:07", ist, "2026-10-03 10:29 IST"},
		{"2026-10-03 04:59:07", time.UTC, "2026-10-03 04:59 UTC"},
		{"2026-10-03T04:59:07Z", ist, "2026-10-03T04:59:07Z"},
		{"", ist, ""},
		{"2026-10-03 04:59:07", nil, "2026-10-03 04:59:07"},
	}
	for _, c := range cases {
		if got := pushStampLocal(c.in, c.loc); got != c.want {
			t.Errorf("pushStampLocal(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
