package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/telemetry"
	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// telemetrySender is the loop's view of one ingest destination. Two exist:
// the hosted cloud lane (the default: device key, no registration, through the
// consent-gated cloudgateway) and a self-hosted observer_reporter (a node that
// ran `observer telemetry register` holds an install token and keeps sending
// there).
type telemetrySender interface {
	// prepare runs before each batch: identity and server-side tier sync.
	prepare(ctx context.Context) error
	// maxTier is the server's current ceiling; ok=false when unknown.
	maxTier(ctx context.Context) (string, bool)
	// send uploads one envelope and returns the ack the loop advances on.
	send(ctx context.Context, env *contract.Envelope) (*telemetry.IngestResponse, error)
}

// newTelemetrySender picks the destination: a stored reporter install token
// means a self-hosted reporter; otherwise the cloud lane at the endpoint.
func newTelemetrySender(cfg config.Config, collector *telemetry.Collector, logger *slog.Logger) (telemetrySender, error) {
	if tok := telemetryLoadToken(cfg); tok != "" {
		return &reporterSender{cfg: cfg, collector: collector, transport: &telemetry.Transport{Endpoint: cfg.Telemetry.Endpoint, Token: tok}}, nil
	}
	return newCloudSender(cfg, collector, logger)
}

// reporterSender is the self-hosted observer_reporter path (unchanged wire).
type reporterSender struct {
	cfg       config.Config
	collector *telemetry.Collector
	transport *telemetry.Transport
	// synced is the base tier the reporter was last told this install is
	// at ("" until the first successful sync of this daemon run).
	synced string
}

// prepare makes the reporter hold this install's local base tier, as the
// cloud lane does. A reporter registers every install at off and refuses
// batches from an off install, and baseline is the client's default with no
// consent command ever run, so without this a freshly registered default
// install would have every batch refused. One POST per daemon run and per
// tier change. Deep is not synced here: it needs its own receipt, which the
// consent command records in the same act.
func (r *reporterSender) prepare(context.Context) error {
	if r.collector == nil {
		return nil
	}
	tier := r.collector.Tier()
	if tier == r.synced || tier == contract.TierOff || tier == contract.TierDeep {
		return nil
	}
	cfg := r.cfg
	if err := postBaseConsent(&cfg, tier); err != nil {
		// A reporter with no consent route keeps no per-install tier to
		// sync; its ingest decides on its own. Not an error.
		var refused *consentRefusedError
		if !errors.As(err, &refused) || refused.StatusCode != http.StatusNotFound {
			return fmt.Errorf("make the reporter hold the local tier %s: %w", tier, err)
		}
	}
	r.synced = tier
	return nil
}

func (r *reporterSender) maxTier(ctx context.Context) (string, bool) {
	return fetchConsentFloor(ctx, r.cfg)
}

func (r *reporterSender) send(ctx context.Context, env *contract.Envelope) (*telemetry.IngestResponse, error) {
	return r.transport.Send(ctx, env)
}
