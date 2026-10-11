package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/retention"
)

// t373Session ingests one session with one action.
func t373Session(t *testing.T, s *Store, id string) {
	t.Helper()
	if _, err := s.Ingest(context.Background(), []models.ToolEvent{{
		Tool: models.ToolClaudeCode, SourceFile: "/t/" + id + ".jsonl", SourceEventID: id + ":1",
		SessionID: id, ProjectRoot: "/tmp/p_t373", Timestamp: time.Now().UTC(),
		ActionType: models.ActionRunCommand, Target: "ls", RawToolName: "Bash", Success: true,
	}}, nil, IngestOptions{}); err != nil {
		t.Fatal(err)
	}
}

// t373DropSession removes a session the way the orphaned-session sweep does
// for one with no rows (its action is removed first).
func t373DropSession(t *testing.T, s *Store, id string) {
	t.Helper()
	for _, q := range []string{`DELETE FROM actions WHERE session_id = ?`, `DELETE FROM sessions WHERE id = ?`} {
		if _, err := s.db.ExecContext(context.Background(), q, id); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPushCursorAboveSessionRowidStillShipsNewSessions pins T-373's push half:
// when the newest sessions are deleted (the orphaned-session sweep at daemon
// start), SQLite hands the next session a rowid AT OR BELOW the persisted
// cursor, and `rowid > cursor` used to skip it for good.
func TestPushCursorAboveSessionRowidStillShipsNewSessions(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"s1", "s2", "s3"} {
		t373Session(t, s, id)
	}
	maxIDs, err := s.CurrentMaxIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Everything so far was shipped.
	if err := s.SavePushCursor(ctx, maxIDs); err != nil {
		t.Fatal(err)
	}
	// s3 ends up with no rows; the daemon-start sweep removes it, with the
	// composition root's hook (cmd/observer/prune.go::runRetention).
	if _, err := s.db.ExecContext(ctx, `DELETE FROM actions WHERE session_id = 's3'`); err != nil {
		t.Fatal(err)
	}
	pruner := retention.New(s.db).WithSessionsDeletedHook(func(ctx context.Context, tx *sql.Tx) error {
		return ClampSessionPushCursor(ctx, tx)
	})
	if res, err := pruner.Run(ctx, retention.Options{}); err != nil || res.OrphanedSessionsDeleted != 1 {
		t.Fatalf("sweep = %+v, err %v; want one orphaned session deleted", res, err)
	}
	t373Session(t, s, "s4") // SQLite hands it s3's rowid
	cur, err := s.LoadPushCursor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := s.SelectUnpushedSince(ctx, cur, 1<<20, "org-1", "dev@acme.example", ShareOptions{}, ScopeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var shipped []string
	for _, r := range batch.Sessions {
		shipped = append(shipped, r.ID)
	}
	if len(shipped) != 1 || shipped[0] != "s4" {
		t.Fatalf("sessions shipped = %v, want [s4]", shipped)
	}
}

// TestPushEligibleCountsNeverNegative pins T-373's status half: the eligible
// count is the rows above the cursor, so a cursor above MAX(rowid) reads 0,
// not a negative number.
func TestPushEligibleCountsNeverNegative(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"s1", "s2", "s3"} {
		t373Session(t, s, id)
	}
	cur, err := s.CurrentMaxIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t373DropSession(t, s, "s3")
	got, err := s.PushEligibleCounts(ctx, cur)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sessions != 0 || got.Actions != 0 {
		t.Fatalf("eligible above a cursor past the max = %+v, want zeros", got)
	}
	all, err := s.PushEligibleCounts(ctx, PushCursor{})
	if err != nil {
		t.Fatal(err)
	}
	if all.Sessions != 2 || all.Actions != 2 {
		t.Fatalf("rows above the zero cursor = %+v, want 2 sessions and 2 actions", all)
	}
}

// TestDeleteEnrolmentClearsPushLog pins T-373's unenrol half: the push
// history belongs to the org being left.
func TestDeleteEnrolmentClearsPushLog(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	if err := s.RecordPush(ctx, 10, 2048, "ok", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEnrolment(ctx); err != nil {
		t.Fatal(err)
	}
	lp, err := s.LastPushLog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if lp != nil {
		t.Fatalf("org_push_log survived unenrol: %+v", lp)
	}
}
