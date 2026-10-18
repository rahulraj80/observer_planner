package main

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/telemetry"
	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// telemetryLoopTestDB applies the full migration lineage to a temp-file
// SQLite DB (temp FILE, not :memory: — the modernc pool lands :memory:
// connections on independent databases).
func telemetryLoopTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "loop.db")
	database, err := db.Open(context.Background(), db.Options{Path: path})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

// TestRunTelemetryLoopSendsOnceAndAcks pins the W2.1 watermark wiring: the
// loop derives + stages (day_active is allowed at tier usage), sends each
// staged event at most once, acks the server's last_seq into the durable
// watermark, and subsequent cycles build an empty envelope (the derive
// dedup key prevents re-staging and the watermark prevents re-sending).
func TestRunTelemetryLoopSendsOnceAndAcks(t *testing.T) {
	database := telemetryLoopTestDB(t)
	c := telemetry.NewCollector(database, telemetry.CollectorConfig{
		Tier: contract.TierUsage, ClientVersion: "test",
	})
	ctx := context.Background()

	var sends atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ingest" {
			http.NotFound(w, r)
			return
		}
		sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		// Accept one event per batch; last_seq 1 covers the single staged
		// row, so every later cycle must build an empty envelope.
		_, _ = w.Write([]byte(`{"accepted":1,"last_seq":1,"duplicate":false}`))
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Telemetry.Tier = contract.TierUsage
	cfg.Telemetry.Enabled = true
	cfg.Telemetry.Endpoint = srv.URL
	// A stored install token selects the self-hosted reporter path (the
	// fake /v1/ingest above); without one the loop uses the cloud lane.
	cfg.Telemetry.InstallTokenFile = writeTestInstallToken(t)
	cfg.Telemetry.BatchIntervalSeconds = 1 // jittered to ~0.9-1.1s per tick

	loopCtx, cancel := context.WithTimeout(ctx, 11*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runTelemetryLoop(loopCtx, c, cfg, logger)
	}()

	// Wait for the first send (up to ~10s of ticks).
	deadline := time.Now().Add(10 * time.Second)
	for sends.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if sends.Load() == 0 {
		t.Fatal("loop never sent a batch")
	}
	// A couple more ticks must NOT produce another send: acked rows are
	// excluded by the watermark, not re-selected.
	time.Sleep(2500 * time.Millisecond)
	if got := sends.Load(); got != 1 {
		t.Fatalf("ingest hit %d times after ack, want exactly 1 (acked rows re-sent?)", got)
	}

	var acked int64
	if err := database.QueryRow(`SELECT last_ack_seq FROM telemetry_state WHERE id = 1`).Scan(&acked); err != nil {
		t.Fatal(err)
	}
	if acked != 1 {
		t.Errorf("last_ack_seq = %d, want 1", acked)
	}
	cancel()
	<-done
}

// TestRunTelemetryLoopDropsPermanentlyRejectedBatch pins the poison-batch
// path: a 400 bad_schema answer must drop the batch through the watermark
// (dropped_count, rows deleted, watermark untouched) instead of retrying
// the same batch forever. Freshly derived events MAY re-send next cycle —
// the invariant is that rejected rows never accumulate and the loop keeps
// making progress.
func TestRunTelemetryLoopDropsPermanentlyRejectedBatch(t *testing.T) {
	database := telemetryLoopTestDB(t)
	c := telemetry.NewCollector(database, telemetry.CollectorConfig{
		Tier: contract.TierUsage, ClientVersion: "test",
	})
	ctx := context.Background()

	var sends atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ingest" {
			http.NotFound(w, r)
			return
		}
		sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad_schema","detail":"synthetic"}`))
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Telemetry.Tier = contract.TierUsage
	cfg.Telemetry.Enabled = true
	cfg.Telemetry.Endpoint = srv.URL
	// A stored install token selects the self-hosted reporter path (the
	// fake /v1/ingest above); without one the loop uses the cloud lane.
	cfg.Telemetry.InstallTokenFile = writeTestInstallToken(t)
	cfg.Telemetry.BatchIntervalSeconds = 1
	origBackoff := telemetryInitialBackoff
	telemetryInitialBackoff = 100 * time.Millisecond
	t.Cleanup(func() { telemetryInitialBackoff = origBackoff })

	loopCtx, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runTelemetryLoop(loopCtx, c, cfg, logger)
	}()

	deadline := time.Now().Add(8 * time.Second)
	for sends.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if sends.Load() < 2 {
		t.Fatalf("loop sent %d batches, want >= 2 (a rejected batch must not stall the queue)", sends.Load())
	}
	time.Sleep(300 * time.Millisecond) // let the drop land

	var rows int
	if err := database.QueryRow(`SELECT COUNT(*) FROM telemetry_staged`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	// Rejected batches are dropped each cycle, but the loop legitimately
	// re-derives fresh events between sends (day_active + a batch_meta for
	// each new drop count), so a small mid-cycle set is correct — what
	// must NOT happen is unbounded accumulation of the rejected rows.
	if rows > 6 {
		t.Errorf("staged rows = %d, want <= 6 (rejected rows must be dropped, not accumulated)", rows)
	}
	var dropped int64
	if err := database.QueryRow(`SELECT dropped_count FROM telemetry_state WHERE id = 1`).Scan(&dropped); err != nil {
		t.Fatal(err)
	}
	if dropped < 1 {
		t.Errorf("dropped_count = %d, want >= 1 (drops are reported honestly)", dropped)
	}
	var acked int64
	if err := database.QueryRow(`SELECT last_ack_seq FROM telemetry_state WHERE id = 1`).Scan(&acked); err != nil {
		t.Fatal(err)
	}
	if acked != 0 {
		t.Errorf("last_ack_seq = %d, want 0 (a 400 never acks)", acked)
	}
	cancel()
	<-done
}

// TestClampTierToCeiling pins the consent-ceiling semantics (live-smoke
// regression, 2026-09-26): max_tier from GET /v1/consent/config is a
// CEILING (the highest admitted tier), so the loop must downgrade only
// when the LOCAL tier exceeds it — a usage client under a deep ceiling
// sends; a deep client under a usage ceiling is clamped to usage; an
// invalid ceiling fails closed; the ceiling never UPGRADES a local tier.
func TestClampTierToCeiling(t *testing.T) {
	cases := []struct {
		local, ceiling, want string
	}{
		{"usage", "deep", "usage"},  // ceiling above local: unchanged
		{"deep", "usage", "usage"},  // local above ceiling: clamped
		{"deep", "error", "error"},  // clamped two steps
		{"usage", "usage", "usage"}, // equal: unchanged
		{"error", "usage", "error"}, // ceiling never upgrades
		{"deep", "off", "off"},      // server forbids everything
		{"deep", "", "off"},         // empty ceiling fails closed
		{"deep", "garbage", "off"},  // invalid ceiling fails closed
		{"off", "deep", "off"},      // off stays off
	}
	for _, tc := range cases {
		if got := clampTierToCeiling(tc.local, tc.ceiling); got != tc.want {
			t.Errorf("clampTierToCeiling(%q, %q) = %q, want %q", tc.local, tc.ceiling, got, tc.want)
		}
	}
}

// TestTelemetryLoadTokenTrims pins the token-file trim (live-smoke
// regression, 2026-09-26): the token file is written with a trailing
// newline, and a newline inside the Authorization header makes net/http
// refuse every request ("invalid header field value").
func TestTelemetryLoadTokenTrims(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{}
	cfg.Observer.DBPath = filepath.Join(dir, "obs.db")
	tokPath := telemetryTokenPath(cfg)
	if err := os.WriteFile(tokPath, []byte("tok_abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := telemetryLoadToken(cfg); got != "tok_abc" {
		t.Errorf("telemetryLoadToken = %q, want %q (no trailing newline)", got, "tok_abc")
	}
}

// writeTestInstallToken stores a reporter install token in a temp file and
// returns its absolute path (InstallTokenFile).
func writeTestInstallToken(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "telemetry_install_token")
	if err := os.WriteFile(p, []byte("tok_test\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	return p
}
