package main

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/hookspool"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/intelligence/compaction"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// holdRig is a real database + config + spool for the T-320 tests.
type holdRig struct {
	database *sql.DB
	st       *store.Store
	cfgPath  string
	spool    string
	logger   *slog.Logger
}

func newHoldRig(t *testing.T) holdRig {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "observer.db")
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[observer]\ndb_path = \""+filepath.ToSlash(dbPath)+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(context.Background(), db.Options{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return holdRig{
		database: database, st: store.New(database), cfgPath: cfgPath,
		spool: hookspool.Path(cfgPath), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func (r holdRig) deps() lifecycleDrainDeps {
	rec := compaction.New(r.database)
	return lifecycleDrainDeps{
		st:         r.st,
		compaction: func(ctx context.Context, h hookspool.Record) error { return applyCompactionRecord(ctx, rec, h) },
		hold:       hookspool.Append,
	}
}

// drainTwice runs the two-phase protocol to completion for whatever is in the
// live spool now: one pass sets it aside, the next (past Grace) processes it.
func (r holdRig) drainTwice(t *testing.T, at time.Time) (time.Time, int) {
	t.Helper()
	drainHookLifecycleSpool(context.Background(), r.spool, r.deps(), at, r.logger)
	at = at.Add(hookspool.Grace + time.Second)
	n := drainHookLifecycleSpool(context.Background(), r.spool, r.deps(), at, r.logger)
	return at, n
}

func (r holdRig) count(t *testing.T, q string) int {
	t.Helper()
	var n int
	if err := r.database.QueryRowContext(context.Background(), q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (r holdRig) createSession(t *testing.T, sessionID string) {
	t.Helper()
	_, err := r.st.Ingest(context.Background(), []models.ToolEvent{{
		Tool: models.ToolClaudeCode, SourceFile: "/t/" + sessionID + ".jsonl", SourceEventID: sessionID + ":1",
		SessionID: sessionID, ProjectRoot: "/tmp/p_t320", Timestamp: time.Now().UTC(),
		ActionType: models.ActionRunCommand, Target: "ls", RawToolName: "Bash", Success: true,
	}}, nil, store.IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
}

// TestCompactionBeforeSessionIsHeldThenLanded pins T-320 end to end through
// the real hook recorder: a PreCompact for a session the watcher has not
// ingested is not lost; it waits on the spool and lands once the session row
// exists. Before the fix the recorder failed the sessions foreign key and the
// event was gone.
func TestCompactionBeforeSessionIsHeldThenLanded(t *testing.T) {
	r := newHoldRig(t)
	ctx := context.Background()
	b, ok := resolveHookBinding(models.ToolClaudeCode, "PreCompact")
	if !ok || b.Cap != integration.HookCapCompactLifecycle {
		t.Fatalf("no compact binding for claude-code PreCompact: %+v", b)
	}
	now := time.Now().UTC()
	err := recordHookCompaction(ctx, observeRecord{
		Tool: models.ToolClaudeCode, Label: "claude-code:pre-compact", Binding: b,
		Body:       []byte(`{"session_id":"s_t320","cwd":"/tmp/p_t320","trigger":"auto"}`),
		ConfigPath: r.cfgPath, Now: now,
	})
	if err != nil {
		t.Fatalf("a compaction before its session must be held, not fail: %v", err)
	}
	if got := r.count(t, `SELECT COUNT(*) FROM compaction_events`); got != 0 {
		t.Fatalf("compaction_events = %d before the session exists", got)
	}

	// The session is still missing: the drain holds the record.
	at, n := r.drainTwice(t, now)
	if n != 0 || r.count(t, `SELECT COUNT(*) FROM compaction_events`) != 0 {
		t.Fatalf("drain landed %d records with no session", n)
	}

	// The watcher ingests the session; the next drain lands the capture.
	r.createSession(t, "s_t320")
	_, n = r.drainTwice(t, at.Add(time.Second))
	if n != 1 {
		t.Fatalf("drain after the session arrived landed %d, want 1", n)
	}
	if got := r.count(t, `SELECT COUNT(*) FROM compaction_events WHERE session_id = 's_t320'`); got != 1 {
		t.Fatalf("compaction_events for the session = %d, want 1", got)
	}
}

// TestSessionEndBeforeSessionIsHeldThenLanded is the tool-event half: a
// spooled session end for a session not ingested yet used to be skipped by
// Ingest (it must not bootstrap a phantom session) and lost.
func TestSessionEndBeforeSessionIsHeldThenLanded(t *testing.T) {
	r := newHoldRig(t)
	now := time.Now().UTC()
	if err := hookspool.Append(r.spool, hookspool.FromEvent(models.ToolEvent{
		Tool: models.ToolClaudeCode, SourceFile: "claude-code:hook", SourceEventID: "s_end:session_end",
		SessionID: "s_end", ProjectRoot: "/tmp/p_t320", ActionType: models.ActionSessionEnd, Success: true, Timestamp: now,
	})); err != nil {
		t.Fatal(err)
	}
	at, _ := r.drainTwice(t, now)
	r.createSession(t, "s_end")
	if _, n := r.drainTwice(t, at.Add(time.Second)); n != 1 {
		t.Fatalf("session end after the session arrived landed %d, want 1", n)
	}
	if got := r.count(t, `SELECT COUNT(*) FROM actions WHERE session_id = 's_end' AND action_type = 'session_end'`); got != 1 {
		t.Fatalf("session_end rows = %d, want 1", got)
	}
}

// TestHeldRecordExpiresPastTheAgeCap: a record whose session never arrives is
// dropped once it is older than hookspool.MaxHoldAge, so the spool is
// bounded.
func TestHeldRecordExpiresPastTheAgeCap(t *testing.T) {
	r := newHoldRig(t)
	now := time.Now().UTC()
	if err := hookspool.Append(r.spool, hookspool.Record{
		Kind: hookspool.KindCompaction, Phase: "pre", Tool: models.ToolClaudeCode,
		SessionID: "s_never", Timestamp: now,
	}); err != nil {
		t.Fatal(err)
	}
	at, _ := r.drainTwice(t, now)
	if held, _ := filepath.Glob(r.spool + "*"); len(held) != 1 {
		t.Fatalf("spool files while held = %v, want the one re-appended file", held)
	}
	r.drainTwice(t, at.Add(hookspool.MaxHoldAge+time.Minute))
	matches, _ := filepath.Glob(r.spool + "*")
	if len(matches) != 0 {
		t.Fatalf("spool files after expiry = %v, want none", matches)
	}
}
