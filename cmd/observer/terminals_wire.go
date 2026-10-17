//go:build !sbo_omit_terminals

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"

	"github.com/marmutapp/superbased-observer/internal/attachsock"
	"github.com/marmutapp/superbased-observer/internal/termsession"
)

// terminals_wire.go is the on side of terminals_omit.go (lane MP-L1K, T-715):
// the few terminal seams core files (start.go, the tool launchers) call
// without naming a terminals-component package. A `-tags sbo_omit_terminals`
// build links terminals_omit.go instead.

// attachSocketHost is the attach-socket host the daemon serves (attachsock.Host).
type attachSocketHost = attachsock.Host

// ptySupported reports whether this OS has an in-process PTY backend.
func ptySupported() bool { return termsession.PTYSupported() }

// serveAttach serves the attach control channel on ln until ctx ends.
func serveAttach(ctx context.Context, ln net.Listener, host attachSocketHost, logger *slog.Logger) error {
	return attachsock.Serve(ctx, ln, host, logger)
}

// acquireBareResumeClaim takes the DURABLE cross-process resume flock for a bare
// `observer <tool> --resume <id>` launch (review finding H3), so that a bare
// native resume run during daemon downtime cannot collide with the daemon's own
// attach-resume of the same session (which would duplicate the transcript). It
// is a NO-OP when:
//   - sessionID or dbPath is empty (nothing to guard), or
//   - this process is a daemon-owned child (runningAsDaemonChild): the daemon
//     already holds the claim for the run it spawned, so the inner launcher must
//     not try to re-acquire it (a different process → it would self-conflict).
//
// On a filesystem error it FAILS OPEN (proceeds without the durable claim) - the
// guard must never block a legitimate resume; the daemon-side guard still
// applies when the daemon is up. On a genuine conflict it prints exactly one
// honest line and returns ok=false; the caller exits nonzero. On success it
// returns a releaser the caller DEFERS for the child's lifetime (the flock is
// held until the bare child exits, and the OS releases it if the process dies).
func acquireBareResumeClaim(stderr io.Writer, dbPath, tool, sessionID string) (release func(), ok bool) {
	if sessionID == "" || dbPath == "" || runningAsDaemonChild() {
		return func() {}, true
	}
	dir := filepath.Dir(attachSocketPath(dbPath))
	claim, acquired, err := attachsock.AcquireResumeClaim(dir, sessionID)
	if err != nil {
		return func() {}, true // fail open - never block a legit resume on a guard error
	}
	if !acquired {
		fmt.Fprintf(stderr, "observer %s: resume already in progress for session %s\n", tool, sessionID)
		return func() {}, false
	}
	return claim.Release, true
}

// attachSocketReachable dials the daemon's attach socket for the given observer
// DB path and reports whether the connection succeeded - the cheap reachability
// probe the default-on decision consults. It reuses the SAME attachsock.Dial the
// interactive attach client uses (so the two never disagree about "is the daemon
// there"), immediately closing the probe connection. A missing socket file fails
// fast (ENOENT), so opted-out/scripted paths that never call this pay nothing.
func attachSocketReachable(dbPath string) bool {
	conn, err := attachsock.Dial(attachSocketPath(dbPath))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// attachInBuild reports that the attach client and server are linked.
const attachInBuild = true
