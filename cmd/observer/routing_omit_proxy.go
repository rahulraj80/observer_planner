//go:build sbo_omit_proxy_routing && !sbo_omit_capture_proxy

package main

import (
	"context"
	"log/slog"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/proxy"
	"github.com/marmutapp/superbased-observer/internal/selfobs/emit"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// routing_omit_proxy.go is the proxy-typed half of routing_omit.go (lane
// MP-L1L, T-715): only a build that has the proxy calls wireRouting, so a
// build without capture.proxy links neither this file nor internal/proxy.

// wireRouting wires no router. When [routing] asks for one, it logs the one
// reason the setting is not honoured and the proxy starts without it.
func wireRouting(_ context.Context, cfg config.Config, _ *store.Store, _ *proxy.Options, logger *slog.Logger, _ emit.Sink) (func() map[string]string, *RoutingStateHandle) {
	if line, off := settingOffLine("routing.enabled", cfg.Routing.Enabled && cfg.Routing.Mode != "off", nil); off && logger != nil {
		logger.Warn("routing: "+line, "mode", cfg.Routing.Mode)
	}
	return nil, nil
}
