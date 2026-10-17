//go:build !sbo_omit_processobs

package dashboard

// allowAllProcessCapture makes every process-capture backend runnable for a
// test that drives POST /api/process/enable-capture through another code
// path (the T-572 save pipeline). The omit build has no capability probe.
func allowAllProcessCapture(s *Server) {
	s.processCapabilityFn = fixedCapability(processCapability{GOOS: "linux", PollOK: true, EBPFOK: true, BridgeOK: true})
}
