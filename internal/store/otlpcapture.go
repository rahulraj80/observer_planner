package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// EnsureCaptureSession makes sure a session row exists for a turn or tool
// call that arrived from an AI tool's own OpenTelemetry export (T-475), and
// never changes a row that already exists.
//
// A native export knows the session id and the tool but rarely the project,
// while the tool's transcript adapter (when enabled) knows the project and
// writes the same session id. The two can arrive in either order, so this
// seam only INSERTS: an existing row keeps its project, model and start
// time, and when the transcript upserts the session later its project
// replaces projectRoot (UpsertSession's normal ON CONFLICT update). created
// reports whether a row was inserted.
//
// projectRoot is the capture's best answer: a cwd the export carried, else
// the bracketed per-tool placeholder ("[codex]") the other root-less capture
// paths already use (internal/adapter/cursor SyntheticProjectRoot).
func (s *Store) EnsureCaptureSession(ctx context.Context, id, tool, projectRoot, model string, startedAt time.Time) (created bool, err error) {
	if id == "" || tool == "" || projectRoot == "" {
		return false, errors.New("store.EnsureCaptureSession: id, tool and projectRoot are required")
	}
	var one int
	switch err := s.db.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, id).Scan(&one); {
	case err == nil:
		return false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return false, fmt.Errorf("store.EnsureCaptureSession: lookup: %w", err)
	}
	projectID, err := s.UpsertProject(ctx, projectRoot, "")
	if err != nil {
		return false, fmt.Errorf("store.EnsureCaptureSession: %w", err)
	}
	if err := s.UpsertSession(ctx, models.Session{
		ID: id, ProjectID: projectID, Tool: tool, Model: model, StartedAt: startedAt,
	}); err != nil {
		return false, fmt.Errorf("store.EnsureCaptureSession: %w", err)
	}
	return true, nil
}
