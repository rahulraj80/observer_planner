// Package procmodel is the neutral MODEL of the Process Observability feature
// (docs/process-observability.md): the ProcessRun / ProcessEvent /
// MetricSample / NetworkBodyCapture row and envelope types, the attribution
// source and confidence vocabulary, action correlation, cross-OS session
// matching, launch seeds, findings, command-run derivation, and the
// sink-error and transport-health vocabulary.
//
// It was split out of internal/processobs in modular packaging phase PB
// (plan rev F1): the store, proxy, metrics, diag, dashboard and arena only
// need these shapes and pure derivations, and importing them from
// internal/processobs kept the whole capture runtime (the Observer loop, the
// attributor, the eBPF / ETW / poll backends behind it) compiled into every
// distro. internal/processobs re-exports everything here as aliases, so the
// capture runtime and its backends compile unchanged.
//
// The package is PURE LOGIC, like processobs before it: no database/sql, no
// net/http, no fsnotify, no /proc reads. Privacy posture is unchanged: these
// types never carry file contents, command output, network payloads or a
// full environment, and the tables they feed stay NODE-LOCAL, pinned out of
// the org-push wire by tests/invariant/privacy_test.go.
package procmodel
