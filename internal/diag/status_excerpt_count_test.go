package diag

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db"
)

// TestSnapshotExcerptCountAvoidsFTSScan pins T-415: the status snapshot's
// excerpt count must not ask the FTS5 virtual table for COUNT(*) (a full
// scan of every stored excerpt), and must still equal the number of FTS
// rows, legacy duplicates included.
func TestSnapshotExcerptCountAvoidsFTSScan(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "obs.db")
	d, err := db.Open(ctx, db.Options{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	rows, err := d.QueryContext(ctx, "EXPLAIN QUERY PLAN SELECT COUNT(*) FROM "+excerptCountTable)
	if err != nil {
		t.Fatal(err)
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
	for _, line := range plan {
		if strings.Contains(line, "VIRTUAL TABLE") {
			t.Errorf("excerpt count scans the FTS5 virtual table: %q", line)
		}
	}

	for _, stmt := range []string{
		`INSERT INTO action_excerpts (action_id, excerpt) VALUES (1, 'a')`,
		`INSERT INTO action_excerpts (rowid, action_id, excerpt) VALUES (987654, 1, 'legacy duplicate')`,
		`INSERT INTO action_excerpts (action_id, excerpt) VALUES (2, 'b')`,
	} {
		if _, err := d.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := Snapshot(ctx, d, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var fts int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM action_excerpts`).Scan(&fts); err != nil {
		t.Fatal(err)
	}
	if snap.Counts.ActionExcerpts != fts || fts != 3 {
		t.Errorf("ActionExcerpts = %d, FTS rows = %d, want both 3", snap.Counts.ActionExcerpts, fts)
	}
}
