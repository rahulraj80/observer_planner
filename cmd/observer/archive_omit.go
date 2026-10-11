//go:build sbo_omit_archive

package main

import (
	"context"
	"log/slog"
	"sync"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
)

// archive_omit.go stands in for archive_cmd.go, archive_cleanup.go and
// prune_archive.go in a build without the archive component (-tags
// sbo_omit_archive, lane MP-L1H, T-715): internal/archivestore and
// internal/archivesvc are not linked, so this binary never opens
// ~/.observer/archive.db. `observer archive` answers "not in this build"
// (exit 3). The pure row, marker and digest vocabulary (internal/archive)
// stays core because the store's hot-side seam speaks it.
//
// With [archive].enabled set, the retention pass still takes the archive
// branch and SKIPS it (nothing moved, nothing deleted), the same fail-open
// direction as an archive file that cannot be opened: archival was asked
// for, so the delete path is never substituted.
func init() { components.MarkOmitted("archive") }

// newArchiveCmd registers `observer archive` as a compiled-out verb.
func newArchiveCmd() *cobra.Command {
	return compiledOutVerb("archive", "Corpus archive (not in this build)", "archive")
}

// openProcessArchiveReader has no cold store to open.
func openProcessArchiveReader(context.Context, config.Config) (dashapi.ProcessArchiveReader, func()) {
	return nil, func() {}
}

// processArchiveSweepResult carries the fields the retention pass reads.
type processArchiveSweepResult struct {
	Archived     int
	RowsArchived int64
	Expired      int
}

var archiveSkipWarn sync.Once

func warnArchiveNotInBuild() {
	archiveSkipWarn.Do(func() {
		slog.Warn("archive sweep skipped: the archive component is not in this build (rows stay hot)")
	})
}

// runCodeIntelArchiveSweep moves nothing.
func runCodeIntelArchiveSweep(context.Context, config.Config) (int, int64, error) {
	warnArchiveNotInBuild()
	return 0, 0, nil
}

// runProcessArchiveSweep moves nothing.
func runProcessArchiveSweep(context.Context, config.Config) (processArchiveSweepResult, error) {
	warnArchiveNotInBuild()
	return processArchiveSweepResult{}, nil
}
