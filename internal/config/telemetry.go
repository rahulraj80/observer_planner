package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// DefaultTelemetryEndpoint is the central telemetry ingest the [telemetry]
// seed points at: the hosted cloud's pseudonymous telemetry lane (telemetry plan
// Step 5.2, revised 2026-10-17; /v1/telemetry/* on observer-cloud), reached
// with the device key alone. A node registered with a self-hosted
// observer_reporter (an install token from `observer telemetry register`)
// keeps sending there instead. Overridable for a mirror or a test, and on a
// development machine by OBSERVER_TELEMETRY_ENDPOINT (see ResolveEndpoint).
const DefaultTelemetryEndpoint = "https://cloud.superbased.app"

// TelemetryEndpointEnv names the environment variable that replaces the
// shipped default endpoint (DefaultTelemetryEndpoint) for this process: a
// development or staging box sets it to its own telemetry server. Absent or
// empty, nothing changes. It never overrides an endpoint the user or the org
// configured explicitly in [telemetry] endpoint.
const TelemetryEndpointEnv = "OBSERVER_TELEMETRY_ENDPOINT"

// Telemetry endpoint sources ResolveEndpoint reports.
const (
	// TelemetryEndpointSourceDefault: the shipped default
	// (DefaultTelemetryEndpoint, or an empty [telemetry] endpoint).
	TelemetryEndpointSourceDefault = "default"
	// TelemetryEndpointSourceEnv: OBSERVER_TELEMETRY_ENDPOINT replaced the
	// shipped default.
	TelemetryEndpointSourceEnv = TelemetryEndpointEnv
	// TelemetryEndpointSourceConfig: an explicit [telemetry] endpoint other
	// than the shipped default (a self-hosted reporter or a mirror).
	TelemetryEndpointSourceConfig = "config"
)

// TelemetryEndpoint is the endpoint in force and why.
type TelemetryEndpoint struct {
	// URL is the ingest base URL every telemetry call uses ("" only when the
	// config holds no endpoint and no valid override is set).
	URL string
	// Source is one of the TelemetryEndpointSource* values.
	Source string
	// Warning is set when OBSERVER_TELEMETRY_ENDPOINT was set but refused;
	// URL is then the default and the caller should say so.
	Warning string
}

// ErrTelemetryEndpointInvalid is what ParseTelemetryEndpointOverride refuses
// with.
var ErrTelemetryEndpointInvalid = errors.New("must be an https URL with a host (plain http only to a loopback address) and no credentials, query or fragment")

// ParseTelemetryEndpointOverride validates an OBSERVER_TELEMETRY_ENDPOINT
// value and returns it normalized (trimmed, no trailing slash). https is
// required; plain http is accepted only to localhost or a loopback IP, the
// same rule the cloud client applies to its base URL.
func ParseTelemetryEndpointOverride(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", ErrTelemetryEndpointInvalid
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		ip := net.ParseIP(u.Hostname())
		if !strings.EqualFold(u.Hostname(), "localhost") && (ip == nil || !ip.IsLoopback()) {
			return "", ErrTelemetryEndpointInvalid
		}
	default:
		return "", ErrTelemetryEndpointInvalid
	}
	return strings.TrimRight(v, "/"), nil
}

// ResolveEndpoint is the one place the telemetry endpoint is decided; every
// telemetry call (sends, consent, register, own-data actions, `observer
// telemetry show`, `observer privacy`, the dashboard) goes through it.
// Precedence:
//
//  1. an explicit [telemetry] endpoint other than the shipped default
//     (a self-hosted reporter or a mirror the user or org configured);
//  2. OBSERVER_TELEMETRY_ENDPOINT, when set, non-empty and valid
//     (ParseTelemetryEndpointOverride);
//  3. the shipped default, DefaultTelemetryEndpoint.
//
// An invalid override is refused: the default stays in force and Warning
// says why.
func (t TelemetryConfig) ResolveEndpoint() TelemetryEndpoint {
	configured := strings.TrimRight(strings.TrimSpace(t.Endpoint), "/")
	if configured != "" && configured != DefaultTelemetryEndpoint {
		return TelemetryEndpoint{URL: t.Endpoint, Source: TelemetryEndpointSourceConfig}
	}
	def := TelemetryEndpoint{URL: t.Endpoint, Source: TelemetryEndpointSourceDefault}
	raw := strings.TrimSpace(os.Getenv(TelemetryEndpointEnv))
	if raw == "" {
		return def
	}
	u, err := ParseTelemetryEndpointOverride(raw)
	if err != nil {
		shown := t.Endpoint
		if shown == "" {
			shown = "(none)"
		}
		def.Warning = fmt.Sprintf("%s=%q refused (%v); using the default %s", TelemetryEndpointEnv, raw, err, shown)
		return def
	}
	return TelemetryEndpoint{URL: u, Source: TelemetryEndpointSourceEnv}
}

// EffectiveEndpoint is ResolveEndpoint().URL: the endpoint every telemetry
// call uses.
func (t TelemetryConfig) EffectiveEndpoint() string { return t.ResolveEndpoint().URL }

// TelemetryConfig is the [telemetry] surface — the central telemetry rail
// CLIENT collector (telemetry-rail plan §D2,
// docs/plans/telemetry-rail-plan-2026-09-13.md, canonical in
// observer_reporter). The collector batches consent-tiered, content-free
// events to Endpoint: the hosted cloud (DefaultTelemetryEndpoint) by default,
// or a self-hosted observer_reporter ingest the node was registered with.
//
// Consent model (telemetry plan Step 5.7, 2026-10-18): the content-free
// baseline tier is ON by default (config Default()); the zero value of the
// struct is still off. Higher rungs (enrichment, deep) need an explicit
// `observer telemetry consent <tier>`. The whole rail is gated by Enabled
// and by the OBSERVER_TELEMETRY kill switch (hard stop, see
// KillSwitchActive).
//
// LOCAL-ONLY config for an egress rail the user controls (content-free
// baseline on by default, one command turns it off): it is never distributed via
// the org policy registry, and the org plane has no authority over it (the
// server's own consent floor is fetched at batch time and can only
// DOWNGRADE what this node sends, never upgrade — enforced in the
// collector, not by trust).
type TelemetryConfig struct {
	// Tier is the consent tier: off | baseline | enrichment | deep, the
	// telemetry rungs of the one sharing ladder (contract vocabulary
	// contract.ValidTiers). Default `baseline` (Step 5.7). `off` collects
	// nothing.
	// `baseline` = the content-free counters and prompt-cycle sizes/timings.
	// `enrichment` is reserved for the opt-in enrichment events. `deep`
	// additionally allows the deep-diagnostics events, which carry their own
	// disclosure. Legacy `usage` reads as baseline; `error` (error classes
	// only) is still accepted but no longer offered.
	Tier string `toml:"tier"`
	// Endpoint is the central ingest base URL. Default
	// DefaultTelemetryEndpoint (the hosted cloud lane, /v1/telemetry/*); a
	// self-hosted observer_reporter speaks /v1/register, /v1/ingest and
	// /v1/consent/config instead.
	Endpoint string `toml:"endpoint"`
	// Enabled gates the daemon-side collector loop (staging drain +
	// batcher + transport). Default true with the baseline tier (Step 5.7);
	// `enabled = false` stops the loop while keeping the consent setting.
	Enabled bool `toml:"enabled"`
	// BatchIntervalSeconds is the target batch cadence in seconds (plan
	// §D3/A3: 10–15 min ± jitter). Default 900 (15 min); the collector
	// jitters ±10% around it and ALSO fires early when staged bytes reach
	// MaxBatchBytes. Ignored when Enabled is false.
	BatchIntervalSeconds int `toml:"batch_interval_seconds"`
	// MaxBatchBytes is the compressed-payload cap in bytes (plan §D3/A3:
	// ~256 KB). Default 262144. A batch is cut BEFORE exceeding this so no
	// single upload grows past the cap; events that do not fit stay staged
	// for the next cycle.
	MaxBatchBytes int `toml:"max_batch_bytes"`
	// InstallTokenFile is the path (relative paths resolve against the DB
	// directory) holding the minted install token from POST /v1/register.
	// Managed by `observer telemetry register` / auto-registration; 0600.
	// NEVER distributed, never logged.
	InstallTokenFile string `toml:"install_token_file"`
}

// EffectiveTier returns the configured tier normalized to the contract
// vocabulary with the off default applied ("" and unknown → off; callers
// re-validate against contract.ValidTier before emitting).
func (t TelemetryConfig) EffectiveTier() string {
	switch t.Tier {
	case "baseline", "enrichment", "error", "usage", "deep":
		return t.Tier
	default:
		return "off"
	}
}

// KillSwitchEnv is the environment variable that hard-stops the telemetry
// rail regardless of config (plan §D2: OBSERVER_TELEMETRY=off kills emit
// AND network). Any value other than "" (unset) / "on" is treated as off —
// a kill switch must fail closed, so "0", "false", "no", "off" and any
// typo all disable.
const KillSwitchEnv = "OBSERVER_TELEMETRY"

// TelemetryKillSwitchActive reports whether the OBSERVER_TELEMETRY env
// kill switch is engaged. Unset/"" and explicit "on" (case-insensitive)
// leave the config tier in charge; EVERYTHING else — including "0",
// "false", "off" and any unrecognized value — is a hard stop.
func TelemetryKillSwitchActive() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(KillSwitchEnv)))
	if v == "" || v == "on" {
		return false
	}
	return true
}

// Describe renders the endpoint with why it is in force, for `observer
// telemetry show`, `observer privacy` and logs: "<url> (default)",
// "<url> (OBSERVER_TELEMETRY_ENDPOINT)" or "<url> (config)".
func (e TelemetryEndpoint) Describe() string {
	u := e.URL
	if u == "" {
		u = "(none)"
	}
	return u + " (" + e.Source + ")"
}
