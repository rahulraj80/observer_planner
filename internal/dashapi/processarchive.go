package dashapi

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/archive"
)

// ProcessArchiveReader is the slice of cold storage the process surfaces need.
//
// Declared HERE, at the consumer, so this package depends on a two-method
// behaviour contract rather than on internal/archivestore - the dashboard never
// learns that cold storage is a second SQLite file, and a test can supply a
// fake without one.
type ProcessArchiveReader interface {
	// HasArchivedSessionProcesses is the cheap "is it worth looking?" probe.
	HasArchivedSessionProcesses(ctx context.Context, sessionID string) (bool, error)
	// ProcessRunsForSession streams the archived runs for one session.
	ProcessRunsForSession(ctx context.Context, sessionID string, batchRows int, sink archive.WindowSink) error
}
