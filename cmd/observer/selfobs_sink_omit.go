//go:build sbo_omit_export_otel

package main

import (
	"log/slog"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/selfobs/emit"
)

// selfobs_sink_omit.go is the off side of selfobs_sink.go: in a
// `-tags sbo_omit_export_otel` build (component export.otel compiled out,
// D-436) there is no OTLP/HTTP sink, so every producer emits through
// emit.Nop. A [selfobs] section that asks for export is reported, never
// obeyed (plan section 4: config cannot turn on a component that is not in
// the build).
func buildSelfObsSink(cfg config.Config, logger *slog.Logger) (emit.Sink, func(), error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.SelfObs.Enabled {
		logger.Warn("selfobs: [selfobs] enabled is ignored: OpenTelemetry export (export.otel) is not in this build")
	}
	return emit.Nop(), func() {}, nil
}
