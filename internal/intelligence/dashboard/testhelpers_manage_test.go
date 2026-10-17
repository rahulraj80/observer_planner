package dashboard

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db"
)

// testhelpers_manage_test.go holds the shared server builders many unrelated
// test files use (lane MP-L1K, T-715). They used to live in remote-tagged
// test files, which forced about 110 test files to carry !sbo_omit_remote.
// The only remote-specific part, the ready RemoteController and the confirm
// probe, comes from testhelpers_remote_test.go (remote compiled in) or
// testhelpers_remote_omit_test.go (remote compiled out).

// testRemoteHost is the allow-listed remote Host the remote tests use.
const testRemoteHost = "remote.example:8443"

// newRemoteTestServer builds a Server over a fresh test DB with opts.
func newRemoteTestServer(t *testing.T, opts Options) *Server {
	t.Helper()
	database, err := openTestDB(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "d.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	opts.DB = database
	s, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// newManageServer builds a Server with a writable config path (so the
// management writes work), a real DB and, when remote is in the build, a
// ready controller.
func newManageServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	s := newManageServerOpts(t, Options{})
	return s, s.Handler() // loopback path: Local routes reachable
}

// newManageServerWithLaunch is newManageServer plus an injected LaunchManager,
// for the operator-grant PTY-spawn path.
func newManageServerWithLaunch(t *testing.T, lm LaunchManager) http.Handler {
	t.Helper()
	return newManageServerOpts(t, Options{LaunchManager: lm}).Handler()
}

func newManageServerOpts(t *testing.T, opts Options) *Server {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "observer.db")
	database, err := openTestDB(context.Background(), db.Options{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cfgPath := filepath.Join(dir, "config.toml")
	body := "[observer]\ndb_path = \"" + filepath.ToSlash(dbPath) + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts.DB = database
	opts.ConfigPath = cfgPath
	opts.Remote = testManageRemote(t)
	s, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}
