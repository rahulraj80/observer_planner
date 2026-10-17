//go:build sbo_omit_export_otel

package main

import "github.com/marmutapp/superbased-observer/internal/components"

// components_build_noexportotel.go reports OpenTelemetry export compiled
// out: a `-tags sbo_omit_export_otel` build links otel_omit.go,
// guardotel_omit.go and selfobs_sink_omit.go instead of the exporter, the
// guard-event tail and the self-observability OTLP sink, so
// internal/exporter/otel, internal/selfobs/emit/otelsink and the
// OpenTelemetry SDK leave the binary (D-436).
func init() { components.MarkOmitted("export.otel") }
