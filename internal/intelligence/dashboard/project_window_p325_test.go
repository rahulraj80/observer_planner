package dashboard

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestProjectDetailWindowFollowsGlobalWindow pins REVIEW-3 P3-25 (T-485): the
// Projects drawer's routes (/api/project/{id} and its children) resolve the
// page's global window, including a closed range's until, instead of a
// days-only trailing window ending now.
func TestProjectDetailWindowFollowsGlobalWindow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	since := now.Add(-10 * 24 * time.Hour)
	until := now.Add(-5 * 24 * time.Hour)
	near := func(a, b time.Time) bool { d := a.Sub(b); return d > -time.Minute && d < time.Minute }

	cases := []struct {
		name                 string
		query                string
		wantSince, wantUntil time.Time
		wantDays             int
	}{
		{"closed range", "?" + closedRangeQuery(since, until), since, until, 5},
		{"days", "?days=7", now.Add(-7 * 24 * time.Hour), now, 7},
		{"hours", "?hours=12", now.Add(-12 * time.Hour), now, 1},
		{"default", "", now.Add(-30 * 24 * time.Hour), now, 30},
		{"days capped at 365", "?days=1000", now.Add(-365 * 24 * time.Hour), now, 365},
	}
	for _, c := range cases {
		gotSince, gotUntil, gotDays := projectDetailWindow(httptest.NewRequest("GET", "/api/project/1"+c.query, nil))
		if !near(gotSince, c.wantSince) || !near(gotUntil, c.wantUntil) || gotDays != c.wantDays {
			t.Errorf("%s: got [%s, %s) days=%d, want [%s, %s) days=%d", c.name,
				gotSince.Format(time.RFC3339), gotUntil.Format(time.RFC3339), gotDays,
				c.wantSince.Format(time.RFC3339), c.wantUntil.Format(time.RFC3339), c.wantDays)
		}
	}
}
