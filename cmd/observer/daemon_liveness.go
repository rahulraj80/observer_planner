package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/diag"
)

// daemon_liveness.go answers "is an `observer start` daemon running on this
// database" WITHOUT a listener (REVIEW-4 P4-09 / P4-19). A daemon built
// without ui.dashboard (or started with --no-dashboard) answers no dashboard
// port, so a port probe alone concludes "no daemon" while one is running. The
// lockfile `observer start` writes beside the database (internal/diag
// WriteLock) is present in every build, so it is the core liveness fact.

// liveDaemonFor returns the live daemon lock serving dbPath, if any. It skips
// this process's own lock, a lock that names a different database, and a
// lock whose PID no longer looks like an observer process (PID reuse,
// statuslinePIDLooksLikeObserver). An unreadable lock directory answers
// "none": a caller must never fail just because it could not prove a daemon.
func liveDaemonFor(dbPath string) (diag.LockInfo, bool) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return diag.LockInfo{}, false
	}
	locks, err := diag.LiveLocks(filepath.Dir(dbPath))
	if err != nil {
		return diag.LockInfo{}, false
	}
	self := os.Getpid()
	for _, l := range locks {
		if l.PID == self {
			continue
		}
		if l.DBPath != "" && filepath.Clean(l.DBPath) != filepath.Clean(dbPath) {
			continue
		}
		if !statuslinePIDLooksLikeObserver(l) {
			continue
		}
		return l, true
	}
	return diag.LockInfo{}, false
}
