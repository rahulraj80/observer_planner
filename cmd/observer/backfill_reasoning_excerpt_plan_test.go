package main

import (
	"context"
	"strings"
	"testing"
)

// TestReasoningExcerptSQLSeeksByC0 pins T-415 for the reasoning-converge
// pass: the excerpt count and delete must resolve action ids through the
// migration-147 c0 index on the FTS5 shadow table, and the delete must
// reach the virtual table by rowid (idxStr "="). The pre-fix
// `... FROM action_excerpts WHERE action_id IN (...)` planned as
// `SCAN action_excerpts VIRTUAL TABLE INDEX 0:`, a scan of every excerpt
// per 400-id chunk.
func TestReasoningExcerptSQLSeeksByC0(t *testing.T) {
	ctx := context.Background()
	database := seedReasoningFixture(t, ctx, nil)
	ph := placeholders(3)
	args := []any{int64(1), int64(2), int64(3)}
	for _, tc := range []struct {
		name string
		q    string
	}{
		{"count", reasoningExcerptCountSQL(ph)},
		{"delete", reasoningExcerptDeleteSQL(ph)},
	} {
		rows, err := database.QueryContext(ctx, "EXPLAIN QUERY PLAN "+tc.q, args...)
		if err != nil {
			t.Fatalf("%s: explain: %v", tc.name, err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, notUsed int
			var detail string
			if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		joined := strings.Join(plan, "\n")
		if !strings.Contains(joined, "idx_action_excerpts_content_c0 (c0=?)") {
			t.Errorf("%s: c0 index not seeked:\n%s", tc.name, joined)
		}
		for _, line := range plan {
			if strings.Contains(line, "VIRTUAL TABLE") && !strings.Contains(line, "INDEX 0:=") {
				t.Errorf("%s: FTS5 table scanned, not reached by rowid: %q\n%s", tc.name, line, joined)
			}
		}
	}
}

// TestReasoningExcerptSQLCountsAndDeletesEveryRow pins that the reshaped
// statements still see a legacy duplicate excerpt row and leave other
// actions' rows alone.
func TestReasoningExcerptSQLCountsAndDeletesEveryRow(t *testing.T) {
	ctx := context.Background()
	database := seedReasoningFixture(t, ctx, nil)
	for _, stmt := range []string{
		`INSERT INTO action_excerpts (action_id, excerpt) VALUES (7, 'first body')`,
		`INSERT INTO action_excerpts (rowid, action_id, excerpt) VALUES (987654, 7, 'legacy duplicate')`,
		`INSERT INTO action_excerpts (action_id, excerpt) VALUES (8, 'other action')`,
	} {
		if _, err := database.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	ph := placeholders(2)
	args := []any{int64(7), int64(99)}
	var n int
	if err := database.QueryRowContext(ctx, reasoningExcerptCountSQL(ph), args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
	res, err := database.ExecContext(ctx, reasoningExcerptDeleteSQL(ph), args...)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := res.RowsAffected(); got != 2 {
		t.Errorf("deleted = %d, want 2", got)
	}
	if left := rcScanInt(t, ctx, database, `SELECT COUNT(*) FROM action_excerpts`); left != 1 {
		t.Errorf("remaining excerpts = %d, want 1 (action 8 untouched)", left)
	}
}
