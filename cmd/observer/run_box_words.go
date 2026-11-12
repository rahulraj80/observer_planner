//go:build !sbo_omit_terminals

package main

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/marmutapp/superbased-observer/internal/attachsock"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// run_box_words.go: what the node said it made for a run's box (demo
// screenshots 2026-11-12: a template asked a SandHold node for Shielded with
// a Limited network and got exactly that, yet `observer launch --json`,
// `observer terminals show`, the Terminals grid tile and the Sandboxes tab's
// recent runs all said process / Shared and internet / Open). A run on such
// a backend records Observer's own class for the row (its floor, the lowest
// isolation the node offers) and the launch's network tier, because the box
// is made after the run is recorded, by the run's helper process. The helper
// leaves the node's answer as a note; every surface that names a run's
// isolation reads it through runBoxWords and shows the node's words, or says
// the node has not answered yet, never the floor as if it were the box.

// boxWordsSources read what a run's node said it made: ok=false when the
// run's backend reports no such words (every local row). A driver whose
// node decides the box's isolation registers one.
var boxWordsSources []func(observerDir, runID, backend string, running bool) (dashapi.BoxWords, bool)

// boxClassBackfills record, at a daemon start, the class each node said it
// made on the stored runs that still carry the row's floor (a driver whose
// node decides the box's isolation registers one; the count is the rows
// changed).
var boxClassBackfills []func(ctx context.Context, st *store.Store, observerDir string) (int, error)

// backfillBoxClasses runs every registered backfill once (best effort: a
// failure is logged, the rows keep what they had).
func backfillBoxClasses(ctx context.Context, database *sql.DB, observerDir string, logger *slog.Logger) {
	if database == nil || observerDir == "" {
		return
	}
	st := store.New(database)
	for _, f := range boxClassBackfills {
		n, err := f(ctx, st, observerDir)
		if err != nil {
			logger.Warn("sandbox: recording the isolation nodes gave earlier runs failed", "err", err)
			continue
		}
		if n > 0 {
			logger.Info("sandbox: recorded the isolation the node gave earlier runs", "runs", n)
		}
	}
}

// runBoxWords is the first registered source's answer for a run.
func runBoxWords(observerDir, runID, backend string, running bool) (dashapi.BoxWords, bool) {
	if observerDir == "" || runID == "" || backend == "" {
		return dashapi.BoxWords{}, false
	}
	for _, src := range boxWordsSources {
		if w, ok := src(observerDir, runID, backend, running); ok {
			return w, true
		}
	}
	return dashapi.BoxWords{}, false
}

// applyBoxWords puts a node's words on a run as the control ops report it:
// the node's tier word as the isolation, with its network and target, or
// no isolation at all while the node has not said (never the row's floor).
func applyBoxWords(ri *attachsock.RunInfo, w dashapi.BoxWords) {
	ri.Isolation, ri.Network, ri.RunsOn = w.Isolation, w.Network, w.RunsOn
	ri.IsolationPending = !w.Known() && w.Pending
}

// sandboxBoxWordsSeam is dashapi.Options.SandboxBoxWords for a daemon whose
// state lives in observerDir.
func sandboxBoxWordsSeam(observerDir string) func(runID, backend string, running bool) (dashapi.BoxWords, bool) {
	return func(runID, backend string, running bool) (dashapi.BoxWords, bool) {
		return runBoxWords(observerDir, runID, backend, running)
	}
}
