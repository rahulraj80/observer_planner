package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// explainDetail returns the detail column of EXPLAIN QUERY PLAN for q.
func explainDetail(t *testing.T, d *sql.DB, q string, args ...any) []string {
	t.Helper()
	rows, err := d.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+q, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		out = append(out, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestActionDetailsQueryUsesExcerptIndex pins T-415: get_action_details
// must reach the FTS5 excerpt table by rowid (idxStr "=") through the
// migration-147 c0 index, never as a full scan of every excerpt per action.
// The pre-fix `LEFT JOIN action_excerpts ae ON ae.action_id = a.id` planned
// as `SCAN action_excerpts AS ae VIRTUAL TABLE INDEX 0:` (empty idxStr).
func TestActionDetailsQueryUsesExcerptIndex(t *testing.T) {
	_, database, _ := testServer(t)
	plan := explainDetail(t, database, actionDetailsQuery("?,?,?"), int64(1), int64(2), int64(3))
	joined := strings.Join(plan, "\n")
	sawSeek, sawVTab := false, false
	for _, line := range plan {
		if strings.Contains(line, "VIRTUAL TABLE") {
			sawVTab = true
			if !strings.Contains(line, "INDEX 0:=") {
				t.Errorf("excerpt table not reached by rowid: %q\nplan:\n%s", line, joined)
			}
		}
		if strings.Contains(line, "action_excerpts_content") {
			if !strings.Contains(line, "idx_action_excerpts_content_c0 (c0=?)") {
				t.Errorf("shadow table not seeked through the c0 index: %q\nplan:\n%s", line, joined)
			}
			sawSeek = true
		}
	}
	if !sawSeek || !sawVTab {
		t.Errorf("plan missing the c0 seek path (seek=%v vtab=%v):\n%s", sawSeek, sawVTab, joined)
	}
}

// TestActionDetailsQueryMatchesLegacyJoin pins that the reshaped join
// returns exactly the rows the old full-scan join returned: one row per
// excerpt row (a legacy duplicate yields two), one row with an empty
// excerpt for an action that has none, and nothing for an unknown id.
func TestActionDetailsQueryMatchesLegacyJoin(t *testing.T) {
	s, _ := seedExtra(t)
	database := s.db
	ctx := context.Background()
	var withExcerpt, without int64
	if err := database.QueryRowContext(ctx,
		`SELECT MIN(id) FROM actions WHERE id IN (SELECT c0 FROM action_excerpts_content)`).Scan(&withExcerpt); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT MIN(id) FROM actions WHERE id NOT IN (SELECT c0 FROM action_excerpts_content)`).Scan(&without); err != nil {
		t.Fatal(err)
	}
	if withExcerpt == 0 || without == 0 {
		t.Fatalf("fixture needs an action with and one without an excerpt: with=%d without=%d", withExcerpt, without)
	}
	// A legacy duplicate excerpt row for the same action at a far rowid.
	if _, err := database.ExecContext(ctx,
		`INSERT INTO action_excerpts (rowid, action_id, tool_name, target, excerpt, error_message)
		 VALUES (987654, ?, 'Bash', 'dup', 'legacy duplicate body', '')`, withExcerpt); err != nil {
		t.Fatal(err)
	}
	const legacy = `SELECT a.id, COALESCE(ae.excerpt,'')
		 FROM actions a LEFT JOIN action_excerpts ae ON ae.action_id = a.id
		 WHERE a.id IN (?,?,?)`
	args := []any{withExcerpt, without, int64(424242)}
	collect := func(q string) []string {
		rows, err := database.QueryContext(ctx, q, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id int64
			var ex string
			if err := rows.Scan(&id, &ex); err != nil {
				t.Fatal(err)
			}
			out = append(out, fmt.Sprintf("%d|%s", id, ex))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		sort.Strings(out)
		return out
	}
	// Project the reshaped query onto the same two columns.
	reshaped := `SELECT id, ex FROM (SELECT a.id AS id, COALESCE(ae.excerpt,'') AS ex
		 FROM actions a
		 LEFT JOIN action_excerpts_content aec ON aec.c0 = +a.id
		 LEFT JOIN action_excerpts ae ON ae.rowid = aec.id
		 WHERE a.id IN (?,?,?))`
	if !strings.Contains(actionDetailsQuery("?,?,?"), "LEFT JOIN action_excerpts_content aec ON aec.c0 = +a.id") {
		t.Fatal("actionDetailsQuery no longer uses the reshaped join; update this equivalence test with it")
	}
	got, want := collect(reshaped), collect(legacy)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("reshaped join rows differ from the legacy join\n got: %q\nwant: %q", got, want)
	}
	if len(want) != 3 {
		t.Errorf("legacy join returned %d rows, want 3 (two excerpts + one empty): %q", len(want), want)
	}
}
