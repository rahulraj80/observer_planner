package cloudclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/cloudcred"
	"github.com/marmutapp/superbased-observer/internal/cloudpop"
)

// TelemetrySigningContext domain-separates the anonymous telemetry exchange
// signature from the account exchange (exchangeSigningContext). The hosted
// server (internal/cloudserver/api/telemetry.go) verifies exactly these bytes.
const TelemetrySigningContext = "sbo-telemetry-exchange.v1"

// TelemetrySigningInput is what the device key signs for
// POST /v1/telemetry/exchange: context, server nonce and the base64url device
// public key, newline-joined (same construction as ExchangeSigningInput).
func TelemetrySigningInput(nonce string, pub ed25519.PublicKey) []byte {
	return []byte(TelemetrySigningContext + "\n" + nonce + "\n" + b64.EncodeToString(pub))
}

// telemetryTokenSkew re-exchanges this long before the server-side expiry.
const telemetryTokenSkew = 2 * time.Minute

// TelemetryOptions configures a TelemetryClient.
type TelemetryOptions struct {
	// BaseURL is the cloud origin (https://cloud.superbased.app).
	BaseURL string
	// Cred holds the device key (shared with the account plane; created on
	// first use exactly like cloudclient.New does).
	Cred       cloudcred.Store
	HTTPClient *http.Client
	Clock      func() time.Time
}

// TelemetryClient is the anonymous baseline-telemetry lane (telemetry plan
// Step 5.2, migration 0042 server side). The device key alone authenticates:
// no account, no WorkOS. The telemetry token lives only in memory; a process
// restart re-exchanges. It never touches the account token or sign-in state.
type TelemetryClient struct {
	baseURL string
	http    *http.Client
	clock   func() time.Time
	dev     device

	mu        sync.Mutex
	token     string
	expiresAt time.Time
	installID string
	tier      string
}

// TelemetryExchangeResult is the server's answer to a telemetry exchange.
type TelemetryExchangeResult struct {
	InstallID       string `json:"install_id"`
	Thumbprint      string `json:"thumbprint"`
	Tier            string `json:"tier"`
	ExpiresAt       string `json:"expires_at"`
	ContractVersion string `json:"contract_version"`
	Token           string `json:"telemetry_token"`
}

// TelemetryAck is the server's answer to one ingest.
type TelemetryAck struct {
	BatchUUID string `json:"batch_uuid"`
	Accepted  int    `json:"accepted"`
	Duplicate bool   `json:"duplicate"`
	LastSeq   int64  `json:"last_seq"`
}

// TelemetryServerConfig is GET /v1/telemetry/config.
type TelemetryServerConfig struct {
	ContractVersion string `json:"contract_version"`
	MaxTier         string `json:"max_tier"`
	Paused          bool   `json:"paused"`
	DefaultTier     string `json:"default_tier"`
}

// NewTelemetry builds the telemetry lane client, loading (or creating) the
// device key.
func NewTelemetry(opts TelemetryOptions) (*TelemetryClient, error) {
	if strings.TrimSpace(opts.BaseURL) == "" {
		return nil, fmt.Errorf("cloudclient.NewTelemetry: BaseURL is required")
	}
	if opts.Cred == nil {
		return nil, fmt.Errorf("cloudclient.NewTelemetry: Cred is required")
	}
	dev, err := loadOrCreateDevice(opts.Cred)
	if err != nil {
		return nil, fmt.Errorf("cloudclient.NewTelemetry: %w", err)
	}
	c := &TelemetryClient{
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		http:    opts.HTTPClient,
		clock:   opts.Clock,
		dev:     dev,
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 30 * time.Second}
	}
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if c.clock == nil {
		c.clock = time.Now
	}
	return c, nil
}

// DeviceThumbprint is the device key's thumbprint (the install id derives from it).
func (c *TelemetryClient) DeviceThumbprint() string { return c.dev.thumbprint }

// Exchange signs a fresh nonce and mints a telemetry token. The server creates
// the install on first sight; the returned install id is derived from the
// device thumbprint.
func (c *TelemetryClient) Exchange(ctx context.Context) (TelemetryExchangeResult, error) {
	var nr nonceResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/auth/nonce", nil, "", &nr); err != nil {
		return TelemetryExchangeResult{}, fmt.Errorf("cloudclient: telemetry nonce: %w", err)
	}
	sig := ed25519.Sign(c.dev.priv, TelemetrySigningInput(nr.Nonce, c.dev.pub))
	body, _ := json.Marshal(map[string]string{
		"device_public_key": b64.EncodeToString(c.dev.pub),
		"nonce":             nr.Nonce,
		"signature":         base64.RawURLEncoding.EncodeToString(sig),
	})
	var res TelemetryExchangeResult
	if err := c.doJSON(ctx, http.MethodPost, "/v1/telemetry/exchange", body, "", &res); err != nil {
		return TelemetryExchangeResult{}, fmt.Errorf("cloudclient: telemetry exchange: %w", err)
	}
	exp, err := time.Parse(time.RFC3339, res.ExpiresAt)
	if err != nil {
		return TelemetryExchangeResult{}, fmt.Errorf("cloudclient: telemetry exchange: expires_at: %w", err)
	}
	c.mu.Lock()
	c.token, c.expiresAt, c.installID, c.tier = res.Token, exp, res.InstallID, res.Tier
	c.mu.Unlock()
	res.Token = "" // the raw token never leaves this type
	return res, nil
}

// InstallID returns the server-derived install id, exchanging first if needed.
func (c *TelemetryClient) InstallID(ctx context.Context) (string, error) {
	if _, err := c.ensureToken(ctx); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.installID, nil
}

// ServerTier is the tier the server currently holds for this install
// (as of the last exchange or consent call).
func (c *TelemetryClient) ServerTier(ctx context.Context) (string, error) {
	if _, err := c.ensureToken(ctx); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tier, nil
}

func (c *TelemetryClient) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	tok, exp := c.token, c.expiresAt
	c.mu.Unlock()
	if tok != "" && c.clock().Add(telemetryTokenSkew).Before(exp) {
		return tok, nil
	}
	if _, err := c.Exchange(ctx); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token, nil
}

// Config reads the public server posture (no token).
func (c *TelemetryClient) Config(ctx context.Context) (TelemetryServerConfig, error) {
	var cfg TelemetryServerConfig
	err := c.doJSON(ctx, http.MethodGet, "/v1/telemetry/config", nil, "", &cfg)
	return cfg, err
}

// Ingest uploads one gzip-encoded contract envelope.
func (c *TelemetryClient) Ingest(ctx context.Context, gzBody []byte) (TelemetryAck, error) {
	var ack TelemetryAck
	err := c.authed(ctx, http.MethodPost, "/v1/telemetry/ingest", gzBody, map[string]string{
		"Content-Type": "application/json", "Content-Encoding": "gzip",
	}, &ack)
	return ack, err
}

// SetTier records this install's consented tier server-side. deepDigest is
// required for "deep" (the digest of the disclosure the user accepted).
func (c *TelemetryClient) SetTier(ctx context.Context, tier, deepDigest string) error {
	body, _ := json.Marshal(map[string]string{"tier": tier, "deep_digest": deepDigest})
	var out struct {
		Tier string `json:"tier"`
	}
	if err := c.authed(ctx, http.MethodPost, "/v1/telemetry/consent", body, map[string]string{"Content-Type": "application/json"}, &out); err != nil {
		return err
	}
	c.mu.Lock()
	c.tier = out.Tier
	c.mu.Unlock()
	return nil
}

// Self returns everything the server holds for this install (full adds every
// stored event) as raw JSON.
func (c *TelemetryClient) Self(ctx context.Context, full bool) (json.RawMessage, error) {
	path := "/v1/telemetry/me"
	if full {
		path += "?full=1"
	}
	var raw json.RawMessage
	err := c.authed(ctx, http.MethodGet, path, nil, nil, &raw)
	return raw, err
}

// Delete removes everything the server holds for this install.
func (c *TelemetryClient) Delete(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.authed(ctx, http.MethodDelete, "/v1/telemetry/me", nil, nil, &raw)
	if err == nil {
		c.forget()
	}
	return raw, err
}

// Revoke revokes the install server-side (rotation's first step).
func (c *TelemetryClient) Revoke(ctx context.Context) error {
	err := c.authed(ctx, http.MethodPost, "/v1/telemetry/revoke", nil, nil, nil)
	if err == nil {
		c.forget()
	}
	return err
}

func (c *TelemetryClient) forget() {
	c.mu.Lock()
	c.token, c.expiresAt = "", time.Time{}
	c.mu.Unlock()
}

// authed sends a token + cloudpop request; on a 401 it re-exchanges once (the
// token may have expired early) and retries with a fresh proof.
func (c *TelemetryClient) authed(ctx context.Context, method, path string, body []byte, headers map[string]string, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := c.ensureToken(ctx)
		if err != nil {
			return err
		}
		proof, err := cloudpop.Create(cloudpop.CreateParams{
			PrivateKey:  c.dev.priv,
			Method:      method,
			URL:         c.baseURL + strings.SplitN(path, "?", 2)[0],
			AccessToken: tok,
			IssuedAt:    c.clock(),
			Body:        body,
		})
		if err != nil {
			return fmt.Errorf("cloudclient: telemetry proof: %w", err)
		}
		hdr := map[string]string{"Authorization": "Bearer " + tok, headerPoP: proof}
		for k, v := range headers {
			hdr[k] = v
		}
		err = c.do(ctx, method, path, body, hdr, out)
		var apiErr *APIError
		if attempt == 0 && errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
			c.forget()
			continue
		}
		return err
	}
	return fmt.Errorf("cloudclient: telemetry: unauthorized after re-exchange")
}

func (c *TelemetryClient) doJSON(ctx context.Context, method, path string, body []byte, token string, out any) error {
	hdr := map[string]string{}
	if body != nil {
		hdr["Content-Type"] = "application/json"
	}
	if token != "" {
		hdr["Authorization"] = "Bearer " + token
	}
	return c.do(ctx, method, path, body, hdr, out)
}

// do performs one request. Non-2xx answers become *APIError with the (bounded)
// body; a redirect is never followed.
func (c *TelemetryClient) do(ctx context.Context, method, path string, body []byte, headers map[string]string, out any) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if err != nil {
		return fmt.Errorf("cloudclient: telemetry request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cloudclient: telemetry %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("cloudclient: telemetry read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b := string(raw)
		if len(b) > 512 {
			b = b[:512]
		}
		return &APIError{StatusCode: resp.StatusCode, Body: b}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("cloudclient: telemetry decode: %w", err)
	}
	return nil
}

// TelemetryErrorCode extracts the server's closed-vocabulary error code
// ("consent_missing", "bad_schema", "telemetry_paused", ...) from an APIError.
func TelemetryErrorCode(err error) (status int, code string, ok bool) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return 0, "", false
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal([]byte(apiErr.Body), &body)
	return apiErr.StatusCode, body.Code, true
}
