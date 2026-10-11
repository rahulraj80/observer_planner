// Package distro is the modular packaging smoke harness (plan section 5.9,
// lane MP-SEC): it runs a built observer binary and checks the runtime
// surface a component selection promises against what the process actually
// does, so a catalog Surface row or an L2 claim that drifts from the code
// fails loudly (ledger MODPKG-1, MODPKG-4).
//
// The tests are opt-in: they need a prebuilt binary (SBO_DISTRO_BIN) and
// SBO_DISTRO_SMOKE=1, and they must run with a throwaway HOME, /mnt/c and
// /tmp hidden and their own network namespace, because they start a real
// daemon. Without the variables every test skips, so `go test ./...` never
// starts a daemon.
package distro
