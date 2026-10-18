package cloudgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/marmutapp/superbased-observer/internal/cloudclient"
)

// The baseline-telemetry lane (telemetry plan Step 5.2, revised 2026-10-17):
// content-free contract envelopes to the hosted cloud under the device key
// alone, no account. Its consent is the node's TELEMETRY consent (the sharing
// ladder's tier, the [telemetry] loop switch and the OBSERVER_TELEMETRY kill
// switch), not a cloud-intelligence purpose grant, so the caller supplies it as
// a live read and the lane re-checks it before every network attempt.
//
// Own* methods are the user's explicit actions on their own server-side data
// (view, delete, revoke). Like Bootstrap* they pass without the consent check —
// a user who withdrew consent must still be able to see and delete what was
// sent — and the prefix keeps that visible at every call site.

// ErrTelemetryNotConsented: the live telemetry consent is off (tier off, loop
// disabled, or the kill switch engaged). Nothing was sent.
var ErrTelemetryNotConsented = errors.New("cloudgateway: telemetry consent is off; nothing sent")

// TelemetryConsent reports the live telemetry tier the user consented to
// ("off" when the rail must not send). It is called before every egress.
type TelemetryConsent func() string

// TelemetryLane is the consent-gated handle on the telemetry network client.
type TelemetryLane struct {
	consent TelemetryConsent
	client  *cloudclient.TelemetryClient
}

type telemetryLaneState struct {
	once sync.Once
	lane *cloudclient.TelemetryClient
	err  error
}

// Telemetry returns the telemetry lane. consent must not be nil.
func (g *Gateway) Telemetry(consent TelemetryConsent) (*TelemetryLane, error) {
	if consent == nil {
		return nil, fmt.Errorf("cloudgateway: telemetry lane needs a consent source")
	}
	if g.opts.BaseURL == "" {
		return nil, ErrNoBaseURL
	}
	g.telemetry.once.Do(func() {
		g.telemetry.lane, g.telemetry.err = cloudclient.NewTelemetry(cloudclient.TelemetryOptions{
			BaseURL:    g.opts.BaseURL,
			Cred:       g.cred,
			HTTPClient: g.opts.HTTPClient,
			Clock:      g.now,
		})
	})
	if g.telemetry.err != nil {
		return nil, fmt.Errorf("cloudgateway: build telemetry client: %w", g.telemetry.err)
	}
	return &TelemetryLane{consent: consent, client: g.telemetry.lane}, nil
}

func (l *TelemetryLane) gate() error {
	if t := l.consent(); t == "" || t == "off" {
		return ErrTelemetryNotConsented
	}
	return nil
}

// Config reads the server's telemetry posture (max tier, paused).
func (l *TelemetryLane) Config(ctx context.Context) (cloudclient.TelemetryServerConfig, error) {
	if err := l.gate(); err != nil {
		return cloudclient.TelemetryServerConfig{}, err
	}
	return l.client.Config(ctx)
}

// InstallID exchanges (creating the server install on first sight) and returns
// the install id the server derived from the device key.
func (l *TelemetryLane) InstallID(ctx context.Context) (string, error) {
	if err := l.gate(); err != nil {
		return "", err
	}
	return l.client.InstallID(ctx)
}

// SyncTier makes the server hold the user's local tier (deep needs the digest
// of the deep disclosure the user accepted). A no-op when they already match.
func (l *TelemetryLane) SyncTier(ctx context.Context, tier, deepDigest string) error {
	if err := l.gate(); err != nil {
		return err
	}
	have, err := l.client.ServerTier(ctx)
	if err != nil {
		return err
	}
	if have == tier {
		return nil
	}
	return l.client.SetTier(ctx, tier, deepDigest)
}

// Send uploads one gzip-encoded contract envelope.
func (l *TelemetryLane) Send(ctx context.Context, gzBody []byte) (cloudclient.TelemetryAck, error) {
	if err := l.gate(); err != nil {
		return cloudclient.TelemetryAck{}, err
	}
	return l.client.Ingest(ctx, gzBody)
}

// OwnData returns what the server holds for this install (ungated: see above).
func (l *TelemetryLane) OwnData(ctx context.Context, full bool) (json.RawMessage, error) {
	return l.client.Self(ctx, full)
}

// OwnDelete deletes everything the server holds for this install (ungated).
func (l *TelemetryLane) OwnDelete(ctx context.Context) (json.RawMessage, error) {
	return l.client.Delete(ctx)
}

// OwnRevoke revokes this install server-side (ungated; rotation step one).
func (l *TelemetryLane) OwnRevoke(ctx context.Context) error {
	return l.client.Revoke(ctx)
}

// OwnWithdraw tells the server the user withdrew telemetry consent (tier
// off), which also purges any deep-only events (ungated: it is the user's
// own withdrawal, made after the local consent already reads off).
func (l *TelemetryLane) OwnWithdraw(ctx context.Context) error {
	return l.client.SetTier(ctx, "off", "")
}

// TelemetryErrorCode returns the HTTP status and the server's closed-vocabulary
// error code ("consent_missing", "bad_schema", "telemetry_paused", ...) from a
// lane error, so callers outside the network seam can act on refusals.
func TelemetryErrorCode(err error) (status int, code string, ok bool) {
	return cloudclient.TelemetryErrorCode(err)
}
