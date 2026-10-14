//go:build !sbo_omit_codeintel && !sbo_omit_archive

package main

// prune_codeintel_archive_test.go holds the runRetention wiring proof for the
// [archive] codeintel sweep. It drives internal/archivesvc through
// prune_archive.go, which a `-tags sbo_omit_archive` build replaces with
// archive_omit.go (the sweep moves nothing there by design), so its build line
// also excludes sbo_omit_archive. Moved from prune_codeintel_test.go
// unchanged; prune_codeintel_archive_omit_test.go pins the omitted build's
// side of the same configuration.

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
