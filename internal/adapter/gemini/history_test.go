package gemini_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/adapter/gemini"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestRestatedTokenEventsNeverDuplicatedOnDisk is the T-543 history check:
// even the pre-fix parse (each restated call emitted twice under one
// source_event_id) stored ONE token_usage row per call, because the store
// upserts on (source_file, source_event_id). So the duplicates lived only in
// the parse result and no stored history needs a repair pass.
func TestRestatedTokenEventsNeverDuplicatedOnDisk(t *testing.T) {
	ctx := context.Background()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "gemini", "session-live-toolcalls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), ".gemini", "tmp", "h", "chats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "session-live-toolcalls.jsonl")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := gemini.New().ParseSessionFile(ctx, path, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Rebuild the pre-fix shape: every call restated (emitted twice).
	var restated []models.TokenEvent
	for _, e := range res.TokenEvents {
		restated = append(restated, e, e)
	}
	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "o.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := store.New(database).Ingest(ctx, res.ToolEvents, restated, store.IngestOptions{}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM token_usage WHERE tool = ?`, models.ToolGeminiCLI).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != len(res.TokenEvents) {
		t.Fatalf("stored rows = %d, want %d (one per call)", n, len(res.TokenEvents))
	}
}
