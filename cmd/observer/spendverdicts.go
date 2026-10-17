package main

import (
	"context"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/spendverdict"
)

// spendVerdictInterval is how often the daemon re-derives the sessions whose
// rows changed. A windowed read (the cost engine, the guard's budget windows,
// the predictor's prior) refreshes before reading anyway; the ticker keeps
// the queue short so that refresh is normally one empty probe, and it works
// off the one-time backfill after an upgrade in the background.
const spendVerdictInterval = 5 * time.Second

// spendVerdictBackoff is the wait before each retry of a refresh that hit
// lock contention (SQLITE_BUSY / SQLITE_LOCKED, U6 lane FIX-UPGRADE): on a
// first start after an upgrade the backfill and the watcher's catch-up
// contend for the write lock, and that is not a failure worth a WARN.
var spendVerdictBackoff = []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}

// refreshRetryingTransient runs refresh, retrying a transient lock error
// after each backoff step. It returns the last result and whether the last
// error was transient (the caller logs that quietly: the next tick retries).
func refreshRetryingTransient(ctx context.Context, refresh func() (int, error), backoff []time.Duration, isTransient func(error) bool) (int, bool, error) {
	n, err := refresh()
	for _, wait := range backoff {
		if err == nil || !isTransient(err) || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			return n, true, err
		case <-time.After(wait):
		}
		n, err = refresh()
	}
	return n, err != nil && isTransient(err), err
}

// spendVerdictLoop keeps the node's stored sessionmsg dedup verdicts (agent
// migration 143, internal/spendverdict) current for the daemon's lifetime.
// P1 fail-soft like every sibling loop: a failed pass is logged and retried
// on the next tick, never cancels the daemon.
func spendVerdictLoop(ctx context.Context, configPath string) {
	cfg, database, cleanup, err := loadConfigAndDB(ctx, configPath)
	if err != nil {
		return
	}
	defer cleanup()
	logger := newLogger(cfg.Observer.LogLevel)
	pass := func() {
		start := time.Now()
		n, transient, err := refreshRetryingTransient(ctx, func() (int, error) {
			return spendverdict.Refresh(ctx, database, spendverdict.Options{MaxDuration: 30 * time.Second})
		}, spendVerdictBackoff, db.IsTransientWriteError)
		if err != nil && ctx.Err() == nil {
			if transient {
				logger.Debug("spend verdicts: database busy, retrying on the next tick", "err", err)
				return
			}
			logger.Warn("spend verdicts: refresh failed", "err", err)
			return
		}
		if n > 100 {
			logger.Info("spend verdicts: re-derived sessions", "sessions", n, "took", time.Since(start).Round(time.Millisecond))
		}
	}
	pass()
	ticker := time.NewTicker(spendVerdictInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pass()
		}
	}
}
