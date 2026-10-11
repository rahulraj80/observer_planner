//go:build sbo_omit_ingest_otlp

package dashboard

import "net/http"

// setup_telemetry_omit.go is the off side of setup_telemetry.go: in a `-tags sbo_omit_ingest_otlp` build
// (component ingest.otlp compiled out) the route stays registered and answers 404
// component_not_installed, reason not_compiled, and this package does not
// import the feature's packages for pointing an AI tool's OpenTelemetry exporter at Observer (modular packaging plan 5.1, lane
// MP-L1B).

// handleSetupTelemetry refuses: ingest.otlp is not in this build.
func (s *Server) handleSetupTelemetry(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "ingest.otlp")
}
