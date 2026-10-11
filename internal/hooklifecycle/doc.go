// Package hooklifecycle is the ONE lifecycle handler's pure core (hook-parity
// Lane C1, docs/audits/hook-parity-audit-2026-10-02.md section 6): it turns a
// session / turn / sub-agent lifecycle hook payload from any vendor into a
// Plan (the action row to record, if any, and whether to seed pidbridge).
//
// The decision is a table keyed on the binding's capability and phase
// (integration.HookBinding), never on the tool: a vendor gains lifecycle
// coverage by declaring a wired lifecycle binding. Payload field spellings
// differ across hosts (session_id / sessionId / conversation_id); Normalize
// resolves them at the boundary.
//
// Pure: no SQL, no HTTP, no fsnotify, no subprocess (imports_test.go). The
// caller (cmd/observer/hook_lifecycle.go) replies to the host, appends the
// row to the DB-free hook spool and seeds pidbridge.
package hooklifecycle
