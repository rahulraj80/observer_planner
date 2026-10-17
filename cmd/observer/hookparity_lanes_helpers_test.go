package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// Shared fixtures for the hook-parity Lane B/E/F receiver tests
// (2026-10-03): a stdin/stdout harness and a throwaway config + database.

// captureStdio runs fn with os.Stdin fed from body and returns what it wrote
// to stdout (stderr is discarded into a temp file).
func captureStdio(t *testing.T, body string, fn func()) string {
	t.Helper()
	dir := t.TempDir()
	inPath := filepath.Join(dir, "stdin")
	if err := os.WriteFile(inPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(inPath) //nolint:gosec // G304: a path this test just created under t.TempDir().
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	outPath := filepath.Join(dir, "out")
	out, err := os.Create(outPath) //nolint:gosec // G304: test temp dir.
	if err != nil {
		t.Fatal(err)
	}
	errf, err := os.Create(filepath.Join(dir, "err"))
	if err != nil {
		t.Fatal(err)
	}
	defer errf.Close()
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = in, out, errf
	resetHookInvocationState()
	func() {
		defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()
		fn()
	}()
	_ = out.Close()
	b, err := os.ReadFile(outPath) //nolint:gosec // G304: test temp dir.
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// observeTestEnv is a throwaway config + database with one seeded session
// per tool, so the compaction recorder can resolve the project.
type observeTestEnv struct {
	cfgPath string
	dbPath  string
	root    string
}

func newObserveTestEnv(t *testing.T, sessions map[string]string) observeTestEnv {
	t.Helper()
	ctx := context.Background()
	home := t.TempDir()
	env := observeTestEnv{
		cfgPath: filepath.Join(home, "config.toml"),
		dbPath:  filepath.Join(home, "observer.db"),
		root:    filepath.Join(home, "repo"),
	}
	cfg := "[observer]\ndb_path = \"" + filepath.ToSlash(env.dbPath) + "\"\n\n[guard]\nenabled = false\n"
	if err := os.WriteFile(env.cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := dbtemplate.Open(ctx, db.Options{Path: env.dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var evs []models.ToolEvent
	for sid, tool := range sessions {
		evs = append(evs, models.ToolEvent{
			SourceFile: tool + ":seed", SourceEventID: "seed-" + sid, SessionID: sid,
			ProjectRoot: env.root, Timestamp: time.Now().UTC(), Tool: tool,
			ActionType: models.ActionUserPrompt, Target: "seed", Success: true,
		})
	}
	if _, err := store.New(database).Ingest(ctx, evs, nil, store.IngestOptions{}); err != nil {
		t.Fatal(err)
	}
	return env
}

func (e observeTestEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	d, err := dbtemplate.Open(context.Background(), db.Options{Path: e.dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var n int
	if err := d.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
