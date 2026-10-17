// Package shutdown is the daemon's bounded-stop bookkeeping: which named
// components are still running, which stage the stop has reached, and a
// watchdog that reports (and lets the caller act) when the stop overruns its
// bound.
//
// It exists because of T-553 (2026-10-06): `observer start` daemons released
// their ports on SIGTERM and then kept running for minutes at 10 to 50% CPU
// with the DB open, one needed SIGKILL, and nothing said which of the forty
// background loops was still working. A stop must end within a bound, and an
// overrun must name what blocked it.
//
// Three pieces, all pure:
//
//   - Tracker counts running components by name and records the current stop
//     stage ("components", "checkpoint", "closing"). cmd/observer wraps every
//     daemon goroutine through it, so the watchdog can name the stragglers.
//   - Watchdog arms once (when the stop begins), calls a progress callback
//     part-way through the bound and an overrun callback at the bound, each
//     with a Report of what is still running. What an overrun DOES (log, dump
//     goroutine stacks, exit) belongs to the caller: os.Exit stays in main.go.
//   - Report is the plain value both callbacks receive.
//
// Purity: no database/sql, no net/http, no os/exec, no fsnotify, no os
// (pinned by imports_test.go). This package counts and times; it does not know
// what a watcher, a proxy or a database is.
package shutdown
