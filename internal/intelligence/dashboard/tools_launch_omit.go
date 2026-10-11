//go:build sbo_omit_terminals

package dashboard

import "net/http"

// tools_launch_omit.go is the off side of tools_launch.go: in a `-tags sbo_omit_terminals` build
// (component terminals compiled out) the route stays registered and answers 404
// component_not_installed, reason not_compiled, and this package does not
// import the feature's packages for launching an AI tool from Settings > Tools (modular packaging plan 5.1, lane
// MP-L1B).

// handleToolsLaunch refuses: terminals is not in this build.
func (s *Server) handleToolsLaunch(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "terminals")
}
