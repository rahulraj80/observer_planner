package retention

import (
	"context"
	"strings"
	"testing"
)

// TestDeleteAgedExcerptsSeeksByRowid pins T-415: the retention pass must
// reach the FTS5 excerpt table by rowid (idxStr "=") from the aged actions'
// timestamp range through the migration-147 c0 index. The pre-fix
// `DELETE FROM action_excerpts WHERE action_id IN (SELECT id FROM actions
// WHERE timestamp < ?)` planned as `SCAN action_excerpts VIRTUAL TABLE
// INDEX 0:` (empty idxStr), a scan of every excerpt on every pass.
func TestDeleteAgedExcerptsSeeksByRowid(t *testing.T) {
	dbPath, _ := seed(t)
	d := openExisting(t, dbPath)
	rows, err := d.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+deleteAgedExcerptsSQL, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	sawVTab, sawC0, sawTS := false, false, false
	for _, line := range plan {
		if strings.Contains(line, "action_excerpts VIRTUAL TABLE") {
			sawVTab = true
			if !strings.Contains(line, "INDEX 0:=") {
				t.Errorf("excerpt table not reached by rowid: %q\nplan:\n%s", line, joined)
			}
		}
		if strings.Contains(line, "action_excerpts_content") {
			sawC0 = true
			if !strings.Contains(line, "(c0=?)") {
				t.Errorf("shadow table not seeked by c0: %q\nplan:\n%s", line, joined)
			}
		}
		if strings.Contains(line, "idx_actions_") && strings.Contains(line, "timestamp<?") {
			sawTS = true
		}
	}
	if !sawVTab || !sawC0 || !sawTS {
		t.Errorf("plan missing the aged-range seek path (vtab=%v c0=%v ts=%v):\n%s", sawVTab, sawC0, sawTS, joined)
	}
}

// TestRun_AgePruningRemovesLegacyDuplicateExcerpts pins that the reshaped
// delete still removes every excerpt row of an aged action, including a
// legacy duplicate at an arbitrary rowid, and leaves younger actions' rows.
func TestRun_AgePruningRemovesLegacyDuplicateExcerpts(t *testing.T) {
	dbPath, _ := seed(t)
	d := openExisting(t, dbPath)
	ctx := context.Background()
	var oldest int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM actions ORDER BY timestamp LIMIT 1`).Scan(&oldest); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx,
		`INSERT INTO action_excerpts (rowid, action_id, tool_name, target, excerpt, error_message)
		 VALUES (987654, ?, 'Bash', 'dup', 'legacy duplicate body', '')`, oldest); err != nil {
		t.Fatal(err)
	}
	res, err := New(d).Run(ctx, Options{MaxAgeDays: 100, DBPath: dbPath})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExcerptsDeleted != 2 {
		t.Errorf("excerpts deleted = %d, want 2 (the aged row and its legacy duplicate)", res.ExcerptsDeleted)
	}
	var left, total int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM action_excerpts_content WHERE c0 = ?`, oldest).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("aged action still has %d excerpt rows", left)
	}
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM action_excerpts`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("remaining excerpts = %d, want 2 (the two younger actions)", total)
	}
}
