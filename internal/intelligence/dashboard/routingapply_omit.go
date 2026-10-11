//go:build sbo_omit_proxy_routing

package dashboard

import "net/http"

// routingapply_omit.go is the off side of routingapply.go: in a `-tags sbo_omit_proxy_routing` build
// (component proxy.routing compiled out) the route stays registered and answers 404
// component_not_installed, reason not_compiled, and this package does not
// import the feature's packages for routing apply (writing an AI tool's model settings from the routing policy) (modular packaging plan 5.1, lane
// MP-L1B).

// handleRoutingApply refuses: proxy.routing is not in this build.
func (s *Server) handleRoutingApply(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "proxy.routing")
}

// handleRoutingApplyRevert refuses: proxy.routing is not in this build.
func (s *Server) handleRoutingApplyRevert(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "proxy.routing")
}

// handleRoutingApplyLedger refuses: proxy.routing is not in this build.
func (s *Server) handleRoutingApplyLedger(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "proxy.routing")
}
