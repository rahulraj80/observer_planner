// Package planlimitslocal performs the Plans & limits module's reads of a
// vendor app's OWN local files (docs/plans-and-limits.md): the rung-2,
// experimental, per-provider opt-in sources (Claude Code's credential file,
// the Codex CLI's auth.json, Cursor's and Windsurf's state.vscdb).
//
// Every read is read-only: a file is opened O_RDONLY, a SQLite store is
// opened with mode=ro and only one ItemTable value is selected. Nothing is
// ever written, renamed, refreshed or copied; the bytes go back to the caller
// in memory for one refresh and are never stored or logged. The package makes
// no network call (tests/invariant/planlimits_egress_test.go).
package planlimitslocal
