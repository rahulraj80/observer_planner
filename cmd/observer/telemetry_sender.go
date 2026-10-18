package main

import (
	"context"
	"log/slog"

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
		return &reporterSender{cfg: cfg, transport: &telemetry.Transport{Endpoint: cfg.Telemetry.Endpoint, Token: tok}}, nil
	}
	return newCloudSender(cfg, collector, logger)
}

// reporterSender is the self-hosted observer_reporter path (unchanged wire).
type reporterSender struct {
	cfg       config.Config
	transport *telemetry.Transport
}

func (r *reporterSender) prepare(context.Context) error { return nil }

func (r *reporterSender) maxTier(ctx context.Context) (string, bool) {
	return fetchConsentFloor(ctx, r.cfg)
}

func (r *reporterSender) send(ctx context.Context, env *contract.Envelope) (*telemetry.IngestResponse, error) {
	return r.transport.Send(ctx, env)
}
