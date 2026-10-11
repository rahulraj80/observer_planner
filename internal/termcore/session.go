package termcore

import "time"

// MinLinkConfidence is the threshold at or above which a terminal run's
// session correlation is considered established enough to attach cost,
// status, decoration and action links (plan section 2.1a: links attach only
// once a correlation is established). Below it a run shows metadata only,
// honestly uncorrelated. internal/termrun re-exports it; the store queries
// that join runs to sessions take it as a parameter, so core callers name it
// here.
const MinLinkConfidence = 0.50

// SessionExit is the metadata a dashboard terminal session's exit carries to
// its observers (the session-finished push, the run recorder).
// internal/termsession re-exports it as termsession.SessionExit, so the
// remote component's notifier can name it without importing the terminals
// component.
type SessionExit struct {
	Handle     string
	SessionID  string
	Subcommand string
	ExitCode   int
	At         time.Time
	// PID is the OS pid of the exited PTY child, or 0 when the backend did not
	// report one. A consumer that seeded process attribution against this pid
	// needs the pid back to retract it.
	PID int
}
