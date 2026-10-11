//go:build sbo_omit_remote

package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
)

// remote_omit.go is the off side of remote_controller.go, remote_manage.go,
// remote_tailscale.go and standing_takeover.go: in a `-tags sbo_omit_remote`
// build (component remote compiled out, lane MP-L1I, T-715) the remote
// management and tailnet routes stay registered and answer 404
// component_not_installed, reason not_compiled, and this package does not
// import internal/remoteauth, internal/remotecfg or internal/tailnet. No
// RemoteController exists, so the dashboard stays loopback-only and every
// remote seam (remote_seams.go) answers "no remote access".

// isStandingSecret is always false: no standing terminal-control secret can
// be minted or verified without the remote component.
func isStandingSecret(string) bool { return false }

// hashSessionID keeps the device-key helpers' derivation (sha256 hex, the
// same as internal/remoteauth); no remote device session exists in this
// build, so it only ever sees an empty or foreign cookie.
func hashSessionID(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// OnStandingLocalTakeover has nothing to revoke: there is no standing
// remote access in this build.
func (s *Server) OnStandingLocalTakeover(_, _ string) {}

// handleRemoteConfig refuses: remote is not in this build.
func (s *Server) handleRemoteConfig(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteEnable refuses: remote is not in this build.
func (s *Server) handleRemoteEnable(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteSetAllowTerminal refuses: remote is not in this build.
func (s *Server) handleRemoteSetAllowTerminal(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteSetAllowTerminalView refuses: remote is not in this build.
func (s *Server) handleRemoteSetAllowTerminalView(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteDisable refuses: remote is not in this build.
func (s *Server) handleRemoteDisable(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteRotate refuses: remote is not in this build.
func (s *Server) handleRemoteRotate(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteAddDevice refuses: remote is not in this build.
func (s *Server) handleRemoteAddDevice(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteApproveExecute refuses: remote is not in this build.
func (s *Server) handleRemoteApproveExecute(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleStandingTerminalStatus refuses: remote is not in this build.
func (s *Server) handleStandingTerminalStatus(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleStandingTerminalMint refuses: remote is not in this build.
func (s *Server) handleStandingTerminalMint(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleStandingTerminalRevoke refuses: remote is not in this build.
func (s *Server) handleStandingTerminalRevoke(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteAudit refuses: remote is not in this build.
func (s *Server) handleRemoteAudit(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteSessions refuses: remote is not in this build.
func (s *Server) handleRemoteSessions(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteSessionRevoke refuses: remote is not in this build.
func (s *Server) handleRemoteSessionRevoke(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteSessionsRevokeAll refuses: remote is not in this build.
func (s *Server) handleRemoteSessionsRevokeAll(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteSelfcheck refuses: remote is not in this build.
func (s *Server) handleRemoteSelfcheck(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteTailscaleStatus refuses: remote is not in this build.
func (s *Server) handleRemoteTailscaleStatus(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteTailscaleServe refuses: remote is not in this build.
func (s *Server) handleRemoteTailscaleServe(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteTailscaleOperatorGrant refuses: remote is not in this build.
func (s *Server) handleRemoteTailscaleOperatorGrant(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteTailscaleLogin refuses: remote is not in this build.
func (s *Server) handleRemoteTailscaleLogin(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteTailscaleInstall refuses: remote is not in this build.
func (s *Server) handleRemoteTailscaleInstall(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteSetRevokeStandingOnTakeover refuses: remote is not in this build.
func (s *Server) handleRemoteSetRevokeStandingOnTakeover(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}

// handleRemoteSetAllowRemoteTakeover refuses: remote is not in this build.
func (s *Server) handleRemoteSetAllowRemoteTakeover(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "remote")
}
