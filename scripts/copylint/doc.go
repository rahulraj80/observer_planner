// Command copylint runs the repository's copy and wording lints: checks on
// source TEXT (spelling, punctuation in user-visible strings, comment
// wording, the one spelling of the enterprise registry) that used to run as
// Go tests under tests/invariant (operator ruling D-612). They are lint, not
// behaviour, so they run once as a cheap step of ci.yml's go job instead of
// inside the race-sharded test binaries.
//
// Usage, from anywhere inside the repository:
//
//	go run ./scripts/copylint
//
// It exits 1 and prints one line per finding when any check fails. Each
// check is one file that registers itself in init(); the checks that name
// private-only surfaces (registry.go, posture.go) are listed in
// scripts/release.sh PRIVATE_ONLY_FILES, so the public tree builds and runs
// with the public checks only.
package main
