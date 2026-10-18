package dashboard

import (
	"net/http"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TelemetryNoticeResult is GET /api/telemetry/notice: whether usage telemetry
// is on for this node, so the shell can show the one-time default-on banner
// (telemetry plan Step 5.7). It carries no data that was sent, only posture.
type TelemetryNoticeResult struct {
	Active   bool   `json:"active"`
	Tier     string `json:"tier"`
	Endpoint string `json:"endpoint"`
	// Version bumps when the notice text changes, so the banner re-shows once.
	Version int `json:"version"`
}

// handleTelemetryNotice serves GET /api/telemetry/notice. A dashboard started
// without a config path reports inactive rather than guessing.
func (s *Server) handleTelemetryNotice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	res := TelemetryNoticeResult{Version: 1}
	if s.opts.ConfigPath == "" {
		writeJSON(w, res)
		return
	}
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	res.Tier = cfg.Telemetry.EffectiveTier()
	res.Endpoint = cfg.Telemetry.Endpoint
	res.Active = cfg.Telemetry.Enabled && res.Tier != "off" && !config.TelemetryKillSwitchActive()
	writeJSON(w, res)
}
