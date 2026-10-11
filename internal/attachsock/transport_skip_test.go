package attachsock

import "testing"

// skipUnlessUnixSocketTransport skips a test whose subject is the AF_UNIX
// transport's own behaviour (0700 parent dirs, 0600 socket modes, the
// stale-socket probe-dial, UNIX_PATH_MAX) when this platform's default
// transport is something else. It dispatches on the transport's SHAPE
// (Describe), never on GOOS (CLAUDE.md #3). The transport tests themselves
// live in internal/localipc.
func skipUnlessUnixSocketTransport(t *testing.T) {
	t.Helper()
	if got := DefaultTransport().Describe(); got != TransportUnixSocket {
		t.Skipf("default transport is %q, not %q: this test pins AF_UNIX-socket behaviour", got, TransportUnixSocket)
	}
}
