package indexing

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// explainPlan returns the detail column of EXPLAIN QUERY PLAN for q.
func explainPlan(t testing.TB, d *sql.DB, q string, args ...any) []string {
	t.Helper()
	rows, err := d.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+q, args...)
	if err != nil {
		t.Fatalf("explain %q: %v", q, err)
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

// seedExcerpts inserts n excerpt rows for action ids 1000+i with
// auto-assigned rowids (the legacy writer's shape).
func seedExcerpts(t testing.TB, d *sql.DB, n int) {
	t.Helper()
	ctx := context.Background()
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	st, err := tx.PrepareContext(ctx, insertExcerptSQL)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := 0; i < n; i++ {
		if _, err := st.ExecContext(ctx, int64(1000+i), "Bash", "go test ./...",
			fmt.Sprintf("ok package %d PASS some output words %d", i%977, i), ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// TestDeletePriorUsesRowidSeek pins T-412: the replace-delete must reach
// the FTS5 table by rowid equality (idxStr "=") via the shadow table's c0
// index, never as a full scan of every excerpt. The pre-fix statement,
// `DELETE FROM action_excerpts WHERE action_id = ?`, plans as
// `SCAN action_excerpts VIRTUAL TABLE INDEX 0:` (empty idxStr = full scan).
func TestDeletePriorUsesRowidSeek(t *testing.T) {
	t.Parallel()
	d := seedDB(t)
	seedExcerpts(t, d, 2000)
	plan := explainPlan(t, d, deletePriorSQL, int64(1500))
	joined := strings.Join(plan, "\n")
	sawVTab, sawSeek := false, false
	for _, line := range plan {
		if strings.Contains(line, "action_excerpts VIRTUAL TABLE") {
			sawVTab = true
			// FTS5 encodes a rowid-equality constraint as "=" in idxStr;
			// a bare "INDEX 0:" is the full scan.
			if !strings.Contains(line, "INDEX 0:=") {
				t.Errorf("FTS5 table is not reached by rowid: %q\nplan:\n%s", line, joined)
			}
		}
		if strings.HasPrefix(line, "SCAN action_excerpts_content") {
			t.Errorf("shadow content table scanned instead of seeked: %q\nplan:\n%s", line, joined)
		}
		if strings.HasPrefix(line, "SEARCH action_excerpts_content USING") &&
			strings.Contains(line, "idx_action_excerpts_content_c0") {
			sawSeek = true
		}
	}
	if !sawVTab || !sawSeek {
		t.Errorf("plan missing the rowid seek path (vtab=%v c0-seek=%v):\n%s", sawVTab, sawSeek, joined)
	}
}

// TestIndexReplacesLegacyRows pins that the rowid-seek delete still finds
// rows written by the old code (arbitrary auto-assigned rowids, and even a
// duplicate pair for one action) and leaves every other action alone.
func TestIndexReplacesLegacyRows(t *testing.T) {
	t.Parallel()
	d := seedDB(t)
	ctx := context.Background()
	seedExcerpts(t, d, 50)
	// A legacy duplicate for action 1010 at a far-away rowid.
	if _, err := d.ExecContext(ctx,
		`INSERT INTO action_excerpts (rowid, action_id, tool_name, target, excerpt, error_message)
		 VALUES (987654, 1010, 'Bash', 'dup', 'stale duplicate zebra', '')`); err != nil {
		t.Fatal(err)
	}
	idx := New(d, 0)
	if err := idx.Index(ctx, 1010, "Bash", "go test ./...", "fresh output giraffe", ""); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM action_excerpts_content WHERE c0 = 1010`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("action 1010 has %d excerpt rows after re-index, want 1", n)
	}
	if res, _ := idx.Search(ctx, "zebra", 10); len(res) != 0 {
		t.Errorf("legacy duplicate still searchable: %+v", res)
	}
	if res, _ := idx.Search(ctx, "giraffe", 10); len(res) != 1 || res[0].ActionID != 1010 {
		t.Errorf("new excerpt not searchable: %+v", res)
	}
	var total int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM action_excerpts`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 50 {
		t.Errorf("total excerpts = %d, want 50 (other actions untouched)", total)
	}
}

// TestIndexNewInsertsWithoutDelete pins the fresh-insert path: IndexNew
// adds the row, and a later Index of the same action still replaces it.
func TestIndexNewInsertsWithoutDelete(t *testing.T) {
	t.Parallel()
	d := seedDB(t)
	ctx := context.Background()
	idx := New(d, 0)
	if err := idx.IndexNew(ctx, 7, "Bash", "ls", "first body walrus", ""); err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexNew(ctx, 0, "Bash", "ls", "x", ""); err == nil {
		t.Error("IndexNew accepted actionID 0")
	}
	if err := idx.Index(ctx, 7, "Bash", "ls", "second body otter", ""); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM action_excerpts_content WHERE c0 = 7`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("action 7 has %d rows, want 1", n)
	}
	if res, _ := idx.Search(ctx, "walrus", 10); len(res) != 0 {
		t.Errorf("replaced body still searchable: %+v", res)
	}
}

// BenchmarkReplaceExcerpt compares the pre-T-412 replace-delete (a full
// scan of the FTS5 table) with Index's rowid seek on a synthetic table.
// Rows default to 50,000; set OBSERVER_FTS_BENCH_ROWS (e.g. 500000) for
// the production-sized measurement.
func BenchmarkReplaceExcerpt(b *testing.B) {
	rowsN := 50000
	if v, err := strconv.Atoi(os.Getenv("OBSERVER_FTS_BENCH_ROWS")); err == nil && v > 0 {
		rowsN = v
	}
	d := seedDB(b)
	seedExcerpts(b, d, rowsN)
	ctx := context.Background()
	idx := New(d, 0)
	b.Run(fmt.Sprintf("legacy_delete_by_action_id_rows=%d", rowsN), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			id := int64(1000 + (i*7919)%rowsN)
			if _, err := d.ExecContext(ctx, `DELETE FROM action_excerpts WHERE action_id = ?`, id); err != nil {
				b.Fatal(err)
			}
			if _, err := d.ExecContext(ctx, insertExcerptSQL, id, "Bash", "t", "body", ""); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run(fmt.Sprintf("Index_rowid_seek_rows=%d", rowsN), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			id := int64(1000 + (i*7919)%rowsN)
			if err := idx.Index(ctx, id, "Bash", "t", "body", ""); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run(fmt.Sprintf("IndexNew_rows=%d", rowsN), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if err := idx.IndexNew(ctx, int64(10_000_000+i), "Bash", "t", "body", ""); err != nil {
				b.Fatal(err)
			}
		}
	})
}
