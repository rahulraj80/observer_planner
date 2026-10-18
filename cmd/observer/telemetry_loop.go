package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/telemetry"
	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// buildTelemetryCollector constructs the W2 telemetry collector + loop for
// the daemon. It mirrors buildOTelExporter's posture exactly: constructed
// only when [telemetry] enabled = true (the second consent key after the
// tier), a construction failure is WARN-only and never blocks the daemon
// (P1), and it opens its OWN DB handle (the returned cleanup closes it).
//
// Returns (nil, false, nil) whenever the rail is off: tier off, loop
// disabled, or the OBSERVER_TELEMETRY kill switch engaged — no collector,
// no goroutine, no network call, nothing staged.
func buildTelemetryCollector(ctx context.Context, configPath string) (c *telemetry.Collector, cleanup func(), enabled bool, err error) {
	cleanup = func() {}
	cfg, err := config.Load(config.LoadOptions{GlobalPath: configPath})
	if err != nil {
		return nil, cleanup, false, fmt.Errorf("load config: %w", err)
	}
	if !telemetryShouldStart(cfg) {
		return nil, cleanup, false, nil
	}
	database, err := db.Open(ctx, db.Options{Path: cfg.Observer.DBPath})
	if err != nil {
		return nil, cleanup, false, fmt.Errorf("open db %s: %w", cfg.Observer.DBPath, err)
	}
	cleanup = func() { _ = database.Close() }
	collector := telemetry.NewCollector(database, telemetry.CollectorConfig{
		Tier:          cfg.Telemetry.EffectiveTier(),
		ClientVersion: version,
		BatchInterval: time.Duration(cfg.Telemetry.BatchIntervalSeconds) * time.Second,
		MaxBatchBytes: cfg.Telemetry.MaxBatchBytes,
		Nudge:         loadTelemetryNudgeRules(cfg),
	})
	return collector, cleanup, true, nil
}

// telemetryShouldStart is the three-key gate: consent tier above off AND
// the loop enabled AND the kill switch not engaged. The kill switch is
// checked live here (and again every cycle in runTelemetryLoop) so setting
// OBSERVER_TELEMETRY=off stops a RUNNING daemon's next cycle, not just the
// next one.
func telemetryShouldStart(cfg config.Config) bool {
	if cfg.Telemetry.EffectiveTier() == "off" {
		return false
	}
	if !cfg.Telemetry.Enabled {
		return false
	}
	return !config.TelemetryKillSwitchActive()
}

// runTelemetryLoop drives the plan §D2/D3 batch cadence: derive + stage on
// every tick, build a size-capped envelope, POST it, ack the watermark.
// Backoff on failure (1m→2m→…→1h cap); the server's consent ceiling can
// only downgrade the tier. Never returns an error: a failing telemetry rail
// must never cancel the daemon (P1).
func runTelemetryLoop(ctx context.Context, collector *telemetry.Collector, cfg config.Config, logger *slog.Logger) error {
	base := time.Duration(cfg.Telemetry.BatchIntervalSeconds) * time.Second
	if base <= 0 {
		base = 15 * time.Minute
	}
	timer := time.NewTimer(jitterInterval(base))
	defer timer.Stop()

	sender, err := newTelemetrySender(cfg, collector, logger)
	if err != nil {
		// P1: a broken rail never cancels the daemon; it just stays quiet.
		logger.Warn("telemetry: no sender; rail idle", "err", err)
		return nil
	}
	backoff := telemetryInitialBackoff

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		timer.Reset(jitterInterval(base))

		// Live kill-switch recheck (plan §D2: hard stop, fail closed).
		if config.TelemetryKillSwitchActive() {
			continue
		}
		// Server consent ceiling (plan §D2/§F3): GET /v1/consent/config
		// returns max_tier — the HIGHEST tier the server admits right now
		// (the live reporter seed sets org tier_policy = highest-allowed).
		// Semantics, fixed 2026-09-26: compare the LOCAL tier against the
		// ceiling and downgrade when local EXCEEDS it. The earlier form
		// inverted this (it skipped sending unless the local tier covered
		// the ceiling), so a usage-tier client against a deep-ceiling
		// server never sent a batch. A fetched downgrade is applied to the
		// collector (derive re-gates immediately) and purges staged rows
		// the new tier may not emit — the CLI downgrade path's semantics.
		if maxTier, ok := sender.maxTier(ctx); ok {
			if down := clampTierToCeiling(cfg.Telemetry.EffectiveTier(), maxTier); down != cfg.Telemetry.EffectiveTier() {
				logger.Warn("telemetry: server consent ceiling downgrades the local tier",
					"local", cfg.Telemetry.EffectiveTier(), "max_tier", maxTier)
				collector.SetTier(down)
				if _, err := collector.PurgeAboveTier(ctx, down); err != nil {
					logger.Warn("telemetry: purge on server downgrade failed", "err", err)
				}
				if down == contract.TierOff {
					continue // nothing may be emitted this cycle
				}
			}
		}
		// Identity + server-side tier sync (cloud lane: exchange, adopt the
		// device-derived install id, make the server hold the local tier).
		if err := sender.prepare(ctx); err != nil {
			if backoff < time.Hour {
				backoff *= 2
			}
			logger.Warn("telemetry: prepare failed; backing off", "err", err, "backoff", backoff)
			timer.Reset(backoff)
			continue
		}
		if _, err := collector.Derive(ctx); err != nil {
			logger.Warn("telemetry: derive failed", "err", err)
			continue
		}
		// BuildEnvelope reads the durable ack watermark itself, so acked
		// rows are never re-selected (W2.1: each event uploads at most
		// once; the server dedupes on batch_uuid for crash-replay).
		env, batchEnd, err := collector.BuildEnvelope(ctx)
		if err != nil {
			logger.Warn("telemetry: build envelope failed", "err", err)
			continue
		}
		if env == nil {
			backoff = time.Minute
			continue
		}
		acc, err := sender.send(ctx, env)
		if err != nil {
			// Permanent rejection (bad_schema / unknown_version): drop the
			// batch through the watermark so it is never retried — otherwise
			// a rejected batch retries hourly forever (W2.1 review found
			// the loop never honored the documented drop). 429/401/403 and
			// 5xx are transient: back off and retry the same rows.
			if errors.Is(err, contract.ErrBadSchema) || errors.Is(err, contract.ErrUnknownVersion) {
				acked, _ := collector.LastAckSeq(ctx)
				if n, derr := collector.DropThrough(ctx, acked, batchEnd); derr != nil {
					logger.Warn("telemetry: drop rejected batch failed", "err", derr)
				} else if n > 0 {
					logger.Warn("telemetry: batch permanently rejected; dropped", "events", n, "err", err)
				}
				timer.Reset(backoff)
				continue
			}
			// Backoff doubles to a 1h cap; everything transient retries.
			if backoff < time.Hour {
				backoff *= 2
			}
			logger.Warn("telemetry: send failed; backing off", "err", err, "backoff", backoff)
			timer.Reset(backoff)
			continue
		}
		backoff = time.Minute
		if err := collector.Ack(ctx, acc.LastSeq); err != nil {
			logger.Warn("telemetry: ack failed", "err", err)
			continue
		}
	}
}

// telemetryInitialBackoff is the first send-failure backoff; it doubles to
// the 1h cap. A var so tests can shorten the cadence.
var telemetryInitialBackoff = time.Minute

// telemetryLoadToken reads the stored install token ("" when absent — the
// server rejects unregistered ingests and the loop backs off honestly).
// Trimmed: the token file is written with a trailing newline, and a raw
// newline inside an Authorization header makes net/http refuse the request
// outright ("invalid header field value"), so every batch would fail
// forever instead of being retried or dropped.
func telemetryLoadToken(cfg config.Config) string {
	p := telemetryTokenPath(cfg)
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// fetchConsentFloor fetches GET /v1/consent/config with the install token.
// ok is false on any failure (floor unknown → local tier rules, fail-soft).
func fetchConsentFloor(ctx context.Context, cfg config.Config) (string, bool) {
	req, err := httpNewRequest(ctx, http.MethodGet,
		trimRight(cfg.Telemetry.Endpoint, "/")+"/v1/consent/config", telemetryLoadToken(cfg))
	if err != nil {
		return "", false
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	var out struct {
		MaxTier    string `json:"max_tier"`
		KillSwitch bool   `json:"kill_switch"`
	}
	if err := jsonNewDecoder(resp.Body).Decode(&out); err != nil {
		return "", false
	}
	if out.KillSwitch {
		return "off", true
	}
	return out.MaxTier, out.MaxTier != ""
}

// httpNewRequest / jsonNewDecoder are thin indirections kept only to make
// this file's dependencies explicit.
func httpNewRequest(ctx context.Context, method, url, token string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

func jsonNewDecoder(r io.Reader) *json.Decoder { return json.NewDecoder(r) }

func trimRight(s, cutset string) string { return strings.TrimRight(s, cutset) }

// contractTierCovers is the contract's ladder ordering (TierCovers: usage ==
// baseline, enrichment covers deep). The loop's ceiling clamp uses it.
func contractTierCovers(have, want string) bool { return contract.TierCovers(have, want) }

// clampTierToCeiling caps the local consent tier at the server's max_tier
// ceiling: min(local, maxTier) in tier-rank order. An invalid or empty
// ceiling fails closed (downgrade to off); the local tier is never raised.
func clampTierToCeiling(local, maxTier string) string {
	if contractTierCovers(maxTier, local) {
		return local
	}
	if contract.ValidTier(maxTier) && maxTier != contract.TierOff {
		return maxTier
	}
	return contract.TierOff
}

func tierRank(t string) int {
	// The sharing ladder (contract.TierCovers): off < error < baseline (=
	// legacy usage) < deep < enrichment.
	switch t {
	case "off":
		return 0
	case "error":
		return 1
	case "usage", "baseline":
		return 2
	case "deep":
		return 3
	case "enrichment":
		return 4
	default:
		return 0
	}
}
