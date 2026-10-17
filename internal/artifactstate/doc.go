// Package artifactstate derives the state of the components that ship as
// separate artifacts (modular packaging plan 3.3.1 and 3.3.2, lane MP-ART,
// T-601): the browser extension, the VS Code extension, the plugin shims
// Observer writes into in-process plugin hosts, and the two helper binaries.
//
// Observer does not control an artifact's lifecycle the way it controls a
// binary component: it cannot install or uninstall a browser or editor
// extension, so "not installed" means something different for each one.
// This package turns what the caller observed (a handshake record, a
// presence probe, an install directory listing) into one honest state per
// artifact, a remedy line and a leftover flag, through ordered rule tables
// walked top-down (CLAUDE.md module rule 5).
//
// The package is pure: no SQL, no HTTP, no fsnotify, no os/exec, no file
// I/O. Every observation is passed in by the caller (cmd/observer composes
// the probes), and imports_test.go pins that.
package artifactstate
