package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestTelemetryWatermark pins the "collected from" rule: a start with no
// record, or after telemetry was off, moves the watermark to now; a restart
// with telemetry on throughout keeps it.
func TestTelemetryWatermark(t *testing.T) {
	cfg := config.Default()
	cfg.Observer.DBPath = filepath.Join(t.TempDir(), "obs.db")
	t0 := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

	if _, _, ok := readTelemetryWatermark(cfg); ok {
		t.Fatal("a fresh install has a watermark record")
	}
	if got := telemetryMarkPermitted(cfg, t0); !got.Equal(t0) {
		t.Fatalf("first start: watermark = %v, want %v", got, t0)
	}
	// Restart an hour later, telemetry on throughout: the watermark stands.
	if got := telemetryMarkPermitted(cfg, t0.Add(time.Hour)); !got.Equal(t0) {
		t.Fatalf("restart while on: watermark = %v, want %v", got, t0)
	}
	// Turned off at t0+2h, back on at t0+5h: nothing before t0+5h.
	telemetryMarkNotPermitted(cfg, t0.Add(2*time.Hour))
	if from, permitted, ok := readTelemetryWatermark(cfg); !ok || permitted || !from.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("after off: record = %v %v %v", from, permitted, ok)
	}
	on := t0.Add(5 * time.Hour)
	if got := telemetryMarkPermitted(cfg, on); !got.Equal(on) {
		t.Fatalf("back on: watermark = %v, want %v", got, on)
	}
	if got := telemetryMarkPermitted(cfg, on.Add(time.Minute)); !got.Equal(on) {
		t.Fatalf("next cycle while on: watermark = %v, want %v", got, on)
	}
}
