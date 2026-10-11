// Package emit is the neutral self-observability sink contract: the Sink
// interface every decision-run producer writes to, and Nop, the real no-op
// Sink (never a nil interface) used whenever no exporter is configured or
// compiled in.
//
// It is core (modular packaging ruling D-436): it imports only the neutral
// run model and the standard library, never the OpenTelemetry SDK, the
// internal/exporter/otel package or internal/obs. The concrete
// credential-authenticated OTLP/HTTP sink lives in the otelsink subpackage,
// owned by the export.otel component, so a build without export.otel keeps
// every producer compiling against Sink and emits through Nop. Pinned by
// imports_test.go.
package emit
