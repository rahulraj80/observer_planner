package dashboard

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

// REVIEW-3 P3-19: /api/suggestions, /api/discover and
// /api/compression/retrieval read `days` only, so a closed custom range
// showed the trailing 7 / 14 / 30 days ending NOW while the top bar showed
// the past range. They now resolve the window through windowRange and bound
// every read by `< until`.

// copyActionAt duplicates one seeded action row with a new timestamp.
func copyActionAt(t *testing.T, db *sql.DB, at time.Time) {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(actions)`)
	if err != nil {
		t.Fatal(err)
	}
	var cols, sel []string
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "id" {
			continue
		}
		cols = append(cols, name)
		switch name {
		case "timestamp":
			sel = append(sel, "?")
		case "source_event_id":
			sel = append(sel, "'p319-'||?")
		default:
			sel = append(sel, name)
		}
	}
	_ = rows.Close()
	q := `INSERT INTO actions (` + strings.Join(cols, ",") + `) SELECT ` + strings.Join(sel, ",") + ` FROM actions ORDER BY id LIMIT 1`
	ts := at.UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(q, ts, ts); err != nil {
		t.Fatalf("copy action: %v", err)
	}
}

func TestP319ClosedRangeDiscover(t *testing.T) {
	s, _ := newTestServer(t)
	now := time.Now().UTC()
	copyActionAt(t, s.opts.DB, now.Add(-8*24*time.Hour))
	copyActionAt(t, s.opts.DB, now.Add(-time.Hour))

	q := closedRangeQuery(now.Add(-10*24*time.Hour), now.Add(-5*24*time.Hour))
	got := getJSONT550(t, s, "/api/discover?"+q)
	sum := got["summary"].(map[string]any)
	if sum["total_actions"].(float64) != 1 {
		t.Errorf("summary.total_actions: got %v want 1 (only the in-range action)", sum["total_actions"])
	}
}

func TestP319ClosedRangeCompressionRetrieval(t *testing.T) {
	s, _ := newTestServer(t)
	now := time.Now().UTC()
	for _, at := range []time.Time{now.Add(-8 * 24 * time.Hour), now.Add(-time.Hour), now.Add(-2 * time.Hour)} {
		if _, err := s.opts.DB.Exec(
			`INSERT INTO retrieval_signals (action_id, signal_type, signal_at, payload)
			 VALUES (NULL, 'retrieve_stashed', ?, 'sha-A')`, at.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	q := closedRangeQuery(now.Add(-10*24*time.Hour), now.Add(-5*24*time.Hour))
	got := getJSONT550(t, s, "/api/compression/retrieval?"+q)
	if got["stash_retrievals"].(float64) != 1 {
		t.Errorf("stash_retrievals: got %v want 1 (closed range)", got["stash_retrievals"])
	}
	if got["days"].(float64) != 5 {
		t.Errorf("days: got %v want 5 (the closed range's span)", got["days"])
	}
}

func TestP319ClosedRangeSuggestionsWindow(t *testing.T) {
	s, _ := newTestServer(t)
	now := time.Now().UTC()
	q := closedRangeQuery(now.Add(-10*24*time.Hour), now.Add(-5*24*time.Hour))
	got := getJSONT550(t, s, "/api/suggestions?"+q)
	if got["window_days"].(float64) != 5 {
		t.Errorf("window_days: got %v want 5 (the closed range, not the 14-day default)", got["window_days"])
	}
}

func TestWindowSpanDays(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name         string
		since, until time.Time
		fallback     int
		want         int
	}{
		{"no lower bound keeps fallback", time.Time{}, time.Time{}, 14, 14},
		{"closed five days", now.Add(-10 * 24 * time.Hour), now.Add(-5 * 24 * time.Hour), 14, 5},
		{"part day rounds up", now.Add(-36 * time.Hour), now.Add(-24 * time.Hour), 14, 1},
		{"open since", now.Add(-3*24*time.Hour + time.Minute), time.Time{}, 14, 3},
		{"inverted is at least one", now, now.Add(-time.Hour), 14, 1},
	}
	for _, c := range cases {
		if got := windowSpanDays(c.since, c.until, c.fallback); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}
