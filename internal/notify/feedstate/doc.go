// Package feedstate is the ONE owner of the node notification bell's read /
// dismiss state (D-323, T-424): a small JSON file beside the node database,
// notifications-state.json, written atomically (internal/fsatomic).
//
// It is a file, not a table, on purpose: the state is a per-node convenience
// that a lost file only resets (every notification shows unread again; none
// is lost, the facts behind them live in the database), so it needs no schema
// migration, and it stays node-local by construction (nothing here is
// reachable from the org push seam). It is NOT browser storage either: a
// dismissal made from a phone over the remote dashboard holds on the laptop.
//
// Bounded: an entry is kept for Retention after its last change and at most
// MaxEntries are kept (oldest dropped), so ids that no longer occur age out.
package feedstate
