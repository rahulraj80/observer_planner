//go:build !sbo_omit_adapter_antigravity && !sbo_omit_adapter_antigravity_cli

package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/antigravity"
	"github.com/marmutapp/superbased-observer/internal/platform/crossmount"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// adapter.antigravity's watcher wiring (see adapterWirers), and the bridge
// helper's locator for the artifact probes (components_artifacts.go).
func init() {
	antigravityBridgeLocator = antigravity.LocateBridgeBinary
	adapterWirers = append(adapterWirers, func(a adapter.Adapter, env adapterWireEnv) {
		cfg, st, logger := env.cfg, env.st, env.logger
		// Wire per-adapter config into adapters that support it.
		// Antigravity has the network_recovery opt-in for the
		// gRPC fallback against the running language_server.
		if ag, ok := a.(*antigravity.Adapter); ok {
			ag.WithNetworkRecovery(cfg.Observer.Antigravity.NetworkRecovery)
			// Startup nudge: CLI .pb files require the gRPC fallback
			// to surface in the dashboard (the agy CLI doesn't ship
			// the desktop oscrypt secret). Warn once on start so the
			// operator notices the gap before opening the dashboard
			// and finding an empty Antigravity section.
			warnAntigravityCLIWithoutNetworkRecovery(logger, cfg.Observer.Antigravity.NetworkRecovery)
			// Persistent decrypt+gRPC-failure tracker (Issue #4
			// follow-up). Skips files we know are unrecoverable
			// across CLI invocations - the in-memory cache from
			// the v1.6.11 first pass only helped the long-running
			// daemon. Scoped to adapter="antigravity" so a future
			// adapter using the same store can't collide.
			ag.WithUnrecoverableTracker(antigravityTrackerShim{st: st})
			// DB-aware dedup reader for the plaintext-transcript
			// augmentation path (Phase 1 of
			// docs/plans/antigravity-token-coverage-design-2026-05-24.md).
			// Closes the universal-duplication bug where later parse
			// cycles re-emitted every transcript USER_INPUT after the
			// originating agy.exe terminated - see the
			// TargetCoverageReader docstring for the cross-cycle race.
			ag.WithTargetCoverageReader(antigravityTargetCoverageShim{st: st})
			// Optional shape-mismatch wire-bytes dump (Issue #5
			// follow-up). Off by default - operator enables via
			// [observer.antigravity] dump_shape_mismatches_dir in
			// config.toml to capture proto payloads for offline
			// path-mapping analysis.
			ag.WithShapeMismatchDumpDir(cfg.Observer.Antigravity.DumpShapeMismatchesDir)
		}
	})
}

// warnAntigravityCLIWithoutNetworkRecovery scans every cross-mount
// home for ~/.gemini/antigravity-cli/conversations/*.pb and emits one
// startup WARN if any are found while network_recovery is off.
// Idempotent + cheap (one Stat per home, one ReadDir on hits) - runs
// once per daemon start. Best-effort: silent on permission errors.
func warnAntigravityCLIWithoutNetworkRecovery(logger *slog.Logger, networkRecovery string) {
	if strings.EqualFold(strings.TrimSpace(networkRecovery), "local") {
		return
	}
	for _, h := range crossmount.AllHomes() {
		dir := filepath.Join(h.Path, ".gemini", "antigravity-cli", "conversations")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		n := 0
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".pb") {
				n++
			}
		}
		if n > 0 {
			logger.Warn(
				"antigravity-cli .pb files present but network_recovery is off - CLI conversations will not appear in the dashboard until you set [observer.antigravity] network_recovery = \"local\" in ~/.observer/config.toml",
				"dir", dir,
				"pb_count", n,
				"network_recovery", networkRecovery,
			)
			return
		}
	}
}

// antigravityTrackerShim adapts *store.Store to the
// antigravity.UnrecoverableTracker interface, scoping every call to
// adapter="antigravity". Defined here (not in internal/store) so the
// adapter package stays store-free per the architecture quick
// reference in CLAUDE.md. Issue #4 follow-up.
type antigravityTrackerShim struct {
	st *store.Store
}

func (s antigravityTrackerShim) Lookup(ctx context.Context, sourceFile string, size, mtimeUnix int64) (string, bool, error) {
	entry, err := s.st.LookupUnrecoverable(ctx, "antigravity", sourceFile, size, mtimeUnix)
	if err != nil {
		return "", false, err
	}
	if entry == nil {
		return "", false, nil
	}
	return entry.Reason, true, nil
}

func (s antigravityTrackerShim) Mark(ctx context.Context, sourceFile string, size, mtimeUnix int64, reason string) error {
	return s.st.MarkUnrecoverable(ctx, "antigravity", sourceFile, size, mtimeUnix, reason)
}

func (s antigravityTrackerShim) Clear(ctx context.Context, sourceFile string) error {
	return s.st.ClearUnrecoverable(ctx, "antigravity", sourceFile)
}

// antigravityTargetCoverageShim adapts *store.Store to the
// antigravity.TargetCoverageReader interface. Lives here (not in
// internal/store) for the same reason as antigravityTrackerShim -
// the adapter package stays store-free per CLAUDE.md's architecture
// quick reference.
type antigravityTargetCoverageShim struct {
	st *store.Store
}

func (s antigravityTargetCoverageShim) LoadActionTargets(ctx context.Context, sourceFile string) ([]string, []string, error) {
	return s.st.LoadActionTargets(ctx, sourceFile)
}
