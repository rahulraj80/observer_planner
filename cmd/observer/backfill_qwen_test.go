package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/qwencode"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestQwenCodeRescanCorrectsReasoningDoubleCount is the history repair for
// T-542: a qwen-code token row stored before the fix carried the turn's
// thoughts both inside output (84) and as reasoning (40). The upsert is
// MAX-monotone, so a plain rescan keeps 84; the --qwen-code-rescan
// correcting pass makes the stored row equal to the fixed parse (44 / 40)
// and a second run changes nothing.
func TestQwenCodeRescanCorrectsReasoningDoubleCount(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	projects := filepath.Join(root, ".qwen", "projects")
	path := filepath.Join(projects, "-home-dev-proj", "chats", "session.jsonl")
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "qwencode", "simple-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	qa := qwencode.NewWithOptions(nil, projects)
	parse := func(ctx context.Context, p string) (adapter.ParseResult, error) {
		return qa.ParseSessionFile(ctx, p, 0)
	}

	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "o.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	st := store.New(database)

	// The pre-fix row: output still holds the thoughts.
	res, err := parse(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	stale := append([]models.TokenEvent(nil), res.TokenEvents...)
	for i := range stale {
		stale[i].OutputTokens += stale[i].ReasoningTokens
	}
	if _, err := st.Ingest(ctx, res.ToolEvents, stale, store.IngestOptions{}); err != nil {
		t.Fatal(err)
	}
	// A plain re-ingest of the fixed parse cannot lower the count.
	if _, err := st.Ingest(ctx, res.ToolEvents, res.TokenEvents, store.IngestOptions{}); err != nil {
		t.Fatal(err)
	}
	readOut := func() (out, reasoning int64) {
		if err := database.QueryRow(`SELECT output_tokens, reasoning_tokens FROM token_usage WHERE tool = 'qwen-code'`).Scan(&out, &reasoning); err != nil {
			t.Fatal(err)
		}
		return out, reasoning
	}
	if out, _ := readOut(); out != 84 {
		t.Fatalf("setup: stored output = %d, want the stale 84 (MAX upsert keeps it)", out)
	}

	rep, err := runTokenCorrection(ctx, st, models.ToolQwenCode, parse)
	if err != nil {
		t.Fatal(err)
	}
	if rep.FilesChecked != 1 || rep.Updated != 1 || rep.Errors != 0 || rep.Deleted != 0 {
		t.Errorf("first run = %+v, want 1 file, 1 updated", rep)
	}
	if out, rs := readOut(); out != 44 || rs != 40 {
		t.Errorf("corrected output=%d reasoning=%d, want 44 / 40", out, rs)
	}
	again, err := runTokenCorrection(ctx, st, models.ToolQwenCode, parse)
	if err != nil {
		t.Fatal(err)
	}
	if again.Updated != 0 || again.Deleted != 0 {
		t.Errorf("second run = %+v, want no changes (idempotent)", again)
	}
}
