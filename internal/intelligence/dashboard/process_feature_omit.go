//go:build sbo_omit_processobs

package dashboard

import (
	"log/slog"
	"net/http"
)

// process_feature_omit.go is the off side of process_feature.go: a
// `-tags sbo_omit_processobs` build has no process-observation capture
// (component processobs compiled out, modular packaging plan 5.1), so the
// process setup routes answer 404 component_not_installed with reason
// not_compiled, and nothing here imports internal/processobs/* or
// internal/processbridge/setup. The read-only process findings routes stay:
// they only read rows the store already holds.

// processObsState is empty when process observation is compiled out.
type processObsState struct{}

// initProcessObs has nothing to bind; it resets the empty state.
func (s *Server) initProcessObs(_ *slog.Logger) { s.processObsState = processObsState{} }

// handleProcessEnableCapture refuses: there is no capture backend to enable.
func (s *Server) handleProcessEnableCapture(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "processobs")
}

// handleProcessETWStatus refuses: the ETW bridge setup is not in this build.
func (s *Server) handleProcessETWStatus(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "processobs")
}

// handleProcessETWRegister refuses: the ETW bridge setup is not in this build.
func (s *Server) handleProcessETWRegister(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "processobs")
}
