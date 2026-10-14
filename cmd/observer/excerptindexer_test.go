package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestHookCaptureStoreObeysIndexingSwitch pins T-420: the hook capture store
// writes action_excerpts exactly when [compression.indexing].enabled says so,
// the same switch the watcher obeys. One row per switch state.
func TestHookCaptureStoreObeysIndexingSwitch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		enabled  bool
		excerpts int
	}{
		{name: "off writes no excerpt rows", enabled: false, excerpts: 0},
		{name: "on indexes the tool output", enabled: true, excerpts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "observer.db")})
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			st := hookCaptureStore(database, config.IndexingConfig{Enabled: tc.enabled, MaxExcerptBytes: 2048})
			// The cursor stop path's call shape: Ingest with empty options, so
			// the store-bound indexer is the only one in play.
			events := []models.ToolEvent{{
				SourceFile: "/t/t420.jsonl", SourceEventID: "toolu_t420",
				SessionID: "s_t420", ProjectRoot: "/tmp/p_t420", Timestamp: time.Now().UTC(),
				Tool: models.ToolCursor, ActionType: models.ActionRunCommand,
				Target: "rg zathras", RawToolName: "Shell", Success: true,
				ToolOutput: "matched zathras in cmd/observer/main.go",
			}}
			if _, err := st.Ingest(ctx, events, nil, store.IngestOptions{}); err != nil {
				t.Fatal(err)
			}
			var actions, excerpts int
			if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM actions`).Scan(&actions); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM action_excerpts`).Scan(&excerpts); err != nil {
				t.Fatal(err)
			}
			if actions != 1 {
				t.Fatalf("actions = %d, want 1 (capture must not depend on the switch)", actions)
			}
			if excerpts != tc.excerpts {
				t.Fatalf("action_excerpts = %d, want %d with indexing.enabled=%v", excerpts, tc.excerpts, tc.enabled)
			}
		})
	}
}
