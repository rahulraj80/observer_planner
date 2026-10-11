//go:build !sbo_omit_terminals_sandbox

package main

import (
	"os"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/platform/crossmount"
)

// TestMain lets the test binary stand in for the observer binary as a sandbox
// network helper: the live sandbox tests plan the host/guest helper verbs
// against os.Executable() (this binary), exactly as the daemon plans them
// against itself, so argv[1] naming a helper verb must run the helper, not the
// test suite.
//
// It also stubs the launcher's daemon lane probe (daemonLanes) so no test
// can query a real daemon's proxy.
//
// It also exports crossmount.EnvDisableForeignHomes for the whole run. This
// binary is already covered by crossmount's test-binary guard; the env var is
// for the REAL observer binaries some tests build and exec (probe-hook, hook
// subprocess tests), which inherit it and so can never resolve the operator's
// real /mnt/c/Users/<u>/.claude either (incident 2026-10-02).
func TestMain(m *testing.M) {
	if code, ok := sandboxHelperDispatch(os.Args); ok {
		os.Exit(code)
	}
	if err := os.Setenv(crossmount.EnvDisableForeignHomes, "1"); err != nil {
		os.Exit(2)
	}
	// No test may reach a REAL running daemon's proxy through the launcher
	// lane preflight (T-308): a developer machine's daemon would answer for
	// it. Tests that exercise the daemon answer stub daemonLanes themselves.
	daemonLanes = func(string) (map[string]bool, bool) { return nil, false }
	os.Exit(m.Run())
}
