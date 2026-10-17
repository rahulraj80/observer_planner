//go:build !sbo_omit_adapter_cursor

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/adapter/cursor"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
)

func TestBackfillCursorHookUsage(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "obs.db")
	database, err := dbtemplate.Open(ctx, db.Options{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.ExecContext(ctx,
		`INSERT INTO projects (id, root_path, created_at) VALUES (1, '/repo', '2026-04-29T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO sessions (id, tool, started_at, project_id) VALUES ('cursor-session', 'cursor', '2026-04-29T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}

	logs := filepath.Join(root, "Cursor", "logs", "20260429T190737", "window1", "output_20260429T190743")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logs, "cursor.hooks.workspaceId-test.log")
	body := strings.Join([]string{
		"INPUT:",
		`{"conversation_id":"cursor-session","generation_id":"gen-123","hook_event_name":"stop","workspace_roots":["/repo"],"model":"default","input_tokens":100,"output_tokens":20,"cache_read_tokens":50,"cache_write_tokens":0}`,
		"OUTPUT:",
		`{"permission":"allow","continue":true}`,
		"",
	}, "\n")
	if err := os.WriteFile(logPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	roots := []string{filepath.Join(root, "Cursor", "logs"), filepath.Join(root, "missing-root")}
	res, err := backfillCursorHookUsage(ctx, database, roots, 0)
	if err != nil {
		t.Fatalf("backfillCursorHookUsage: %v", err)
	}
	if res.FilesScanned != 1 || res.TokenRowsWritten != 1 {
		t.Fatalf("unexpected summary: %+v", res)
	}
	// Idempotent: a re-run replays the same payload onto the same
	// (source_file, source_event_id) key.
	if _, err := backfillCursorHookUsage(ctx, database, roots, 0); err != nil {
		t.Fatalf("backfillCursorHookUsage rerun: %v", err)
	}
	var rows int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM token_usage WHERE session_id = 'cursor-session'`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("token rows after rerun = %d (err %v), want 1", rows, err)
	}

	var model string
	var input, output int64
	if err := database.QueryRowContext(ctx,
		`SELECT model, input_tokens, output_tokens FROM token_usage WHERE source_event_id = ?`,
		"gen-123:"+cursor.EventStop).Scan(&model, &input, &output); err != nil {
		t.Fatal(err)
	}
	// input_tokens is NET non-cached: fixture's gross 100 with
	// cache_read_tokens=50 → 50. See cursor.BuildStopTokenEvent.
	if model != "default" || input != 50 || output != 20 {
		t.Fatalf("token row mismatch: model=%q in=%d out=%d (want in=50 net of 50 cached)", model, input, output)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT model FROM sessions WHERE id = 'cursor-session'`).Scan(&model); err != nil {
		t.Fatal(err)
	}
	if model != "default" {
		t.Fatalf("session model = %q", model)
	}
}

// TestBackfillCursorHookUsageSweepsSquatterBeforeReplay pins S10-CURSOR
// review finding 2: a historical mislabelled stop already stored under
// generation B's key (carrying generation A's usage) must never absorb
// B's REAL usage through the upsert's per-column MAX. Since review finding
// F3 (2026-09-26) that is a PROVEN pair, not a counter-equality sweep: the
// replayed afterAgentResponse is B's authoritative usage, it differs from
// the stored row, and the stored row is a byte copy of A's - so the
// squatter is evicted before B's real usage lands.
func TestBackfillCursorHookUsageSweepsSquatterBeforeReplay(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(root, "obs.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, q := range []string{
		`INSERT INTO projects (id, root_path, created_at) VALUES (1, '/repo', '2026-09-08T00:00:00Z')`,
		`INSERT INTO sessions (id, tool, started_at, project_id) VALUES ('cc', 'cursor', '2026-09-08T04:00:00Z', 1)`,
		// A: the real row. B: the squatter restating A's tuple 1 s later.
		`INSERT INTO token_usage (session_id, timestamp, tool, model, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, source, source_file, source_event_id, message_id)
		 VALUES ('cc', '2026-09-08T04:36:43.395Z', 'cursor', 'm', 420473, 53139, 15524224, 0, 'hook', 'cursor:hook', 'gen-A:stop', 'gen-A')`,
		`INSERT INTO token_usage (session_id, timestamp, tool, model, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, source, source_file, source_event_id, message_id)
		 VALUES ('cc', '2026-09-08T04:36:44.749Z', 'cursor', 'm', 420473, 53139, 15524224, 0, 'hook', 'cursor:hook', 'gen-B:stop', 'gen-B')`,
	} {
		if _, err := database.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	logs := filepath.Join(root, "Cursor", "logs", "20260908T121927", "window1", "output_20260908T123734")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	// B's REAL usage, smaller in two columns and larger in one, so a
	// MAX-merge into the squatter would be visible.
	body := strings.Join([]string{
		"[2026-09-08T04:40:00.000Z] Running hook 1",
		"INPUT:",
		`{"conversation_id":"cc","generation_id":"gen-B","hook_event_name":"afterAgentResponse","workspace_roots":["/repo"],"model":"m","text":"ok","input_tokens":1000,"output_tokens":60000,"cache_read_tokens":400,"cache_write_tokens":0}`,
		"OUTPUT:",
		`{}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logs, "cursor.hooks.workspaceId-t.log"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := backfillCursorHookUsage(ctx, database, []string{filepath.Join(root, "Cursor", "logs")}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.DuplicatesRemoved != 1 {
		t.Fatalf("duplicates removed = %d, want the squatter swept once", res.DuplicatesRemoved)
	}
	var in, out, cr int64
	if err := database.QueryRowContext(ctx, `SELECT input_tokens, output_tokens, cache_read_tokens FROM token_usage WHERE source_event_id = 'gen-B:stop'`).Scan(&in, &out, &cr); err != nil {
		t.Fatal(err)
	}
	// input is NET of cache read: 1000 - 400.
	if in != 600 || out != 60000 || cr != 400 {
		t.Fatalf("gen-B = %d/%d/%d, want its real 600/60000/400 (not a MAX-blend with the squatter)", in, out, cr)
	}
	if err := database.QueryRowContext(ctx, `SELECT input_tokens FROM token_usage WHERE source_event_id = 'gen-A:stop'`).Scan(&in); err != nil || in != 420473 {
		t.Fatalf("gen-A input = %d (err %v), want untouched 420473", in, err)
	}
}

func TestBackfillCursorTranscriptActions(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "obs.db")
	database, err := dbtemplate.Open(ctx, db.Options{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.ExecContext(ctx,
		`INSERT INTO projects (id, root_path, created_at) VALUES (1, '/repo', '2026-04-29T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO sessions (id, tool, model, started_at, project_id) VALUES ('cursor-session', 'cursor', 'default', '2026-04-29T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		msg string
		ts  string
	}{
		{"gen-1", "2026-04-29T00:00:10Z"},
		{"gen-2", "2026-04-29T00:00:20Z"},
	} {
		if _, err := database.ExecContext(ctx,
			`INSERT INTO token_usage (session_id, timestamp, tool, model, source_file, source_event_id, message_id, input_tokens, output_tokens, source, reliability)
			 VALUES ('cursor-session', ?, 'cursor', 'default', 'cursor:hook', ?, ?, 10, 5, 'hook', 'accurate')`,
			row.ts, row.msg+":"+cursor.EventStop, row.msg); err != nil {
			t.Fatal(err)
		}
	}

	transcriptDir := filepath.Join(root, "projects", "demo", "agent-transcripts", "cursor-session")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(transcriptDir, "cursor-session.jsonl")
	body := strings.Join([]string{
		`{"role":"user","message":{"content":[{"type":"text","text":"one"}]}}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"scan"},{"type":"tool_use","name":"Glob","input":{"glob_pattern":"*"}}]}}`,
		`{"role":"user","message":{"content":[{"type":"text","text":"two"}]}}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"read"},{"type":"tool_use","name":"ReadFile","input":{"path":"d:\\repo\\README.md"}}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(transcriptPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := backfillCursorTranscriptActions(ctx, database, filepath.Join(root, "projects"), 0)
	if err != nil {
		t.Fatalf("backfillCursorTranscriptActions: %v", err)
	}
	// Each assistant turn emits 1 cursor.assistant_text row (text part)
	// plus 1 tool_use row = 2 actions per turn, 4 total across the two
	// generations covered by token_usage. The pre-v1.4.49 expected count
	// was 2 (tool_use only); v1.4.49 doubles it via the new emission.
	if res.ActionsUpdated != 4 {
		t.Fatalf("ActionsUpdated: got %d want 4 (2 turns × {assistant_text + tool_use})", res.ActionsUpdated)
	}
	var count int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM actions WHERE tool = 'cursor' AND message_id IN ('gen-1','gen-2')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("action count = %d want 4", count)
	}
}

// TestBackfillCursorSubagents pins the v1.4.21 sidechain ingest:
// agent-transcripts/<parent>/subagents/<sub>.jsonl is walked, parsed
// via the cursor adapter's transcript path, and emitted as ToolEvents
// with IsSidechain=true under the parent session_id. Tests both the
// session-id linkage and the sidechain flag landing on inserted rows.
func TestBackfillCursorSubagents(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "obs.db")
	database, err := dbtemplate.Open(ctx, db.Options{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// Build a tiny cursor project tree with one parent transcript and
	// one subagent transcript inside it.
	parentSessionID := "parent-sess"
	subUUID := "sub-AAA"
	transcriptDir := filepath.Join(root, "projects", "demo", "agent-transcripts", parentSessionID)
	subagentDir := filepath.Join(transcriptDir, "subagents")
	if err := os.MkdirAll(subagentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	subPath := filepath.Join(subagentDir, subUUID+".jsonl")
	body := strings.Join([]string{
		`{"role":"user","message":{"content":[{"type":"text","text":"<user_query>research X</user_query>"}]}}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"On it."},{"type":"tool_use","name":"WebFetch","input":{"url":"https://example.com"}}]}}`,
	}, "\n")
	if err := os.WriteFile(subPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	// Seed parent project + session - backfill skips files whose
	// parent session_id has no DB entry (no projectRoot to attribute).
	if _, err := database.ExecContext(ctx,
		`INSERT INTO projects (id, root_path, created_at) VALUES (1, '/tmp/demo', '2026-05-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO sessions (id, tool, started_at, project_id) VALUES (?, 'cursor', '2026-05-01T00:00:00Z', 1)`, parentSessionID); err != nil {
		t.Fatal(err)
	}

	res, err := backfillCursorSubagents(ctx, database, filepath.Join(root, "projects"), 0)
	if err != nil {
		t.Fatalf("backfillCursorSubagents: %v", err)
	}
	if res.FilesScanned != 1 {
		t.Errorf("FilesScanned = %d, want 1", res.FilesScanned)
	}
	if res.EventsBuilt != 3 {
		// 1 user_prompt + 1 cursor.assistant_text + 1 WebFetch tool_use
		t.Errorf("EventsBuilt = %d, want 3", res.EventsBuilt)
	}
	if res.ActionsInserted != 3 {
		t.Errorf("ActionsInserted = %d, want 3", res.ActionsInserted)
	}

	// All inserted rows must carry IsSidechain=1 and the parent SessionID.
	var sidechainCount, otherCount int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM actions WHERE session_id = ? AND is_sidechain = 1`, parentSessionID).Scan(&sidechainCount); err != nil {
		t.Fatal(err)
	}
	if sidechainCount != 3 {
		t.Errorf("sidechain rows: %d want 3", sidechainCount)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM actions WHERE session_id = ? AND is_sidechain = 0`, parentSessionID).Scan(&otherCount); err != nil {
		t.Fatal(err)
	}
	if otherCount != 0 {
		t.Errorf("non-sidechain rows for parent: %d want 0", otherCount)
	}

	// Idempotency: re-running must not duplicate rows. v1.4.28's
	// ON CONFLICT DO UPDATE for actions.duration_ms shifted the
	// inserted-counter semantics to "rows touched"; check the
	// table-level row count instead.
	var beforeCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM actions`).Scan(&beforeCount); err != nil {
		t.Fatal(err)
	}
	if _, err := backfillCursorSubagents(ctx, database, filepath.Join(root, "projects"), 0); err != nil {
		t.Fatal(err)
	}
	var afterCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM actions`).Scan(&afterCount); err != nil {
		t.Fatal(err)
	}
	if afterCount != beforeCount {
		t.Errorf("second run added rows: before=%d after=%d", beforeCount, afterCount)
	}
}

// writeCursorHooksLog places body as a Cursor hooks output log under root
// and returns the logs root to hand to backfillCursorHookUsage.
func writeCursorHooksLog(t *testing.T, root, body string) string {
	t.Helper()
	logs := filepath.Join(root, "Cursor", "logs", "20260908T121927", "window1", "output_20260908T123734")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "cursor.hooks.workspaceId-t.log"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "Cursor", "logs")
}

// TestBackfillCursorHookUsageProvenRestatement pins review finding F3
// (2026-09-26) end to end through the replay: a historical squatter under
// generation B's key is removed ONLY because the replayed log itself shows
// the pair (A's afterAgentResponse, then a stop naming B with A's exact
// usage, and no afterAgentResponse for B before it); and in the same run,
// two REAL consecutive requests whose counters are equal both survive.
func TestBackfillCursorHookUsageProvenRestatement(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(root, "obs.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, q := range []string{
		`INSERT INTO projects (id, root_path, created_at) VALUES (1, '/repo', '2026-09-08T00:00:00Z')`,
		`INSERT INTO sessions (id, tool, started_at, project_id) VALUES ('cc', 'cursor', '2026-09-08T04:00:00Z', 1)`,
		// The squatter a live mislabelled stop left before this fix.
		`INSERT INTO token_usage (session_id, timestamp, tool, model, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, source, source_file, source_event_id, message_id)
		 VALUES ('cc', '2026-09-08T04:36:44.244Z', 'cursor', 'm', 420473, 53139, 0, 0, 'hook', 'cursor:hook', 'gen-B:stop', 'gen-B')`,
	} {
		if _, err := database.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	body := cursorUsageBlock("2026-09-08T04:36:42.860Z", "gen-A", "afterAgentResponse", 420473) +
		cursorUsageBlock("2026-09-08T04:36:44.244Z", "gen-B", "stop", 420473) +
		// Two real requests, equal counters, each stop after its own response.
		cursorUsageBlock("2026-09-08T05:00:00.000Z", "gen-C", "afterAgentResponse", 1000) +
		cursorUsageBlock("2026-09-08T05:00:00.400Z", "gen-C", "stop", 1000) +
		cursorUsageBlock("2026-09-08T05:00:02.000Z", "gen-D", "afterAgentResponse", 1000) +
		cursorUsageBlock("2026-09-08T05:00:02.400Z", "gen-D", "stop", 1000)
	logsRoot := writeCursorHooksLog(t, root, body)

	res, err := backfillCursorHookUsage(ctx, database, []string{logsRoot}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.DuplicatesRemoved != 1 {
		t.Fatalf("duplicates removed = %d, want the proven squatter only", res.DuplicatesRemoved)
	}
	got := map[string]bool{}
	rows, err := database.QueryContext(ctx, `SELECT message_id FROM token_usage WHERE session_id = 'cc'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var m string
		_ = rows.Scan(&m)
		got[m] = true
	}
	rows.Close()
	if !got["gen-A"] || got["gen-B"] || !got["gen-C"] || !got["gen-D"] || len(got) != 3 {
		t.Fatalf("rows = %v, want gen-A, gen-C and gen-D (the squatter gone, both equal real requests kept)", got)
	}
	// Idempotent.
	res, err = backfillCursorHookUsage(ctx, database, []string{logsRoot}, 0)
	if err != nil || res.DuplicatesRemoved != 0 {
		t.Fatalf("re-run removed %d (err %v), want 0", res.DuplicatesRemoved, err)
	}
}
