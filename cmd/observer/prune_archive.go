//go:build !sbo_omit_archive

package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/marmutapp/superbased-observer/internal/archive"
	"github.com/marmutapp/superbased-observer/internal/archivestore"
	"github.com/marmutapp/superbased-observer/internal/archivesvc"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// prune_archive.go is the retention pass's archive half, split out of
// prune.go so a build without the archive component (-tags
// sbo_omit_archive, lane MP-L1H, T-715) links neither archivestore nor
// archivesvc; prune_archive_omit.go is its pair.

// runCodeIntelArchiveSweep moves one bounded batch of stale code-intelligence
// projects into ~/.observer/archive.db.
//
// It opens the archive database for the duration of the sweep and closes it
// again: the archive is touched once per retention pass, so holding a second
// connection pool open for the daemon's whole lifetime would cost idle
// connections (each a candidate temp-file holder) for no benefit.
//
// FAIL-OPEN, and specifically fail-open in ONE direction: if the archive
// database cannot be opened, the codeintel sweep is SKIPPED for this pass -
// it never falls back to the delete path. An operator who asked for archival
// and got deletion because a file was momentarily unavailable would have lost
// data to a config option whose entire purpose was to stop losing data. The
// projects stay hot and are re-offered next pass.
func runCodeIntelArchiveSweep(ctx context.Context, cfg config.Config) (projects int, rows int64, err error) {
	path := cfg.Archive.Path
	if path == "" {
		return 0, 0, nil
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		slog.Warn("archive sweep skipped: archive directory unavailable",
			"path", path, "error", mkErr)
		return 0, 0, nil
	}
	cold, openErr := archivestore.Open(ctx, archivestore.Options{Path: path})
	if openErr != nil {
		slog.Warn("archive sweep skipped: archive database unavailable (projects stay hot)",
			"path", path, "error", openErr)
		return 0, 0, nil
	}
	defer func() { _ = cold.Close() }()

	// A SEPARATE hot handle, not the caller's: an archive move streams a
	// whole project through the pool, and borrowing the shared handle for
	// that would put a long cold-path read on the same connections the
	// daemon's hot work uses (design §1.2's explicit rejection).
	hotDB, dbErr := db.Open(ctx, db.Options{Path: cfg.Observer.DBPath})
	if dbErr != nil {
		slog.Warn("archive sweep skipped: could not open a dedicated hot handle",
			"error", dbErr)
		return 0, 0, nil
	}
	defer func() { _ = hotDB.Close() }()

	mover := &archivesvc.Mover{
		Hot:       store.New(hotDB),
		Cold:      cold,
		BatchRows: cfg.Archive.BatchRows,
	}
	res, sweepErr := mover.SweepCodeIntel(ctx, cfg.CodeIntel.RetentionDays,
		archive.PlanOptions{MaxUnitsPerPass: cfg.Archive.MaxProjectsPerPass})
	if res.Considered > res.Attempted {
		slog.Info("archive sweep capped for this pass",
			"considered", res.Considered, "attempted", res.Attempted,
			"max_projects_per_pass", cfg.Archive.MaxProjectsPerPass)
	}
	return res.Archived, res.RowsArchived, sweepErr
}

// runProcessArchiveSweep moves one bounded batch of stale process-capture
// day-windows into ~/.observer/archive.db, then applies the archive file's own
// (later) expiry horizon.
//
// Same open-for-the-pass, separate-hot-handle, FAIL-OPEN-IN-ONE-DIRECTION
// posture as runCodeIntelArchiveSweep - and the fail-open direction matters
// more here than anywhere else in the arc. If the archive database cannot be
// opened, the sweep is SKIPPED and NOTHING is deleted; it never falls back to
// the hot prune. An operator who turned archival on and got their only copy of
// a process trail deleted because a file was momentarily unavailable would have
// lost unrecoverable data to a setting whose entire purpose was to stop losing
// it. The windows stay hot and are re-offered next pass.
func runProcessArchiveSweep(ctx context.Context, cfg config.Config) (archivesvc.ProcessSweepResult, error) {
	var zero archivesvc.ProcessSweepResult
	path := cfg.Archive.Path
	if path == "" {
		return zero, nil
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		slog.Warn("process archive sweep skipped: archive directory unavailable",
			"path", path, "error", mkErr)
		return zero, nil
	}
	cold, openErr := archivestore.Open(ctx, archivestore.Options{Path: path})
	if openErr != nil {
		slog.Warn("process archive sweep skipped: archive database unavailable (windows stay hot)",
			"path", path, "error", openErr)
		return zero, nil
	}
	defer func() { _ = cold.Close() }()

	hotDB, dbErr := db.Open(ctx, db.Options{Path: cfg.Observer.DBPath})
	if dbErr != nil {
		slog.Warn("process archive sweep skipped: could not open a dedicated hot handle",
			"error", dbErr)
		return zero, nil
	}
	defer func() { _ = hotDB.Close() }()

	mover := &archivesvc.ProcessMover{
		Hot:       store.New(hotDB),
		Cold:      cold,
		BatchRows: cfg.Archive.BatchRows,
	}
	res, sweepErr := mover.SweepProcess(ctx, cfg.Observer.Process.ArchiveDays, cfg.Archive.MaxProjectsPerPass)

	// The cold expiry runs even when the move half reported errors: a window
	// that failed to archive is unrelated to windows whose cold copy is now
	// past the delete horizon, and letting one block the other would grow the
	// archive without bound.
	expired, expErr := mover.ExpireProcessWindows(ctx, cfg.Observer.Process.RetentionDays, cfg.Archive.MaxProjectsPerPass)
	res.Expired = expired
	if res.Considered > res.Archived+res.Skipped+res.Failed {
		slog.Info("process archive sweep capped for this pass", "considered", res.Considered)
	}
	return res, errors.Join(sweepErr, expErr)
}
