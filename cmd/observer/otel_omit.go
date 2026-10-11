//go:build sbo_omit_export_otel

package main

import (
	"context"
	"errors"
)

// otel_omit.go is the off side of otel.go: in a `-tags sbo_omit_export_otel`
// build (component export.otel compiled out) internal/exporter/otel and the
// OpenTelemetry SDK are not in the binary. start.go keeps the same call
// sites; the exporter is never constructed, so its goroutines never start.

// omittedOTelExporter is never constructed; it only gives the handle the
// methods start.go calls on a non-nil exporter.
type omittedOTelExporter struct{}

// otelExporterHandle is always nil in this build.
type otelExporterHandle = *omittedOTelExporter

// Start never runs: the handle is always nil.
func (*omittedOTelExporter) Start(context.Context) error { return nil }

// Stop never runs: the handle is always nil.
func (*omittedOTelExporter) Stop(context.Context) error { return nil }

// errOTelExportNotInBuild is the honest refusal for an [exporter.otel]
// section that asks for export in a build without it.
var errOTelExportNotInBuild = errors.New("OpenTelemetry export (export.otel) is not in this build")

// buildOTelExporter refuses: [exporter.otel] is reported, never obeyed.
func buildOTelExporter(_ context.Context, _ string) (otelExporterHandle, func(), bool, error) {
	return nil, func() {}, false, errOTelExportNotInBuild
}
