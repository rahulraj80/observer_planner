package cursor

import (
	"context"
	"database/sql"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestParseStateDBFile_CloudAgentKeyOnly: a state.vscdb holding only a
// `bcCachedDetails:<bc-id>` row (Cursor 3.23's Cloud Agent store) yields
// exactly one KEY-ONLY session_start under the placeholder. The value is
// protobuf bytes the reader must never decode; the fixture's bytes are
// arbitrary and carry no content.
func TestParseStateDBFile_CloudAgentKeyOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := newTestStateDB(t)
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cursorDiskKV VALUES(?, ?)`, cloudAgentKeyPrefix+testCloudID,
		[]byte{0x12, 0x02, 0x0a, 0x00, 0x1a, 0xff, 0xfe}); err != nil {
		t.Fatal(err)
	}
	// A key that is not a Cloud Agent id is ignored.
	if _, err := db.Exec(`INSERT INTO cursorDiskKV VALUES(?, ?)`, cloudAgentKeyPrefix+testLocalID, []byte{0x12}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	a := New()
	res, err := a.parseStateDBFile(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolEvents) != 1 {
		t.Fatalf("got %d events, want 1 key-only session_start: %+v", len(res.ToolEvents), res.ToolEvents)
	}
	ev := res.ToolEvents[0]
	if ev.SessionID != testCloudID || ev.ActionType != models.ActionSessionStart || ev.ProjectRoot != SyntheticProjectRoot ||
		ev.SourceEventID != "cursor-statedb-cloud-agent:"+testCloudID || ev.RawToolInput != "" || ev.ToolOutput != "" ||
		ev.Model != "" || ev.PrecedingReasoning != "" {
		t.Fatalf("cloud agent row = %+v", ev)
	}
	if res.NewOffset <= 0 {
		t.Fatalf("NewOffset = %d: the bcCachedDetails family must advance the watermark", res.NewOffset)
	}
	// The same key rewritten (REPLACE bumps its rowid) re-emits the same
	// identity, which the store's UNIQUE upsert ignores.
	db, err = sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cursorDiskKV VALUES(?, ?)`, cloudAgentKeyPrefix+testCloudID, []byte{0x12, 0x02}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	res2, err := a.parseStateDBFile(context.Background(), path, res.NewOffset)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.ToolEvents) != 1 || res2.ToolEvents[0].SourceEventID != ev.SourceEventID {
		t.Fatalf("rewrite re-emit = %+v, want the same identity", res2.ToolEvents)
	}
	// Dedup with the hook path by session id: a conversation the hook
	// already captured is skipped.
	a.hookCheck = func(_ context.Context, sid string) (bool, error) { return sid == testCloudID, nil }
	res3, err := a.parseStateDBFile(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res3.ToolEvents) != 0 {
		t.Fatalf("hook-captured cloud agent re-emitted: %+v", res3.ToolEvents)
	}
}

// TestParseStateDBFile_CloudAgentComposerWins: when the same delta holds the
// conversation's composerData row, that richer row registers it and the
// key-only row is not added on top.
func TestParseStateDBFile_CloudAgentComposerWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := newTestStateDB(t)
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cursorDiskKV VALUES(?, ?)`, "composerData:"+testCloudID, []byte(`{"name":"Cloud agent"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cursorDiskKV VALUES(?, ?)`, cloudAgentKeyPrefix+testCloudID, []byte{0x12}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	res, err := New().parseStateDBFile(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolEvents) != 1 || res.ToolEvents[0].RawToolName != "state.vscdb:composerData" {
		t.Fatalf("events = %+v, want only the composerData session_start", res.ToolEvents)
	}
}
