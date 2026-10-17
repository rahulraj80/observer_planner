package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/retention"
)

// T-423: does a VACUUM renumber sessions rowids and strand the org push
// cursor? sessions has no INTEGER PRIMARY KEY, so its push cursor is the
// implicit rowid, and SQLite documents that VACUUM "may change" such rowids.
//
// It does not for sessions. VACUUM and VACUUM INTO copy each table through
// SQLite's transfer optimization, which keeps the source rowid whenever the
// table has at least one index (insert.c xferOptimization: OP_Rowid, not
// OP_NewRowid). sessions always has one: its TEXT PRIMARY KEY autoindex,
// plus the idx_sessions_* indexes. Only an index-less rowid table is
// renumbered, and only by a plain VACUUM. Grounded 2026-10-05 on the bundled
// modernc SQLite 3.51.3 and the Ubuntu sqlite3 CLI 3.31.1 (a manual
// external VACUUM). These tests pin that, through every VACUUM path the
// product has, so a future SQLite, driver or schema change that breaks it
// fails here instead of silently stranding sessions.

// t423RowidMap returns sessions' rowid -> id mapping.
func t423RowidMap(t *testing.T, database *sql.DB) map[int64]string {
	t.Helper()
	rows, err := database.QueryContext(context.Background(), `SELECT rowid, id FROM sessions`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var rowid int64
		var id string
		if err := rows.Scan(&rowid, &id); err != nil {
			t.Fatal(err)
		}
		out[rowid] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func t423SameMap(t *testing.T, label string, got, want map[int64]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d sessions, want %d (%v vs %v)", label, len(got), len(want), got, want)
	}
	for rowid, id := range want {
		if got[rowid] != id {
			t.Fatalf("%s renumbered sessions: rowid %d is %q, want %q (got %v)", label, rowid, got[rowid], id, got)
		}
	}
}

// t423ControlRowids returns the rowids of the index-less control table, the
// shape SQLite DOES renumber under a plain VACUUM. It proves the comparison
// above can see a renumbering on this SQLite build.
func t423ControlRowids(t *testing.T, database *sql.DB) []int64 {
	t.Helper()
	rows, err := database.QueryContext(context.Background(), `SELECT rowid FROM t423_control ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var r int64
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// TestVacuumPathsKeepSessionRowids runs each product VACUUM path over a
// sessions table with interior and trailing gaps (deleted sessions) and
// asserts every surviving session keeps its rowid: VACUUM INTO (`observer
// archive reclaim`, `observer db backup`, the update pre-apply snapshot),
// VACUUM (`observer db vacuum`, the dashboard Storage page) and the
// auto_vacuum conversion (`observer prune --vacuum`).
func TestVacuumPathsKeepSessionRowids(t *testing.T) {
	t.Parallel()
	s, database := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"v1", "v2", "v3", "v4", "v5", "v6", "v7", "v8"} {
		t373Session(t, s, id)
	}
	for _, id := range []string{"v2", "v3", "v8"} { // interior gap and the newest
		t373DropSession(t, s, id)
	}
	for _, q := range []string{
		`CREATE TABLE t423_control (x TEXT)`,
		`INSERT INTO t423_control (x) VALUES ('a'), ('b'), ('c'), ('d'), ('e')`,
		`DELETE FROM t423_control WHERE rowid IN (2, 3)`,
	} {
		if _, err := database.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	want := t423RowidMap(t, database)

	copyPath := filepath.Join(t.TempDir(), "reclaim-copy.db")
	if err := db.BackupInto(ctx, database, copyPath); err != nil {
		t.Fatalf("BackupInto: %v", err)
	}
	copyDB, err := db.OpenPlain(ctx, copyPath)
	if err != nil {
		t.Fatal(err)
	}
	t423SameMap(t, "VACUUM INTO", t423RowidMap(t, copyDB), want)
	_ = copyDB.Close()

	if _, err := db.Vacuum(ctx, database); err != nil {
		t.Fatalf("Vacuum: %v", err)
	}
	t423SameMap(t, "VACUUM", t423RowidMap(t, database), want)
	if got := t423ControlRowids(t, database); len(got) == 3 && got[2] == 3 {
		t.Logf("control: the index-less table WAS renumbered by the same VACUUM (%v), so a sessions renumbering would be visible", got)
	} else {
		t.Logf("control: this SQLite build kept even the index-less table's rowids (%v)", got)
	}

	if _, err := retention.ConvertToIncrementalVacuum(ctx, database); err != nil {
		t.Fatalf("ConvertToIncrementalVacuum: %v", err)
	}
	t423SameMap(t, "auto_vacuum conversion VACUUM", t423RowidMap(t, database), want)
}

// t423PushAll drains the push the way the org client does (select, save
// cursor, ack re-sends) and returns the session ids shipped, in order.
func t423PushAll(t *testing.T, s *Store) []string {
	t.Helper()
	ctx := context.Background()
	var shipped []string
	for i := 0; i < 8; i++ {
		cur, err := s.LoadPushCursor(ctx)
		if err != nil {
			t.Fatal(err)
		}
		batch, err := s.SelectUnpushedSince(ctx, cur, 1<<20, "org-1", "dev@acme.example", ShareOptions{}, ScopeOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if batch.RowCount() == 0 {
			return shipped
		}
		for _, r := range batch.Sessions {
			shipped = append(shipped, r.ID)
		}
		if err := s.SavePushCursor(ctx, batch.Cursor); err != nil {
			t.Fatal(err)
		}
		if err := s.AckPushedChanges(ctx, batch.Acks); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("push did not drain")
	return nil
}

// TestVacuumAfterDeletedThenNewSessionsShipsEachSessionOnce is the T-423
// end-to-end pin: sessions are shipped, the newest shipped one and an
// interior one are deleted (through the clamping deleter seam), a session is
// created and left unshipped, the database is VACUUMed, more sessions are
// created, and the database is then compacted with VACUUM INTO and swapped
// in, the way `observer archive reclaim` does. Every session must ship, and
// none twice. The org would absorb a duplicate anyway (its sessions key is
// (id, user_id) with ON CONFLICT DO NOTHING, newer node_rev re-sends aside),
// but a stranded session never reaches it, so the node side is what is pinned.
func TestVacuumAfterDeletedThenNewSessionsShipsEachSessionOnce(t *testing.T) {
	t.Parallel()
	s, database := newTestStore(t)
	ctx := context.Background()
	if err := s.SavePushCursor(ctx, PushCursor{}); err != nil { // enrolled before any activity
		t.Fatal(err)
	}
	var shipped []string
	for _, id := range []string{"e1", "e2", "e3", "e4", "e5"} {
		t373Session(t, s, id)
	}
	shipped = append(shipped, t423PushAll(t, s)...)

	for _, id := range []string{"e5", "e2"} {
		t373DropSession(t, s, id)
	}
	if err := ClampSessionPushCursor(ctx, database); err != nil {
		t.Fatal(err)
	}
	t373Session(t, s, "e6") // created before the VACUUM, not yet shipped
	if _, err := db.Vacuum(ctx, database); err != nil {
		t.Fatal(err)
	}
	t373Session(t, s, "e7")
	shipped = append(shipped, t423PushAll(t, s)...)

	// archive reclaim: VACUUM INTO a copy, then the copy becomes the database.
	t373Session(t, s, "e8") // unshipped when the copy is taken
	copyPath := filepath.Join(t.TempDir(), "reclaimed.db")
	if err := db.BackupInto(ctx, database, copyPath); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := db.OpenPlain(ctx, copyPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reclaimed.Close() })
	rs := New(reclaimed)
	t373Session(t, rs, "e9")
	shipped = append(shipped, t423PushAll(t, rs)...)

	seen := map[string]int{}
	for _, id := range shipped {
		seen[id]++
	}
	for _, id := range []string{"e1", "e2", "e3", "e4", "e5", "e6", "e7", "e8", "e9"} {
		if seen[id] != 1 {
			t.Errorf("session %s shipped %d times, want exactly once (all shipped: %v)", id, seen[id], shipped)
		}
	}
	if len(shipped) != 9 {
		t.Errorf("shipped %d sessions, want 9: %v", len(shipped), shipped)
	}
}
