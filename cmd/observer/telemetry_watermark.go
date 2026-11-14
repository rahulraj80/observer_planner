package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// The "collected from" watermark.
//
// The collector derives events from the local capture tables with a
// look-back window (internal/telemetry.lookbackWindow), so that rows the
// watchers write late are still counted. Without a lower bound, the first
// pass after telemetry is turned on would also count activity from before
// that moment: time when the person had it turned off, or, on a new install,
// time before the one-time notice was shown.
//
// The watermark is that lower bound: the last moment telemetry was known to
// be NOT permitted (consent off, the loop disabled, the kill switch set), or
// the first start. Derive never looks at activity before it. It lives in a
// small file next to the store, like the install token and the notice
// marker, so it needs no schema change:
//
//	<RFC 3339 UTC time>\n<on|off>\n
//
// The second line is whether telemetry was permitted when the file was last
// written. A daemon that starts permitted after an "off" record (or with no
// record at all) moves the watermark to now; one that starts permitted after
// an "on" record keeps it, so a restart with telemetry on throughout loses
// nothing.
const telemetryWatermarkFile = "telemetry_collect_from"

func telemetryWatermarkPath(cfg config.Config) string {
	return filepath.Join(filepath.Dir(cfg.Observer.DBPath), telemetryWatermarkFile)
}

// readTelemetryWatermark returns the stored watermark and whether telemetry
// was permitted when it was written. ok is false when there is no usable
// record.
func readTelemetryWatermark(cfg config.Config) (from time.Time, permitted, ok bool) {
	b, err := os.ReadFile(telemetryWatermarkPath(cfg))
	if err != nil {
		return time.Time{}, false, false
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) < 2 {
		return time.Time{}, false, false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(lines[0]))
	if err != nil {
		return time.Time{}, false, false
	}
	return t.UTC(), strings.TrimSpace(lines[1]) == "on", true
}

func writeTelemetryWatermark(cfg config.Config, from time.Time, permitted bool) error {
	state := "off"
	if permitted {
		state = "on"
	}
	p := telemetryWatermarkPath(cfg)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(from.UTC().Format(time.RFC3339)+"\n"+state+"\n"), 0o600)
}

// telemetryMarkNotPermitted records that telemetry is not permitted at now:
// nothing from before now may be collected once it is permitted again.
// Best-effort; a write failure only leaves an older, still valid record.
func telemetryMarkNotPermitted(cfg config.Config, now time.Time) {
	_ = writeTelemetryWatermark(cfg, now, false)
}

// telemetryMarkPermitted records that telemetry is permitted at now and
// returns the watermark Derive must respect. It moves the watermark to now
// when the last record says telemetry was off, or when there is no record
// (first start); otherwise the stored watermark stands.
func telemetryMarkPermitted(cfg config.Config, now time.Time) time.Time {
	from, permitted, ok := readTelemetryWatermark(cfg)
	if ok && permitted {
		return from
	}
	now = now.UTC().Truncate(time.Second)
	_ = writeTelemetryWatermark(cfg, now, true)
	return now
}
