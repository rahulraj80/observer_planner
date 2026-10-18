package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// telemetryNoticeMarker records that the one-time default-on notice was shown
// (telemetry plan Step 5.7: baseline telemetry is on by default for every
// install, Rahul's ruling 2026-10-18, with a notice before the first batch).
// Bump the version to show a changed notice once more.
const telemetryNoticeMarker = "telemetry_notice_v1"

// telemetryNotice is printed once by the daemon and logged. It states what
// is sent, where, and how to see, delete or stop it.
const telemetryNotice = `Usage telemetry is on (baseline tier).
  What is sent: content-free counters only: daily activity, session counts and
  durations, tool-use counts, token totals, error classes, and per prompt the
  model and provider family, token sizes, cache share, end-to-end time and a
  label (your prompt, system nudge or tool call) classified on this machine.
  No prompt or output text, file paths, project names or commands.
  Where: %s, identified by an install id derived from this machine's device
  key (no account). First batch in about 15 minutes.
  See it:   observer telemetry data
  Delete:   observer telemetry delete --yes
  Turn off: observer telemetry consent off   (or OBSERVER_TELEMETRY=off)
  Details:  observer privacy
`

// showTelemetryNoticeOnce prints and logs the default-on notice the first time
// the daemon starts with telemetry active, then leaves a marker next to the
// store so it is not repeated. Best-effort: a marker write failure only means
// the notice may show again.
func showTelemetryNoticeOnce(configPath string, out io.Writer, logger *slog.Logger) {
	cfg, err := config.Load(config.LoadOptions{GlobalPath: configPath})
	if err != nil || !telemetryShouldStart(cfg) {
		return
	}
	marker := filepath.Join(filepath.Dir(cfg.Observer.DBPath), telemetryNoticeMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	fmt.Fprintf(out, telemetryNotice, cfg.Telemetry.Endpoint)
	logger.Info("telemetry: usage telemetry is on (baseline); one-time notice shown",
		"endpoint", cfg.Telemetry.Endpoint, "tier", cfg.Telemetry.EffectiveTier(),
		"turn_off", "observer telemetry consent off")
	_ = os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}
