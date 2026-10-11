package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestIngestNotifierFiresOnInsertedRows pins the P3 seam: an ingest that
// writes a token row tells the daemon (tokens=1), an ingest with no rows does
// not, and a nil notifier is never called. (A re-ingest of the same row is an
// upsert Ingest also counts; the extra kick costs at most an empty push
// cycle, which makes no network call.)
func TestIngestNotifierFiresOnInsertedRows(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "w.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s := store.New(database)

	watchRoot := t.TempDir()
	path := filepath.Join(watchRoot, "fresh.stub")
	if err := os.WriteFile(path, []byte("dummy"), 0o600); err != nil {
		t.Fatal(err)
	}
	stub := &stubRetryAdapter{
		name: "stub",
		root: watchRoot,
		res: adapter.ParseResult{
			NewOffset: 5,
			TokenEvents: []models.TokenEvent{{
				SourceFile: path, SourceEventID: "e1", SessionID: "s1", ProjectRoot: watchRoot,
				Timestamp: time.Now().UTC(), Tool: models.ToolClaudeCode, Model: "m",
				InputTokens: 10, OutputTokens: 20,
				Source: models.TokenSourceJSONL, Reliability: models.ReliabilityUnreliable,
			}},
		},
	}
	reg := adapter.NewRegistry()
	reg.Register(stub)
	w := New(s, reg, Options{
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Debounce: 50 * time.Millisecond,
	})
	type note struct{ tokens, actions int }
	var notes []note
	w.SetIngestNotifier(func(tokens, actions int) { notes = append(notes, note{tokens, actions}) })

	if err := w.ingestProcessResult(ctx, stub, stub.res); err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].tokens != 1 {
		t.Fatalf("notes after first ingest = %+v, want one with tokens=1", notes)
	}
	if err := w.ingestProcessResult(ctx, stub, adapter.ParseResult{NewOffset: 5}); err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("an ingest with no rows notified: %+v", notes)
	}
	w.SetIngestNotifier(nil)
	stub.res.TokenEvents[0].SourceEventID = "e2"
	if err := w.ingestProcessResult(ctx, stub, stub.res); err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("a nil notifier was called: %+v", notes)
	}
}
