//go:build !sbo_omit_codeintel

package main

// prune_codeintel_test.go holds the runRetention wiring proofs that read the
// codeintel store methods, which internal/store/codeintel.go compiles out
// under sbo_omit_codeintel (REVIEW-4 P4-08: a test build line matches the
// code it drives). Moved from prune_test.go unchanged.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestRunRetentionPrunesCodeIntelProjects is the WIRING proof for the
// Ticket-B codeintel retention sweep (docs/plans/claude-code-hook-stall-
// ticket-and-db-prune-plan-2026-07-12.md): it exercises the runRetention
// orchestration path (not just store.CodeIntelPruneStaleProjects) and
// asserts a project whose last index pass is older than the default
// [codeintel].retention_days is deleted while a freshly indexed one
// survives, guarding against the historical "prune func shipped but
// never wired" regression.
func TestRunRetentionPrunesCodeIntelProjects(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "prune-codeintel.db")
	database, err := dbtemplate.Open(ctx, db.Options{Path: path})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	now := time.Now().Unix()
	const day = int64(24 * 60 * 60)
	seed := func(project string, indexedAt int64) {
		t.Helper()
		if _, err := database.ExecContext(ctx,
			`INSERT INTO codeintel_files(project, path, lang, status, indexed_at)
			 VALUES(?, ?, 'go', 'indexed', ?)`,
			project, project+"/a.go", indexedAt); err != nil {
			t.Fatalf("seed %s: %v", project, err)
		}
	}
	seed("/repo/stale", now-200*day)
	seed("/repo/fresh", now-5*day)

	cfg := config.Default()
	cfg.Observer.DBPath = path
	if cfg.CodeIntel.RetentionDays != 90 {
		t.Fatalf("default codeintel retention = %d, want 90", cfg.CodeIntel.RetentionDays)
	}

	res, err := runRetention(ctx, cfg, database)
	if err != nil {
		t.Fatalf("runRetention: %v", err)
	}
	if res.CodeIntelProjectsDeleted != 1 {
		t.Errorf("CodeIntelProjectsDeleted = %d, want 1", res.CodeIntelProjectsDeleted)
	}

	remaining, err := store.New(database).CodeIntelListProjects(ctx)
	if err != nil {
		t.Fatalf("CodeIntelListProjects: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != "/repo/fresh" {
		t.Fatalf("after prune remaining projects = %v, want [/repo/fresh]", remaining)
	}

	// Second run within the same horizon is a clean no-op (idempotent).
	res2, err := runRetention(ctx, cfg, database)
	if err != nil {
		t.Fatalf("runRetention(2): %v", err)
	}
	if res2.CodeIntelProjectsDeleted != 0 {
		t.Errorf("second run CodeIntelProjectsDeleted = %d, want 0", res2.CodeIntelProjectsDeleted)
	}
}

// TestRunRetentionArchivesCodeIntelProjectsWhenEnabled is the WIRING proof for
// the [archive] sweep: the SAME staleness horizon that deletes today must
// instead MOVE the project to ~/.observer/archive.db when the block is on.
//
// It asserts both halves of that sentence, because either alone is a bug: the
// hot rows are gone AND the project is recorded on the marker, so the operator
// can tell "archived" from "never indexed", and the delete counter stays at
// zero, so the two fates never get conflated in the report.
func TestRunRetentionArchivesCodeIntelProjectsWhenEnabled(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "prune-archive.db")
	database, err := dbtemplate.Open(ctx, db.Options{Path: path})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	now := time.Now().Unix()
	const day = int64(24 * 60 * 60)
	seedFile := func(project string, indexedAt int64) {
		t.Helper()
		if _, err := database.ExecContext(ctx,
			`INSERT INTO codeintel_files(project, path, lang, status, indexed_at)
			 VALUES(?, ?, 'go', 'indexed', ?)`,
			project, project+"/a.go", indexedAt); err != nil {
			t.Fatalf("seed %s: %v", project, err)
		}
	}
	seedFile("/repo/stale", now-200*day)
	seedFile("/repo/fresh", now-5*day)

	cfg := config.Default()
	cfg.Observer.DBPath = path
	cfg.Archive.Enabled = true
	cfg.Archive.Path = filepath.Join(dir, "archive.db")

	res, err := runRetention(ctx, cfg, database)
	if err != nil {
		t.Fatalf("runRetention: %v", err)
	}
	if res.CodeIntelProjectsArchived != 1 {
		t.Errorf("CodeIntelProjectsArchived = %d, want 1", res.CodeIntelProjectsArchived)
	}
	if res.CodeIntelProjectsDeleted != 0 {
		t.Errorf("CodeIntelProjectsDeleted = %d - the archive sweep must not also report deletions",
			res.CodeIntelProjectsDeleted)
	}
	if res.CodeIntelRowsArchived <= 0 {
		t.Error("CodeIntelRowsArchived was not reported")
	}

	s := store.New(database)
	remaining, err := s.CodeIntelListProjects(ctx)
	if err != nil {
		t.Fatalf("CodeIntelListProjects: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != "/repo/fresh" {
		t.Fatalf("after archive remaining projects = %v, want [/repo/fresh]", remaining)
	}
	marker, found, err := s.CodeIntelArchivedProject(ctx, "/repo/stale")
	if err != nil {
		t.Fatalf("marker: %v", err)
	}
	if !found {
		t.Fatal("no marker for the archived project - it is now indistinguishable from never-indexed")
	}
	if marker.RowsArchived != res.CodeIntelRowsArchived {
		t.Errorf("marker rows %d != reported rows %d", marker.RowsArchived, res.CodeIntelRowsArchived)
	}
	if _, err := os.Stat(cfg.Archive.Path); err != nil {
		t.Fatalf("archive database was not created at %s: %v", cfg.Archive.Path, err)
	}

	// Second pass is a clean no-op: the project is no longer stale-eligible
	// because its rows are gone from codeintel_files entirely.
	res2, err := runRetention(ctx, cfg, database)
	if err != nil {
		t.Fatalf("runRetention(2): %v", err)
	}
	if res2.CodeIntelProjectsArchived != 0 {
		t.Errorf("second pass archived %d projects, want 0", res2.CodeIntelProjectsArchived)
	}
}

// TestRunRetentionArchiveSweepFailsOpenToSkip pins the one-directional
// fail-open: when the archive database cannot be opened, the codeintel sweep
// is SKIPPED, never downgraded to the delete path. An operator who asked for
// archival and got deletion because a file was momentarily unavailable would
// have lost data to the option meant to stop losing it.
func TestRunRetentionArchiveSweepFailsOpenToSkip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "prune-archive-fail.db")
	database, err := dbtemplate.Open(ctx, db.Options{Path: path})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	now := time.Now().Unix()
	if _, err := database.ExecContext(ctx,
		`INSERT INTO codeintel_files(project, path, lang, status, indexed_at)
		 VALUES('/repo/stale', '/repo/stale/a.go', 'go', 'indexed', ?)`,
		now-200*24*60*60); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A directory where the archive file should be: Open must fail.
	badPath := filepath.Join(dir, "archive.db")
	if err := os.MkdirAll(badPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfg := config.Default()
	cfg.Observer.DBPath = path
	cfg.Archive.Enabled = true
	cfg.Archive.Path = badPath

	res, err := runRetention(ctx, cfg, database)
	if err != nil {
		t.Fatalf("runRetention must fail open, got error: %v", err)
	}
	if res.CodeIntelProjectsArchived != 0 || res.CodeIntelProjectsDeleted != 0 {
		t.Fatalf("archived/deleted = %d/%d, want 0/0 - the sweep must be skipped, not downgraded to a delete",
			res.CodeIntelProjectsArchived, res.CodeIntelProjectsDeleted)
	}
	remaining, err := store.New(database).CodeIntelListProjects(ctx)
	if err != nil {
		t.Fatalf("CodeIntelListProjects: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("the stale project was removed despite the skipped sweep: %v", remaining)
	}
}
