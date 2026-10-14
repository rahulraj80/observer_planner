//go:build !sbo_omit_codeintel && sbo_omit_archive

package main

// prune_codeintel_archive_omit_test.go is the omitted build's side of
// prune_codeintel_archive_test.go: with the archive component compiled out
// (-tags sbo_omit_archive), archive_omit.go keeps the retention pass on the
// archive branch and SKIPS it.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestRunRetentionArchiveNotInBuildSkipsNeverDeletes pins archive_omit.go's
// contract on the same configuration the in-build wiring proof uses:
// [archive].enabled set with a usable path. Archival was asked for, so the
// stale project must neither move (nothing can) nor be deleted (the delete
// path is never substituted), and no archive database is created.
func TestRunRetentionArchiveNotInBuildSkipsNeverDeletes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "prune-archive-omit.db")
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

	cfg := config.Default()
	cfg.Observer.DBPath = path
	cfg.Archive.Enabled = true
	cfg.Archive.Path = filepath.Join(dir, "archive.db")

	res, err := runRetention(ctx, cfg, database)
	if err != nil {
		t.Fatalf("runRetention: %v", err)
	}
	if res.CodeIntelProjectsArchived != 0 || res.CodeIntelProjectsDeleted != 0 || res.CodeIntelRowsArchived != 0 {
		t.Fatalf("archived/deleted/rows = %d/%d/%d, want 0/0/0 - without the archive component the sweep is skipped, never downgraded to a delete",
			res.CodeIntelProjectsArchived, res.CodeIntelProjectsDeleted, res.CodeIntelRowsArchived)
	}
	remaining, err := store.New(database).CodeIntelListProjects(ctx)
	if err != nil {
		t.Fatalf("CodeIntelListProjects: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != "/repo/stale" {
		t.Fatalf("remaining projects = %v, want [/repo/stale] (rows stay hot)", remaining)
	}
	if _, err := os.Stat(cfg.Archive.Path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a build without the archive component created %s (stat err = %v)", cfg.Archive.Path, err)
	}
}
