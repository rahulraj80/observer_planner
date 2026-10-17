package main

import (
	"context"
	"log/slog"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// taskBackfillFunc is Store.BackfillTaskItems' shape, injected so the
// start-up wiring is testable without a store.
type taskBackfillFunc func(ctx context.Context, limit int) (store.TaskBackfillResult, error)

// runTaskBackfillOnStart is the [tasks].backfill_on_start pass (FIX-4). It
// runs on the daemon's context (T-557): a stop cancels it between pages
// instead of leaving it scanning actions with the DB open. The backfill is
// idempotent, so the next start covers the same ground again.
func runTaskBackfillOnStart(ctx context.Context, backfill taskBackfillFunc, logger *slog.Logger) {
	res, err := backfill(ctx, 0)
	if err != nil && ctx.Err() != nil {
		logger.Info("tasks.backfill_on_start stopped by shutdown",
			"actions_scanned", res.ActionsScanned, "transitions_written", res.TransitionsWritten)
		return
	}
	if err != nil {
		logger.Warn("tasks.backfill_on_start failed", "error", err)
		return
	}
	logger.Info("tasks.backfill_on_start complete",
		"actions_scanned", res.ActionsScanned, "transitions_written", res.TransitionsWritten)
}
