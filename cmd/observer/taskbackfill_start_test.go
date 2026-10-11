package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestRunTaskBackfillOnStart_StopsWithTheDaemon pins T-557: the start-up
// task backfill runs on the daemon's context, so a shutdown cancels it
// instead of leaving it scanning the actions table with the DB open, and the
// stop is logged as a stop, not a failure.
func TestRunTaskBackfillOnStart_StopsWithTheDaemon(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	started := make(chan struct{})
	backfill := func(bctx context.Context, _ int) (store.TaskBackfillResult, error) {
		close(started)
		select {
		case <-bctx.Done():
			return store.TaskBackfillResult{ActionsScanned: 7}, bctx.Err()
		case <-time.After(5 * time.Second):
			return store.TaskBackfillResult{}, nil
		}
	}
	done := make(chan struct{})
	go func() {
		runTaskBackfillOnStart(ctx, backfill, logger)
		close(done)
	}()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("task backfill kept running after the daemon context was cancelled")
	}
	out := buf.String()
	if strings.Contains(out, "level=WARN") || !strings.Contains(out, "tasks.backfill_on_start stopped by shutdown") {
		t.Fatalf("log = %q, want an INFO stop line and no WARN", out)
	}
}
