package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// reexec.go implements "restart the daemon from the dashboard" (docs/plans/
// dashboard-daemon-restart-plan-2026-07-14.md). The dashboard restart hook sets
// restartRequested and cancels the root context; the daemon then runs its
// NORMAL graceful shutdown (the same path SIGTERM triggers — component drain,
// launchClose PTY reap, dbCleanup), and only after every defer has run does
// main() re-exec the process image in place (execSelf). No external supervisor
// or CLI relaunch is needed, and config written by `remote enable` binds on the
// restart.
//
// Codex 2026-07-14: never shut down into a brick — preflightRestart validates
// the config would come back up BEFORE the shutdown is triggered.

// restartRequested is set by the dashboard restart hook and read by main()
// after Execute() returns (i.e. after graceful shutdown + all defers). It is
// process-global by necessity: the set happens deep in the start lifecycle, the
// check happens in main.
var restartRequested atomic.Bool

// preflightRestart validates that a restart would actually succeed before the
// daemon tears itself down. The realistic brick cause is a config edited/saved
// to an invalid state (e.g. a bad value, or `remote enable` writing a listener
// that won't validate); re-Load + re-Validate catches it, and the restart is
// REFUSED (the daemon keeps running) rather than shutting down into a process
// that won't come back. A bind conflict on a port this process does not already
// hold is a documented residual (rare; the new process fails to bind and exits,
// the same as any restart into a taken port).
func preflightRestart(configPath string) error {
	cfg, err := config.Load(config.LoadOptions{GlobalPath: configPath})
	if err != nil {
		return fmt.Errorf("restart refused — the current config would not load (fix it first): %w", err)
	}
	if verr := config.Validate(cfg); verr != nil {
		return fmt.Errorf("restart refused — the current config is invalid (fix it first): %w", verr)
	}
	return nil
}

// restartHook builds the dashboard RestartFunc: preflight the config, mark
// restartRequested, then cancel the root context after the HTTP response has
// had time to flush. It returns nil on platforms without execve (native
// Windows) — the /api/admin/restart handler reports 501 ("relaunch the daemon
// manually") for a nil hook, which is the honest answer for a process mode
// that cannot self-restart, instead of a 500 from a hook that could only ever
// fail.
func restartHook(cancel context.CancelFunc, configPath string) func() error {
	if !reexecSupported() {
		return nil
	}
	return func() error {
		if perr := preflightRestart(configPath); perr != nil {
			return perr
		}
		restartRequested.Store(true)
		go func() {
			// Let the HTTP response flush before shutdown begins.
			time.Sleep(300 * time.Millisecond)
			cancel()
		}()
		return nil
	}
}
