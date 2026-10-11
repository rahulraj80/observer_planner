// Package budgetwarn is the PURE budget-warning ladder (2026-10-02
// budget-latency arc, operator decision 2).
//
// The org signs a warn ladder onto every budget (thresholds, e.g. 0.75 / 0.9 /
// 1 of the cap). Before this package the node ignored it: guard rules only
// matched at 100%, and a soft breach was a silent flag whose only channel was a
// desktop notifier. Evaluate walks the ladder for every org-authored window the
// node composed, against the same spend the guard enforces with, and reports:
//
//   - the current level of each window (what every surface renders);
//   - the NEW crossings since the last evaluation - each (window, unit,
//     threshold) is announced ONCE per budget period, node-wide, and a period
//     rollover resets the ladder.
//
// No I/O: no SQL, no HTTP, no fsnotify, no clock (the caller passes now), no
// config. The daemon's boundary (cmd/observer/budgetwarn_wire.go) owns the
// spend lookup, the persisted State, the status file every out-of-process
// surface reads, and the notifications. Pinned by imports_test.go.
package budgetwarn
