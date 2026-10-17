package dashboard

import (
	"fmt"
	"os"
	"testing"
)

// hermeticHome is the throwaway home directory every test in this package
// runs under (T-690).
//
// Why: many handlers resolve paths from the user's home (config.Load's
// default governance sidecar ~/.observer/governance-effective.json, the
// guard policy files, the setup wizard, codex support, routing apply). A
// test whose seeded config names no db_path therefore read the REAL
// developer's ~/.observer. On an org-managed machine the daemon writes a
// governance sidecar there, and its pins (guard.mode, for one) overlay the
// test's own config.toml on every config.Load, so
// TestHandleConfigSection_Guard_RejectsMissingPrompt passed in a clean
// environment and failed on a managed developer machine. No next/1.36
// merge broke it; the test read live user state, which a test must never
// do (and which it must never write either).
//
// Tests that need a specific home still call t.Setenv("HOME", ...) on top
// of this; t.Setenv restores this value, never the real one.
var hermeticHome string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "dashboard-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "dashboard TestMain: create hermetic home:", err)
		os.Exit(2)
	}
	hermeticHome = dir
	for _, k := range []string{"HOME", "USERPROFILE"} {
		if err := os.Setenv(k, dir); err != nil {
			fmt.Fprintln(os.Stderr, "dashboard TestMain: set", k+":", err)
			os.Exit(2)
		}
	}
	// XDG locations derive from HOME when unset; a set one would still
	// point at the developer's real tree, so drop it for the run.
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "OBSERVER_CONFIG"} {
		_ = os.Unsetenv(k)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// TestPackageHomeIsHermetic pins T-690: the package's tests resolve the
// throwaway home, never the developer's real one.
func TestPackageHomeIsHermetic(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	if hermeticHome == "" || home != hermeticHome {
		t.Fatalf("tests resolve home %q, want the hermetic %q (TestMain must isolate HOME)", home, hermeticHome)
	}
}
