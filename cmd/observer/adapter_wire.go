package main

import (
	"context"
	"log/slog"

	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// adapterWireEnv is what a per-adapter watcher wiring step reads.
type adapterWireEnv struct {
	ctx    context.Context
	cfg    config.Config
	st     *store.Store
	logger *slog.Logger
}

// adapterWirers are the per-adapter watcher wiring steps (modular packaging
// P5, lane MP-L1D, T-598). Each lives in its adapter's own build-tagged file
// (wire_adapter_<pkg>.go, built unless the adapter's catalog omit tag is
// set) and registers itself from init(); buildWatcher runs every step on
// every active adapter, and a step acts only on its own adapter (a type
// assertion inside the adapter-owned file). A build that compiles the
// adapter out has neither the step nor any reference to the adapter's
// package, which the package-absence invariant proves.
var adapterWirers []func(a adapter.Adapter, env adapterWireEnv)

// antigravityBridgeLocator finds the antigravity-bridge helper beside the
// binary. Set by wire_adapter_antigravity.go; nil in a build without the
// adapter, where the helper has no user and the artifact probe skips it.
var antigravityBridgeLocator func() (string, error)
