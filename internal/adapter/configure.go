package adapter

import (
	"context"
	"log/slog"

	"github.com/marmutapp/superbased-observer/internal/platform/crossmount"
)

// configure.go holds the optional capability interfaces the watcher builder
// (cmd/observer buildWatcher) uses to hand an adapter its runtime seams
// without naming the adapter's concrete type (modular packaging P5, lane
// MP-ADP, the plan's "Configurer interface replacing buildWatcher type
// asserts"). An adapter that implements one gets the seam; the builder never
// imports the adapter's package, so the package can be compiled out (L1).

// HookDedupConfigurer is implemented by an adapter whose tool also delivers
// events through Observer's hook receiver. hasRows reports whether a session
// already has rows stamped with a given source_file marker; the adapter
// names its own hook marker and uses the answer to skip (or partition) the
// transcript-side events the hook path already captured, so the two paths
// never double-count a session.
type HookDedupConfigurer interface {
	ConfigureHookDedup(hasRows func(ctx context.Context, sessionID, sourceFile string) (bool, error))
}

// CaptureStartupPreparer is implemented by an adapter whose tool writes
// nothing capturable until a vendor-side setting is on (Copilot Chat's debug
// log). The watcher builder calls PrepareCapture once per start, and only for
// an ACTIVE adapter: an inactive one writes no vendor config (L2). homes are
// every cross-mount-resolved home the watcher covers.
type CaptureStartupPreparer interface {
	PrepareCapture(ctx context.Context, logger *slog.Logger, homes []crossmount.HomeRoot)
}

// NativeToolClassifier is implemented by an adapter whose transcripts mix
// the tool's own built-in tools with MCP or user-defined ones. The watcher
// builder hands IsNativeTool to the watcher as that tool's native-tool
// predicate.
type NativeToolClassifier interface {
	IsNativeTool(name string) bool
}

// ProjectReattributor is implemented by an adapter that can re-derive one
// session file's project attribution offline (a backfill pass corrects
// sessions an older parser attributed to the wrong project root). ok is
// false when the file carries no attribution.
type ProjectReattributor interface {
	ProjectAttribution(path string) (sessionID, projectRoot, remote string, ok bool)
}
