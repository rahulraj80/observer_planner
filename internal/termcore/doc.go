// Package termcore is the neutral terminal vocabulary core packages speak
// (lane MP-L1K, T-715): the run-kind table and its remote-view sensitivity,
// the standing-credential denial classes, the reserved shell pseudo-tool
// name, the sandbox launch sentinels and the project-root validators.
//
// It exists so the dashboard, diag and the remote controller can name a
// terminal run kind, a denial or a validated project root without importing
// the terminals component's packages (internal/termrun, internal/termlease,
// internal/termsvc), which re-export everything here. A build with
// `-tags sbo_omit_terminals` keeps this package and leaves the terminal
// stack out.
//
// Pure: no database/sql, net/http or fsnotify (imports_test.go).
package termcore
