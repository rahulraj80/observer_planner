//go:build !sbo_omit_cloud_intelligence

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/marmutapp/superbased-observer/internal/cloudgateway"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/telemetry"
	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// telemetry_cloudlane.go is the cloud telemetry lane's wiring (telemetry plan
// Step 5.2). It reaches the network only through cloudgateway, so a build
// without cloud.intelligence (-tags sbo_omit_cloud_intelligence) swaps in
// telemetry_cloudlane_omit.go and links none of the cloud packages.

// newCloudSender opens the consent-gated lane; its consent is the live
// collector tier, forced off while the kill switch is engaged.
func newCloudSender(cfg config.Config, collector *telemetry.Collector, logger *slog.Logger) (telemetrySender, error) {
	lane, err := openTelemetryLane(cfg, func() string {
		if config.TelemetryKillSwitchActive() {
			return contract.TierOff
		}
		return collector.Tier()
	})
	if err != nil {
		return nil, err
	}
	return &cloudSender{lane: lane, collector: collector, logger: logger}, nil
}

// openTelemetryLane opens the consent-gated cloud telemetry lane at the
// configured endpoint, sharing the cloud device key store.
func openTelemetryLane(cfg config.Config, consent cloudgateway.TelemetryConsent) (*cloudgateway.TelemetryLane, error) {
	gw, err := cloudgateway.Open(cloudgateway.Options{
		CredDir: cloudCredDir(cfg),
		BaseURL: trimRight(cfg.Telemetry.Endpoint, "/"),
	})
	if err != nil {
		return nil, fmt.Errorf("telemetry: open cloud gateway: %w", err)
	}
	lane, err := gw.Telemetry(consent)
	if err != nil {
		return nil, fmt.Errorf("telemetry: open cloud lane: %w", err)
	}
	return lane, nil
}

// cloudSender is the hosted cloud lane.
type cloudSender struct {
	lane      *cloudgateway.TelemetryLane
	collector *telemetry.Collector
	logger    *slog.Logger
}

// prepare exchanges (first sight creates the server install), adopts the
// server-derived install id locally, and makes the server hold the local tier.
func (c *cloudSender) prepare(ctx context.Context) error {
	iid, err := c.lane.InstallID(ctx)
	if err != nil {
		return err
	}
	if err := c.collector.AdoptInstallID(ctx, iid); err != nil {
		return err
	}
	tier := contract.EffectiveTier(c.collector.Tier())
	digest := ""
	if tier == contract.TierDeep {
		digest = contract.DeepDisclosureDigest()
	}
	return c.lane.SyncTier(ctx, tier, digest)
}

func (c *cloudSender) maxTier(ctx context.Context) (string, bool) {
	cfg, err := c.lane.Config(ctx)
	if err != nil {
		return "", false
	}
	return cfg.MaxTier, true
}

func (c *cloudSender) send(ctx context.Context, env *contract.Envelope) (*telemetry.IngestResponse, error) {
	body, err := telemetry.EncodeEnvelope(env)
	if err != nil {
		return nil, fmt.Errorf("telemetry: %w", err)
	}
	ack, err := c.lane.Send(ctx, body)
	if err != nil {
		return nil, mapCloudTelemetryErr(err)
	}
	return &telemetry.IngestResponse{Accepted: ack.Accepted, LastSeq: ack.LastSeq, Duplicate: ack.Duplicate}, nil
}

// mapCloudTelemetryErr folds a cloud-lane refusal onto the closed error
// vocabulary the loop acts on: schema-class refusals are permanent (the batch
// is dropped), everything else is transient (back off, retry the same rows).
func mapCloudTelemetryErr(err error) error {
	if errors.Is(err, cloudgateway.ErrTelemetryNotConsented) {
		return fmt.Errorf("telemetry: %w: %w", contract.ErrConsentMissing, err)
	}
	status, code, ok := cloudgateway.TelemetryErrorCode(err)
	if !ok {
		return fmt.Errorf("telemetry: cloud send: %w", err)
	}
	switch {
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("telemetry: %w (http 429)", contract.ErrRateLimited)
	case status == http.StatusUnauthorized:
		return fmt.Errorf("telemetry: %w (http 401)", contract.ErrInstallRevoked)
	case status == http.StatusForbidden && code == "consent_missing":
		return fmt.Errorf("telemetry: %w (http 403)", contract.ErrConsentMissing)
	case status == http.StatusForbidden:
		// install_mismatch / install_revoked: identity trouble, retry after
		// the next prepare re-adopts the install id.
		return fmt.Errorf("telemetry: %w (http 403 %s)", contract.ErrInstallRevoked, code)
	case status == http.StatusServiceUnavailable:
		return fmt.Errorf("telemetry: cloud ingest unavailable (%s)", code)
	case status >= 400 && status < 500:
		return fmt.Errorf("telemetry: %w (http %d %s)", contract.ErrBadSchema, status, code)
	default:
		return fmt.Errorf("telemetry: cloud server error http %d", status)
	}
}

// cloudConsentSync records a consent change on the hosted cloud lane (the
// default destination; a node with a reporter install token uses the
// reporter endpoints above). Raising or keeping a tier syncs it; withdrawing
// (off) is the user's own withdrawal and purges deep-only events.
func cloudConsentSync(cfg config.Config, tier string) error {
	lane, err := openTelemetryLane(cfg, func() string {
		if config.TelemetryKillSwitchActive() {
			return contract.TierOff
		}
		return tier
	})
	if err != nil {
		return err
	}
	ctx := context.Background()
	if contract.EffectiveTier(tier) == contract.TierOff {
		return lane.OwnWithdraw(ctx)
	}
	digest := ""
	if tier == contract.TierDeep {
		digest = contract.DeepDisclosureDigest()
	}
	return lane.SyncTier(ctx, contract.EffectiveTier(tier), digest)
}
