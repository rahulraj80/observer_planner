// Package defaults exposes the canonical set of session-file adapters
// the observer registers in production. Pulling this out of
// cmd/observer/main.go lets tests in internal/adapter/ and
// internal/watcher/ assemble the same adapter set the production
// binary uses — needed for the all-adapters IsSessionFile invariant
// test and the multi-adapter watcher regression test that pins the
// poller's dispatch rule.
//
// The sub-package shape is intentional: putting this list in
// internal/adapter directly would create an import cycle (each
// adapter package imports internal/adapter for the Adapter
// interface).
//
// Registration (modular packaging P5, lanes MP-ADP and MP-L1D, T-598):
// every adapter registers itself from one file per adapter package,
// adapter_<pkg>.go, built unless the package's catalog omit tag is set
// (`-tags sbo_omit_adapter_zed`). Its pair, adapter_<pkg>_omit.go, is built
// only with that tag and marks the component compiled out, so the binary
// reports its own compiled set. Nothing else in the node binary imports
// those packages (cmd/observer reaches an adapter's hook receiver, backfill
// passes and watcher wiring through its own build-tagged files, the same
// way), which is what the package-absence invariant (tests/invariant)
// proves per tag. Every adapter keeps one slot in the canonical order, so a
// full build's list is exactly the pre-P5 list (pinned by
// TestAdaptersCanonicalOrder).
package defaults

import (
	"sort"

	"github.com/marmutapp/superbased-observer/internal/adapter"
)

// slot is one adapter's place in the canonical order.
type slot struct {
	order int
	build func() adapter.Adapter
}

// registered holds the slots the per-adapter files add from init().
var registered []slot

// register adds one adapter constructor at its canonical position. It is
// called only from init() of an adapter_<pkg>.go file.
func register(order int, build func() adapter.Adapter) {
	registered = append(registered, slot{order: order, build: build})
}

// Adapters returns the canonical set of session-file adapters compiled
// into this binary, with zero-value defaults, in canonical order. Callers
// that need runtime config apply it after this returns, through the
// optional capability interfaces in internal/adapter (HookDedupConfigurer,
// CaptureStartupPreparer, NativeToolClassifier) or, in an adapter-owned
// build-tagged file, by type-asserting them.
//
// Used by:
//   - cmd/observer/main.go::buildWatcher  (registers each into the
//     watcher Registry)
//   - cmd/observer/main.go::recognizesSessionFile  (composes a single
//     IsSessionFile predicate for the dashboard's health endpoint
//     orphan filter)
//   - internal/adapter/defaults/defaults_test.go  (invariants — every
//     adapter must require under-WatchPaths in its IsSessionFile, and
//     no two adapters' watch roots share a prefix)
func Adapters() []adapter.Adapter {
	slots := append([]slot(nil), registered...)
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].order < slots[j].order })
	out := make([]adapter.Adapter, 0, len(slots))
	for _, s := range slots {
		out = append(out, s.build())
	}
	return out
}

// ByTool returns a fresh adapter for one registry tool id, and false when
// this binary was built without it (an omitted adapter package) or no
// adapter reports that name. Callers that need one adapter's parser (a
// backfill correction pass) use this instead of importing the adapter's
// package, so the package can leave the binary.
func ByTool(tool string) (adapter.Adapter, bool) {
	for _, a := range Adapters() {
		if a.Name() == tool {
			return a, true
		}
	}
	return nil, false
}
